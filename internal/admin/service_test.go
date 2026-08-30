package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"

	"dmanager/internal/db"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

// newTestService starts a fake Docker API server backed by handler and
// returns an admin Service wired to it via the moby client.
const testImageID = "sha256:abc123def456"

func newTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	return NewService(dockerClient, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// pingHandler responds to the client's API version negotiation probe.
func pingHandler(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasSuffix(r.URL.Path, "/_ping") {
		w.Header().Set("API-Version", "1.45")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
		return true
	}
	return false
}

func TestListImages(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/images/json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"Id": "sha256:abc123def456",
				"RepoTags": ["nginx:latest", "example/nginx:1.25"],
				"Created": 1717171717,
				"Size": 142606336,
				"Containers": 3
			},
			{
				"Id": "sha256:fff000fff000",
				"RepoTags": [],
				"Created": 1700000000,
				"Size": 4096,
				"Containers": -1
			}
		]`))
	})

	resp, err := svc.ListImages(context.Background(), connect.NewRequest(&v1.ListImagesRequest{}))
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}

	images := resp.Msg.Images
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}

	first := images[0]
	if first.Id != testImageID {
		t.Errorf("expected id sha256:abc123def456, got %q", first.Id)
	}
	if len(first.RepoTags) != 2 || first.RepoTags[0] != "nginx:latest" {
		t.Errorf("expected repo tags [nginx:latest example/nginx:1.25], got %v", first.RepoTags)
	}
	if first.CreatedUnix != 1717171717 {
		t.Errorf("expected created unix 1717171717, got %d", first.CreatedUnix)
	}
	if first.SizeBytes != 142606336 {
		t.Errorf("expected size 142606336, got %d", first.SizeBytes)
	}
	if first.ContainersCount != 3 {
		t.Errorf("expected containers count 3, got %d", first.ContainersCount)
	}

	// Dangling image: no repo tags, container count not calculated (-1).
	second := images[1]
	if len(second.RepoTags) != 0 {
		t.Errorf("expected no repo tags for dangling image, got %v", second.RepoTags)
	}
	if second.ContainersCount != -1 {
		t.Errorf("expected containers count -1, got %d", second.ContainersCount)
	}
}

func TestListVolumes(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/volumes") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"Volumes": [
				{
					"Name": "tardis",
					"Driver": "local",
					"Mountpoint": "/var/lib/docker/volumes/tardis/_data",
					"CreatedAt": "2016-06-07T20:31:11.853781916Z",
					"Labels": {"com.example.some-label": "some-value"}
				},
				{
					"Name": "bad-date",
					"Driver": "local",
					"Mountpoint": "/var/lib/docker/volumes/bad-date/_data",
					"CreatedAt": "not-a-date",
					"Labels": null
				}
			],
			"Warnings": null
		}`))
	})

	resp, err := svc.ListVolumes(context.Background(), connect.NewRequest(&v1.ListVolumesRequest{}))
	if err != nil {
		t.Fatalf("ListVolumes failed: %v", err)
	}

	volumes := resp.Msg.Volumes
	if len(volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(volumes))
	}

	first := volumes[0]
	if first.Name != "tardis" || first.Driver != "local" {
		t.Errorf("unexpected name/driver: %q/%q", first.Name, first.Driver)
	}
	if first.Mountpoint != "/var/lib/docker/volumes/tardis/_data" {
		t.Errorf("unexpected mountpoint: %q", first.Mountpoint)
	}
	wantCreated := timestamppb.New(time.Date(2016, 6, 7, 20, 31, 11, 853781916, time.UTC))
	if first.CreatedAt.AsTime().UnixNano() != wantCreated.AsTime().UnixNano() {
		t.Errorf("expected created at %v, got %v", wantCreated.AsTime(), first.CreatedAt.AsTime())
	}
	if len(first.Labels) != 1 || first.Labels["com.example.some-label"] != "some-value" {
		t.Errorf("expected labels to be mapped, got %v", first.Labels)
	}

	// Unparseable CreatedAt is tolerated: timestamp stays nil, row is kept.
	second := volumes[1]
	if second.CreatedAt != nil {
		t.Errorf("expected nil created at for unparseable date, got %v", second.CreatedAt)
	}
}

func TestListNetworks(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		// List endpoint (no attachment data, matching API >= 1.28).
		if strings.HasSuffix(r.URL.Path, "/networks") || strings.HasSuffix(r.URL.Path, "/networks/json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"Name": "bridge",
					"Id": "7d86d31b1478e7cca9ebed7e73aa0fdeec46c5ca29497431d3007d2d9e15ed99",
					"Created": "2016-10-19T04:33:30.360899459Z",
					"Scope": "local",
					"Driver": "bridge",
					"Internal": false
				},
				{
					"Name": "secure",
					"Id": "abc123",
					"Created": "2024-01-02T03:04:05Z",
					"Scope": "local",
					"Driver": "bridge",
					"Internal": true
				},
				{
					"Name": "flaky",
					"Id": "flk456",
					"Created": "2024-02-03T04:05:06Z",
					"Scope": "local",
					"Driver": "bridge",
					"Internal": false
				}
			]`))
			return
		}
		// Per-network inspect enrichment. bridge carries two attachments
		// (a stopped container still counts), app-net zero, flaky fails.
		if strings.HasPrefix(r.URL.Path, "/networks/7d86d31b") || strings.Contains(r.URL.Path, "7d86d31b") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Name": "bridge", "Id": "7d86d31b1478e7cca9ebed7e73aa0fdeec46c5ca29497431d3007d2d9e15ed99", "Containers": {"c1": {"Name": "web", "IPv4Address": "172.17.0.2/16"}, "c2": {"Name": "db-stopped", "IPv4Address": "172.17.0.3/16"}}}`))
			return
		}
		if strings.Contains(r.URL.Path, "abc123") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Name": "secure", "Id": "abc123", "Containers": {}}`))
			return
		}
		// flk456 inspect intentionally 404s: containers_count degrades to -1.
		http.NotFound(w, r)
	})

	resp, err := svc.ListNetworks(context.Background(), connect.NewRequest(&v1.ListNetworksRequest{}))
	if err != nil {
		t.Fatalf("ListNetworks failed: %v", err)
	}

	networks := resp.Msg.Networks
	if len(networks) != 3 {
		t.Fatalf("expected 3 networks, got %d", len(networks))
	}

	first := networks[0]
	if first.Name != "bridge" || first.Driver != "bridge" || first.Scope != "local" {
		t.Errorf("unexpected name/driver/scope: %q/%q/%q", first.Name, first.Driver, first.Scope)
	}
	if first.Id != "7d86d31b1478e7cca9ebed7e73aa0fdeec46c5ca29497431d3007d2d9e15ed99" {
		t.Errorf("unexpected id: %q", first.Id)
	}
	if first.Internal {
		t.Error("expected internal=false for bridge network")
	}
	wantCreated := time.Date(2016, 10, 19, 4, 33, 30, 360899459, time.UTC)
	if first.CreatedAt.AsTime() != wantCreated {
		t.Errorf("expected created at %v, got %v", wantCreated, first.CreatedAt.AsTime())
	}

	// Enrichment (design.md §9.12, #215).
	if first.ContainersCount != 2 {
		t.Errorf("expected containers_count=2 for bridge (stopped container still attached), got %d", first.ContainersCount)
	}
	if !first.Predefined {
		t.Error("expected predefined=true for bridge")
	}

	second := networks[1]
	if !second.Internal {
		t.Error("expected internal=true for secure network")
	}
	if second.ContainersCount != 0 {
		t.Errorf("expected containers_count=0 for secure, got %d", second.ContainersCount)
	}
	if second.Predefined {
		t.Error("expected predefined=false for user-defined network secure")
	}

	// Per-network inspect failure degrades the count, not the list.
	third := networks[2]
	if third.Name != "flaky" {
		t.Fatalf("unexpected third network: %q", third.Name)
	}
	if third.ContainersCount != -1 {
		t.Errorf("expected containers_count=-1 for failed inspect, got %d", third.ContainersCount)
	}
}

