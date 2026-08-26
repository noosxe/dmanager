package container

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"
	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/auth"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

const (
	stateRunning         = "running"
	stateStopped         = "stopped"
	testContainer1ID     = "test-container-1"
	testContainer2ID     = "test-container-2"
	stateExited          = "exited"
	testActionID         = "c-action-1"
	testImageNginx       = "nginx:latest"
	testImageNginxAlpine = "nginx:alpine"
	testImageID123       = "sha256:123"
)

func newTestDB(t *testing.T) (*sql.DB, *db.Queries) {
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })

	if err := db.RunMigrations(dbConn); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return dbConn, db.New(dbConn)
}

func TestSyncContainers(t *testing.T) {
	dbConn, queries := newTestDB(t)
	ctx := context.Background()

	// 1. Create a mocked Docker API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"Id": "test-container-1",
					"Names": ["/my-container-1"],
					"Image": "nginx:latest",
					"ImageID": "sha256:1",
					"State": "running"
				},
				{
					"Id": "test-container-2",
					"Names": ["/my-container-2"],
					"Image": "redis:alpine",
					"ImageID": "sha256:2",
					"State": "stopped"
				}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// 2. Create Docker client pointing to the mock server
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}

	// 3. Run SyncContainers first time (creates container records)
	if syncErr := SyncContainers(ctx, dbConn, dockerClient); syncErr != nil {
		t.Fatalf("SyncContainers failed: %v", syncErr)
	}

	// 4. Assert containers were created
	containers, err := queries.ListContainers(ctx)
	if err != nil {
		t.Fatalf("ListContainers from db failed: %v", err)
	}

	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	// Verify details
	c1 := containers[0] // ordered by name: my-container-1, then my-container-2
	if c1.ID != testContainer1ID || c1.Name != "my-container-1" || c1.State != stateRunning {
		t.Errorf("unexpected container 1 data: %+v", c1)
	}

	c2 := containers[1]
	if c2.ID != testContainer2ID || c2.Name != "my-container-2" || c2.State != stateStopped {
		t.Errorf("unexpected container 2 data: %+v", c2)
	}

	// 5. Simulate updating auto_update and update_available on test-container-1
	err = queries.SetContainerAutoUpdate(ctx, db.SetContainerAutoUpdateParams{
		AutoUpdate: 1,
		ID:         testContainer1ID,
	})
	if err != nil {
		t.Fatalf("failed to set auto update: %v", err)
	}
	err = queries.UpdateContainerUpdateState(ctx, db.UpdateContainerUpdateStateParams{
		UpdateAvailable:   1,
		LatestImageDigest: "sha256:newdigest",
		LastCheckedAt:     "2026-07-03T18:00:00Z",
		ID:                testContainer1ID,
	})
	if err != nil {
		t.Fatalf("failed to update container state: %v", err)
	}

	// 6. Run SyncContainers again, verify we preserve auto_update and update_available
	if syncErr2 := SyncContainers(ctx, dbConn, dockerClient); syncErr2 != nil {
		t.Fatalf("SyncContainers failed: %v", syncErr2)
	}

	updated, err := queries.GetContainer(ctx, testContainer1ID)
	if err != nil {
		t.Fatalf("failed to get container: %v", err)
	}

	if updated.AutoUpdate != 1 || updated.UpdateAvailable != 1 {
		t.Errorf("expected auto_update and update_available to be preserved, got auto_update=%d, update_available=%d",
			updated.AutoUpdate, updated.UpdateAvailable)
	}

	// 7. Simulate deletion/orphan cleanup: change mock to return only 1 container
	server.Close() // close previous server and create one with only container-2
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"Id": "test-container-2",
					"Names": ["/my-container-2"],
					"Image": "redis:alpine",
					"ImageID": "sha256:2",
					"State": "stopped"
				}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dockerClient, err = client.New(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("failed to recreate docker client: %v", err)
	}

	if syncErr3 := SyncContainers(ctx, dbConn, dockerClient); syncErr3 != nil {
		t.Fatalf("SyncContainers failed: %v", syncErr3)
	}

	// Assert test-container-1 was deleted (orphan deletion)
	_, err = queries.GetContainer(ctx, testContainer1ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected test-container-1 to be deleted as orphan, but error is: %v", err)
	}

	// Verify test-container-2 is still present
	_, err = queries.GetContainer(ctx, testContainer2ID)
	if err != nil {
		t.Errorf("expected test-container-2 to still exist, got err: %v", err)
	}
}

