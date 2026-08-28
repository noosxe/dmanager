package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"

	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

const (
	testUsername   = "testuser"
	viewerRole     = "viewer"
	dummyHashValue = "dummyhash"
)

func TestInterceptorAuthenticateTwoClocks(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()

	// Create a user
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     testUsername,
		PasswordHash: dummyHashValue,
		Role:         viewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	idleTimeout := 10 * time.Minute
	interceptor := NewInterceptor(queries, slog.Default(), idleTimeout)

	t.Run("valid session before half-idle window does not touch DB", func(t *testing.T) {
		sessionID := "session-no-touch"
		initialLastSeen := time.Now().Add(-2 * time.Minute) // 2m into 10m window (< 5m threshold)
		initialExpires := initialLastSeen.Add(idleTimeout)
		absExpires := time.Now().Add(60 * time.Minute)

		_, err := queries.CreateSession(ctx, db.CreateSessionParams{
			SessionID:         sessionID,
			UserID:            user.ID,
			ExpiresAt:         initialExpires,
			LastSeenAt:        initialLastSeen,
			AbsoluteExpiresAt: absExpires,
		})
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		cookie := fmt.Sprintf("session_id=%s", sessionID)
		authCtx, err := interceptor.authenticate(ctx, cookie)
		if err != nil {
			t.Fatalf("unexpected auth error: %v", err)
		}
		u, ok := UserFromContext(authCtx)
		if !ok || u.Username != testUsername {
			t.Fatalf("expected authenticated user testuser, got %v", u)
		}

		// Verify DB was NOT touched
		s, err := queries.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("failed to get session: %v", err)
		}
		if !s.LastSeenAt.Equal(initialLastSeen) {
			t.Errorf("expected LastSeenAt unchanged, got %v (initial %v)", s.LastSeenAt, initialLastSeen)
		}
		if !s.ExpiresAt.Equal(initialExpires) {
			t.Errorf("expected ExpiresAt unchanged, got %v (initial %v)", s.ExpiresAt, initialExpires)
		}
	})

	t.Run("valid session after half-idle window slides expires_at and updates last_seen_at", func(t *testing.T) {
		sessionID := "session-slide"
		initialLastSeen := time.Now().Add(-6 * time.Minute) // 6m into 10m window (> 5m threshold)
		initialExpires := initialLastSeen.Add(idleTimeout)  // expires in 4m
		absExpires := time.Now().Add(60 * time.Minute)

		_, err := queries.CreateSession(ctx, db.CreateSessionParams{
			SessionID:         sessionID,
			UserID:            user.ID,
			ExpiresAt:         initialExpires,
			LastSeenAt:        initialLastSeen,
			AbsoluteExpiresAt: absExpires,
		})
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		cookie := fmt.Sprintf("session_id=%s", sessionID)
		beforeAuth := time.Now()
		authCtx, err := interceptor.authenticate(ctx, cookie)
		if err != nil {
			t.Fatalf("unexpected auth error: %v", err)
		}
		u, ok := UserFromContext(authCtx)
		if !ok || u.Username != testUsername {
			t.Fatalf("expected authenticated user testuser, got %v", u)
		}

		// Verify DB WAS updated
		s, err := queries.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("failed to get session: %v", err)
		}
		if s.LastSeenAt.Before(beforeAuth) {
			t.Errorf("expected LastSeenAt updated to around %v, got %v", beforeAuth, s.LastSeenAt)
		}
		if s.ExpiresAt.Before(beforeAuth.Add(idleTimeout - time.Second)) {
			t.Errorf("expected ExpiresAt slid to around %v, got %v", beforeAuth.Add(idleTimeout), s.ExpiresAt)
		}
	})

	t.Run("slide clamped to absolute_expires_at", func(t *testing.T) {
		sessionID := "session-clamp"
		initialLastSeen := time.Now().Add(-6 * time.Minute)
		initialExpires := initialLastSeen.Add(idleTimeout)
		absExpires := time.Now().Add(5 * time.Minute) // absolute is only 5m from now (< now + 10m)

		_, err := queries.CreateSession(ctx, db.CreateSessionParams{
			SessionID:         sessionID,
			UserID:            user.ID,
			ExpiresAt:         initialExpires,
			LastSeenAt:        initialLastSeen,
			AbsoluteExpiresAt: absExpires,
		})
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		cookie := fmt.Sprintf("session_id=%s", sessionID)
		_, err = interceptor.authenticate(ctx, cookie)
		if err != nil {
			t.Fatalf("unexpected auth error: %v", err)
		}

		s, err := queries.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("failed to get session: %v", err)
		}
		if s.ExpiresAt.After(absExpires) {
			t.Errorf("expected ExpiresAt clamped to %v, got %v", absExpires, s.ExpiresAt)
		}
		if !s.ExpiresAt.Equal(absExpires) {
			t.Errorf("expected ExpiresAt equal to absExpires (%v), got %v", absExpires, s.ExpiresAt)
		}
	})

	t.Run("session expired by idle is rejected and deleted", func(t *testing.T) {
		sessionID := "session-idle-expired"
		initialLastSeen := time.Now().Add(-15 * time.Minute)
		initialExpires := time.Now().Add(-5 * time.Minute) // expired 5m ago
		absExpires := time.Now().Add(60 * time.Minute)

		_, err := queries.CreateSession(ctx, db.CreateSessionParams{
			SessionID:         sessionID,
			UserID:            user.ID,
			ExpiresAt:         initialExpires,
			LastSeenAt:        initialLastSeen,
			AbsoluteExpiresAt: absExpires,
		})
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		cookie := fmt.Sprintf("session_id=%s", sessionID)
		_, err = interceptor.authenticate(ctx, cookie)
		if err == nil {
			t.Fatal("expected unauthenticated error, got nil")
		}
		var connectErr *connect.Error
		if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", err)
		}

		// Verify session was deleted
		_, err = queries.GetSession(ctx, sessionID)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected session deleted from DB, got %v", err)
		}
	})

	t.Run("session expired by absolute cap is rejected and deleted", func(t *testing.T) {
		sessionID := "session-abs-expired"
		initialLastSeen := time.Now().Add(-1 * time.Minute)
		initialExpires := time.Now().Add(9 * time.Minute) // idle clock still valid
		absExpires := time.Now().Add(-1 * time.Minute)    // absolute clock expired

		_, err := queries.CreateSession(ctx, db.CreateSessionParams{
			SessionID:         sessionID,
			UserID:            user.ID,
			ExpiresAt:         initialExpires,
			LastSeenAt:        initialLastSeen,
			AbsoluteExpiresAt: absExpires,
		})
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		cookie := fmt.Sprintf("session_id=%s", sessionID)
		_, err = interceptor.authenticate(ctx, cookie)
		if err == nil {
			t.Fatal("expected unauthenticated error, got nil")
		}
		var connectErr *connect.Error
		if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnauthenticated {
			t.Errorf("expected CodeUnauthenticated, got %v", err)
		}

		// Verify session was deleted
		_, err = queries.GetSession(ctx, sessionID)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected session deleted from DB, got %v", err)
		}
	})
}

