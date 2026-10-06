package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

// Policy-pinning and red-team tests for the passkey ceremony, driven through
// dmanager's real service methods with a synthetic authenticator speaking the
// real wire format (webauthn_fake_test.go). The library defaults dmanager
// implicitly relies on are pinned here so a go-webauthn upgrade that flips
// one fails CI instead of production (#306). Real client payload captures
// complement these synthetics; see docs/passkeys.md for the client matrix.

const (
	testRPID   = "localhost"
	testOrigin = "https://localhost:9283"

	regFlagsSynced    = flagUP | flagUV | flagAT | flagBE | flagBS // synced passkey: syncable
	regFlagsDevice    = flagUP | flagUV | flagAT                   // device-bound: no backup
	assertFlagsNormal = flagUP | flagUV
	assertFlagsSynced = flagUP | flagUV | flagBE // assertions must keep BE consistent with enrollment
)

// newPasskeyService builds a service over a fresh in-memory database and
// also returns the raw handle for tests that need to tamper with rows
// (expiry simulation).
func newPasskeyService(t *testing.T) (*sql.DB, *db.Queries, *Service) {
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
	queries := db.New(dbConn)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	return dbConn, queries, svc
}

func newPasskeyUser(t *testing.T, queries *db.Queries, name string) db.User {
	t.Helper()
	user, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Username:     name,
		PasswordHash: testDummyHash,
		Role:         testViewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create user %s: %v", name, err)
	}
	return user
}

func userHandleFor(user db.User) []byte {
	return (&WebAuthnUser{ID: user.ID, Username: user.Username, DisplayName: user.Username}).WebAuthnID()
}

// beginFakeRegistration starts a registration ceremony for the user and
// returns the challenge from the returned creation options.
func beginFakeRegistration(t *testing.T, svc *Service, user db.User) string {
	t.Helper()
	authCtx := context.WithValue(context.Background(), userContextKey, user)
	begin, err := svc.BeginPasskeyRegistration(authCtx, connect.NewRequest(&v1.BeginPasskeyRegistrationRequest{
		Name: "test key",
	}))
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	return challengeFromOptions(t, begin.Msg.OptionsJson)
}

// enrollFakePasskey drives a full registration ceremony through the service.
// clientExt simulates the client's extension outputs (nil = none returned);
// flags and counter simulate the authenticator's presentation.
func enrollFakePasskey(t *testing.T, svc *Service, user db.User, auth *fakeAuthenticator, clientExt map[string]any, flags byte, counter uint32) {
	t.Helper()
	authCtx := context.WithValue(context.Background(), userContextKey, user)
	challenge := beginFakeRegistration(t, svc, user)
	_, err := svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{
		Name:         "test key",
		ResponseJson: auth.RegistrationResponseJSON(challenge, clientExt, flags, counter),
	}))
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
}

func beginFakeLogin(t *testing.T, svc *Service) string {
	t.Helper()
	begin, err := svc.BeginPasskeyLogin(context.Background(), connect.NewRequest(&v1.BeginPasskeyLoginRequest{}))
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	return challengeFromOptions(t, begin.Msg.OptionsJson)
}

func finishFakeLogin(t *testing.T, svc *Service, auth *fakeAuthenticator, challenge string, userHandle []byte, flags byte, counter uint32, origin string) error {
	t.Helper()
	_, err := svc.FinishPasskeyLogin(context.Background(), connect.NewRequest(&v1.FinishPasskeyLoginRequest{
		ResponseJson: auth.AssertionResponseJSON(challenge, userHandle, nil, flags, counter, origin),
	}))
	return err
}

func finishFakeLoginRaw(t *testing.T, svc *Service, responseJSON string) error {
	t.Helper()
	_, err := svc.FinishPasskeyLogin(context.Background(), connect.NewRequest(&v1.FinishPasskeyLoginRequest{
		ResponseJson: responseJSON,
	}))
	return err
}

func challengeFromOptions(t *testing.T, optionsJSON string) string {
	t.Helper()
	var optMap map[string]any
	if err := json.Unmarshal([]byte(optionsJSON), &optMap); err != nil {
		t.Fatalf("failed to parse options_json: %v", err)
	}
	challenge, _ := optMap["challenge"].(string)
	if challenge == "" {
		t.Fatalf("expected challenge in options_json: %s", optionsJSON)
	}
	return challenge
}

func credentialRow(t *testing.T, queries *db.Queries, credID []byte) db.WebauthnCredential {
	t.Helper()
	row, err := queries.GetWebAuthnCredential(context.Background(), credID)
	if err != nil {
		t.Fatalf("failed to fetch credential: %v", err)
	}
	return row
}