func TestListEmptyResources(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/volumes"):
			_, _ = w.Write([]byte(`{"Volumes": [], "Warnings": null}`))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()

	images, err := svc.ListImages(ctx, connect.NewRequest(&v1.ListImagesRequest{}))
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}
	if len(images.Msg.Images) != 0 {
		t.Errorf("expected empty images list, got %d", len(images.Msg.Images))
	}

	volumes, err := svc.ListVolumes(ctx, connect.NewRequest(&v1.ListVolumesRequest{}))
	if err != nil {
		t.Fatalf("ListVolumes failed: %v", err)
	}
	if len(volumes.Msg.Volumes) != 0 {
		t.Errorf("expected empty volumes list, got %d", len(volumes.Msg.Volumes))
	}

	networks, err := svc.ListNetworks(ctx, connect.NewRequest(&v1.ListNetworksRequest{}))
	if err != nil {
		t.Fatalf("ListNetworks failed: %v", err)
	}
	if len(networks.Msg.Networks) != 0 {
		t.Errorf("expected empty networks list, got %d", len(networks.Msg.Networks))
	}
}

func TestDockerDaemonErrors(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		http.Error(w, "daemon unavailable", http.StatusInternalServerError)
	})
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "images",
			call: func() error {
				_, err := svc.ListImages(ctx, connect.NewRequest(&v1.ListImagesRequest{}))
				return err
			},
		},
		{
			name: "volumes",
			call: func() error {
				_, err := svc.ListVolumes(ctx, connect.NewRequest(&v1.ListVolumesRequest{}))
				return err
			},
		},
		{
			name: "networks",
			call: func() error {
				_, err := svc.ListNetworks(ctx, connect.NewRequest(&v1.ListNetworksRequest{}))
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			connErr, ok := err.(*connect.Error)
			if !ok {
				t.Fatalf("expected *connect.Error, got %T", err)
			}
			if connErr.Code() != connect.CodeUnavailable {
				t.Errorf("expected code %v, got %v", connect.CodeUnavailable, connErr.Code())
			}
		})
	}
}

