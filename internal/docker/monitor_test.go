package docker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/db"
)

const (
	statusRunning = "running"
	statusExited  = "exited"
)

func newTestDB(t *testing.T) *db.Queries {
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })

	if err := db.RunMigrations(dbConn); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return db.New(dbConn)
}

func TestStartEventMonitor(t *testing.T) {
	eventCh := make(chan string, 10)
	var inspectState = statusRunning

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

	queries := newTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsChan := make(chan string, 10)
	StartEventMonitor(ctx, slog.Default(), queries, dockerClient, nil, func(action string, containerID string) {
		eventsChan <- fmt.Sprintf("%s:%s", action, containerID)
	})

	inspectState = statusRunning
	eventCh <- `{"Type":"container","Action":"start","Actor":{"ID":"container-1"}}`

	var c db.Container
	err = retry(func() error {
		var getErr error
		c, getErr = queries.GetContainer(ctx, "container-1")
		if getErr != nil {
			return getErr
		}
		if c.State != statusRunning {
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

	inspectState = statusExited
	eventCh <- `{"Type":"container","Action":"stop","Actor":{"ID":"container-1"}}`

	err = retry(func() error {
		var getErr error
		c, getErr = queries.GetContainer(ctx, "container-1")
		if getErr != nil {
			return getErr
		}
		if c.State != statusExited {
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

	err = retry(func() error {
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

func retry(f func() error) error {
	const (
		attempts = 50
		sleep    = 100 * time.Millisecond
	)
	var err error
	for i := 0; i < attempts; i++ {
		if err = f(); err == nil {
			return nil
		}
		time.Sleep(sleep)
	}
	return fmt.Errorf("after %d attempts, last error: %w", attempts, err)
}

func newMonitorTestClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := client.New(client.WithHost(server.URL), client.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	return c
}

func TestEventMonitorPauseAndRename(t *testing.T) {
	eventCh := make(chan string, 10)
	var mu sync.Mutex
	status, name := statusRunning, "/old-name"

	dockerClient := newMonitorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.45")
			_, _ = w.Write([]byte("OK"))
		case strings.Contains(r.URL.Path, "/events"):
			w.WriteHeader(http.StatusOK)
			f, _ := w.(http.Flusher)
			f.Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case msg := <-eventCh:
					_, _ = w.Write([]byte(msg + "\n"))
					f.Flush()
				}
			}
		case strings.Contains(r.URL.Path, "/containers/container-1/json"):
			mu.Lock()
			defer mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"Id":"container-1","Name":"%s","State":{"Status":"%s"},"Image":"sha256:i","Config":{"Image":"nginx"}}`, name, status)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	queries := newTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartEventMonitor(ctx, slog.Default(), queries, dockerClient, nil, nil)

	waitFor := func(check func(db.Container) bool) {
		t.Helper()
		err := retry(func() error {
			c, err := queries.GetContainer(ctx, "container-1")
			if err != nil {
				return err
			}
			if !check(c) {
				return fmt.Errorf("unexpected container: %+v", c)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	status = "paused"
	mu.Unlock()
	eventCh <- `{"Type":"container","Action":"pause","Actor":{"ID":"container-1"}}`
	waitFor(func(c db.Container) bool { return c.State == "paused" })

	mu.Lock()
	status = statusRunning
	mu.Unlock()
	eventCh <- `{"Type":"container","Action":"unpause","Actor":{"ID":"container-1"}}`
	waitFor(func(c db.Container) bool { return c.State == statusRunning })

	mu.Lock()
	name = "/new-name"
	mu.Unlock()
	eventCh <- `{"Type":"container","Action":"rename","Actor":{"ID":"container-1","Attributes":{"name":"new-name","oldName":"/old-name"}}}`
	waitFor(func(c db.Container) bool { return c.Name == "new-name" })

	mu.Lock()
	status = statusExited
	mu.Unlock()
	eventCh <- `{"Type":"container","Action":"kill","Actor":{"ID":"container-1"}}`
	waitFor(func(c db.Container) bool { return c.State == statusExited })
}

func TestEventMonitorResyncsAfterReconnect(t *testing.T) {
	var eventRequests atomic.Int32

	dockerClient := newMonitorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.45")
			_, _ = w.Write([]byte("OK"))
		case strings.Contains(r.URL.Path, "/events"):
			w.WriteHeader(http.StatusOK)
			f, _ := w.(http.Flusher)
			f.Flush()
			if eventRequests.Add(1) == 1 {
				return // first stream ends, forcing a reconnect
			}
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	queries := newTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var resyncs atomic.Int32
	StartEventMonitor(ctx, slog.Default(), queries, dockerClient, func(context.Context) error {
		// A failing resync must not stop the monitor.
		if resyncs.Add(1) == 1 {
			return errors.New("boom")
		}
		return nil
	}, nil)

	err := retry(func() error {
		if resyncs.Load() < 1 {
			return errors.New("no resync yet")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if eventRequests.Load() < 2 {
		t.Fatalf("expected the monitor to resubscribe, got %d event requests", eventRequests.Load())
	}
}