// TestPolicyUnsolicitedExtensionOutputRejected pins the library's default
// unsolicited-output policy (reject): a client returning an extension output
// the RP never requested must fail the ceremony. #305's fix requests
// credProps at begin, so a spec-mandated credProps output is accepted —
// replaying that exact acceptance here is the #305 acceptance criterion.
func TestPolicyUnsolicitedExtensionOutputRejected(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "alice")
	auth := newFakeAuthenticator(t)

	// Positive control: requested credProps output accepted (the #305 replay,
	// full ceremony this time rather than a unit-level probe).
	enrollFakePasskey(t, svc, user, auth, map[string]any{
		"credProps": map[string]any{"rk": true},
	}, regFlagsSynced, 1)

	// Windows Hello has historically returned the uvm extension without the
	// RP requesting it. With credProps requested, uvm remains unsolicited and
	// must be rejected — a library upgrade silently flipping the default to
	// ignore would fail right here.
	auth2 := newFakeAuthenticator(t)
	authCtx := context.WithValue(context.Background(), userContextKey, user)
	challenge := beginFakeRegistration(t, svc, user)
	_, err := svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{
		Name:         "hello key",
		ResponseJson: auth2.RegistrationResponseJSON(challenge, map[string]any{"uvm": []any{}}, regFlagsDevice, 1),
	}))
	if err == nil {
		t.Fatal("expected unsolicited uvm extension output to be rejected")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
	if !strings.Contains(err.Error(), "not requested") {
		t.Errorf("expected library unsolicited-output error, got: %v", err)
	}
}

// TestPolicyBackupFlagLifecycle pins the backup-flag policy: a BS flip alone
// (passkey synced to another device) is legal and persisted; a BE change
// between ceremonies is a spec-mandated error; BS set without BE is invalid.
func TestPolicyBackupFlagLifecycle(t *testing.T) {
	_, queries, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "bob")
	handle := userHandleFor(user)
	auth := newFakeAuthenticator(t)

	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 5)

	// BS flip: passkey synced to another device presents BS=0 while BE stays
	// 1 — must succeed and persist the new backup state.
	if err := finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), handle, flagUP|flagUV|flagBE, 6, ""); err != nil {
		t.Fatalf("BS flip on synced passkey must be accepted: %v", err)
	}
	row := credentialRow(t, queries, auth.credID)
	if row.BackupState != 0 || row.BackupEligible != 1 {
		t.Errorf("after BS flip want backup_state=0 backup_eligible=1, got %d/%d", row.BackupState, row.BackupEligible)
	}

	// BE change between ceremonies (1 → 0): spec-mandated error.
	challenge := beginFakeLogin(t, svc)
	err := finishFakeLogin(t, svc, auth, challenge, handle, flagUP|flagUV, 7, "")
	if err == nil {
		t.Fatal("expected BE change between ceremonies to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "backup") {
		t.Errorf("expected backup-flag validation error, got: %v", err)
	}

	// BS without BE: a device-bound credential (never backup-eligible)
	// presenting BS=1 — invalid combination, rejected.
	device := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, device, nil, regFlagsDevice, 1)
	challenge = beginFakeLogin(t, svc)
	err = finishFakeLogin(t, svc, device, challenge, handle, flagUP|flagUV|flagBS, 2, "")
	if err == nil {
		t.Fatal("expected BS without BE to be rejected")
	}
}

// TestPolicySignCounterAndCloneDetection pins the counter policy: synced
// authenticators reporting 0 must never trip clone detection; a real counter
// must advance monotonically; a regression is flagged and rejected.
func TestPolicySignCounterAndCloneDetection(t *testing.T) {
	_, queries, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "carol")
	handle := userHandleFor(user)

	// Synced authenticator: 0 → 0 forever, no clone warning.
	synced := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, synced, nil, regFlagsSynced, 0)
	if err := finishFakeLogin(t, svc, synced, beginFakeLogin(t, svc), handle, assertFlagsSynced, 0, ""); err != nil {
		t.Fatalf("sign-count 0 assertion must be accepted (synced authenticator): %v", err)
	}
	row := credentialRow(t, queries, synced.credID)
	if row.SignCount != 0 || row.CloneWarning != 0 {
		t.Errorf("synced 0→0: want sign_count=0 clone_warning=0, got %d/%d", row.SignCount, row.CloneWarning)
	}

	// Counter authenticator: monotonic advance accepted, regression rejected.
	counter := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, counter, nil, regFlagsDevice, 7)
	if row := credentialRow(t, queries, counter.credID); row.SignCount != 7 {
		t.Fatalf("expected enrolled sign_count=7, got %d", row.SignCount)
	}
	if err := finishFakeLogin(t, svc, counter, beginFakeLogin(t, svc), handle, assertFlagsNormal, 9, ""); err != nil {
		t.Fatalf("monotonic counter 7→9 must be accepted: %v", err)
	}
	if row := credentialRow(t, queries, counter.credID); row.SignCount != 9 {
		t.Errorf("expected persisted sign_count=9, got %d", row.SignCount)
	}

	challenge := beginFakeLogin(t, svc)
	err := finishFakeLogin(t, svc, counter, challenge, handle, assertFlagsNormal, 4, "")
	if err == nil {
		t.Fatal("expected counter regression 9→4 to be rejected")
	}
	if !strings.Contains(err.Error(), "counter regression") {
		t.Errorf("expected clone-detection error, got: %v", err)
	}
	if row := credentialRow(t, queries, counter.credID); row.CloneWarning != 1 {
		t.Errorf("expected clone_warning=1 persisted after regression, got %d", row.CloneWarning)
	}
}