func TestListContainers(t *testing.T) {
	dbConn, queries := newTestDB(t)
	ctx := context.Background()

	// Insert mock container data using queries
	err := queries.SaveContainer(ctx, db.SaveContainerParams{
		ID:              "c-id-1",
		Name:            "z-container",
		Image:           "nginx:latest",
		ImageID:         "sha256:123",
		State:           stateRunning,
		AutoUpdate:      1,
		UpdateAvailable: 0,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to insert container: %v", err)
	}

	err = queries.SaveContainer(ctx, db.SaveContainerParams{
		ID:              "c-id-2",
		Name:            "a-container",
		Image:           "redis:alpine",
		ImageID:         "sha256:456",
		State:           stateStopped,
		AutoUpdate:      0,
		UpdateAvailable: 1,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to insert container: %v", err)
	}

	// Create service
	svc := NewService(dbConn, NewBroker(), nil, slog.Default(), nil)

	// Call ListContainers
	resp, err := svc.ListContainers(ctx, connect.NewRequest(&v1.ListContainersRequest{}))
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}

	containers := resp.Msg.Containers
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	// Assert order (should be sorted by name: a-container, then z-container)
	if containers[0].Name != "a-container" || containers[0].Id != "c-id-2" {
		t.Errorf("expected first container to be a-container, got: %+v", containers[0])
	}
	if containers[1].Name != "z-container" || containers[1].Id != "c-id-1" {
		t.Errorf("expected second container to be z-container, got: %+v", containers[1])
	}

	// Check mapping properties
	c0 := containers[0]
	if c0.Image != "redis:alpine" || c0.ImageId != "sha256:456" || c0.State != stateStopped {
		t.Errorf("unexpected properties on mapped container 0: %+v", c0)
	}
	if c0.AutoUpdate != false || c0.UpdateAvailable != true {
		t.Errorf("unexpected flags on mapped container 0: %+v", c0)
	}

	c1 := containers[1]
	if c1.Image != "nginx:latest" || c1.ImageId != "sha256:123" || c1.State != stateRunning {
		t.Errorf("unexpected properties on mapped container 1: %+v", c1)
	}
	if c1.AutoUpdate != true || c1.UpdateAvailable != false {
		t.Errorf("unexpected flags on mapped container 1: %+v", c1)
	}
}

func TestStreamContainers(t *testing.T) {
	dbConn, _ := newTestDB(t)
	broker := NewBroker()
	svc := NewService(dbConn, broker, nil, slog.Default(), nil)

	mux := http.NewServeMux()
	path, handler := dmanagerv1connect.NewContainerServiceHandler(svc)
	mux.Handle(path, handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	client := dmanagerv1connect.NewContainerServiceClient(server.Client(), server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Publish events repeatedly in a goroutine until the context is cancelled to avoid subscription race conditions
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				broker.Publish(&v1.StreamContainersResponse{
					Action:      "delete",
					ContainerId: "test-delete-id",
				})
			}
		}
	}()

	stream, err := client.StreamContainers(ctx, connect.NewRequest(&v1.StreamContainersRequest{}))
	if err != nil {
		t.Fatalf("failed to call StreamContainers: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Wait and receive the event
	if stream.Receive() {
		msg := stream.Msg()
		if msg.Action != "delete" || msg.ContainerId != "test-delete-id" {
			t.Errorf("unexpected event: %+v", msg)
		}
	} else {
		t.Errorf("failed to receive event: %v", stream.Err())
	}
}

func TestContainerActions(t *testing.T) {
	dbConn, queries := newTestDB(t)
	broker := NewBroker()

	// Insert mock container into DB
	err := queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID:              testActionID,
		Name:            "action-container",
		Image:           testImageNginx,
		ImageID:         testImageID123,
		State:           stateExited,
		AutoUpdate:      0,
		UpdateAvailable: 0,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to insert test container: %v", err)
	}

	var startCalled, stopCalled, inspectCalled int
	var inspectState string

	// Create mocked Docker API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+testActionID+"/json") {
			inspectCalled++
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"Id": "%s", "State": {"Status": "%s"}}`, testActionID, inspectState)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/"+testActionID+"/start") {
			startCalled++
			inspectState = stateRunning // update state for next inspect
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/"+testActionID+"/stop") {
			stopCalled++
			// Check if query param t=15 is passed
			q := r.URL.Query()
			if q.Get("t") != "15" {
				t.Errorf("expected timeout query param t=15, got %s", q.Get("t"))
			}
			inspectState = stateExited // update state for next inspect
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}

	svc := NewService(dbConn, broker, dockerClient, slog.Default(), nil)

	// Contexts
	adminCtx := auth.WithUser(context.Background(), db.User{
		Role: roleAdmin,
	})
	viewerCtx := auth.WithUser(context.Background(), db.User{
		Role: roleViewer,
	})
	unauthCtx := context.Background()

	// 1. Test StartContainer Auth check - Unauthenticated
	_, startErr := svc.StartContainer(unauthCtx, connect.NewRequest(&v1.StartContainerRequest{Id: testActionID}))
	if startErr == nil || connect.CodeOf(startErr) != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated error, got: %v", startErr)
	}

	// 2. Test StartContainer Auth check - Viewer
	_, startErr = svc.StartContainer(viewerCtx, connect.NewRequest(&v1.StartContainerRequest{Id: testActionID}))
	if startErr == nil || connect.CodeOf(startErr) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied error, got: %v", startErr)
	}

	// 3. Test StartContainer Validation Check - Empty ID
	_, startErr = svc.StartContainer(adminCtx, connect.NewRequest(&v1.StartContainerRequest{Id: ""}))
	if startErr == nil || connect.CodeOf(startErr) != connect.CodeInvalidArgument {
		t.Errorf("expected InvalidArgument error, got: %v", startErr)
	}

	// 4. Test StartContainer - Container not in DB
	_, startErr = svc.StartContainer(adminCtx, connect.NewRequest(&v1.StartContainerRequest{Id: "non-existent"}))
	if startErr == nil || connect.CodeOf(startErr) != connect.CodeNotFound {
		t.Errorf("expected NotFound error (DB), got: %v", startErr)
	}

	// 5. Test StartContainer - Success
	inspectState = stateExited
	startCalled = 0
	inspectCalled = 0
	startResp, startErr := svc.StartContainer(adminCtx, connect.NewRequest(&v1.StartContainerRequest{Id: testActionID}))
	if startErr != nil {
		t.Fatalf("StartContainer failed: %v", startErr)
	}
	if startResp.Msg.Id != testActionID || startResp.Msg.PreviousState != stateExited || startResp.Msg.CurrentState != stateRunning {
		t.Errorf("unexpected StartContainer response: %+v", startResp.Msg)
	}
	if startCalled != 1 || inspectCalled != 2 {
		t.Errorf("unexpected call counts: startCalled=%d, inspectCalled=%d", startCalled, inspectCalled)
	}

	// Verify DB state updated
	dbC, err := queries.GetContainer(context.Background(), testActionID)
	if err != nil {
		t.Fatalf("failed to query DB: %v", err)
	}
	if dbC.State != stateRunning {
		t.Errorf("expected DB state to be updated to running, got: %s", dbC.State)
	}

	// 6. Test StopContainer - Success
	inspectState = stateRunning
	stopCalled = 0
	inspectCalled = 0
	stopResp, stopErr := svc.StopContainer(adminCtx, connect.NewRequest(&v1.StopContainerRequest{Id: testActionID}))
	if stopErr != nil {
		t.Fatalf("StopContainer failed: %v", stopErr)
	}
	if stopResp.Msg.Id != testActionID || stopResp.Msg.PreviousState != stateRunning || stopResp.Msg.CurrentState != stateExited {
		t.Errorf("unexpected StopContainer response: %+v", stopResp.Msg)
	}
	if stopCalled != 1 || inspectCalled != 2 {
		t.Errorf("unexpected call counts: stopCalled=%d, inspectCalled=%d", stopCalled, inspectCalled)
	}

	// Verify DB state updated
	dbC, err = queries.GetContainer(context.Background(), testActionID)
	if err != nil {
		t.Fatalf("failed to query DB: %v", err)
	}
	if dbC.State != stateExited {
		t.Errorf("expected DB state to be updated to exited, got: %s", dbC.State)
	}
}

func TestUpgradeContainer(t *testing.T) {
	dbConn, queries := newTestDB(t)
	broker := NewBroker()

	oldID := "old-container-123"
	newID := "new-container-999"
	imageName := testImageNginx
	oldImageID := "sha256:oldimage111"
	newImageID := "sha256:newimage222"

	// 1. Seed database with old container record
	err := queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID:              oldID,
		Name:            "upgrade-container-test",
		Image:           imageName,
		ImageID:         oldImageID,
		State:           stateRunning,
		AutoUpdate:      1, // Seed with auto-update enabled to verify it is preserved
		UpdateAvailable: 1,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to seed test container: %v", err)
	}

	var inspectOldCalled, pullCalled, inspectImageCalled, stopCalled, removeCalled, createCalled, connectNetworkCalled, startCalled, inspectNewCalled int

	// 2. Setup mock Docker server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		// Inspect old container
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+oldID+"/json") {
			inspectOldCalled++
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"Name": "/upgrade-container-test",
				"Image": "%s",
				"Config": {"Image": "%s"},
				"State": {"Status": "running", "Running": true},
				"HostConfig": {},
				"NetworkSettings": {
					"Networks": {
						"net1": {},
						"net2": {}
					}
				}
			}`, oldID, oldImageID, imageName)
			return
		}

		// Pull image
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create") {
			pullCalled++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"pulling..."}`))
			return
		}

		// Inspect pulled image
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/images/"+imageName+"/json") {
			inspectImageCalled++
			_, _ = fmt.Fprintf(w, `{"Id": "%s"}`, newImageID)
			return
		}

		// Stop container
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/"+oldID+"/stop") {
			stopCalled++
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Remove container
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/"+oldID) {
			removeCalled++
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Create container
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create") {
			createCalled++
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"Id": "%s"}`, newID)
			return
		}

		// Connect network net1 or net2 via NetworkConnect (map iteration is randomized)
		if r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/networks/net1/connect") || strings.HasSuffix(r.URL.Path, "/networks/net2/connect")) {
			connectNetworkCalled++
			w.WriteHeader(http.StatusOK)
			return
		}

		// Start new container
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/"+newID+"/start") {
			startCalled++
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Inspect new container
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+newID+"/json") {
			inspectNewCalled++
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"Name": "/upgrade-container-test",
				"Config": {"Image": "%s"},
				"State": {"Status": "running", "Running": true}
			}`, newID, imageName)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}

	svc := NewService(dbConn, broker, dockerClient, slog.Default(), nil)

	adminCtx := auth.WithUser(context.Background(), db.User{Role: roleAdmin})
	viewerCtx := auth.WithUser(context.Background(), db.User{Role: "viewer"})

	// 3. Verify permissions (Viewer must fail)
	_, err = svc.UpgradeContainer(viewerCtx, connect.NewRequest(&v1.UpgradeContainerRequest{Id: oldID}))
	if err == nil || connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied for viewer, got: %v", err)
	}

	// 4. Verify UpgradeContainer (Admin must succeed)
	resp, err := svc.UpgradeContainer(adminCtx, connect.NewRequest(&v1.UpgradeContainerRequest{Id: oldID}))
	if err != nil {
		t.Fatalf("UpgradeContainer failed: %v", err)
	}

	if resp.Msg.Id != newID || resp.Msg.PreviousImageId != oldImageID || resp.Msg.CurrentImageId != newImageID {
		t.Errorf("unexpected UpgradeContainer response: %+v", resp.Msg)
	}

	// Verify all mocked docker daemon calls were executed
	if inspectOldCalled != 1 {
		t.Errorf("expected inspectOldCalled=1, got %d", inspectOldCalled)
	}
	if pullCalled != 1 {
		t.Errorf("expected pullCalled=1, got %d", pullCalled)
	}
	if inspectImageCalled != 1 {
		t.Errorf("expected inspectImageCalled=1, got %d", inspectImageCalled)
	}
	if stopCalled != 1 {
		t.Errorf("expected stopCalled=1, got %d", stopCalled)
	}
	if removeCalled != 1 {
		t.Errorf("expected removeCalled=1, got %d", removeCalled)
	}
	if createCalled != 1 {
		t.Errorf("expected createCalled=1, got %d", createCalled)
	}
	if connectNetworkCalled != 1 {
		t.Errorf("expected connectNetworkCalled=1 (for net2), got %d", connectNetworkCalled)
	}
	if startCalled != 1 {
		t.Errorf("expected startCalled=1, got %d", startCalled)
	}
	if inspectNewCalled != 1 {
		t.Errorf("expected inspectNewCalled=1, got %d", inspectNewCalled)
	}

	// 5. Verify database records
	// Old container record must be deleted
	_, err = queries.GetContainer(context.Background(), oldID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected old container record to be deleted, got: %v", err)
	}

	// New container record must be created, and auto_update must be preserved
	newRecord, err := queries.GetContainer(context.Background(), newID)
	if err != nil {
		t.Fatalf("failed to query new container record: %v", err)
	}

	if newRecord.Name != "upgrade-container-test" || newRecord.ImageID != newImageID || newRecord.State != "running" {
		t.Errorf("unexpected fields in new container record: %+v", newRecord)
	}
	if newRecord.AutoUpdate != 1 || newRecord.UpdateAvailable != 0 {
		t.Errorf("expected auto_update=1 and update_available=0, got auto_update=%d, update_available=%d",
			newRecord.AutoUpdate, newRecord.UpdateAvailable)
	}
}

func TestGetContainerLogs(t *testing.T) {
	dbConn, queries := newTestDB(t)
	broker := NewBroker()

	containerID := "test-log-container"

	// 1. Seed database with container record
	err := queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID:              containerID,
		Name:            "log-container",
		Image:           testImageNginx,
		ImageID:         testImageID123,
		State:           stateRunning,
		AutoUpdate:      0,
		UpdateAvailable: 0,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to seed test container: %v", err)
	}

	// Seed viewer and admin users
	viewerUser, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Username:     "viewer-user",
		PasswordHash: "hash",
		Role:         "viewer",
	})
	if err != nil {
		t.Fatalf("failed to create viewer user: %v", err)
	}

	adminUser, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Username:     "admin-user",
		PasswordHash: "hash",
		Role:         roleAdmin,
	})
	if err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}

	// Seed sessions
	viewerSessionID := "viewer-session-token-12345"
	now := time.Now()
	_, err = queries.CreateSession(context.Background(), db.CreateSessionParams{
		SessionID:         viewerSessionID,
		UserID:            viewerUser.ID,
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to create viewer session: %v", err)
	}

	adminSessionID := "admin-session-token-12345"
	_, err = queries.CreateSession(context.Background(), db.CreateSessionParams{
		SessionID:         adminSessionID,
		UserID:            adminUser.ID,
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to create admin session: %v", err)
	}

	var inspectTty bool
	var logPayload []byte

	// 2. Setup mock Docker server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		// Inspect container
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+containerID+"/json") {
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"Name": "/log-container",
				"Config": {"Tty": %t},
				"State": {"Status": "running"}
			}`, containerID, inspectTty)
			return
		}

		// Logs
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+containerID+"/logs") {
			// Check query parameters
			q := r.URL.Query()
			if q.Get("stdout") != "1" || q.Get("stderr") != "1" || q.Get("timestamps") != "1" {
				t.Errorf("expected stdout=1, stderr=1, timestamps=1, got stdout=%s, stderr=%s, timestamps=%s",
					q.Get("stdout"), q.Get("stderr"), q.Get("timestamps"))
			}
			if q.Get("tail") != "100" {
				t.Errorf("expected tail=100, got %s", q.Get("tail"))
			}

			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(logPayload)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}

	svc := NewService(dbConn, broker, dockerClient, slog.Default(), nil)

	// Setup Connect handler / server with the Authentication Interceptor
	interceptor := auth.NewInterceptor(queries, slog.Default(), 0)
	mux := http.NewServeMux()
	path, handler := dmanagerv1connect.NewContainerServiceHandler(svc, connect.WithInterceptors(interceptor))
	mux.Handle(path, handler)

	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	connClient := dmanagerv1connect.NewContainerServiceClient(testServer.Client(), testServer.URL)

	// A. Check auth permissions
	// Unauthenticated
	reqUnauth := connect.NewRequest(&v1.GetContainerLogsRequest{Id: containerID})
	streamUnauth, errUnauth := connClient.GetContainerLogs(context.Background(), reqUnauth)
	if errUnauth == nil {
		if streamUnauth.Receive() {
			t.Errorf("expected stream to fail, but got message")
		}
		errUnauth = streamUnauth.Err()
	}
	if errUnauth == nil || connect.CodeOf(errUnauth) != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated error, got: %v", errUnauth)
	}

	// Viewer role should succeed to initiate stream
	// Note: We need a stream that does not block indefinitely, so follow=false.
	inspectTty = true
	logPayload = []byte("2026-07-04T12:00:00.000000000Z Line 1 of log\n2026-07-04T12:00:01.000000000Z Line 2 of log\n")

	reqViewer := connect.NewRequest(&v1.GetContainerLogsRequest{Id: containerID, Follow: false})
	reqViewer.Header().Set("Cookie", "session_id="+viewerSessionID)
	stream, err := connClient.GetContainerLogs(context.Background(), reqViewer)
	if err != nil {
		t.Fatalf("GetContainerLogs failed for viewer: %v", err)
	}

	// Read raw logs (Tty = true)
	var logs []*v1.GetContainerLogsResponse
	for stream.Receive() {
		logs = append(logs, stream.Msg())
	}
	if sErr := stream.Err(); sErr != nil {
		t.Fatalf("stream encountered error: %v", sErr)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 log lines, got %d", len(logs))
	} else {
		if logs[0].LogLine != "Line 1 of log" || logs[0].Timestamp != "2026-07-04T12:00:00.000000000Z" || logs[0].StreamType != streamTypeStdout {
			t.Errorf("unexpected log 0: %+v", logs[0])
		}
		if logs[1].LogLine != "Line 2 of log" || logs[1].Timestamp != "2026-07-04T12:00:01.000000000Z" || logs[1].StreamType != streamTypeStdout {
			t.Errorf("unexpected log 1: %+v", logs[1])
		}
	}

	// B. Multiplexed stream (Tty = false)
	inspectTty = false
	frame1 := makeLogFrame(1, "2026-07-04T12:00:00.100Z Out 1\n")
	frame2 := makeLogFrame(2, "2026-07-04T12:00:00.200Z Err 1\n")
	logPayload = append(frame1, frame2...)

	reqAdmin := connect.NewRequest(&v1.GetContainerLogsRequest{Id: containerID, Follow: false})
	reqAdmin.Header().Set("Cookie", "session_id="+adminSessionID)
	stream2, err := connClient.GetContainerLogs(context.Background(), reqAdmin)
	if err != nil {
		t.Fatalf("GetContainerLogs failed for admin: %v", err)
	}

	var multiplexedLogs []*v1.GetContainerLogsResponse
	for stream2.Receive() {
		multiplexedLogs = append(multiplexedLogs, stream2.Msg())
	}
	if sErr2 := stream2.Err(); sErr2 != nil {
		t.Fatalf("stream2 encountered error: %v", sErr2)
	}
	if len(multiplexedLogs) != 2 {
		t.Errorf("expected 2 multiplexed log lines, got %d", len(multiplexedLogs))
	} else {
		if multiplexedLogs[0].LogLine != "Out 1" || multiplexedLogs[0].Timestamp != "2026-07-04T12:00:00.100Z" || multiplexedLogs[0].StreamType != streamTypeStdout {
			t.Errorf("unexpected multiplexed log 0: %+v", multiplexedLogs[0])
		}
		if multiplexedLogs[1].LogLine != "Err 1" || multiplexedLogs[1].Timestamp != "2026-07-04T12:00:00.200Z" || multiplexedLogs[1].StreamType != streamTypeStderr {
			t.Errorf("unexpected multiplexed log 1: %+v", multiplexedLogs[1])
		}
	}
}