func TestDeleteImage(t *testing.T) {
	var gotMethod, gotForce string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/images/sha256:abc123def456") {
			http.NotFound(w, r)
			return
		}
		gotMethod = r.Method
		gotForce = r.URL.Query().Get("force")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"Deleted": "sha256:abc123def456"}]`))
	})

	resp, err := svc.DeleteImage(context.Background(), connect.NewRequest(&v1.DeleteImageRequest{
		Id:    testImageID,
		Force: true,
	}))
	if err != nil {
		t.Fatalf("DeleteImage failed: %v", err)
	}
	if resp == nil || resp.Msg == nil {
		t.Fatal("expected non-nil response")
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE request, got %s", gotMethod)
	}
	if gotForce != "1" {
		t.Errorf("expected force=1 query parameter, got %q", gotForce)
	}
}

func TestDeleteImageRequiresID(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("daemon should not be called with an empty image ID")
	})

	_, err := svc.DeleteImage(context.Background(), connect.NewRequest(&v1.DeleteImageRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected code %v, got %v", connect.CodeInvalidArgument, connErr.Code())
	}
}

func TestDeleteImageErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantCode   connect.Code
		wantSubstr string
	}{
		{
			name:       "not found",
			status:     http.StatusNotFound,
			body:       `{"message": "no such image: sha256:missing"}`,
			wantCode:   connect.CodeNotFound,
			wantSubstr: "image not found on Docker host",
		},
		{
			name:       "in use conflict",
			status:     http.StatusConflict,
			body:       `{"message": "conflict: unable to remove repository reference"}`,
			wantCode:   connect.CodeFailedPrecondition,
			wantSubstr: "tag conflict",
		},
		{
			name:       "daemon error",
			status:     http.StatusInternalServerError,
			body:       `{"message": "daemon unavailable"}`,
			wantCode:   connect.CodeUnavailable,
			wantSubstr: "failed to delete image",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if pingHandler(w, r) {
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := svc.DeleteImage(context.Background(), connect.NewRequest(&v1.DeleteImageRequest{
				Id: testImageID,
			}))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			connErr, ok := err.(*connect.Error)
			if !ok {
				t.Fatalf("expected *connect.Error, got %T", err)
			}
			if connErr.Code() != tc.wantCode {
				t.Errorf("expected code %v, got %v", tc.wantCode, connErr.Code())
			}
			if !strings.Contains(connErr.Error(), tc.wantSubstr) {
				t.Errorf("expected error containing %q, got %q", tc.wantSubstr, connErr.Error())
			}
		})
	}
}

func TestCheckEngine(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantConnect bool
		wantVersion string
		wantError   string
	}{
		{
			name: "healthy daemon",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("API-Version", "1.51")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			},
			wantConnect: true,
			wantVersion: "1.51",
		},
		{
			name: "daemon error is a status not an RPC error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "docker daemon is down", http.StatusInternalServerError)
			},
			wantConnect: false,
			wantError:   "docker daemon is down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, tt.handler)

			resp, err := svc.CheckEngine(context.Background(), connect.NewRequest(&v1.CheckEngineRequest{}))
			if err != nil {
				t.Fatalf("CheckEngine returned an error: %v", err)
			}

			if got := resp.Msg.Connected; got != tt.wantConnect {
				t.Errorf("Connected = %v, want %v", got, tt.wantConnect)
			}
			if got := resp.Msg.ApiVersion; got != tt.wantVersion {
				t.Errorf("ApiVersion = %q, want %q", got, tt.wantVersion)
			}
			if tt.wantError != "" && resp.Msg.Error == "" {
				t.Error("Error is empty, want the daemon failure reason")
			}
		})
	}
}

func TestCheckEngineDaemonDown(t *testing.T) {
	// Point the client at a closed port: the transport itself fails, which is
	// the closest a unit test gets to "engine down". Still a status, not an error.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	dockerClient, err := client.New(client.WithHost(url))
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	svc := NewService(dockerClient, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	resp, err := svc.CheckEngine(context.Background(), connect.NewRequest(&v1.CheckEngineRequest{}))
	if err != nil {
		t.Fatalf("CheckEngine returned an error, want a successful disconnected response: %v", err)
	}
	if resp.Msg.Connected {
		t.Error("Connected = true, want false with the transport error as the reason")
	}
	if resp.Msg.Error == "" {
		t.Error("Error is empty, want the transport failure reason")
	}
}

func TestPruneImages(t *testing.T) {
	var gotMethod, gotFilters string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/images/prune") {
			http.NotFound(w, r)
			return
		}
		gotMethod = r.Method
		gotFilters = r.URL.Query().Get("filters")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ImagesDeleted": [{"Untagged": "nginx:latest"}, {"Deleted": "sha256:bbb222333444"}], "SpaceReclaimed": 142600000}`))
	})

	resp, err := svc.PruneImages(context.Background(), connect.NewRequest(&v1.PruneImagesRequest{}))
	if err != nil {
		t.Fatalf("PruneImages failed: %v", err)
	}
	if resp == nil || resp.Msg == nil {
		t.Fatal("expected non-nil response")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST request, got %s", gotMethod)
	}
	// Default scope: dangling=false — the daemon prunes every unused image,
	// not just untagged ones (its absent-filter default is dangling-only).
	parsed, err := parsePruneFilters(t, gotFilters)
	if err != nil {
		t.Fatalf("filters query param is not valid JSON: %v", err)
	}
	if !parsed["dangling"]["false"] {
		t.Errorf("expected dangling=false filter for the default prune, got %q", gotFilters)
	}
	if len(resp.Msg.ImagesDeleted) != 2 {
		t.Fatalf("expected 2 deleted entries, got %d", len(resp.Msg.ImagesDeleted))
	}
	if resp.Msg.ImagesDeleted[0].Untagged != "nginx:latest" || resp.Msg.ImagesDeleted[0].Deleted != "" {
		t.Errorf("entry 0 mismatch: %+v", resp.Msg.ImagesDeleted[0])
	}
	if resp.Msg.ImagesDeleted[1].Deleted != "sha256:bbb222333444" || resp.Msg.ImagesDeleted[1].Untagged != "" {
		t.Errorf("entry 1 mismatch: %+v", resp.Msg.ImagesDeleted[1])
	}
	if resp.Msg.SpaceReclaimed != 142600000 {
		t.Errorf("expected space_reclaimed 142600000, got %d", resp.Msg.SpaceReclaimed)
	}
}

func TestPruneImagesDanglingFilter(t *testing.T) {
	var gotFilters string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		gotFilters = r.URL.Query().Get("filters")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ImagesDeleted": [], "SpaceReclaimed": 0}`))
	})

	if _, err := svc.PruneImages(context.Background(), connect.NewRequest(&v1.PruneImagesRequest{DanglingOnly: true})); err != nil {
		t.Fatalf("PruneImages failed: %v", err)
	}
	parsed, err := parsePruneFilters(t, gotFilters)
	if err != nil {
		t.Fatalf("filters query param is not valid JSON: %v", err)
	}
	if !parsed["dangling"]["true"] {
		t.Errorf("expected dangling=true filter for dangling_only prune, got %q", gotFilters)
	}
}

// parsePruneFilters decodes the filters query param the moby client sends
// (client.Filters serialized as JSON) so tests can assert exact filter terms.
func parsePruneFilters(t *testing.T, raw string) (map[string]map[string]bool, error) {
	t.Helper()
	var parsed map[string]map[string]bool
	err := json.Unmarshal([]byte(raw), &parsed)
	return parsed, err
}