// TestRedTeamCeremonyEdges covers the ceremony/session edge catalog:
// challenge single-use (replay), expiry, unknown credential, and duplicate
// credential registration.
func TestRedTeamCeremonyEdges(t *testing.T) {
	dbConn, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "dave")
	handle := userHandleFor(user)
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 1)

	// Replay: the same assertion must not work twice — challenges are
	// single-use (consumed on first successful finish).
	first := auth.AssertionResponseJSON(beginFakeLogin(t, svc), handle, nil, assertFlagsSynced, 2, "")
	if err := finishFakeLoginRaw(t, svc, first); err != nil {
		t.Fatalf("first assertion must succeed: %v", err)
	}
	if err := finishFakeLoginRaw(t, svc, first); err == nil {
		t.Fatal("expected replayed assertion to be rejected (challenge consumed)")
	} else if !strings.Contains(err.Error(), "invalid or expired") {
		t.Errorf("expected challenge-consumed error, got: %v", err)
	}

	// Expiry: a challenge past its expires_at must be rejected even though it
	// is unconsumed (the 120s gate against slow/hybrid flows).
	challenge := beginFakeLogin(t, svc)
	past := time.Now().Add(-time.Minute).UTC()
	if _, err := dbConn.Exec(`UPDATE webauthn_challenges SET expires_at = ? WHERE kind = 'login' AND consumed = 0`, past); err != nil {
		t.Fatalf("failed to expire challenge: %v", err)
	}
	err := finishFakeLogin(t, svc, auth, challenge, handle, assertFlagsSynced, 3, "")
	if err == nil || !strings.Contains(err.Error(), "invalid or expired") {
		t.Fatalf("expected expired-challenge rejection, got: %v", err)
	}

	// Unknown credential: an assertion from a credential the server has never
	// seen is rejected before user resolution.
	stranger := newFakeAuthenticator(t)
	challenge = beginFakeLogin(t, svc)
	err = finishFakeLogin(t, svc, stranger, challenge, handle, assertFlagsNormal, 1, "")
	if err == nil {
		t.Fatal("expected unknown credential to be rejected")
	}
	if !strings.Contains(err.Error(), "credential not found") {
		t.Errorf("expected unknown-credential error, got: %v", err)
	}

	// Duplicate registration: re-registering the same credential ID must fail
	// with CodeAlreadyExists, not create a second row.
	authCtx := context.WithValue(context.Background(), userContextKey, user)
	challenge = beginFakeRegistration(t, svc, user)
	_, err = svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{
		Name:         "dupe",
		ResponseJson: auth.RegistrationResponseJSON(challenge, nil, regFlagsSynced, 2),
	}))
	if err == nil {
		t.Fatal("expected duplicate credential registration to be rejected")
	}
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("expected CodeAlreadyExists, got %v: %v", connect.CodeOf(err), err)
	}
}

// TestRedTeamContextMismatch pins origin and rpID binding: a valid signature
// over the wrong origin or wrong rpIdHash must fail verification.
func TestRedTeamContextMismatch(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "eve")
	handle := userHandleFor(user)
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 1)

	// Origin mismatch: real signature, wrong origin in clientDataJSON.
	challenge := beginFakeLogin(t, svc)
	err := finishFakeLogin(t, svc, auth, challenge, handle, assertFlagsNormal, 2, "https://evil.example")
	if err == nil {
		t.Fatal("expected wrong-origin assertion to be rejected")
	}

	// rpID mismatch: same key material, rpIdHash computed over a different
	// RP ID (phishing-domain style clone).
	shadow := auth.shadowOf("evil.example", testOrigin)
	challenge = beginFakeLogin(t, svc)
	if err = finishFakeLogin(t, svc, shadow, challenge, handle, assertFlagsNormal, 2, ""); err == nil {
		t.Fatal("expected wrong-rpIdHash assertion to be rejected")
	}
}