func makeLogFrame(streamType byte, payload string) []byte {
	payloadLen := len(payload)
	if payloadLen < 0 || payloadLen > 0xffffffff {
		panic("invalid payload length")
	}
	buf := make([]byte, 8+payloadLen)
	buf[0] = streamType
	binary.BigEndian.PutUint32(buf[4:8], uint32(payloadLen))
	copy(buf[8:], payload)
	return buf
}

func TestSetContainerAutoUpdate(t *testing.T) {
	dbConn, queries := newTestDB(t)
	broker := NewBroker()
	svc := NewService(dbConn, broker, nil, slog.Default(), nil)

	containerID := "test-autoupdate-id"
	if err := queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID:         containerID,
		Name:       "test-autoupdate",
		Image:      testImageNginxAlpine,
		ImageID:    testImageID123,
		State:      stateRunning,
		AutoUpdate: 0,
	}); err != nil {
		t.Fatalf("failed to save container: %v", err)
	}

	adminCtx := auth.WithUser(context.Background(), db.User{Role: roleAdmin})
	viewerCtx := auth.WithUser(context.Background(), db.User{Role: roleViewer})

	// 1. Viewer should be denied permission
	_, err := svc.SetContainerAutoUpdate(viewerCtx, connect.NewRequest(&v1.SetContainerAutoUpdateRequest{
		Id:         containerID,
		AutoUpdate: true,
	}))
	if err == nil || connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied for viewer, got: %v", err)
	}

	// 2. Admin should succeed to set auto-update to true
	resp, err := svc.SetContainerAutoUpdate(adminCtx, connect.NewRequest(&v1.SetContainerAutoUpdateRequest{
		Id:         containerID,
		AutoUpdate: true,
	}))
	if err != nil {
		t.Fatalf("SetContainerAutoUpdate failed: %v", err)
	}
	if resp.Msg.Id != containerID || !resp.Msg.AutoUpdate {
		t.Errorf("unexpected response: %+v", resp.Msg)
	}

	// Verify in DB
	c, err := queries.GetContainer(context.Background(), containerID)
	if err != nil {
		t.Fatalf("failed to query container: %v", err)
	}
	if c.AutoUpdate != 1 {
		t.Errorf("expected AutoUpdate=1 in DB, got %d", c.AutoUpdate)
	}

	// 3. Admin sets to false
	resp, err = svc.SetContainerAutoUpdate(adminCtx, connect.NewRequest(&v1.SetContainerAutoUpdateRequest{
		Id:         containerID,
		AutoUpdate: false,
	}))
	if err != nil {
		t.Fatalf("SetContainerAutoUpdate failed: %v", err)
	}
	if resp.Msg.Id != containerID || resp.Msg.AutoUpdate {
		t.Errorf("unexpected response: %+v", resp.Msg)
	}

	// Verify in DB
	c, err = queries.GetContainer(context.Background(), containerID)
	if err != nil {
		t.Fatalf("failed to query container: %v", err)
	}
	if c.AutoUpdate != 0 {
		t.Errorf("expected AutoUpdate=0 in DB, got %d", c.AutoUpdate)
	}
}