func TestPruneImagesErrors(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "daemon unavailable"}`))
	})

	_, err := svc.PruneImages(context.Background(), connect.NewRequest(&v1.PruneImagesRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connErr.Code() != connect.CodeUnavailable {
		t.Errorf("expected code %v, got %v", connect.CodeUnavailable, connErr.Code())
	}
	if !strings.Contains(connErr.Message(), "failed to prune images") {
		t.Errorf("expected message to contain %q, got %q", "failed to prune images", connErr.Message())
	}
}

func TestGetBuildCacheStats(t *testing.T) {
	// Modern daemons (API >= 1.52) supply the aggregates in BuildCacheUsage;
	// serve a matching ping so the client takes the modern decode path
	// (the shared pingHandler advertises 1.45, which forces the legacy one).
	var gotMethod, gotType string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.53")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/system/df") {
			http.NotFound(w, r)
			return
		}
		gotMethod = r.Method
		gotType = r.URL.Query().Get("type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"BuildCache": [], "BuildCacheUsage": {"TotalSize": 33541322317, "Reclaimable": 27653977878, "TotalCount": 634, "ActiveCount": 2}}`))
	})

	resp, err := svc.GetBuildCacheStats(context.Background(), connect.NewRequest(&v1.GetBuildCacheStatsRequest{}))
	if err != nil {
		t.Fatalf("GetBuildCacheStats failed: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("expected GET request, got %s", gotMethod)
	}
	// The type filter is hyphenated — type=buildcache is rejected by the daemon.
	if gotType != "build-cache" {
		t.Errorf("expected type=build-cache query param, got %q", gotType)
	}
	// Daemon-supplied aggregates map 1:1 — no client-side summation.
	if resp.Msg.TotalBytes != 33541322317 {
		t.Errorf("expected total_bytes 33541322317, got %d", resp.Msg.TotalBytes)
	}
	if resp.Msg.ReclaimableBytes != 27653977878 {
		t.Errorf("expected reclaimable_bytes 27653977878, got %d", resp.Msg.ReclaimableBytes)
	}
	if resp.Msg.RecordCount != 634 {
		t.Errorf("expected record_count 634, got %d", resp.Msg.RecordCount)
	}
	if resp.Msg.ActiveCount != 2 {
		t.Errorf("expected active_count 2, got %d", resp.Msg.ActiveCount)
	}
}

func TestGetBuildCacheStatsLegacyAggregation(t *testing.T) {
	// The shared fake ping advertises API 1.45, so the client falls back to the
	// legacy records-based decode and aggregates client-side: TotalSize sums
	// non-shared records, Reclaimable excludes in-use ones.
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"BuildCache": [
			{"ID": "a", "Size": 100, "Shared": false, "InUse": false},
			{"ID": "b", "Size": 200, "Shared": false, "InUse": true},
			{"ID": "c", "Size": 50, "Shared": true, "InUse": false}
		]}`))
	})

	resp, err := svc.GetBuildCacheStats(context.Background(), connect.NewRequest(&v1.GetBuildCacheStatsRequest{}))
	if err != nil {
		t.Fatalf("GetBuildCacheStats failed: %v", err)
	}
	if resp.Msg.TotalBytes != 300 {
		t.Errorf("expected total_bytes 300, got %d", resp.Msg.TotalBytes)
	}
	if resp.Msg.ReclaimableBytes != 100 {
		t.Errorf("expected reclaimable_bytes 100, got %d", resp.Msg.ReclaimableBytes)
	}
	if resp.Msg.RecordCount != 3 {
		t.Errorf("expected record_count 3, got %d", resp.Msg.RecordCount)
	}
	if resp.Msg.ActiveCount != 1 {
		t.Errorf("expected active_count 1, got %d", resp.Msg.ActiveCount)
	}
}

func TestGetBuildCacheStatsZeroRecords(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// A daemon without build-cache data omits BuildCacheUsage entirely.
		_, _ = w.Write([]byte(`{"BuildCache": null}`))
	})

	resp, err := svc.GetBuildCacheStats(context.Background(), connect.NewRequest(&v1.GetBuildCacheStatsRequest{}))
	if err != nil {
		t.Fatalf("GetBuildCacheStats failed: %v", err)
	}
	if resp.Msg.TotalBytes != 0 || resp.Msg.ReclaimableBytes != 0 || resp.Msg.RecordCount != 0 || resp.Msg.ActiveCount != 0 {
		t.Errorf("expected zero-valued stats, got %+v", resp.Msg)
	}
}

func TestPruneBuildCache(t *testing.T) {
	var gotMethod, gotAll string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/build/prune") {
			http.NotFound(w, r)
			return
		}
		gotMethod = r.Method
		gotAll = r.URL.Query().Get("all")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"CachesDeleted": ["aaa", "bbb", "ccc"], "SpaceReclaimed": 27653977878}`))
	})

	resp, err := svc.PruneBuildCache(context.Background(), connect.NewRequest(&v1.PruneBuildCacheRequest{All: false}))
	if err != nil {
		t.Fatalf("PruneBuildCache failed: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST request, got %s", gotMethod)
	}
	// Default scope preserves buildkit-internal types: the client omits all.
	if gotAll != "" {
		t.Errorf("expected no all query param for scoped prune, got %q", gotAll)
	}
	if resp.Msg.CachesDeleted != 3 {
		t.Errorf("expected caches_deleted 3, got %d", resp.Msg.CachesDeleted)
	}
	if resp.Msg.SpaceReclaimed != 27653977878 {
		t.Errorf("expected space_reclaimed 27653977878, got %d", resp.Msg.SpaceReclaimed)
	}
}

