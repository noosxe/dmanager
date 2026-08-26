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
	"dmanager/internal/db"
)

const (
	testUsername = "testuser"
	viewerRole   = "viewer"
)

func TestInterceptorAuthenticateTwoClocks(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()

	// Create a user
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     testUsername,
		PasswordHash: "dummyhash",
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