func TestCheckContainerUpdates(t *testing.T) {
	dbConn, queries := newTestDB(t)
	broker := NewBroker()

	containerID := "test-checkupdates-id"
	imageName := testImageNginxAlpine
	localImageID := "sha256:local-image-123"

	// Stub remote registry digest
	remoteDigest := "sha256:remote-manifest-digest-xyz"
	var currentRepoDigest string

	// Setup mock docker daemon
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Ping
		if r.Method == http.MethodGet && r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		// Inspect container
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+containerID+"/json") {
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"Name": "/test-checkupdates",
				"Image": "%s",
				"Config": {"Image": "%s"},
				"State": {"Status": "running", "Running": true}
			}`, containerID, localImageID, imageName)
			return
		}

		// Distribution Inspect (remote registry check)
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/distribution/"+imageName+"/json") {
			_, _ = fmt.Fprintf(w, `{
				"Descriptor": {
					"digest": "%s"
				}
			}`, remoteDigest)
			return
		}

		// Inspect local image
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/images/"+localImageID+"/json") {
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"RepoDigests": ["nginx@%s"]
			}`, localImageID, currentRepoDigest)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}

	svc := NewService(dbConn, broker, dockerClient, slog.Default(), nil)

	adminCtx := auth.WithUser(context.Background(), db.User{Role: roleAdmin})
	viewerCtx := auth.WithUser(context.Background(), db.User{Role: roleViewer})

	// Initialize DB record
	if saveErr := queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID:              containerID,
		Name:            "test-checkupdates",
		Image:           imageName,
		ImageID:         localImageID,
		State:           stateRunning,
		UpdateAvailable: 0,
	}); saveErr != nil {
		t.Fatalf("failed to save container: %v", saveErr)
	}

	// 1. Viewer should be denied
	_, err = svc.CheckContainerUpdates(viewerCtx, connect.NewRequest(&v1.CheckContainerUpdatesRequest{Id: containerID}))
	if err == nil || connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied for viewer, got: %v", err)
	}

	// 2. Admin should check and find update IS available (local digest doesn't match remote digest)
	currentRepoDigest = "sha256:different-local-digest-123"
	resp, err := svc.CheckContainerUpdates(adminCtx, connect.NewRequest(&v1.CheckContainerUpdatesRequest{Id: containerID}))
	if err != nil {
		t.Fatalf("CheckContainerUpdates failed: %v", err)
	}
	if !resp.Msg.UpdateAvailable || resp.Msg.LatestImageDigest != remoteDigest {
		t.Errorf("unexpected response when update available: %+v", resp.Msg)
	}

	// Verify in DB
	c, err := queries.GetContainer(context.Background(), containerID)
	if err != nil {
		t.Fatalf("failed to query container: %v", err)
	}
	if c.UpdateAvailable != 1 {
		t.Errorf("expected UpdateAvailable=1, got %d", c.UpdateAvailable)
	}
	if digestStr, ok := c.LatestImageDigest.(string); !ok || digestStr != remoteDigest {
		t.Errorf("expected LatestImageDigest=%s, got %v", remoteDigest, c.LatestImageDigest)
	}

	// 3. Admin checks and finds update is NOT available (local digest matches remote digest)
	currentRepoDigest = remoteDigest
	resp, err = svc.CheckContainerUpdates(adminCtx, connect.NewRequest(&v1.CheckContainerUpdatesRequest{Id: containerID}))
	if err != nil {
		t.Fatalf("CheckContainerUpdates failed: %v", err)
	}
	if resp.Msg.UpdateAvailable || resp.Msg.LatestImageDigest != remoteDigest {
		t.Errorf("unexpected response when update not available: %+v", resp.Msg)
	}

	// Verify in DB
	c, err = queries.GetContainer(context.Background(), containerID)
	if err != nil {
		t.Fatalf("failed to query container: %v", err)
	}
	if c.UpdateAvailable != 0 {
		t.Errorf("expected UpdateAvailable=0, got %d", c.UpdateAvailable)
	}
}