func TestPruneBuildCacheAllFlag(t *testing.T) {
	var gotAll string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		gotAll = r.URL.Query().Get("all")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"CachesDeleted": [], "SpaceReclaimed": 0}`))
	})

	if _, err := svc.PruneBuildCache(context.Background(), connect.NewRequest(&v1.PruneBuildCacheRequest{All: true})); err != nil {
		t.Fatalf("PruneBuildCache failed: %v", err)
	}
	if gotAll != "1" {
		t.Errorf("expected all=1 query param for full prune, got %q", gotAll)
	}
}

func TestPruneBuildCacheErrors(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "daemon unavailable"}`))
	})

	_, err := svc.PruneBuildCache(context.Background(), connect.NewRequest(&v1.PruneBuildCacheRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connErr.Code() != connect.CodeUnavailable {
		t.Errorf("expected code %v, got %v", connect.CodeUnavailable, connErr.Code())
	}
	if !strings.Contains(connErr.Message(), "failed to prune build cache") {
		t.Errorf("expected message to contain %q, got %q", "failed to prune build cache", connErr.Message())
	}
}

func TestListBuildCacheRecords(t *testing.T) {
	// Modern daemon shape: BuildCacheUsage aggregates + Items. Items are only
	// decoded when the request is verbose, so the fake asserts the query.
	var gotVerbose, gotType string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.53")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/system/df") {
			http.NotFound(w, r)
			return
		}
		gotVerbose = r.URL.Query().Get("verbose")
		gotType = r.URL.Query().Get("type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"BuildCacheUsage": {"TotalSize": 5000, "Reclaimable": 3000, "TotalCount": 3, "ActiveCount": 0, "Items": [` +
			`{"ID": "big", "Type": "exec.cachemount", "Description": "exec mount /bin/sh", "Size": 4800, "Shared": false, "InUse": false, "CreatedAt": "2026-01-01T00:00:00Z", "LastUsedAt": "2026-08-01T12:00:00Z", "UsageCount": 7},` +
			`{"ID": "small", "Type": "source.local", "Description": "local source", "Size": 100, "Shared": true, "InUse": true, "CreatedAt": "2026-01-02T00:00:00Z"},` +
			`{"ID": "mid", "Type": "regular", "Description": "", "Size": 100, "Shared": false, "InUse": false, "CreatedAt": "2026-01-03T00:00:00Z", "LastUsedAt": "2026-08-02T00:00:00Z", "UsageCount": 2}]}}`))
	})

	resp, err := svc.ListBuildCacheRecords(context.Background(), connect.NewRequest(&v1.ListBuildCacheRecordsRequest{}))
	if err != nil {
		t.Fatalf("ListBuildCacheRecords failed: %v", err)
	}
	if gotVerbose != "1" {
		t.Errorf("expected verbose=1 query param, got %q", gotVerbose)
	}
	if gotType != "build-cache" {
		t.Errorf("expected type=build-cache query param, got %q", gotType)
	}

	records := resp.Msg.Records
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	// Sorted by size desc; equal sizes break the tie by ID ascending.
	orders := []string{records[0].GetId(), records[1].GetId(), records[2].GetId()}
	if orders[0] != "big" || orders[1] != "mid" || orders[2] != "small" {
		t.Errorf("expected [big mid small], got %v", orders)
	}

	big := records[0]
	if big.GetType() != "exec.cachemount" || big.GetDescription() != "exec mount /bin/sh" {
		t.Errorf("unexpected type/description: %q / %q", big.GetType(), big.GetDescription())
	}
	if big.GetSizeBytes() != 4800 || big.GetUsageCount() != 7 || big.GetShared() || big.GetInUse() {
		t.Errorf("unexpected scalar fields on the top record: %+v", big)
	}
	if big.GetLastUsedAt() == nil || big.GetLastUsedAt().AsTime().Unix() != 1785585600 {
		t.Errorf("expected last_used_at mapped, got %v", big.GetLastUsedAt())
	}
	if big.GetCreatedAt() == nil {
		t.Error("expected created_at mapped")
	}

	// A never-used record ships no last_used_at ("small" has none).
	if records[2].GetLastUsedAt() != nil {
		t.Errorf("expected nil last_used_at on never-used record, got %v", records[2].GetLastUsedAt())
	}

	inUse := records[2]
	if !inUse.GetInUse() || !inUse.GetShared() {
		t.Error("expected in_use/shared mapped on the in-use record")
	}
}

func TestListBuildCacheRecordsEmpty(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.53")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"BuildCacheUsage": {"TotalSize": 0, "TotalCount": 0, "Items": []}}`))
	})

	resp, err := svc.ListBuildCacheRecords(context.Background(), connect.NewRequest(&v1.ListBuildCacheRecordsRequest{}))
	if err != nil {
		t.Fatalf("ListBuildCacheRecords failed: %v", err)
	}
	if len(resp.Msg.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(resp.Msg.Records))
	}
}

func TestListBuildCacheRecordsErrors(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "daemon unavailable"}`))
	})

	_, err := svc.ListBuildCacheRecords(context.Background(), connect.NewRequest(&v1.ListBuildCacheRecordsRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connErr.Code() != connect.CodeUnavailable {
		t.Errorf("expected code %v, got %v", connect.CodeUnavailable, connErr.Code())
	}
}

func TestPruneBuildCacheRecord(t *testing.T) {
	var gotAll, gotFilters string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/build/prune") {
			http.NotFound(w, r)
			return
		}
		gotAll = r.URL.Query().Get("all")
		gotFilters = r.URL.Query().Get("filters")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"CachesDeleted": ["sha256:abc"], "SpaceReclaimed": 4800}`))
	})

	resp, err := svc.PruneBuildCacheRecord(context.Background(), connect.NewRequest(&v1.PruneBuildCacheRecordRequest{Id: "sha256:abc"}))
	if err != nil {
		t.Fatalf("PruneBuildCacheRecord failed: %v", err)
	}
	// all=1 with the id filter is deterministic: the filter already scopes
	// candidates to the one record, all lifts internal-type protection for it.
	if gotAll != "1" {
		t.Errorf("expected all=1 query param, got %q", gotAll)
	}
	if !strings.Contains(gotFilters, "sha256:abc") || !strings.Contains(gotFilters, "\"id\"") {
		t.Errorf("expected id filter carrying the record ID, got %q", gotFilters)
	}
	if resp.Msg.CachesDeleted != 1 {
		t.Errorf("expected caches_deleted 1, got %d", resp.Msg.CachesDeleted)
	}
	if resp.Msg.SpaceReclaimed != 4800 {
		t.Errorf("expected space_reclaimed 4800, got %d", resp.Msg.SpaceReclaimed)
	}
}

