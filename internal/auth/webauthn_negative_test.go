package auth

import (
	"context"
	"encoding/hex"
	"testing"

	connect "connectrpc.com/connect"
	"golang.org/x/crypto/bcrypt"

	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

// Negative cases from the passkey hardening review (#396) that the other
// policy tests did not pin: cross-user credential management, and how failed
// passkey assertions interact with the lockout counters.

func TestCredentialManagementIsScopedToOwner(t *testing.T) {
	_, queries, svc := newPasskeyService(t)
	owner := newPasskeyUser(t, queries, "credowner")
	intruder := newPasskeyUser(t, queries, "credintruder")
	enrollFakePasskey(t, svc, owner, newFakeAuthenticator(t), nil, regFlagsSynced, 0)

	creds, err := queries.ListWebAuthnCredentialsByUser(context.Background(), owner.ID)
	if err != nil || len(creds) != 1 {
		t.Fatalf("expected one credential, got %d (%v)", len(creds), err)
	}
	id := hex.EncodeToString(creds[0].CredentialID)
	intruderCtx := WithUser(context.Background(), intruder)

	_, err = svc.RenamePasskey(intruderCtx, connect.NewRequest(&v1.RenamePasskeyRequest{Id: id, Name: "pwned"}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("renaming another user's passkey: code = %v, want NotFound (%v)", connect.CodeOf(err), err)
	}
	_, err = svc.DeletePasskey(intruderCtx, connect.NewRequest(&v1.DeletePasskeyRequest{Id: id}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("deleting another user's passkey: code = %v, want NotFound (%v)", connect.CodeOf(err), err)
	}

	after, err := queries.GetWebAuthnCredential(context.Background(), creds[0].CredentialID)
	if err != nil {
		t.Fatalf("the owner's credential must survive: %v", err)
	}
	if after.Name != creds[0].Name {
		t.Errorf("name changed to %q by another user", after.Name)
	}
}

// failAssertion presents an assertion whose BE flag differs from enrollment, a
// spec-mandated failure that names a known user.
func failAssertion(t *testing.T, svc *Service, auth *fakeAuthenticator, handle []byte) {
	t.Helper()
	if err := finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), handle, assertFlagsNormal, 0, ""); err == nil {
		t.Fatal("expected the BE-flip assertion to fail")
	}
}

func TestRepeatedPasskeyFailuresLockPasskeyButNotPassword(t *testing.T) {
	_, queries, svc := newPasskeyService(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Username: "lockme", PasswordHash: string(hash), Role: testViewerRole,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)
	handle := userHandleFor(t, svc, user)

	for i := 0; i < RateLimitThreshold; i++ {
		failAssertion(t, svc, auth, handle)
	}

	// A correct assertion is now refused: the passkey counter is locked.
	err = finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), handle, assertFlagsSynced, 0, "")
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("good passkey login while locked: code = %v, want ResourceExhausted (%v)", connect.CodeOf(err), err)
	}

	// Password sign-in has its own counter and still works.
	if _, err := svc.Login(context.Background(), connect.NewRequest(&v1.LoginRequest{
		Username: "lockme", Password: "correct horse",
	})); err != nil {
		t.Fatalf("password login must not be locked by passkey failures: %v", err)
	}
}

func TestRepeatedPasswordFailuresDoNotBlockPasskeyLogin(t *testing.T) {
	const guesser, wrongPassword = "pwguess", "wrong"
	_, queries, svc := newPasskeyService(t)
	user := newPasskeyUser(t, queries, guesser)
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)
	handle := userHandleFor(t, svc, user)

	for i := 0; i < RateLimitThreshold; i++ {
		if _, err := svc.Login(context.Background(), connect.NewRequest(&v1.LoginRequest{
			Username: guesser, Password: wrongPassword,
		})); err == nil {
			t.Fatal("wrong password must fail")
		}
	}
	if _, err := svc.Login(context.Background(), connect.NewRequest(&v1.LoginRequest{
		Username: guesser, Password: wrongPassword,
	})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("password method must be locked, got %v", err)
	}

	if err := finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), handle, assertFlagsSynced, 0, ""); err != nil {
		t.Fatalf("passkey login must not be blocked by password failures: %v", err)
	}
	// And the passkey success did not clear the password lockout.
	if _, err := svc.Login(context.Background(), connect.NewRequest(&v1.LoginRequest{
		Username: guesser, Password: wrongPassword,
	})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("a passkey login must not reset the password counter, got %v", err)
	}
}
