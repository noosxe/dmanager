package container

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"

	"dmanager/internal/auth"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

// TestUpgradeContainerStartFailureRestoresOld verifies the old container is
// kept (renamed aside) until the replacement runs, and that a failed start
// removes the half-made replacement and restores the original name and run
// state, leaving the DB row on the original container.
func TestUpgradeContainerStartFailureRestoresOld(t *testing.T) {
	dbConn, queries := newTestDB(t)

	const (
		oldID    = "old-container-123"
		newID    = "new-container-999"
		name     = "upgrade-rollback-test"
		imageRef = testImageNginx
	)
	if err := queries.SaveContainer(context.Background(), db.SaveContainerParams{
		ID: oldID, Name: name, Image: imageRef, ImageID: "sha256:old",
		State: stateRunning, AutoUpdate: 1, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var mu sync.Mutex
	var calls []string
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, s)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/_ping"):
			w.Header().Set("API-Version", "1.45")
			_, _ = w.Write([]byte("OK"))
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/containers/"+oldID+"/json"):
			_, _ = fmt.Fprintf(w, `{"Id":"%s","Name":"/%s","Image":"sha256:old","Config":{"Image":"%s"},"State":{"Status":"running","Running":true},"HostConfig":{}}`, oldID, name, imageRef)
		case strings.HasSuffix(p, "/images/create"):
			_, _ = w.Write([]byte(`{"status":"pulling..."}`))
		case strings.HasSuffix(p, "/images/"+imageRef+"/json"):
			_, _ = w.Write([]byte(`{"Id":"sha256:new"}`))
		case strings.HasSuffix(p, "/containers/"+oldID+"/stop"):
			record("stop-old")
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(p, "/containers/"+oldID+"/rename"):
			record("rename-old:" + r.URL.Query().Get("name"))
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(p, "/containers/create"):
			record("create:" + r.URL.Query().Get("name"))
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"Id":"%s"}`, newID)
		case strings.HasSuffix(p, "/containers/"+newID+"/start"):
			record("start-new")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"port is already allocated"}`))
		case r.Method == http.MethodDelete && strings.HasSuffix(p, "/containers/"+newID):
			record("remove-new")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(p, "/containers/"+oldID):
			record("remove-old")
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(p, "/containers/"+oldID+"/start"):
			record("start-old")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	dockerClient, err := client.New(client.WithHost(server.URL), client.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	svc := NewService(dbConn, NewBroker(), dockerClient, slog.Default(), nil, nil)
	adminCtx := auth.WithUser(context.Background(), db.User{Role: roleAdmin})

	_, err = svc.UpgradeContainer(adminCtx, connect.NewRequest(&v1.UpgradeContainerRequest{Id: oldID}))
	if err == nil {
		t.Fatal("expected upgrade to fail when the new container cannot start")
	}

	mu.Lock()
	got := strings.Join(calls, ",")
	mu.Unlock()
	if strings.Contains(got, "remove-old") {
		t.Errorf("old container must not be removed on failure, calls: %s", got)
	}
	for _, want := range []string{"remove-new", "rename-old:" + name, "start-old"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected call %q, calls: %s", want, got)
		}
	}
	// Order: rename aside -> create -> start-new -> remove-new -> restore name -> start-old.
	order := []string{"stop-old", "rename-old:" + upgradeAsideName(name, oldID), "create:" + name, "start-new", "remove-new", "rename-old:" + name, "start-old"}
	if !strings.Contains(got, strings.Join(order, ",")) {
		t.Errorf("unexpected call order: %s", got)
	}

	rec, err := queries.GetContainer(context.Background(), oldID)
	if err != nil {
		t.Fatalf("old DB row must remain: %v", err)
	}
	if rec.Name != name || rec.State != stateRunning {
		t.Errorf("unexpected DB row after rollback: %+v", rec)
	}
	if _, err := queries.GetContainer(context.Background(), newID); err != sql.ErrNoRows {
		t.Errorf("new container must not be recorded, got: %v", err)
	}
}