func TestPruneBuildCacheRecordProtected(t *testing.T) {
	// In-use records are daemon-protected: an empty report is honest 0/0.
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"CachesDeleted": [], "SpaceReclaimed": 0}`))
	})

	resp, err := svc.PruneBuildCacheRecord(context.Background(), connect.NewRequest(&v1.PruneBuildCacheRecordRequest{Id: "sha256:busy"}))
	if err != nil {
		t.Fatalf("PruneBuildCacheRecord failed: %v", err)
	}
	if resp.Msg.CachesDeleted != 0 || resp.Msg.SpaceReclaimed != 0 {
		t.Errorf("expected 0/0 for a protected record, got %d/%d", resp.Msg.CachesDeleted, resp.Msg.SpaceReclaimed)
	}
}

func TestPruneBuildCacheRecordValidation(t *testing.T) {
	// Blank IDs are rejected before the daemon is touched — the fake fails
	// the test if any request arrives.
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("daemon should not be called for a blank id")
	})

	for _, id := range []string{"", "   "} {
		_, err := svc.PruneBuildCacheRecord(context.Background(), connect.NewRequest(&v1.PruneBuildCacheRecordRequest{Id: id}))
		if err == nil {
			t.Fatalf("expected error for id %q, got nil", id)
		}
		connErr, ok := err.(*connect.Error)
		if !ok {
			t.Fatalf("expected *connect.Error, got %T", err)
		}
		if connErr.Code() != connect.CodeInvalidArgument {
			t.Errorf("expected code %v, got %v", connect.CodeInvalidArgument, connErr.Code())
		}
	}
}

func TestPruneBuildCacheRecordErrors(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "daemon unavailable"}`))
	})

	_, err := svc.PruneBuildCacheRecord(context.Background(), connect.NewRequest(&v1.PruneBuildCacheRecordRequest{Id: "sha256:abc"}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connErr.Code() != connect.CodeUnavailable {
		t.Errorf("expected code %v, got %v", connect.CodeUnavailable, connErr.Code())
	}
}

func TestGetVolumeUsage(t *testing.T) {
	// The fake pings API 1.52 so the modern decode path runs: aggregates are
	// decoded from VolumeUsage while Items require Verbose=true — the same
	// trap as the build-cache records call.
	var gotType, gotVerbose string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.52")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/system/df") {
			http.NotFound(w, r)
			return
		}
		gotType = r.URL.Query().Get("type")
		gotVerbose = r.URL.Query().Get("verbose")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"VolumeUsage": {"TotalSize": 10000, "Reclaimable": 300, "TotalCount": 3, "ActiveCount": 1, "Items": [` +
			`{"Name": "pgdata", "UsageData": {"Size": 9000, "RefCount": 2}},` +
			`{"Name": "cache", "UsageData": {"Size": 300, "RefCount": 0}},` +
			`{"Name": "broken", "UsageData": {"Size": -1, "RefCount": 0}},` +
			`{"Name": "noref", "UsageData": {"RefCount": 0}}` +
			`]}}`))
	})

	resp, err := svc.GetVolumeUsage(context.Background(), connect.NewRequest(&v1.GetVolumeUsageRequest{}))
	if err != nil {
		t.Fatalf("GetVolumeUsage failed: %v", err)
	}
	if gotType != "volume" {
		t.Errorf("expected type=volume query param, got %q", gotType)
	}
	// Without verbose=1 the modern decode drops Items entirely.
	if gotVerbose != "1" {
		t.Errorf("expected verbose=1 query param, got %q", gotVerbose)
	}
	if len(resp.Msg.Volumes) != 4 {
		t.Fatalf("expected 4 volume usages, got %d", len(resp.Msg.Volumes))
	}
	first := resp.Msg.Volumes[0]
	if first.Name != "pgdata" || first.SizeBytes != 9000 || first.RefCount != 2 {
		t.Errorf("unexpected first volume: %s %d %d", first.Name, first.SizeBytes, first.RefCount)
	}
	// -1 (walk failure) passes through verbatim and is excluded from sums…
	if resp.Msg.Volumes[2].SizeBytes != -1 {
		t.Errorf("expected -1 passthrough for broken volume, got %d", resp.Msg.Volumes[2].SizeBytes)
	}
	// …while a missing UsageData maps as unknown (0/0), not an error.
	if resp.Msg.Volumes[3].SizeBytes != 0 || resp.Msg.Volumes[3].RefCount != 0 {
		t.Errorf("expected zeroed usage for missing UsageData, got %d/%d", resp.Msg.Volumes[3].SizeBytes, resp.Msg.Volumes[3].RefCount)
	}
	// Aggregates are computed server-side, -1 and unknown sizes excluded:
	// total = 9000 + 300; reclaimable = 300 (only the measured unused volume).
	if resp.Msg.TotalSizeBytes != 9300 {
		t.Errorf("expected total_size_bytes 9300, got %d", resp.Msg.TotalSizeBytes)
	}
	if resp.Msg.ReclaimableBytes != 300 {
		t.Errorf("expected reclaimable_bytes 300, got %d", resp.Msg.ReclaimableBytes)
	}
	if resp.Msg.UnusedCount != 3 {
		t.Errorf("expected unused_count 3, got %d", resp.Msg.UnusedCount)
	}
}

func TestPruneVolumes(t *testing.T) {
	var gotFilters string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/volumes/prune") {
			http.NotFound(w, r)
			return
		}
		gotFilters = r.URL.Query().Get("filters")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"VolumesDeleted": ["cache", "old"], "SpaceReclaimed": 450}`))
	})

	resp, err := svc.PruneVolumes(context.Background(), connect.NewRequest(&v1.PruneVolumesRequest{}))
	if err != nil {
		t.Fatalf("PruneVolumes failed: %v", err)
	}
	// all=true reaches the daemon inside the filters JSON (the client maps the
	// All option to an "all" filter) — it includes named volumes, which the
	// daemon would otherwise skip (anonymous-only by default).
	if !strings.Contains(gotFilters, "\"all\"") {
		t.Errorf("expected all filter in %q, missing", gotFilters)
	}
	if resp.Msg.VolumesDeleted != 2 {
		t.Errorf("expected volumes_deleted 2, got %d", resp.Msg.VolumesDeleted)
	}
	if len(resp.Msg.Names) != 2 || resp.Msg.Names[0] != "cache" || resp.Msg.Names[1] != "old" {
		t.Errorf("expected names [cache old], got %v", resp.Msg.Names)
	}
	if resp.Msg.SpaceReclaimed != 450 {
		t.Errorf("expected space_reclaimed 450, got %d", resp.Msg.SpaceReclaimed)
	}
}

func TestPruneVolumesNothingToDelete(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/volumes/prune") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"VolumesDeleted": [], "SpaceReclaimed": 0}`))
	})

	resp, err := svc.PruneVolumes(context.Background(), connect.NewRequest(&v1.PruneVolumesRequest{}))
	if err != nil {
		t.Fatalf("PruneVolumes failed: %v", err)
	}
	// The daemon-protected case maps honestly: 0 deleted, 0 reclaimed.
	if resp.Msg.VolumesDeleted != 0 || resp.Msg.SpaceReclaimed != 0 {
		t.Errorf("expected honest 0/0 report, got %d/%d", resp.Msg.VolumesDeleted, resp.Msg.SpaceReclaimed)
	}
}

