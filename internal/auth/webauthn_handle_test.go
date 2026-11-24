package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	connect "connectrpc.com/connect"

	"dmanager/internal/db"
)

func TestNewUserGetsRandomHandleAndCanSignIn(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "newuser")
	auth := newFakeAuthenticator(t)

	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)

	stored, err := svc.Queries.GetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if len(stored.WebauthnUserID) != WebAuthnHandleLen {
		t.Fatalf("handle length = %d, want %d", len(stored.WebauthnUserID), WebAuthnHandleLen)
	}
	if bytes.Equal(stored.WebauthnUserID, legacyWebAuthnHandle(user.ID)) {
		t.Fatal("a new user must not get the row-ID handle")
	}

	if err := finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), stored.WebauthnUserID, assertFlagsSynced, 0, ""); err != nil {
		t.Fatalf("sign-in with the random handle: %v", err)
	}
}

func TestTwoNewUsersGetDifferentHandles(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	a := userHandleFor(t, svc, newPasskeyUser(t, svc.Queries, "usera"))
	b := userHandleFor(t, svc, newPasskeyUser(t, svc.Queries, "userb"))
	if bytes.Equal(a, b) {
		t.Fatal("two users must not share a handle")
	}
}

func TestHandleIsStableOnceChosen(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "stable")
	first := userHandleFor(t, svc, user)
	// Even with a stale user value (no handle) and a passkey added later.
	enrollFakePasskey(t, svc, user, newFakeAuthenticator(t), nil, regFlagsSynced, 0)
	if again := userHandleFor(t, svc, user); !bytes.Equal(first, again) {
		t.Fatal("a chosen handle must never change")
	}
}

func TestLegacyUserKeepsRowIDHandle(t *testing.T) {
	dbConn, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "legacy")
	auth := newFakeAuthenticator(t)
	legacy := legacyWebAuthnHandle(user.ID)

	// A passkey registered before random handles existed: its authenticator
	// stores the row-ID handle, and the users row has no handle stored.
	n, err := svc.Queries.SetUserWebAuthnID(context.Background(), setHandleParams(legacy, user.ID))
	if err != nil || n != 1 {
		t.Fatalf("seed legacy handle: %v (rows %d)", err, n)
	}
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)
	if _, err := dbConn.Exec("UPDATE users SET webauthn_user_id = NULL WHERE id = ?", user.ID); err != nil {
		t.Fatalf("clear handle: %v", err)
	}

	if err := finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), legacy, assertFlagsSynced, 0, ""); err != nil {
		t.Fatalf("legacy passkey must still sign in: %v", err)
	}
	stored, _ := svc.Queries.GetUser(context.Background(), user.ID)
	if !bytes.Equal(stored.WebauthnUserID, legacy) {
		t.Fatalf("legacy handle must be persisted for a user with passkeys, got %x", stored.WebauthnUserID)
	}
}

// Pins go-webauthn's own comparison of the returned handle with the
// credential owner's stored handle; dmanager adds no second check.
func TestSignInRejectsUnknownOrForeignHandle(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "owner")
	other := newPasskeyUser(t, svc.Queries, "stranger")
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)

	for name, handle := range map[string][]byte{
		"unknown": bytes.Repeat([]byte{7}, WebAuthnHandleLen),
		"foreign": userHandleFor(t, svc, other),
		"empty":   nil,
	} {
		if err := finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), handle, assertFlagsSynced, 0, ""); err == nil {
			t.Errorf("%s handle must not sign in", name)
		}
	}
}

func setHandleParams(handle []byte, id int64) db.SetUserWebAuthnIDParams {
	return db.SetUserWebAuthnIDParams{WebauthnUserID: handle, ID: id}
}

// TestFinishPasskeyLoginFailuresAreIndistinguishable pins #392: every
// verification failure returns the same Unauthenticated message, with no
// library detail; the reason is kept in the audit event.
func TestFinishPasskeyLoginFailuresAreIndistinguishable(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "failures")
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)
	handle := userHandleFor(t, svc, user)

	cases := map[string]func() error{
		"unparseable response": func() error {
			return finishFakeLoginRaw(t, svc, "not json")
		},
		"expired or unknown challenge": func() error {
			return finishFakeLogin(t, svc, auth, "unknown-challenge", handle, assertFlagsSynced, 0, "")
		},
		"unknown credential": func() error {
			return finishFakeLogin(t, svc, newFakeAuthenticator(t), beginFakeLogin(t, svc), handle, assertFlagsSynced, 0, "")
		},
		// BE flag differs from enrollment: the library reports a detailed error.
		"failed assertion": func() error {
			return finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), handle, assertFlagsNormal, 0, "")
		},
	}
	for name, run := range cases {
		err := run()
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("%s: code = %v, want Unauthenticated (%v)", name, connect.CodeOf(err), err)
			continue
		}
		var cerr *connect.Error
		if !errors.As(err, &cerr) || cerr.Message() != errPasskeySignInFailed.Error() {
			t.Errorf("%s: message = %q, want %q", name, err.Error(), errPasskeySignInFailed.Error())
		}
	}

	// The specific reason stays in the audit events.
	events, err := svc.Queries.ListAuthEventsByUser(context.Background(), db.ListAuthEventsByUserParams{
		UserID: sql.NullInt64{Int64: user.ID, Valid: true}, Limit: 20,
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var detailed bool
	for _, e := range events {
		if e.Event == eventLoginFailed && strings.Contains(e.Detail, "validation failed:") {
			detailed = true
		}
	}
	if !detailed {
		t.Error("the failed assertion's reason must be recorded in the audit event")
	}
}

func TestTruncateForAudit(t *testing.T) {
	if got := truncateForAudit("short", 200); got != "short" {
		t.Errorf("short text changed: %q", got)
	}
	long := strings.Repeat("é", 300) // two bytes per rune
	got := truncateForAudit(long, 200)
	if len(got) > 203 || !strings.HasSuffix(got, "...") {
		t.Errorf("not capped: len %d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("cut inside a multi-byte character")
	}
}
