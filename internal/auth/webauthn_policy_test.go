package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
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

// extCredProps is the credential properties extension identifier.
const extCredProps = "credProps"

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
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false, 1)
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

// userHandleFor returns the user's stored WebAuthn handle, creating it if the
// user has none yet (random for a user without passkeys). It is stable, so it
// may be called before or after enrollment.
func userHandleFor(t *testing.T, svc *Service, user db.User) []byte {
	t.Helper()
	handle, err := svc.ensureWebAuthnHandle(context.Background(), user)
	if err != nil {
		t.Fatalf("ensure user handle: %v", err)
	}
	return handle
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
		extCredProps: map[string]any{"rk": true},
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
	handle := userHandleFor(t, svc, user)
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
	// The browser only sees the generic error; the library's reason is audited.
	if !strings.Contains(strings.ToLower(lastLoginFailedDetail(t, svc, user)), "backup") {
		t.Errorf("expected backup-flag reason in the audit event, got: %q", lastLoginFailedDetail(t, svc, user))
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
// must advance monotonically; a regression (including N→0) is allowed but
// flagged, audited, and not counted as a failed login.
func TestPolicySignCounterAndCloneDetection(t *testing.T) {
	dbConn, queries, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "carol")
	handle := userHandleFor(t, svc, user)

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
	if r := credentialRow(t, queries, counter.credID); r.SignCount != 7 {
		t.Fatalf("expected enrolled sign_count=7, got %d", r.SignCount)
	}
	if err := finishFakeLogin(t, svc, counter, beginFakeLogin(t, svc), handle, assertFlagsNormal, 9, ""); err != nil {
		t.Fatalf("monotonic counter 7→9 must be accepted: %v", err)
	}
	if r := credentialRow(t, queries, counter.credID); r.SignCount != 9 {
		t.Errorf("expected persisted sign_count=9, got %d", r.SignCount)
	}

	// Regression 9→4: allowed, flagged, audited; the new counter is stored.
	if err := finishFakeLogin(t, svc, counter, beginFakeLogin(t, svc), handle, assertFlagsNormal, 4, ""); err != nil {
		t.Fatalf("counter regression 9→4 must be allowed: %v", err)
	}
	row = credentialRow(t, queries, counter.credID)
	if row.CloneWarning != 1 || row.SignCount != 4 {
		t.Errorf("after 9→4 want clone_warning=1 sign_count=4, got %d/%d", row.CloneWarning, row.SignCount)
	}
	var warnings int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE event = 'passkey_clone_warning' AND detail LIKE '%test key%'`).Scan(&warnings); err != nil {
		t.Fatal(err)
	}
	if warnings != 1 {
		t.Errorf("expected one passkey_clone_warning audit event, got %d", warnings)
	}

	// The flag is sticky: a normal advance 4→5 keeps it and does not re-audit.
	if err := finishFakeLogin(t, svc, counter, beginFakeLogin(t, svc), handle, assertFlagsNormal, 5, ""); err != nil {
		t.Fatalf("advance after regression must be accepted: %v", err)
	}
	if row := credentialRow(t, queries, counter.credID); row.CloneWarning != 1 || row.SignCount != 5 {
		t.Errorf("flag must persist and counter advance, got %d/%d", row.CloneWarning, row.SignCount)
	}

	// N→0 on a counting authenticator is flagged too.
	zeroed := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, zeroed, nil, regFlagsDevice, 3)
	if err := finishFakeLogin(t, svc, zeroed, beginFakeLogin(t, svc), handle, assertFlagsNormal, 0, ""); err != nil {
		t.Fatalf("counter 3→0 must be allowed: %v", err)
	}
	if row := credentialRow(t, queries, zeroed.credID); row.CloneWarning != 1 || row.SignCount != 0 {
		t.Errorf("after 3→0 want clone_warning=1 sign_count=0, got %d/%d", row.CloneWarning, row.SignCount)
	}
}

// TestCounterRegressionDoesNotCountAsFailure: repeated regressions never
// lock the account, because the rate limiter only sees real failures.
func TestCounterRegressionDoesNotCountAsFailure(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "frank")
	handle := userHandleFor(t, svc, user)
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsDevice, 50)
	for i := 0; i < 12; i++ {
		if err := finishFakeLogin(t, svc, auth, beginFakeLogin(t, svc), handle, assertFlagsNormal, 1, ""); err != nil {
			t.Fatalf("regression login %d must succeed: %v", i, err)
		}
	}
}

// TestRedTeamCeremonyEdges covers the ceremony/session edge catalog:
// challenge single-use (replay), expiry, unknown credential, and duplicate
// credential registration.
func TestRedTeamCeremonyEdges(t *testing.T) {
	dbConn, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "dave")
	handle := userHandleFor(t, svc, user)
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
	} else if !strings.Contains(err.Error(), errPasskeySignInFailed.Error()) {
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
	if err == nil || !strings.Contains(err.Error(), errPasskeySignInFailed.Error()) {
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
	if !strings.Contains(err.Error(), errPasskeySignInFailed.Error()) {
		t.Errorf("expected the generic sign-in error, got: %v", err)
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
	handle := userHandleFor(t, svc, user)
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

// TestPolicyDiscoverableCredentialRequired pins the discoverable-only policy:
// registration asks for a resident key, a credProps result of rk=false is
// rejected with a clear message, and a missing credProps is accepted.
func TestPolicyDiscoverableCredentialRequired(t *testing.T) {
	_, queries, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "carol")
	authCtx := context.WithValue(context.Background(), userContextKey, user)

	begin, err := svc.BeginPasskeyRegistration(authCtx, connect.NewRequest(&v1.BeginPasskeyRegistrationRequest{}))
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	var opts struct {
		AuthenticatorSelection struct {
			ResidentKey        string `json:"residentKey"`
			RequireResidentKey bool   `json:"requireResidentKey"`
		} `json:"authenticatorSelection"`
	}
	if err = json.Unmarshal([]byte(begin.Msg.OptionsJson), &opts); err != nil {
		t.Fatal(err)
	}
	if opts.AuthenticatorSelection.ResidentKey != "required" || !opts.AuthenticatorSelection.RequireResidentKey {
		t.Errorf("expected resident key required, got %+v", opts.AuthenticatorSelection)
	}

	// rk=false: rejected, nothing stored.
	bad := newFakeAuthenticator(t)
	challenge := beginFakeRegistration(t, svc, user)
	_, err = svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{
		ResponseJson: bad.RegistrationResponseJSON(challenge, map[string]any{extCredProps: map[string]any{"rk": false}}, regFlagsDevice, 0),
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
	if !strings.Contains(err.Error(), "discoverable credentials") {
		t.Errorf("expected user-facing message, got: %v", err)
	}
	if _, err := queries.GetWebAuthnCredential(context.Background(), bad.credID); err == nil {
		t.Error("non-discoverable credential must not be stored")
	}

	// credProps omitted: accepted.
	enrollFakePasskey(t, svc, user, newFakeAuthenticator(t), nil, regFlagsSynced, 0)
}

// TestRegistrationExcludesExistingCredentials pins that the creation options
// list the user's existing credentials, and only theirs.
func TestRegistrationExcludesExistingCredentials(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "dave")
	other := newPasskeyUser(t, svc.Queries, "erin")
	authCtx := context.WithValue(context.Background(), userContextKey, user)

	mine := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, mine, nil, regFlagsSynced, 0)
	theirs := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, other, theirs, nil, regFlagsSynced, 0)

	begin, err := svc.BeginPasskeyRegistration(authCtx, connect.NewRequest(&v1.BeginPasskeyRegistrationRequest{}))
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	var opts struct {
		Exclude []struct {
			ID string `json:"id"`
		} `json:"excludeCredentials"`
	}
	if err := json.Unmarshal([]byte(begin.Msg.OptionsJson), &opts); err != nil {
		t.Fatal(err)
	}
	if len(opts.Exclude) != 1 || opts.Exclude[0].ID != b64url(mine.credID) {
		t.Errorf("expected exactly the user's credential excluded, got %+v", opts.Exclude)
	}
}

// TestBeginPasskeyLoginIsThrottled: the unauthenticated begin call is capped
// per IP, and a throttled client can still finish a ceremony it already
// started (the throttle is not a lockout).
func TestBeginPasskeyLoginIsThrottled(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "gina")
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)

	first := beginFakeLogin(t, svc)
	for i := 1; i < BeginLimit; i++ {
		beginFakeLogin(t, svc)
	}
	_, err := svc.BeginPasskeyLogin(context.Background(), connect.NewRequest(&v1.BeginPasskeyLoginRequest{}))
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("expected ResourceExhausted after %d begins, got %v", BeginLimit, err)
	}
	if err := finishFakeLogin(t, svc, auth, first, userHandleFor(t, svc, user), assertFlagsSynced, 0, ""); err != nil {
		t.Fatalf("a throttled client can still finish an open ceremony: %v", err)
	}
}

// newPasskeyServiceWithUV builds a service with the given
// webauthn.require_user_verification value.
func newPasskeyServiceWithUV(t *testing.T, uv string) *Service {
	t.Helper()
	_, queries, _ := newPasskeyService(t)
	cfg := testWebAuthnConfig()
	cfg.RequireUserVerification = uv
	return NewService(queries, slog.Default(), testAuthConfig(), cfg, false, 1)
}

// TestUserVerificationPolicy pins what "preferred" and "required" mean: under
// preferred a registration or assertion without user verification (PIN-less
// key, an already-unlocked password manager) is accepted and the real flag is
// stored; under required both are rejected.
func TestUserVerificationPolicy(t *testing.T) {
	const noUV = flagUP | flagAT
	const noUVAssert = flagUP

	t.Run("preferred accepts UV=0 and records it", func(t *testing.T) {
		svc := newPasskeyServiceWithUV(t, "preferred")
		user := newPasskeyUser(t, svc.Queries, "hank")
		handle := userHandleFor(t, svc, user)

		verified := newFakeAuthenticator(t)
		enrollFakePasskey(t, svc, user, verified, nil, regFlagsDevice, 1)
		if credentialRow(t, svc.Queries, verified.credID).UserVerified != 1 {
			t.Error("a UV=1 registration must be stored as verified")
		}
		// UV=0 assertion on a credential that was registered with UV=1.
		if err := finishFakeLogin(t, svc, verified, beginFakeLogin(t, svc), handle, noUVAssert, 2, ""); err != nil {
			t.Fatalf("UV=0 assertion must be accepted under preferred: %v", err)
		}

		weak := newFakeAuthenticator(t)
		enrollFakePasskey(t, svc, user, weak, nil, noUV, 1)
		if credentialRow(t, svc.Queries, weak.credID).UserVerified != 0 {
			t.Error("a UV=0 registration must be stored as not verified")
		}
		if err := finishFakeLogin(t, svc, weak, beginFakeLogin(t, svc), handle, noUVAssert, 2, ""); err != nil {
			t.Fatalf("UV=0 assertion on a UV=0 credential must be accepted under preferred: %v", err)
		}
		if credentialRow(t, svc.Queries, weak.credID).UserVerified != 0 {
			t.Error("a UV=0 assertion must not mark the credential verified")
		}
	})

	t.Run("required rejects UV=0", func(t *testing.T) {
		svc := newPasskeyServiceWithUV(t, "required")
		user := newPasskeyUser(t, svc.Queries, "iris")
		handle := userHandleFor(t, svc, user)
		authCtx := context.WithValue(context.Background(), userContextKey, user)

		weak := newFakeAuthenticator(t)
		_, err := svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{
			ResponseJson: weak.RegistrationResponseJSON(beginFakeRegistration(t, svc, user), nil, noUV, 1),
		}))
		if err == nil {
			t.Fatal("UV=0 registration must be rejected under required")
		}

		strong := newFakeAuthenticator(t)
		enrollFakePasskey(t, svc, user, strong, nil, regFlagsDevice, 1)
		if err := finishFakeLogin(t, svc, strong, beginFakeLogin(t, svc), handle, noUVAssert, 2, ""); err == nil {
			t.Fatal("UV=0 assertion must be rejected under required")
		}
		if err := finishFakeLogin(t, svc, strong, beginFakeLogin(t, svc), handle, assertFlagsNormal, 3, ""); err != nil {
			t.Fatalf("UV=1 assertion must be accepted under required: %v", err)
		}
	})
}

// TestBeginThrottleAuditsOncePerWindow: a flood past the limit writes a
// single audit row, so it cannot grow the 90-day auth event log.
func TestBeginThrottleAuditsOncePerWindow(t *testing.T) {
	dbConn, _, svc := newPasskeyService(t)
	for i := 0; i < BeginLimit; i++ {
		beginFakeLogin(t, svc)
	}
	for i := 0; i < 100; i++ {
		_, err := svc.BeginPasskeyLogin(context.Background(), connect.NewRequest(&v1.BeginPasskeyLoginRequest{}))
		if connect.CodeOf(err) != connect.CodeResourceExhausted {
			t.Fatalf("call %d over the limit: expected ResourceExhausted, got %v", i, err)
		}
	}
	var rows int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE event = 'rate_limited'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("expected exactly one rate_limited audit row for 100 refused calls, got %d", rows)
	}
}

// TestChallengeKindAndValueUnique pins the unique index that also serves the
// finish lookup.
func TestChallengeKindAndValueUnique(t *testing.T) {
	_, queries, _ := newPasskeyService(t)
	params := db.CreateWebAuthnChallengeParams{
		Challenge: []byte("dup"),
		Kind:      challengeKindLogin,
		ExpiresAt: time.Now().Add(time.Minute),
	}
	if _, err := queries.CreateWebAuthnChallenge(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.CreateWebAuthnChallenge(context.Background(), params); err == nil {
		t.Fatal("duplicate (kind, challenge) must be rejected")
	}
	params.Kind = challengeKindRegistration
	if _, err := queries.CreateWebAuthnChallenge(context.Background(), params); err != nil {
		t.Fatalf("same value under another kind is fine: %v", err)
	}
}

// raceAll runs fn n times in parallel, released together by a barrier, and
// returns the errors.
func raceAll(n int, fn func() error) []error {
	var ready, wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	ready.Add(n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			errs[i] = fn()
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()
	return errs
}

func countNil(errs []error) int {
	n := 0
	for _, err := range errs {
		if err == nil {
			n++
		}
	}
	return n
}

// TestConcurrentFinishConsumesChallengeOnce: many parallel finish calls with
// one challenge produce exactly one success, for login and registration.
func TestConcurrentFinishConsumesChallengeOnce(t *testing.T) {
	const parallel = 16

	t.Run("login", func(t *testing.T) {
		_, _, svc := newPasskeyService(t)
		user := newPasskeyUser(t, svc.Queries, "jules")
		auth := newFakeAuthenticator(t)
		enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)
		response := auth.AssertionResponseJSON(beginFakeLogin(t, svc), userHandleFor(t, svc, user), nil, assertFlagsSynced, 0, "")

		errs := raceAll(parallel, func() error { return finishFakeLoginRaw(t, svc, response) })
		if got := countNil(errs); got != 1 {
			t.Fatalf("expected exactly one successful login finish, got %d (%v)", got, errs)
		}
	})

	t.Run("registration", func(t *testing.T) {
		_, queries, svc := newPasskeyService(t)
		user := newPasskeyUser(t, svc.Queries, "kira")
		authCtx := context.WithValue(context.Background(), userContextKey, user)
		auth := newFakeAuthenticator(t)
		response := auth.RegistrationResponseJSON(beginFakeRegistration(t, svc, user), nil, regFlagsSynced, 0)

		errs := raceAll(parallel, func() error {
			_, err := svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{ResponseJson: response}))
			return err
		})
		if got := countNil(errs); got != 1 {
			t.Fatalf("expected exactly one successful registration finish, got %d (%v)", got, errs)
		}
		rows, err := queries.ListWebAuthnCredentialsByUser(context.Background(), user.ID)
		if err != nil || len(rows) != 1 {
			t.Errorf("expected exactly one stored credential, got %d (%v)", len(rows), err)
		}
	})
}

// TestRegistrationChallengeBoundToUser: another user cannot finish, or burn,
// someone else's registration challenge.
func TestRegistrationChallengeBoundToUser(t *testing.T) {
	_, _, svc := newPasskeyService(t)
	owner := newPasskeyUser(t, svc.Queries, "lena")
	other := newPasskeyUser(t, svc.Queries, "milo")
	auth := newFakeAuthenticator(t)
	response := auth.RegistrationResponseJSON(beginFakeRegistration(t, svc, owner), nil, regFlagsSynced, 0)

	otherCtx := context.WithValue(context.Background(), userContextKey, other)
	_, err := svc.FinishPasskeyRegistration(otherCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{ResponseJson: response}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
	// The failed attempt must not have consumed the owner's challenge.
	ownerCtx := context.WithValue(context.Background(), userContextKey, owner)
	if _, err := svc.FinishPasskeyRegistration(ownerCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{ResponseJson: response})); err != nil {
		t.Fatalf("owner must still be able to finish: %v", err)
	}
}

// TestDuplicateCredentialIsPrimaryKeyViolation: the database rejects a second
// row for one credential ID and isDuplicateKey recognises it, which is what
// maps a lost registration race to AlreadyExists.
func TestDuplicateCredentialIsPrimaryKeyViolation(t *testing.T) {
	_, queries, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "nora")
	auth := newFakeAuthenticator(t)
	enrollFakePasskey(t, svc, user, auth, nil, regFlagsSynced, 0)

	row := credentialRow(t, queries, auth.credID)
	_, err := queries.CreateWebAuthnCredential(context.Background(), db.CreateWebAuthnCredentialParams{
		CredentialID: row.CredentialID, UserID: user.ID, PublicKey: row.PublicKey,
		AttestationType: row.AttestationType, Aaguid: row.Aaguid, Name: "dup", CreatedAt: time.Now(),
	})
	if err == nil || !isDuplicateKey(err) {
		t.Fatalf("expected a primary key violation, got %v", err)
	}
	if isDuplicateKey(sql.ErrNoRows) {
		t.Error("unrelated errors must not be treated as duplicates")
	}
}

// TestConcurrentRegistrationsShareOneChallenge: parallel finishes with ONE
// challenge but a different authenticator each. Only the atomic consume can
// make exactly one win here, because the duplicate-credential check does not
// apply (every credential ID differs). Losers must see the challenge error.
func TestConcurrentRegistrationsShareOneChallenge(t *testing.T) {
	const parallel = 16
	_, queries, svc := newPasskeyService(t)
	user := newPasskeyUser(t, svc.Queries, "otto")
	authCtx := context.WithValue(context.Background(), userContextKey, user)
	challenge := beginFakeRegistration(t, svc, user)

	responses := make([]string, parallel)
	for i := range responses {
		responses[i] = newFakeAuthenticator(t).RegistrationResponseJSON(challenge, nil, regFlagsSynced, 0)
	}
	var next atomic.Int32
	errs := raceAll(parallel, func() error {
		_, err := svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{
			ResponseJson: responses[next.Add(1)-1],
		}))
		return err
	})
	if got := countNil(errs); got != 1 {
		t.Fatalf("expected exactly one success, got %d", got)
	}
	for _, err := range errs {
		if err == nil {
			continue
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "invalid or expired registration challenge") {
			t.Errorf("loser must get the challenge error, got %v", err)
		}
	}
	if rows, _ := queries.ListWebAuthnCredentialsByUser(context.Background(), user.ID); len(rows) != 1 {
		t.Errorf("expected one stored credential, got %d", len(rows))
	}
}

// TestConcurrentRegistrationsSameCredentialID: two finishes with different
// challenges but the same credential ID. One stores it; the other must get
// AlreadyExists (from the existence check or, if both pass it, from the
// primary key), never Internal.
func TestConcurrentRegistrationsSameCredentialID(t *testing.T) {
	for round := 0; round < 10; round++ {
		_, queries, svc := newPasskeyService(t)
		user := newPasskeyUser(t, svc.Queries, "pia")
		authCtx := context.WithValue(context.Background(), userContextKey, user)

		first := newFakeAuthenticator(t)
		second := newFakeAuthenticator(t)
		second.credID = first.credID
		responses := []string{
			first.RegistrationResponseJSON(beginFakeRegistration(t, svc, user), nil, regFlagsSynced, 0),
			second.RegistrationResponseJSON(beginFakeRegistration(t, svc, user), nil, regFlagsSynced, 0),
		}
		var next atomic.Int32
		errs := raceAll(2, func() error {
			_, err := svc.FinishPasskeyRegistration(authCtx, connect.NewRequest(&v1.FinishPasskeyRegistrationRequest{
				ResponseJson: responses[next.Add(1)-1],
			}))
			return err
		})
		if got := countNil(errs); got != 1 {
			t.Fatalf("round %d: expected exactly one success, got %d (%v)", round, got, errs)
		}
		for _, err := range errs {
			if err != nil && connect.CodeOf(err) != connect.CodeAlreadyExists {
				t.Errorf("round %d: loser must get AlreadyExists, got %v", round, err)
			}
		}
		if rows, _ := queries.ListWebAuthnCredentialsByUser(context.Background(), user.ID); len(rows) != 1 {
			t.Errorf("round %d: expected one stored credential, got %d", round, len(rows))
		}
	}
}

const eventLoginFailed = "login_failed"

// lastLoginFailedDetail returns the detail of the user's newest login_failed
// audit event.
func lastLoginFailedDetail(t *testing.T, svc *Service, user db.User) string {
	t.Helper()
	events, err := svc.Queries.ListAuthEventsByUser(context.Background(), db.ListAuthEventsByUserParams{
		UserID: sql.NullInt64{Int64: user.ID, Valid: true}, Limit: 20,
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, e := range events {
		if e.Event == eventLoginFailed {
			return e.Detail
		}
	}
	t.Fatal("no login_failed audit event")
	return ""
}