func TestGetVolumeUsageErrors(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		http.NotFound(w, r)
	})
	_, err := svc.GetVolumeUsage(context.Background(), connect.NewRequest(&v1.GetVolumeUsageRequest{}))
	if err == nil {
		t.Fatal("expected error for daemon-down")
	}
	var connErr *connect.Error
	if !errors.As(err, &connErr) || connErr.Code() != connect.CodeUnavailable {
		t.Errorf("expected code %v, got %v", connect.CodeUnavailable, err)
	}
}

func TestPruneVolumesErrors(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		http.NotFound(w, r)
	})
	_, err := svc.PruneVolumes(context.Background(), connect.NewRequest(&v1.PruneVolumesRequest{}))
	if err == nil {
		t.Fatal("expected error for daemon-down")
	}
	var connErr *connect.Error
	if !errors.As(err, &connErr) || connErr.Code() != connect.CodeUnavailable {
		t.Errorf("expected code %v, got %v", connect.CodeUnavailable, err)
	}
}

const testNetworkID = "net-abc"

func TestDeleteNetwork(t *testing.T) {
	var removedID string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		// DELETE /networks/{id}
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/networks/") {
			parts := strings.Split(r.URL.Path, "/")
			removedID = parts[len(parts)-1]
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	})

	resp, err := svc.DeleteNetwork(context.Background(), connect.NewRequest(&v1.DeleteNetworkRequest{Id: testNetworkID}))
	if err != nil {
		t.Fatalf("DeleteNetwork failed: %v", err)
	}
	if resp.Msg == nil {
		t.Fatal("expected empty response message")
	}
	if removedID != testNetworkID {
		t.Errorf("expected DELETE for the network, got %q", removedID)
	}
}

func TestDeleteNetworkBlankID(t *testing.T) {
	var daemonCalled bool
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		daemonCalled = true
		http.NotFound(w, r)
	})

	_, err := svc.DeleteNetwork(context.Background(), connect.NewRequest(&v1.DeleteNetworkRequest{Id: ""}))
	if err == nil {
		t.Fatal("expected error for blank network ID")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
	}
	if daemonCalled {
		t.Error("expected no daemon call for blank network ID")
	}
}

func TestDeleteNetworkErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		want   connect.Code
	}{
		"in-use or pre-defined (403)": {status: http.StatusForbidden, want: connect.CodeFailedPrecondition},
		"not found (404)":             {status: http.StatusNotFound, want: connect.CodeNotFound},
		"daemon error (500)":          {status: http.StatusInternalServerError, want: connect.CodeUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if pingHandler(w, r) {
					return
				}
				if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/networks/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"message": "network has active endpoints"}`))
					return
				}
				http.NotFound(w, r)
			})

			_, err := svc.DeleteNetwork(context.Background(), connect.NewRequest(&v1.DeleteNetworkRequest{Id: testNetworkID}))
			if err == nil {
				t.Fatal("expected error")
			}
			if connect.CodeOf(err) != tc.want {
				t.Errorf("expected %v, got %v: %v", tc.want, connect.CodeOf(err), err)
			}
		})
	}
}

func TestDeleteNetworkDaemonDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // dead endpoint

	dockerClient, err := client.New(client.WithHost(server.URL))
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	svc := NewService(dockerClient, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err = svc.DeleteNetwork(context.Background(), connect.NewRequest(&v1.DeleteNetworkRequest{Id: testNetworkID}))
	if err == nil {
		t.Fatal("expected error for dead daemon")
	}
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", connect.CodeOf(err))
	}
}

func TestPruneNetworks(t *testing.T) {
	var rawQuery string
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		// POST /networks/prune
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/networks/prune") {
			rawQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"NetworksDeleted": ["app-net", "staging"]}`))
			return
		}
		http.NotFound(w, r)
	})

	resp, err := svc.PruneNetworks(context.Background(), connect.NewRequest(&v1.PruneNetworksRequest{}))
	if err != nil {
		t.Fatalf("PruneNetworks failed: %v", err)
	}
	if resp.Msg.NetworksDeleted != 2 {
		t.Errorf("expected networks_deleted=2, got %d", resp.Msg.NetworksDeleted)
	}
	if len(resp.Msg.Names) != 2 || resp.Msg.Names[0] != "app-net" || resp.Msg.Names[1] != "staging" {
		t.Errorf("expected names [app-net staging], got %v", resp.Msg.Names)
	}
	// No filters shipped: the daemon's unused scope is fixed server-side.
	if rawQuery != "" {
		t.Errorf("expected no query parameters (no filters), got %q", rawQuery)
	}
}

func TestPruneNetworksNothingToDelete(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if pingHandler(w, r) {
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/networks/prune") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"NetworksDeleted": []}`))
			return
		}
		http.NotFound(w, r)
	})

	resp, err := svc.PruneNetworks(context.Background(), connect.NewRequest(&v1.PruneNetworksRequest{}))
	if err != nil {
		t.Fatalf("PruneNetworks failed: %v", err)
	}
	// Protected networks (bridge/host/none, in-use) are daemon-protected at
	// prune time and simply absent from the report: an honest 0.
	if resp.Msg.NetworksDeleted != 0 || len(resp.Msg.Names) != 0 {
		t.Errorf("expected honest empty report, got %d/%v", resp.Msg.NetworksDeleted, resp.Msg.Names)
	}
}

func TestPruneNetworksDaemonDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // dead endpoint

	dockerClient, err := client.New(client.WithHost(server.URL))
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	svc := NewService(dockerClient, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err = svc.PruneNetworks(context.Background(), connect.NewRequest(&v1.PruneNetworksRequest{}))
	if err == nil {
		t.Fatal("expected error for dead daemon")
	}
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", connect.CodeOf(err))
	}
}

// --- Audit trail (issue #219, design.md §12.4) ---

const (
	auditActionImageDelete   = "image.delete"
	auditActionContainerUpg  = "container.upgrade"
	auditActionNetworkDelete = "network.delete"
	auditTestActorSystem     = "system"
	auditTestActorViewer     = "viewer"
)

