package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	connect "connectrpc.com/connect"

	"dmanager/internal/audit"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	dmanagerv1connect "dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

// stubAuditor captures entries for assertions; concurrency-safe because the
// interceptor calls Record from request goroutines.
const (
	auditTestAdminName  = "auditadmin"
	auditTestViewerName = "auditviewer"
)

type stubAuditor struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (a *stubAuditor) Record(_ context.Context, e audit.Entry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
}

func (a *stubAuditor) snapshot() []audit.Entry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]audit.Entry(nil), a.entries...)
}

func (a *stubAuditor) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = nil
}

// seedAuditSessions creates an admin and a viewer with live sessions.
func seedAuditSessions(t *testing.T, queries *db.Queries) (adminSessionID, viewerSessionID string) {
	t.Helper()
	ctx := context.Background()

	adminUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     auditTestAdminName,
		PasswordHash: dummyHashValue,
		Role:         adminRole,
	})
	if err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}
	viewerUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     auditTestViewerName,
		PasswordHash: dummyHashValue,
		Role:         viewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create viewer user: %v", err)
	}

	now := time.Now()
	for _, tc := range []struct {
		id     string
		userID int64
	}{
		{"audit-admin-session", adminUser.ID},
		{"audit-viewer-session", viewerUser.ID},
	} {
		if _, err := queries.CreateSession(ctx, db.CreateSessionParams{
			SessionID:         tc.id,
			UserID:            tc.userID,
			ExpiresAt:         now.Add(24 * time.Hour),
			LastSeenAt:        now,
			AbsoluteExpiresAt: now.Add(720 * time.Hour),
		}); err != nil {
			t.Fatalf("failed to create session %s: %v", tc.id, err)
		}
	}
	return "audit-admin-session", "audit-viewer-session"
}

func TestInterceptorAuditsMutationOutcomes(t *testing.T) {
	queries := newTestDB(t)
	adminSession, viewerSession := seedAuditSessions(t, queries)
	stub := &stubAuditor{}
	interceptor := NewInterceptor(queries, slog.New(slog.NewTextHandler(io.Discard, nil)), 168*time.Hour, stub)

	build := func(procedure string, msg connect.AnyRequest) connect.AnyRequest {
		return &customProcedureRequest{AnyRequest: msg, procedure: procedure}
	}

	tests := []struct {
		name        string
		procedure   string
		sessionID   string
		req         func() connect.AnyRequest
		handlerResp func() connect.AnyResponse
		handlerErr  error
		wantOutcome string
		wantAction  string
		wantActor   string
		wantDetail  string
	}{
		{
			name:        "admin delete image success",
			procedure:   dmanagerv1connect.AdminServiceDeleteImageProcedure,
			sessionID:   adminSession,
			req:         func() connect.AnyRequest { return connect.NewRequest(&v1.DeleteImageRequest{Id: "img123"}) },
			handlerResp: func() connect.AnyResponse { return connect.NewResponse(&v1.DeleteImageResponse{}) },
			wantOutcome: audit.OutcomeSuccess,
			wantAction:  "image.delete",
			wantActor:   auditTestAdminName,
			wantDetail:  "image deleted",
		},
		{
			name:      "admin prune images success with report detail",
			procedure: dmanagerv1connect.AdminServicePruneImagesProcedure,
			sessionID: adminSession,
			req:       func() connect.AnyRequest { return connect.NewRequest(&v1.PruneImagesRequest{}) },
			handlerResp: func() connect.AnyResponse {
				return connect.NewResponse(&v1.PruneImagesResponse{
					ImagesDeleted:  []*v1.PrunedImage{{Deleted: "a"}, {Deleted: "b"}},
					SpaceReclaimed: 123,
				})
			},
			wantOutcome: audit.OutcomeSuccess,
			wantAction:  "image.prune",
			wantActor:   auditTestAdminName,
			wantDetail:  "pruned 2 image(s), reclaimed 123 bytes",
		},
		{
			name:        "admin start container failure carries error message",
			procedure:   dmanagerv1connect.ContainerServiceStartContainerProcedure,
			sessionID:   adminSession,
			req:         func() connect.AnyRequest { return connect.NewRequest(&v1.StartContainerRequest{Id: "c456"}) },
			handlerResp: func() connect.AnyResponse { return nil },
			handlerErr:  connect.NewError(connect.CodeNotFound, errors.New("container not found")),
			wantOutcome: audit.OutcomeFailure,
			wantAction:  "container.start",
			wantActor:   auditTestAdminName,
			wantDetail:  "container not found",
		},
		{
			name:        "viewer attempting admin mutation records denied",
			procedure:   dmanagerv1connect.AdminServiceDeleteNetworkProcedure,
			sessionID:   viewerSession,
			req:         func() connect.AnyRequest { return connect.NewRequest(&v1.DeleteNetworkRequest{Id: "net1"}) },
			handlerResp: func() connect.AnyResponse { return nil },
			wantOutcome: audit.OutcomeDenied,
			wantAction:  "network.delete",
			wantActor:   auditTestViewerName,
			wantDetail:  "admin role required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub.reset()
			req := build(tc.procedure, tc.req())
			if tc.sessionID != "" {
				req.Header().Set("Cookie", fmt.Sprintf("session_id=%s", tc.sessionID))
			}

			wrapped := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
				return tc.handlerResp(), tc.handlerErr
			})
			_, err := wrapped(context.Background(), req)

			if tc.wantOutcome == audit.OutcomeSuccess {
				if err != nil {
					t.Fatalf("expected handler success, got: %v", err)
				}
			} else if err == nil {
				// failure and denied outcomes surface the error to the client.
				t.Fatalf("expected error for %s outcome, got none", tc.wantOutcome)
			}

			entries := stub.snapshot()
			if len(entries) != 1 {
				t.Fatalf("expected exactly 1 audit entry, got %d", len(entries))
			}
			e := entries[0]
			if e.Outcome != tc.wantOutcome || e.Action != tc.wantAction || e.Actor != tc.wantActor {
				t.Fatalf("unexpected entry: %+v", e)
			}
			if tc.wantDetail != "" && e.Detail != tc.wantDetail {
				t.Errorf("expected detail %q, got %q", tc.wantDetail, e.Detail)
			}
			if e.Source != audit.SourceUser {
				t.Errorf("expected user source, got %q", e.Source)
			}
		})
	}
}

