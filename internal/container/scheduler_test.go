package container

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dmanager/internal/db"
	"github.com/moby/moby/client"
)

func TestSchedulerCheckAllContainers(t *testing.T) {
	dbConn, queries := newTestDB(t)
	broker := NewBroker()

	container1ID := "container-no-auto-update"
	container2ID := "container-with-auto-update"
	imageName := testImageNginxAlpine
	oldImageID := "sha256:oldimage111"
	newImageID := "sha256:newimage222"
	remoteDigest := "sha256:remote-digest-abc"

	// 1. Seed database
	// Container 1: Auto-update is disabled (0). UpdateAvailable is 0.
	err := queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID:              container1ID,
		Name:            "no-auto-update",
		Image:           imageName,
		ImageID:         oldImageID,
		State:           stateRunning,
		AutoUpdate:      0,
		UpdateAvailable: 0,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to seed container 1: %v", err)
	}

	// Container 2: Auto-update is enabled (1). UpdateAvailable is 0.
	err = queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID:              container2ID,
		Name:            "with-auto-update",
		Image:           imageName,
		ImageID:         oldImageID,
		State:           stateRunning,
		AutoUpdate:      1,
		UpdateAvailable: 0,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to seed container 2: %v", err)
	}

	var inspectOldCalled, pullCalled, inspectImageCalled, stopCalled, removeCalled, createCalled, startCalled, inspectNewCalled int

	// 2. Setup mock Docker server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		// Inspect old container 2 (only container 2 will be upgraded since it has auto-update=1)
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+container2ID+"/json") {
			inspectOldCalled++
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"Name": "/with-auto-update",
				"Image": "%s",
				"Config": {"Image": "%s"},
				"State": {"Status": "running", "Running": true},
				"HostConfig": {},
				"NetworkSettings": {
					"Networks": {
						"bridge": {}
					}
				}
			}`, container2ID, oldImageID, imageName)
			return
		}

		// Fallback inspect container 1 if queried
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+container1ID+"/json") {
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"Name": "/no-auto-update",
				"Image": "%s",
				"Config": {"Image": "%s"},
				"State": {"Status": "running", "Running": true},
				"HostConfig": {},
				"NetworkSettings": {
					"Networks": {
						"bridge": {}
					}
				}
			}`, container1ID, oldImageID, imageName)
			return
		}

		// Distribution Inspect for registry checks (called for both containers)
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/distribution/"+imageName+"/json") {
			_, _ = fmt.Fprintf(w, `{
				"Descriptor": {
					"digest": "%s"
				}
			}`, remoteDigest)
			return
		}

		// Inspect local image to get RepoDigests (called during registry checks)
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/images/"+oldImageID+"/json") {
			_, _ = fmt.Fprintf(w, `{
				"Id": "%s",
				"RepoDigests": ["nginx@sha256:different-local-digest-123"]
			}`, oldImageID)
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
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/"+container2ID+"/stop") {
			stopCalled++
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Remove container
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/"+container2ID) {
			removeCalled++
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Create container
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create") {
			createCalled++
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"Id": "new-container-id"}`)
			return
		}

		// Start container
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/new-container-id/start") {
			startCalled++
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Inspect new container
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/new-container-id/json") {
			inspectNewCalled++
			_, _ = fmt.Fprintf(w, `{
				"Id": "new-container-id",
				"Name": "/with-auto-update",
				"Config": {"Image": "%s"},
				"State": {"Status": "running", "Running": true}
			}`, imageName)
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

	svc := NewService(dbConn, broker, dockerClient, slog.Default(), nil, nil)

	// 3. Trigger scheduler check loop manually via checkAllContainers
	svc.checkAllContainers(context.Background())

	// 4. Assertions

	// Container 1 (auto-update disabled):
	// Should have update_available = 1 set in DB.
	c1, err := queries.GetContainer(context.Background(), container1ID)
	if err != nil {
		t.Fatalf("failed to get container 1: %v", err)
	}
	if c1.UpdateAvailable != 1 {
		t.Errorf("expected container 1 update_available to be 1, got %d", c1.UpdateAvailable)
	}

	// Container 2 (auto-update enabled):
	// Re-deployment should have run. The old container ID record should be deleted, and a new container ID record inserted.
	// Since queries.DeleteContainer deletes the old record:
	_, err = queries.GetContainer(context.Background(), container2ID)
	if err == nil {
		t.Errorf("expected old container record %s to be deleted, but it exists", container2ID)
	}

	c2New, err := queries.GetContainer(context.Background(), "new-container-id")
	if err != nil {
		t.Fatalf("expected new container record for new-container-id, got error: %v", err)
	}
	if c2New.ImageID != newImageID {
		t.Errorf("expected new image ID %s, got %s", newImageID, c2New.ImageID)
	}
	if c2New.UpdateAvailable != 0 {
		t.Errorf("expected update_available to be reset to 0, got %d", c2New.UpdateAvailable)
	}
	if c2New.AutoUpdate != 1 {
		t.Errorf("expected auto_update configuration to be preserved as 1, got %d", c2New.AutoUpdate)
	}

	// Assertions on mock calls
	if pullCalled != 1 {
		t.Errorf("expected pull to be called exactly 1 time, got %d", pullCalled)
	}
	if stopCalled != 1 {
		t.Errorf("expected stop to be called exactly 1 time, got %d", stopCalled)
	}
	if removeCalled != 1 {
		t.Errorf("expected remove to be called exactly 1 time, got %d", removeCalled)
	}
	if createCalled != 1 {
		t.Errorf("expected create to be called exactly 1 time, got %d", createCalled)
	}
	if startCalled != 1 {
		t.Errorf("expected start to be called exactly 1 time, got %d", startCalled)
	}
}
