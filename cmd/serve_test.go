package cmd

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const exampleCom = "http://example.com"

func TestWithCORS(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins []string
		originHeader   string
		expectedCORS   string
	}{
		{
			name:           "Empty allowed origins",
			allowedOrigins: []string{},
			originHeader:   exampleCom,
			expectedCORS:   "",
		},
		{
			name:           "Allowed origin matches",
			allowedOrigins: []string{"http://localhost:5173", exampleCom},
			originHeader:   exampleCom,
			expectedCORS:   exampleCom,
		},
		{
			name:           "Allowed origin does not match",
			allowedOrigins: []string{"http://localhost:5173"},
			originHeader:   exampleCom,
			expectedCORS:   "",
		},
		{
			name:           "Wildcard matches any",
			allowedOrigins: []string{"*"},
			originHeader:   "http://another.com",
			expectedCORS:   "http://another.com",
		},
		{
			name:           "No origin header in request",
			allowedOrigins: []string{"*"},
			originHeader:   "",
			expectedCORS:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			handler := withCORS(tt.allowedOrigins, dummyHandler)
			req := httptest.NewRequest("POST", "/test", nil)
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			corsHeader := rec.Header().Get("Access-Control-Allow-Origin")
			if corsHeader != tt.expectedCORS {
				t.Errorf("expected Access-Control-Allow-Origin %q, got %q", tt.expectedCORS, corsHeader)
			}
		})
	}
}

func TestPlainListenAddr(t *testing.T) {
	tests := []struct{ host, port, want string }{
		{"", "9283", ":9283"},
		{"127.0.0.1", "9283", "127.0.0.1:9283"},
		{"::1", "8080", "[::1]:8080"},
		{"localhost", "80", "localhost:80"},
	}
	for _, tt := range tests {
		if got := plainListenAddr(tt.host, tt.port); got != tt.want {
			t.Errorf("plainListenAddr(%q, %q) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}
}

type fakeTailnetNode struct {
	connect func(ctx context.Context) error
	serve   func() error
}

func (f fakeTailnetNode) Connect(ctx context.Context) error { return f.connect(ctx) }
func (f fakeTailnetNode) Serve(http.Handler) error          { return f.serve() }

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRunTailnetWaitsWithoutExitingAndStopsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	node := fakeTailnetNode{
		// Connect keeps waiting (retrying inside) until shutdown.
		connect: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		serve:   func() error { t.Error("must not serve before the node is up"); return nil },
	}
	go func() { done <- runTailnet(ctx, quietLogger(), node, http.NotFoundHandler()) }()

	select {
	case err := <-done:
		t.Fatalf("runTailnet exited while the node was still waiting: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("shutdown must not be reported as a failure, got %v", err)
	}
}

func TestRunTailnetReturnsUnrecoverableError(t *testing.T) {
	want := errors.New("auth key rejected")
	node := fakeTailnetNode{connect: func(context.Context) error { return want }}
	if err := runTailnet(context.Background(), quietLogger(), node, nil); !errors.Is(err, want) {
		t.Errorf("err = %v", err)
	}
}

func TestRunTailnetServesOnceUp(t *testing.T) {
	served := false
	node := fakeTailnetNode{
		connect: func(context.Context) error { return nil },
		serve:   func() error { served = true; return nil },
	}
	if err := runTailnet(context.Background(), quietLogger(), node, nil); err != nil || !served {
		t.Errorf("err=%v served=%v", err, served)
	}
	boom := errors.New("listen failed")
	node.serve = func() error { return boom }
	if err := runTailnet(context.Background(), quietLogger(), node, nil); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}
