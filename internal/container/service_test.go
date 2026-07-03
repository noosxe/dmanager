package container

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	stateRunning     = "running"
	stateStopped     = "stopped"
	testContainer1ID = "test-container-1"
	testContainer2ID = "test-container-2"
	stateExited      = "exited"
	testActionID     = "c-action-1"
	testImageNginx   = "nginx:latest"
	testImageID123   = "sha256:123"
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
	svc := NewService(dbConn, NewBroker(), nil)

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
	svc := NewService(dbConn, broker, nil)

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
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/containers/"+testActionID+"/json") {
			inspectCalled++
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"Id": "%s", "State": {"Status": "%s"}}`, testActionID, inspectState)
			return
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/containers/"+testActionID+"/start") {
			startCalled++
			inspectState = stateRunning // update state for next inspect
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/containers/"+testActionID+"/stop") {
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

	svc := NewService(dbConn, broker, dockerClient)

	// Contexts
	adminCtx := auth.WithUser(context.Background(), db.User{
		Role: roleAdmin,
	})
	viewerCtx := auth.WithUser(context.Background(), db.User{
		Role: "viewer",
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
