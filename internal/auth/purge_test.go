package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"dmanager/internal/db"
)

const testDummyHash = "dummyhash"

func TestSessionPurgeJob(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()

	// Create user
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "purgemaster",
		PasswordHash: testDummyHash,
		Role:         adminRole,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	now := time.Now()

	// 1. Valid session (both clocks in future)
	_, err = queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         "valid-session",
		UserID:            user.ID,
		ExpiresAt:         now.Add(1 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to create valid session: %v", err)
	}

	// 2. Idle expired session
	_, err = queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         "idle-expired-session",
		UserID:            user.ID,
		ExpiresAt:         now.Add(-10 * time.Minute),
		LastSeenAt:        now.Add(-20 * time.Minute),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to create idle-expired session: %v", err)
	}

	// 3. Absolute expired session
	_, err = queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         "absolute-expired-session",
		UserID:            user.ID,
		ExpiresAt:         now.Add(10 * time.Minute),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("failed to create absolute-expired session: %v", err)
	}

	// Run purge once
	RunPurgeOnce(ctx, slog.Default(), SessionPurgeFunc(queries))

	// Assert survivors
	if _, err := queries.GetSession(ctx, "valid-session"); err != nil {
		t.Errorf("expected valid-session to survive, got err: %v", err)
	}
	if _, err := queries.GetSession(ctx, "idle-expired-session"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected idle-expired-session to be purged, got: %v", err)
	}
	if _, err := queries.GetSession(ctx, "absolute-expired-session"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected absolute-expired-session to be purged, got: %v", err)
	}
}

func TestExtensiblePurgeRunnerErrorHandling(t *testing.T) {
	ctx := context.Background()

	var firstCalled, secondCalled, thirdCalled atomic.Bool

	fn1 := func(ctx context.Context) error {
		firstCalled.Store(true)
		return nil
	}
	fn2 := func(ctx context.Context) error {
		secondCalled.Store(true)
		return errors.New("simulated error")
	}
	fn3 := func(ctx context.Context) error {
		thirdCalled.Store(true)
		return nil
	}

	RunPurgeOnce(ctx, slog.Default(), fn1, fn2, fn3)

	if !firstCalled.Load() {
		t.Errorf("expected fn1 to be called")
	}
	if !secondCalled.Load() {
		t.Errorf("expected fn2 to be called")
	}
	if !thirdCalled.Load() {
		t.Errorf("expected fn3 to be called despite fn2 error")
	}
}

func TestStartPurgeJobCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var counter atomic.Int32
	purgeFn := func(ctx context.Context) error {
		counter.Add(1)
		return nil
	}

	StartPurgeJob(ctx, slog.Default(), 10*time.Millisecond, purgeFn)

	// Wait for a couple ticks
	time.Sleep(35 * time.Millisecond)

	// Cancel context to stop goroutine
	cancel()

	countBefore := counter.Load()
	if countBefore == 0 {
		t.Errorf("expected purge job to have run at least once, got 0")
	}

	// Wait to verify it has stopped
	time.Sleep(30 * time.Millisecond)
	countAfter := counter.Load()

	// Should not have fired again after cancellation
	if countAfter > countBefore+1 {
		t.Errorf("purge job continued running after cancel: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestAuthEventsPurgeJob(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()

	const eventLoginSuccess = "login_success"

	// 1. Create a recent event
	_, err := queries.CreateAuthEvent(ctx, db.CreateAuthEventParams{
		Username: "user1",
		Event:    eventLoginSuccess,
		Detail:   "ip: 127.0.0.1",
	})
	if err != nil {
		t.Fatalf("failed to create recent event: %v", err)
	}

	// 2. Create an event
	_, err = queries.CreateAuthEvent(ctx, db.CreateAuthEventParams{
		Username: "user2",
		Event:    "login_failed",
		Detail:   "ip: 10.0.0.1",
	})
	if err != nil {
		t.Fatalf("failed to create old event: %v", err)
	}

	// Count should be 2
	count, err := queries.CountAuthEvents(ctx)
	if err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 events before purge, got %d", count)
	}

	// Purge with 90-day retention does not delete recent events (created just now)
	purgeFn := AuthEventsPurgeFunc(queries, 90*24*time.Hour)
	err = purgeFn(ctx)
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}

	count, err = queries.CountAuthEvents(ctx)
	if err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 events to survive 90-day purge, got %d", count)
	}

	// Purge with future cutoff timestamp removes everything
	err = queries.PurgeExpiredAuthEvents(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("purge with future cutoff failed: %v", err)
	}

	count, err = queries.CountAuthEvents(ctx)
	if err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 events after full purge, got %d", count)
	}
}

func TestWebAuthnChallengesPurgeJob(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()
	now := time.Now()
	const (
		challengeKindReg = "registration"
		challengeKindLog = "login"
	)

	// 1. Active unconsumed challenge
	active, err := queries.CreateWebAuthnChallenge(ctx, db.CreateWebAuthnChallengeParams{
		Challenge: []byte("active-challenge"),
		Kind:      challengeKindReg,
		UserID:    sql.NullInt64{Int64: 1, Valid: true},
		ExpiresAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("failed to create active challenge: %v", err)
	}

	// 2. Consumed challenge
	consumed, err := queries.CreateWebAuthnChallenge(ctx, db.CreateWebAuthnChallengeParams{
		Challenge: []byte("consumed-challenge"),
		Kind:      challengeKindLog,
		UserID:    sql.NullInt64{Valid: false},
		ExpiresAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("failed to create consumed challenge: %v", err)
	}
	_ = queries.ConsumeWebAuthnChallenge(ctx, consumed.ID)

	// 3. Expired unconsumed challenge
	_, err = queries.CreateWebAuthnChallenge(ctx, db.CreateWebAuthnChallengeParams{
		Challenge: []byte("expired-challenge"),
		Kind:      challengeKindLog,
		UserID:    sql.NullInt64{Valid: false},
		ExpiresAt: now.Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("failed to create expired challenge: %v", err)
	}

	// Run purge
	purgeFn := WebAuthnChallengesPurgeFunc(queries)
	err = purgeFn(ctx)
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}

	// Check active challenge survives and is unconsumed
	found, err := queries.GetUnconsumedWebAuthnChallenge(ctx, db.GetUnconsumedWebAuthnChallengeParams{
		Challenge: []byte("active-challenge"),
		Kind:      challengeKindReg,
		ExpiresAt: now,
	})
	if err != nil {
		t.Fatalf("expected active challenge to survive: %v", err)
	}
	if found.ID != active.ID {
		t.Errorf("expected active challenge ID %d, got %d", active.ID, found.ID)
	}

	// Check consumed and expired challenges are gone
	_, err = queries.GetUnconsumedWebAuthnChallenge(ctx, db.GetUnconsumedWebAuthnChallengeParams{
		Challenge: []byte("consumed-challenge"),
		Kind:      "login",
		ExpiresAt: now,
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected consumed challenge to be purged, got: %v", err)
	}

	_, err = queries.GetUnconsumedWebAuthnChallenge(ctx, db.GetUnconsumedWebAuthnChallengeParams{
		Challenge: []byte("expired-challenge"),
		Kind:      "login",
		ExpiresAt: now,
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected expired challenge to be purged, got: %v", err)
	}
}