func TestInterceptorDoesNotAuditReadsOrUpgrade(t *testing.T) {
	queries := newTestDB(t)
	adminSession, _ := seedAuditSessions(t, queries)
	stub := &stubAuditor{}
	interceptor := NewInterceptor(queries, slog.New(slog.NewTextHandler(io.Discard, nil)), 168*time.Hour, stub)

	audited := []struct {
		name      string
		procedure string
	}{
		{"read procedure", dmanagerv1connect.AdminServiceListImagesProcedure},
		{"upgrade path records itself", dmanagerv1connect.ContainerServiceUpgradeContainerProcedure},
	}

	for _, tc := range audited {
		t.Run(tc.name, func(t *testing.T) {
			stub.reset()
			req := &customProcedureRequest{
				AnyRequest: connect.NewRequest(&v1.UpgradeContainerRequest{Id: "c1"}),
				procedure:  tc.procedure,
			}
			req.Header().Set("Cookie", fmt.Sprintf("session_id=%s", adminSession))

			wrapped := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&v1.ListImagesResponse{}), nil
			})
			if _, err := wrapped(context.Background(), req); err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
			if entries := stub.snapshot(); len(entries) != 0 {
				t.Fatalf("expected no audit entries for %s, got %+v", tc.procedure, entries)
			}
		})
	}
}

func TestInterceptorAuditFailureDoesNotBreakRequest(t *testing.T) {
	queries := newTestDB(t)
	adminSession, _ := seedAuditSessions(t, queries)
	// A recorder whose storage is closed: audit writes fail, the mutation must not.
	rec := audit.NewRecorder(queries, slog.New(slog.NewTextHandler(io.Discard, nil)))

	interceptor := NewInterceptor(queries, slog.New(slog.NewTextHandler(io.Discard, nil)), 168*time.Hour, rec)

	req := &customProcedureRequest{
		AnyRequest: connect.NewRequest(&v1.DeleteImageRequest{Id: "img1"}),
		procedure:  dmanagerv1connect.AdminServiceDeleteImageProcedure,
	}
	req.Header().Set("Cookie", fmt.Sprintf("session_id=%s", adminSession))

	wrapped := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&v1.DeleteImageResponse{}), nil
	})
	if _, err := wrapped(context.Background(), req); err != nil {
		t.Fatalf("mutation must succeed even when the audit write fails, got: %v", err)
	}
}

func TestInterceptorNilAuditorDisablesRecording(t *testing.T) {
	queries := newTestDB(t)
	adminSession, _ := seedAuditSessions(t, queries)
	interceptor := NewInterceptor(queries, slog.New(slog.NewTextHandler(io.Discard, nil)), 168*time.Hour, nil)

	req := &customProcedureRequest{
		AnyRequest: connect.NewRequest(&v1.DeleteImageRequest{Id: "img1"}),
		procedure:  dmanagerv1connect.AdminServiceDeleteImageProcedure,
	}
	req.Header().Set("Cookie", fmt.Sprintf("session_id=%s", adminSession))

	wrapped := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&v1.DeleteImageResponse{}), nil
	})
	if _, err := wrapped(context.Background(), req); err != nil {
		t.Fatalf("expected success with nil auditor, got: %v", err)
	}
}
