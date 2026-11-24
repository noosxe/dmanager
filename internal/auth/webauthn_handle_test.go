package auth

import (
	"bytes"
	"context"
	"testing"

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
