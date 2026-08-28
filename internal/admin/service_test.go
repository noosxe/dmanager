package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

// newTestService starts a fake Docker API server backed by handler and
// returns an admin Service wired to it via the moby client.
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
	return NewService(dockerClient, nil)
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
	if first.Id != "sha256:abc123def456" {
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
		if !strings.HasSuffix(r.URL.Path, "/networks") {
			http.NotFound(w, r)
			return
		}
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
			}
		]`))
	})

	resp, err := svc.ListNetworks(context.Background(), connect.NewRequest(&v1.ListNetworksRequest{}))
	if err != nil {
		t.Fatalf("ListNetworks failed: %v", err)
	}

	networks := resp.Msg.Networks
	if len(networks) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(networks))
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

	if !networks[1].Internal {
		t.Error("expected internal=true for secure network")
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