func newAuditTestService(t *testing.T) *Service {
	t.Helper()
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })
	if err := db.RunMigrations(dbConn); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return NewService(nil, db.New(dbConn), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func seedAuditEntries(t *testing.T, queries *db.Queries) {
	t.Helper()
	ctx := context.Background()
	seed := []db.CreateAuditLogParams{
		{Actor: "admin", ActorRole: "admin", Source: "user", Action: auditActionImageDelete, ResourceType: "image", ResourceID: "img1", Outcome: "success", Detail: "image deleted"},
		{Actor: auditTestActorSystem, Source: "system", Action: auditActionContainerUpg, ResourceType: "container", ResourceID: "c1", Outcome: "success", Detail: "upgraded"},
		{Actor: auditTestActorViewer, ActorRole: "viewer", Source: "user", Action: auditActionNetworkDelete, ResourceType: "network", ResourceID: "net1", Outcome: "denied", Detail: "admin role required"},
	}
	for _, p := range seed {
		if _, err := queries.CreateAuditLog(ctx, p); err != nil {
			t.Fatalf("failed to seed audit entry: %v", err)
		}
	}
}

func TestListAuditLogsReturnsNewestFirstWithTotal(t *testing.T) {
	svc := newAuditTestService(t)
	seedAuditEntries(t, svc.queries)

	resp, err := svc.ListAuditLogs(context.Background(), connect.NewRequest(&v1.ListAuditLogsRequest{}))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if resp.Msg.Total != 3 {
		t.Fatalf("expected total 3, got %d", resp.Msg.Total)
	}
	if len(resp.Msg.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(resp.Msg.Entries))
	}
	// Newest first: the last-seeded row comes back first.
	if resp.Msg.Entries[0].Action != auditActionNetworkDelete {
		t.Errorf("expected newest entry first, got %q", resp.Msg.Entries[0].Action)
	}
	first := resp.Msg.Entries[0]
	if first.Source != 1 || first.Outcome != 3 {
		t.Errorf("enum mapping broken: %+v", first)
	}
	if first.ActorRole != "viewer" || first.ResourceId != "net1" {
		t.Errorf("field mapping broken: %+v", first)
	}
}

func TestListAuditLogsFilters(t *testing.T) {
	svc := newAuditTestService(t)
	seedAuditEntries(t, svc.queries)

	tests := []struct {
		name      string
		req       func() *v1.ListAuditLogsRequest
		wantTotal uint64
		wantFirst string
	}{
		{
			name: "source filter system",
			req: func() *v1.ListAuditLogsRequest {
				return &v1.ListAuditLogsRequest{Source: 2}
			},
			wantTotal: 1,
			wantFirst: auditActionContainerUpg,
		},
		{
			name: "outcome filter denied",
			req: func() *v1.ListAuditLogsRequest {
				return &v1.ListAuditLogsRequest{Outcome: 3}
			},
			wantTotal: 1,
			wantFirst: auditActionNetworkDelete,
		},
		{
			name:      "query matches action",
			req:       func() *v1.ListAuditLogsRequest { return &v1.ListAuditLogsRequest{Query: "image"} },
			wantTotal: 1,
			wantFirst: auditActionImageDelete,
		},
		{
			name:      "query matches detail",
			req:       func() *v1.ListAuditLogsRequest { return &v1.ListAuditLogsRequest{Query: "admin role required"} },
			wantTotal: 1,
			wantFirst: auditActionNetworkDelete,
		},
		{
			name:      "query matches actor",
			req:       func() *v1.ListAuditLogsRequest { return &v1.ListAuditLogsRequest{Query: "system"} },
			wantTotal: 1,
			wantFirst: auditActionContainerUpg,
		},
		{
			name: "combined filters intersect",
			req: func() *v1.ListAuditLogsRequest {
				return &v1.ListAuditLogsRequest{Query: "delete", Outcome: 3}
			},
			wantTotal: 1,
			wantFirst: auditActionNetworkDelete,
		},
		{
			name:      "no match yields zero with total 0",
			req:       func() *v1.ListAuditLogsRequest { return &v1.ListAuditLogsRequest{Query: "nonexistent"} },
			wantTotal: 0,
			wantFirst: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.ListAuditLogs(context.Background(), connect.NewRequest(tc.req()))
			if err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
			if resp.Msg.Total != tc.wantTotal {
				t.Fatalf("expected total %d, got %d", tc.wantTotal, resp.Msg.Total)
			}
			if tc.wantTotal > 0 && resp.Msg.Entries[0].Action != tc.wantFirst {
				t.Errorf("expected first action %q, got %q", tc.wantFirst, resp.Msg.Entries[0].Action)
			}
		})
	}
}

func TestListAuditLogsPagination(t *testing.T) {
	svc := newAuditTestService(t)
	seedAuditEntries(t, svc.queries)

	resp, err := svc.ListAuditLogs(context.Background(), connect.NewRequest(&v1.ListAuditLogsRequest{Limit: 2}))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(resp.Msg.Entries) != 2 || resp.Msg.Total != 3 {
		t.Fatalf("expected page of 2 with total 3, got %d/%d", len(resp.Msg.Entries), resp.Msg.Total)
	}

	// Page 2: offset 2, limit 2 → the last row.
	resp, err = svc.ListAuditLogs(context.Background(), connect.NewRequest(&v1.ListAuditLogsRequest{Limit: 2, Offset: 2}))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(resp.Msg.Entries) != 1 || resp.Msg.Entries[0].Action != auditActionImageDelete {
		t.Fatalf("expected oldest row on page 2, got %+v", resp.Msg.Entries)
	}

	// Limit clamps to the maximum.
	resp, err = svc.ListAuditLogs(context.Background(), connect.NewRequest(&v1.ListAuditLogsRequest{Limit: 100000}))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(resp.Msg.Entries) != 3 {
		t.Errorf("expected clamped limit to return all rows, got %d", len(resp.Msg.Entries))
	}
}

func TestListAuditLogsWithoutStorageReturnsInternal(t *testing.T) {
	svc := NewService(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := svc.ListAuditLogs(context.Background(), connect.NewRequest(&v1.ListAuditLogsRequest{}))
	if err == nil {
		t.Fatal("expected error when audit storage is unavailable")
	}
	var cerr *connect.Error
	if !errors.As(err, &cerr) || cerr.Code() != connect.CodeInternal {
		t.Fatalf("expected CodeInternal, got: %v", err)
	}
}
