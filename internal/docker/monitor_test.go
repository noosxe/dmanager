package docker

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

	"github.com/moby/moby/client"
	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/db"
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

func TestStartEventMonitor(t *testing.T) {
	eventCh := make(chan string, 10)
	var inspectState = "running"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		if strings.Contains(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			f, _ := w.(http.Flusher)
			if f != nil {
				f.Flush()
			}
			for {
				select {
				case <-r.Context().Done():
					return
				case msg, ok := <-eventCh:
					if !ok {
						return
					}
					_, _ = w.Write([]byte(msg + "\n"))
					if f != nil {
						f.Flush()
					}
				}
			}
		}

		if strings.Contains(r.URL.Path, "/containers/container-1/json") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{
				"Id": "container-1",
				"Name": "/my-container-1",
				"State": {
					"Status": "%s"
				},
				"Image": "sha256:image-1-id",
				"Config": {
					"Image": "nginx:latest"
				}
			}`, inspectState)
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

	_, queries := newTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsChan := make(chan string, 10)
	StartEventMonitor(ctx, queries, dockerClient, func(action string, containerID string) {
		eventsChan <- fmt.Sprintf("%s:%s", action, containerID)
	})

	inspectState = "running"
	eventCh <- `{"Type":"container","Action":"start","Actor":{"ID":"container-1"}}`

	var c db.Container
	err = retry(50, 100*time.Millisecond, func() error {
		var getErr error
		c, getErr = queries.GetContainer(ctx, "container-1")
		if getErr != nil {
			return getErr
		}
		if c.State != "running" {
			return fmt.Errorf("expected state running, got %s", c.State)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to verify container start event: %v", err)
	}

	select {
	case evt := <-eventsChan:
		if evt != "save:container-1" {
			t.Errorf("Expected save:container-1, got %s", evt)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for save event callback")
	}

	if c.Name != "my-container-1" || c.Image != "nginx:latest" || c.ImageID != "sha256:image-1-id" {
		t.Errorf("Unexpected container properties: %+v", c)
	}

	err = queries.SetContainerAutoUpdate(ctx, db.SetContainerAutoUpdateParams{
		AutoUpdate: 1,
		ID:         "container-1",
	})
	if err != nil {
		t.Fatalf("failed to set auto_update: %v", err)
	}

	inspectState = "exited"
	eventCh <- `{"Type":"container","Action":"stop","Actor":{"ID":"container-1"}}`

	err = retry(50, 100*time.Millisecond, func() error {
		var getErr error
		c, getErr = queries.GetContainer(ctx, "container-1")
		if getErr != nil {
			return getErr
		}
		if c.State != "exited" {
			return fmt.Errorf("expected state exited, got %s", c.State)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to verify container stop event: %v", err)
	}

	select {
	case evt := <-eventsChan:
		if evt != "save:container-1" {
			t.Errorf("Expected save:container-1, got %s", evt)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for stop save event callback")
	}

	if c.AutoUpdate != 1 {
		t.Errorf("Expected auto_update = 1 to be preserved, got %d", c.AutoUpdate)
	}

	eventCh <- `{"Type":"container","Action":"destroy","Actor":{"ID":"container-1"}}`

	err = retry(50, 100*time.Millisecond, func() error {
		_, getErr := queries.GetContainer(ctx, "container-1")
		if errors.Is(getErr, sql.ErrNoRows) {
			return nil
		}
		if getErr == nil {
			return fmt.Errorf("container still exists in DB")
		}
		return getErr
	})
	if err != nil {
		t.Fatalf("Failed to verify container destroy event: %v", err)
	}

	select {
	case evt := <-eventsChan:
		if evt != "delete:container-1" {
			t.Errorf("Expected delete:container-1, got %s", evt)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for delete event callback")
	}

	close(eventCh)
}

func retry(attempts int, sleep time.Duration, f func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = f(); err == nil {
			return nil
		}
		time.Sleep(sleep)
	}
	return fmt.Errorf("after %d attempts, last error: %w", attempts, err)
}