func TestProcedureClassificationCoverage(t *testing.T) {
	fileDescriptors := []protoreflect.FileDescriptor{
		v1.File_proto_dmanager_v1_auth_proto,
		v1.File_proto_dmanager_v1_container_proto,
		v1.File_proto_dmanager_v1_log_proto,
		v1.File_proto_dmanager_v1_settings_proto,
		v1.File_proto_dmanager_v1_admin_proto,
	}

	var totalProcedures int
	for _, fd := range fileDescriptors {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				method := methods.Get(j)
				procedure := fmt.Sprintf("/%s/%s", svc.FullName(), method.Name())
				totalProcedures++

				role, ok := getProcedureRole(procedure)
				if !ok {
					t.Errorf("procedure %q is NOT classified in access control matrix", procedure)
					continue
				}
				switch role {
				case RoleUnauthenticated, RoleViewer, RoleAdmin:
					// Valid classification
				default:
					t.Errorf("procedure %q has invalid role %v", procedure, role)
				}
			}
		}
	}

	if len(procedureRoles) != totalProcedures {
		t.Errorf("procedureRoles table has %d entries, but found %d proto procedures across all services", len(procedureRoles), totalProcedures)
	}
}

func TestInterceptorRBACEnforcement(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()

	// 1. Create viewer and admin users
	viewerUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "testviewer",
		PasswordHash: dummyHashValue,
		Role:         viewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create viewer user: %v", err)
	}

	adminUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "testadmin",
		PasswordHash: dummyHashValue,
		Role:         adminRole,
	})
	if err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}

	now := time.Now()
	// Seed sessions
	viewerSessionID := "viewer-token-abc"
	_, err = queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         viewerSessionID,
		UserID:            viewerUser.ID,
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to create viewer session: %v", err)
	}

	adminSessionID := "admin-token-xyz"
	_, err = queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         adminSessionID,
		UserID:            adminUser.ID,
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to create admin session: %v", err)
	}

	interceptor := NewInterceptor(queries, slog.Default(), 168*time.Hour)

	tests := []struct {
		name         string
		procedure    string
		sessionID    string
		wantCode     connect.Code
		wantExecuted bool
	}{
		// Unauthenticated procedure
		{
			name:         "anonymous accessing GetServerStatus succeeds",
			procedure:    dmanagerv1connect.AuthServiceGetServerStatusProcedure,
			sessionID:    "",
			wantCode:     0,
			wantExecuted: true,
		},
		{
			name:         "viewer accessing GetServerStatus succeeds",
			procedure:    dmanagerv1connect.AuthServiceGetServerStatusProcedure,
			sessionID:    viewerSessionID,
			wantCode:     0,
			wantExecuted: true,
		},
		// Viewer procedure
		{
			name:         "anonymous accessing ListContainers returns Unauthenticated",
			procedure:    dmanagerv1connect.ContainerServiceListContainersProcedure,
			sessionID:    "",
			wantCode:     connect.CodeUnauthenticated,
			wantExecuted: false,
		},
		{
			name:         "viewer accessing ListContainers succeeds",
			procedure:    dmanagerv1connect.ContainerServiceListContainersProcedure,
			sessionID:    viewerSessionID,
			wantCode:     0,
			wantExecuted: true,
		},
		{
			name:         "admin accessing ListContainers succeeds",
			procedure:    dmanagerv1connect.ContainerServiceListContainersProcedure,
			sessionID:    adminSessionID,
			wantCode:     0,
			wantExecuted: true,
		},
		// Admin procedure
		{
			name:         "anonymous accessing StartContainer returns Unauthenticated",
			procedure:    dmanagerv1connect.ContainerServiceStartContainerProcedure,
			sessionID:    "",
			wantCode:     connect.CodeUnauthenticated,
			wantExecuted: false,
		},
		{
			name:         "viewer accessing StartContainer returns PermissionDenied",
			procedure:    dmanagerv1connect.ContainerServiceStartContainerProcedure,
			sessionID:    viewerSessionID,
			wantCode:     connect.CodePermissionDenied,
			wantExecuted: false,
		},
		{
			name:         "admin accessing StartContainer succeeds",
			procedure:    dmanagerv1connect.ContainerServiceStartContainerProcedure,
			sessionID:    adminSessionID,
			wantCode:     0,
			wantExecuted: true,
		},
		// SyncLogs procedure (per #134)
		{
			name:         "anonymous accessing SyncLogs returns Unauthenticated",
			procedure:    dmanagerv1connect.LogServiceSyncLogsProcedure,
			sessionID:    "",
			wantCode:     connect.CodeUnauthenticated,
			wantExecuted: false,
		},
		{
			name:         "viewer accessing SyncLogs succeeds",
			procedure:    dmanagerv1connect.LogServiceSyncLogsProcedure,
			sessionID:    viewerSessionID,
			wantCode:     0,
			wantExecuted: true,
		},
		// Unclassified procedure fails closed
		{
			name:         "unclassified procedure returns CodeInternal",
			procedure:    "/dmanager.v1.UnknownService/UnknownMethod",
			sessionID:    adminSessionID,
			wantCode:     connect.CodeInternal,
			wantExecuted: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var handlerExecuted bool
			dummyHandler := func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				handlerExecuted = true
				return connect.NewResponse(&v1.GetServerStatusResponse{}), nil
			}

			req := connect.NewRequest(&v1.GetServerStatusRequest{})
			if tc.sessionID != "" {
				req.Header().Set("Cookie", fmt.Sprintf("session_id=%s", tc.sessionID))
			}

			wrapped := interceptor.WrapUnary(dummyHandler)
			_, err := wrapped(ctx, &customProcedureRequest{AnyRequest: req, procedure: tc.procedure})

			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
				if !handlerExecuted {
					t.Errorf("expected handler to execute")
				}
			} else {
				if err == nil {
					t.Fatalf("expected error code %v, got nil", tc.wantCode)
				}
				if connect.CodeOf(err) != tc.wantCode {
					t.Errorf("expected code %v, got %v (%v)", tc.wantCode, connect.CodeOf(err), err)
				}
				if handlerExecuted {
					t.Errorf("expected handler NOT to execute")
				}
			}
		})
	}
}

// customProcedureRequest wraps a connect.AnyRequest and overrides Spec().Procedure
type customProcedureRequest struct {
	connect.AnyRequest
	procedure string
}

func (c *customProcedureRequest) Spec() connect.Spec {
	spec := c.AnyRequest.Spec()
	spec.Procedure = c.procedure
	return spec
}
