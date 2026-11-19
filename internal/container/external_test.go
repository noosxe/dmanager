package container

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"

	"dmanager/internal/auth"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/managed"
)

const (
	testSelector     = "ci.pool"
	testSelectorExpr = "ci.pool=true"
	testOwnerName    = "nixos"
	externalID       = "external-1"
	externalImage    = "nginx:alpine"
)

func seedExternal(t *testing.T, q *db.Queries, id string, labels map[string]string, autoUpdate int64) {
	t.Helper()
	ctx := context.Background()
	if err := q.SaveContainer(ctx, db.SaveContainerParams{
		ID: id, Name: id, Image: externalImage, ImageID: "sha256:old", State: stateRunning,
		AutoUpdate: autoUpdate, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// SaveContainer leaves labels alone; they come from sync/events.
	if err := q.UpsertContainerFromEvent(ctx, db.UpsertContainerFromEventParams{
		ID: id, Name: id, Image: externalImage, ImageID: "sha256:old", State: stateRunning,
		Labels: managed.EncodeLabels(labels),
	}); err != nil {
		t.Fatalf("seed labels: %v", err)
	}
}

func TestMapContainerRecordExternallyManaged(t *testing.T) {
	policy := managed.NewPolicy([]string{testSelectorExpr})
	cases := []struct {
		labels map[string]string
		ext    bool
		by     string
	}{
		{nil, false, ""},
		{map[string]string{managed.Label: testOwnerName}, true, testOwnerName},
		{map[string]string{testSelector: "true"}, true, testSelectorExpr},
		{map[string]string{testSelector: "false"}, false, ""},
	}
	for _, c := range cases {
		got := MapContainerRecord(db.Container{Labels: managed.EncodeLabels(c.labels)}, policy)
		if got.ExternallyManaged != c.ext || got.ManagedBy != c.by {
			t.Errorf("%v: got (%v,%q) want (%v,%q)", c.labels, got.ExternallyManaged, got.ManagedBy, c.ext, c.by)
		}
	}
}

func TestExternalGuardsAPI(t *testing.T) {
	dbConn, q := newTestDB(t)
	svc := NewService(dbConn, NewBroker(), nil, slog.Default(), nil, nil)
	svc.SetExternalManagement(managed.NewPolicy([]string{testSelectorExpr}), false)

	seedExternal(t, q, externalID, map[string]string{managed.Label: testOwnerName}, 1)
	seedExternal(t, q, "selector-1", map[string]string{testSelector: "true"}, 0)
	seedExternal(t, q, "plain-1", nil, 0)

	admin := auth.WithUser(context.Background(), db.User{Role: roleAdmin})

	for _, id := range []string{externalID, "selector-1"} {
		_, err := svc.UpgradeContainer(admin, connect.NewRequest(&v1.UpgradeContainerRequest{Id: id}))
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("UpgradeContainer(%s): want FailedPrecondition, got %v", id, err)
		}
		_, err = svc.SetContainerAutoUpdate(admin, connect.NewRequest(&v1.SetContainerAutoUpdateRequest{Id: id, AutoUpdate: true}))
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("enable auto-update(%s): want FailedPrecondition, got %v", id, err)
		}
	}

	// Disabling a stale flag stays possible.
	if _, err := svc.SetContainerAutoUpdate(admin, connect.NewRequest(&v1.SetContainerAutoUpdateRequest{Id: externalID, AutoUpdate: false})); err != nil {
		t.Fatalf("disabling auto-update on external container: %v", err)
	}
	if c, _ := q.GetContainer(context.Background(), externalID); c.AutoUpdate != 0 {
		t.Errorf("auto_update should be cleared, got %d", c.AutoUpdate)
	}

	// Unmanaged containers are not blocked by the guard (they reach the
	// missing-docker-client error instead).
	_, err := svc.UpgradeContainer(admin, connect.NewRequest(&v1.UpgradeContainerRequest{Id: "plain-1"}))
	if connect.CodeOf(err) == connect.CodeFailedPrecondition {
		t.Errorf("plain container must not be guarded: %v", err)
	}
	if _, err := svc.SetContainerAutoUpdate(admin, connect.NewRequest(&v1.SetContainerAutoUpdateRequest{Id: "plain-1", AutoUpdate: true})); err != nil {
		t.Errorf("enable auto-update on plain container: %v", err)
	}
}

// TestExternalSchedulerGuard: an externally managed container with a stored
// auto_update=1 is checked but never pulled or recreated, and update
// notifications are suppressed unless notifyExternal is set.
func TestExternalSchedulerGuard(t *testing.T) {
	for _, notify := range []bool{false, true} {
		t.Run(fmt.Sprintf("notify=%v", notify), func(t *testing.T) {
			dbConn, q := newTestDB(t)
			seedExternal(t, q, externalID, map[string]string{managed.Label: testOwnerName}, 1)

			var mutating, notifications atomic.Int32
			gotify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				notifications.Add(1)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer gotify.Close()
			_ = q.UpdateSetting(context.Background(), db.UpdateSettingParams{Key: "gotify_url", Value: gotify.URL})
			_ = q.UpdateSetting(context.Background(), db.UpdateSettingParams{Key: "gotify_token", Value: "tok"})

			docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := r.URL.Path
				switch {
				case strings.HasSuffix(p, "/_ping"):
					w.Header().Set("API-Version", "1.45")
					_, _ = w.Write([]byte("OK"))
				case strings.Contains(p, "/distribution/"):
					_, _ = w.Write([]byte(`{"Descriptor":{"digest":"sha256:remote"}}`))
				case strings.HasSuffix(p, "/json") && strings.Contains(p, "/containers/"):
					_, _ = fmt.Fprintf(w, `{"Id":%q,"Name":"/x","Image":"sha256:old","Config":{"Image":%q},"State":{"Status":"running","Running":true}}`, externalID, externalImage)
				case strings.HasSuffix(p, "/json") && strings.Contains(p, "/images/"):
					_, _ = w.Write([]byte(`{"Id":"sha256:old","RepoDigests":["nginx@sha256:local"]}`))
				default:
					if r.Method != http.MethodGet {
						mutating.Add(1)
					}
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer docker.Close()
			dc, err := client.New(client.WithHost(docker.URL), client.WithHTTPClient(docker.Client()))
			if err != nil {
				t.Fatal(err)
			}

			svc := NewService(dbConn, NewBroker(), dc, slog.Default(), nil, nil)
			svc.SetExternalManagement(managed.Policy{}, notify)
			svc.checkAllContainers(context.Background())

			c, err := q.GetContainer(context.Background(), externalID)
			if err != nil {
				t.Fatalf("container must remain: %v", err)
			}
			if c.UpdateAvailable != 1 {
				t.Errorf("update availability should still be recorded, got %d", c.UpdateAvailable)
			}
			if n := mutating.Load(); n != 0 {
				t.Errorf("scheduler issued %d mutating docker calls for an external container", n)
			}
			want := int32(0)
			if notify {
				want = 1
			}
			if got := notifications.Load(); got != want {
				t.Errorf("notifications: got %d want %d", got, want)
			}
		})
	}
}
