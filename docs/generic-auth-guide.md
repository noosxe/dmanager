# Generic Authentication Implementation Guide

A stack-agnostic guide for AI agents implementing authentication in any
web application (single-tenant or small-team, server-rendered or SPA/API).
It distills a production-grade reference implementation into requirements,
invariants, and decision rules. Adapt naming and mechanics to the target
stack; do **not** adapt away the security invariants — they are marked
`MUST` / `MUST NOT` and are the point of the guide.

---

## 0. Architecture decisions (make these first)

| Decision | Recommendation | Rationale |
| :--- | :--- | :--- |
| Token model | **Server-side sessions in a database** (opaque random token in cookie). No JWTs, no refresh tokens | Revocability matters more than statelessness for apps with a database. Stateless tokens can't be revoked before expiry |
| Transport | **HttpOnly cookie**, not `Authorization` header / localStorage | Immune to XSS token theft; sent automatically by the browser |
| CSRF defense | `SameSite=Lax` cookie + POST-based API | Blocks cross-site POSTs without CSRF-token ceremony. If you serve GETs that mutate state, add per-request CSRF tokens instead |
| Password 2FA | **Passkeys (WebAuthn)** if anything; skip TOTP | Passkeys are phishing-resistant and supersede shared-secret 2FA; supporting both doubles ceremony surface |
| External IdP (OAuth/OIDC) | Out of scope; add only if there's a concrete requirement | Each provider adds redirect-uri, state, and secret-handling surface |

**Session model: two clocks, not one expiry.**

| Clock | Default | "Remember me" | Purpose |
| :--- | :--- | :--- | :--- |
| Idle (sliding) timeout | 7 days | 30 days | Kills abandoned sessions; refreshed by activity |
| Absolute cap | 30 days | 90 days | Forces periodic re-auth; bounds stolen-cookie lifetime |

Both values MUST be configurable. The absolute cap MUST be fixed at session
creation and never extended. The idle deadline slides on activity (§4.3).

---

## 1. Identity & first-run bootstrap

1. `users` table: `username` (unique), `password_hash`, `role`.
2. A public **server-status** endpoint reports whether any user exists
   (`needs_setup`) and whether passkey login is configured. No auth required.
3. A **setup** endpoint creates the first admin. It MUST:
   - Fail with `409/FailedPrecondition` if any user already exists.
   - Validate the password against the policy (§3.2).
   - Hash with the configured cost (§3.1).
   - Assign the admin role.
   - Write an audit event (`setup_admin`, with source IP).
4. The frontend setup flow calls setup, then immediately logs in with the
   new credentials.

---

## 2. Password hashing

1. Hash with **bcrypt** (or argon2id). Cost MUST be configurable, default
   bcrypt cost 12. Validated range 4–31 (bcrypt).
2. Existing hashes are never re-hashed on login; comparison works against
   whatever cost the stored hash carries. New/changed passwords use the
   configured cost.
3. **Password policy** at every set path (NIST SP 800-63B style):
   - Minimum length 12. No composition rules, no forced rotation.
   - Optional, off by default: breached-password check via k-anonymity
     range API (send only the first 5 hex chars of a hash — never the full
     hash — and only if the feature is explicitly enabled, since it phones out).

---

## 3. Password login flow

For the login endpoint:

1. **Extract client IP.** If a reverse proxy is configured/trusted, take the
   first entry of `X-Forwarded-For`; otherwise use the socket remote address.
   Never trust `X-Forwarded-For` when not behind a trusted proxy.
2. **Rate-limit check** (username + IP key) → `429/ResourceExhausted` with a
   human-readable "try again in N seconds" message (§7).
3. Look up the user.
   - **Unknown username:** run the hash comparison against a pre-generated
     **dummy hash** (generated once at startup with the configured cost) and
     discard the result. This equalizes timing between "no such user" and
     "wrong password". MUST NOT return early before a hash comparison runs.
   - Lookup failure → record rate-limit failure + audit `login_failed`
     ("invalid credentials" — never reveal which part was wrong).
4. Compare hash. Failure → same as above.
5. Success → reset the rate-limit counter for that username, issue a session
   (§4.1), set the cookie (§4.4), audit `login_success` with method and IP.
6. Response returns username + role (the client needs the role for UI
   gating; the server still enforces it — §6).

---

## 4. Session management

### 4.1 Issuance (shared by every login method)

1. Generate a **32-byte cryptographically random** token; hex- or base64-
   encode it. Never use a predictable ID (timestamp, counter, UUIDv4 alone
   is acceptable only if you also store 128 bits of entropy — prefer raw
   `crypto/rand`).
2. Insert a row: token (or its hash), user ID, user-agent, `expires_at`
   (= now + idle timeout), `last_seen_at` = now, `absolute_expires_at`
   (= now + absolute cap). "Remember me" selects the longer tier.
3. Return a `Set-Cookie` header (§4.4).
4. All login methods (password, passkey, future methods) MUST converge on
   this one issuance function.

### 4.2 Validation (per request)

On a request bearing a session cookie:

1. Missing/unknown token → `Unauthenticated`.
2. `now > absolute_expires_at` → delete the row, `Unauthenticated`.
3. `now > expires_at` (idle deadline) → delete the row, `Unauthenticated`.
4. Load the user (user deleted → `Unauthenticated`).
5. Inject user + session ID into the request context for handlers.

### 4.3 Sliding renewal

Inside validation, after the session is known valid:

```
if now > expires_at - idle_timeout/2:          # only slide halfway through
    new_idle = now + idle_timeout
    if new_idle > absolute_expires_at:          # clamp — never extend the cap
        new_idle = absolute_expires_at
    update row(expires_at = new_idle, last_seen_at = now)
```

Sliding only past the half-way point avoids a DB write on every request.

### 4.4 Cookie specification

```
session_id=<token>; Path=/; HttpOnly; SameSite=Lax; Max-Age=<idle seconds>[; Secure]
```

- `HttpOnly` — MUST (JS must never read the credential).
- `SameSite=Lax` — MUST (the CSRF defense; fine for POST-based APIs).
- `Max-Age` = the idle deadline — MUST, so browser expiry tracks the server
  row (a session-scoped cookie desyncs from the DB).
- `Secure` — config-driven, three modes: `auto` (set only when the request
  arrived as HTTPS, detected via `X-Forwarded-Proto: https` behind a proxy),
  `always`, `never` (plain-HTTP LAN installs). Default `auto`.
- Do NOT use the `__Host-` prefix if plain-HTTP installs are supported (it
  requires `Secure` unconditionally).
- Logout sets the same cookie with empty value and `Max-Age=0`.

### 4.5 Revocation & visibility

- List my sessions: show device label (parsed from user-agent), created,
  last-seen, both expiries, and an `is_current` flag. Scope queries to the
  calling user's rows — a user MUST NOT see or revoke other users' sessions.
- Revoke one / revoke all others (keeping the current session).
- Server-side deletion on logout; row deletion is the only logout mechanism
  (no client-side "forgetting" without server invalidation).

---

## 5. Passkeys / WebAuthn (optional module)

Include only if enabled by configuration; otherwise endpoints return
`FailedPrecondition` and the status endpoint reports passkeys disabled so
the UI hides the button.

### 5.1 Configuration

- `rp_id` (registrable domain) and `origins` (every scheme+host+port users
  reach the app through) MUST be pinned in config. MUST NOT be derived from
  the request `Origin` header (an attacker with DNS control could register
  credentials for their own origin).
- Mismatched RP ID/origin is the most common failure; surface effective
  values in the UI for debugging.
- `require_user_verification`: `preferred` (default) | `required` | `discouraged`.

### 5.2 Storage

- `credentials` table: credential ID (raw), user ID, public key (COSE),
  attestation type, transports, AAGUID (device model), sign count,
  clone-warning flag, backup-eligible/backup-state flags, user label,
  created/last-used.
- `challenges` table: challenge bytes, kind (`registration`|`login`),
  user ID (nullable), `expires_at`, `consumed` flag. Challenges live
  server-side (not in cookies) so the ceremony is server-authoritative and
  works across tabs.

### 5.3 Registration (authenticated)

1. `Begin`: build options with `resident_key: preferred`,
   `user_verification: <config>`, `exclude_credentials` = the user's
   existing credentials (prevents silent double registration), prefer no
   attestation conveyance. Store challenge (kind=registration, bound to the
   user, TTL 120s).
2. `Finish`: parse the attestation, verify the challenge is unconsumed,
   unexpired, and bound to this user; mark it consumed (single-use);
   verify the ceremony (origin, RP hash, user presence); reject duplicate
   credential IDs; store the credential with sign count and backup flags.
   Default label derives from the AAGUID; user can rename later.

### 5.4 Login (usernameless, discoverable)

1. `Begin`: rate-limit check; issue a **discoverable** login (empty
   allow-credentials) — the browser picker resolves the user. Store
   challenge (kind=login, no user binding, TTL 120s).
2. `Finish`: parse the assertion; verify the challenge (unconsumed,
   unexpired); resolve the credential by ID and the user from it;
   rate-limit check on the resolved username; verify the ceremony (origin,
   RP hash, signature vs stored public key).
3. **Clone detection:** if the new sign count ≤ stored count and both are
   nonzero → set `clone_warning`, record a failure, reject.
4. Success: update sign count / last-used / backup state, reset the
   rate-limit counter, and issue a session through the **same** issuance
   path as password login.
5. Every failure path (bad JSON, bad challenge, unknown credential,
   failed validation) records a rate-limit failure.

### 5.5 Lockout guardrail

Deleting the last passkey MUST fail if the user would be left with zero
login methods (no passkeys and no password set) → `FailedPrecondition`
with an explanatory message.

---

## 6. Request enforcement & RBAC

A single middleware/interceptor wraps **every** endpoint (unary and
streaming/websocket alike). It owns authentication and authorization;
handlers never re-check (defense-in-depth exceptions allowed but
discouraged).

### 6.1 Role matrix

Maintain a static map: **procedure → role**, with exactly three buckets:

- `unauthenticated` — public allowlist: status, setup, login (all methods).
- `viewer` — any authenticated user: reads, own-profile actions, own
  sessions/passkeys/auth events.
- `admin` — requires `role == "admin"`: all mutations (start/stop/delete/
  update/prune/settings).

Rules:

1. **Fail closed:** an endpoint absent from the map is rejected with an
   internal error. New RPCs/routes cannot ship unclassified.
2. A test MUST assert every generated/declared endpoint string is covered
   by exactly one bucket.
3. Admin-check failure → `403/PermissionDenied`, logged with the username,
   and audit-recorded as a denied entry.
4. Unauthenticated-bucket endpoints still upgrade the request context with
   the user if a valid cookie is present (so login pages can personalize).

### 6.2 Per-request logging

The interceptor logs procedure, user (or "unauthenticated"), duration, and
error for every call — one log point, not per-handler.

---

## 7. Brute-force protection

In-memory rate limiter (acceptable for single-process deployments; use a
shared store only if multi-instance):

- **Key:** username + client IP.
- **Window:** sliding 15 minutes.
- **Threshold:** 5 failures in the window → lockout.
- **Backoff:** exponential — 1, 2, 4, … minutes, capped at 15.
- **Success resets** the failure counter for that username.
- Locked responses: `429` + "try again in N seconds" + audit
  `rate_limited`.
- Feeds from: password failures, passkey-finish failures, passkey-begin
  flooding.
- Counter reset on restart is an accepted, documented limitation (the audit
  log retains the history).

---

## 8. Audit trail

A dedicated `auth_events` table, written at **every auth decision point**
by the service and the interceptor:

- Event types: `login_success`, `login_failed`, `logout`, `passkey_added`,
  `passkey_removed`, `rate_limited`, `session_revoked`, `setup_admin`.
- Fields: user ID (nullable for pre-auth failures), attempted username,
  event type, detail (method, coarse reason, IP), timestamp.
- MUST NOT contain credentials, tokens, or challenge bytes.
- Read endpoint: admins see all events; viewers see their own. Paginated,
  newest first, capped page size.
- Retention: rows older than 90 days purged.

Separate (optional) **action audit** for admin mutations: actor, role,
action, resource type/ID, outcome (success/failure/denied), detail.

---

## 9. Background maintenance

One periodic job (hourly) purging:

1. Sessions past either clock,
2. Expired WebAuthn challenges,
3. Auth events past retention.

Failures log a warning; the job never crashes the process.

---

## 10. Frontend contract

- An auth context/provider exposes: current user, `isAuthenticated`,
  `isLoading`, `needsSetup`, `passkeyLoginEnabled`, plus
  login/loginWithPasskey/setupAdmin/logout/checkAuth.
- On mount: fetch server status (setup? passkeys? version), then call a
  `GetMe`-style endpoint to restore the session from the cookie. Treat
  `Unauthenticated` as "logged out", not as an error.
- **The cookie is the only credential.** localStorage may cache display
  data (username/role) but MUST NOT hold a token or anything the server
  trusts.
- Passkey client: marshal ceremony options/responses with a WebAuthn JSON
  helper; pass them through opaquely — the client does not interpret
  challenge bytes.
- Any auth failure state clears the local cache and routes to login.

---

## 11. Configuration reference

| Key | Default | Notes |
| :--- | :--- | :--- |
| `session_idle_timeout` | 168h | Sliding window |
| `session_absolute_timeout` | 720h | ≥ idle timeout (validate) |
| `remember_me_idle_timeout` | 720h | |
| `remember_me_absolute_timeout` | 2160h | ≥ remember idle (validate) |
| `secure_cookies` | auto | auto / always / never |
| `bcrypt_cost` | 12 | 4–31 |
| `trusted_proxy` | false | Enables X-Forwarded-For trust |
| `webauthn.rp_id` | — | Required for passkeys |
| `webauthn.origins` | — | Required for passkeys |
| `webauthn.require_user_verification` | preferred | |
| `breached_password_check` | false | Phones out; opt-in |

All durations accept standard duration strings; validate cross-constraints
(absolute ≥ idle) at load.

---

## 12. Verification checklist (run before declaring done)

**Tests**
- [ ] Sliding renewal boundary math: no slide before half the idle window;
      clamp at the absolute cap.
- [ ] Expired-by-either-clock session → deleted + `Unauthenticated`.
- [ ] Unknown username and wrong password take (statistically) equal time.
- [ ] Rate limiter transitions: 5th failure locks; backoff doubles; success
      resets; lock expiry unlocks.
- [ ] Role-matrix coverage: every endpoint classified exactly once;
      unclassified endpoint rejected.
- [ ] Viewer hitting an admin endpoint → `PermissionDenied` + audit entry.
- [ ] Challenge single-use: replayed registration/login challenge rejected.
- [ ] Wrong origin / wrong RP / bad signature rejected.
- [ ] Sign-count regression → clone warning + rejection.
- [ ] Deleting the last login method → `FailedPrecondition`.
- [ ] Full flow integration: login → slide → revoke → subsequent request
      fails.
- [ ] Frontend: login form modes, security tab states, optimistic revoke.

**Review**
- [ ] No credential, token, or challenge bytes in any log line.
- [ ] Error messages never reveal whether the username or password was wrong.
- [ ] Every new endpoint is in the role matrix (fail-closed proof).
- [ ] Cookie has HttpOnly + SameSite=Lax + Max-Age; Secure per config.
- [ ] Session issuance is one shared code path for all login methods.
- [ ] All auth decision points write audit events.

---

## 13. Anti-patterns (do not introduce)

- JWTs/refresh tokens for a database-backed app — loses revocation.
- Storing tokens in localStorage / `Authorization` headers for browser clients.
- `SameSite=None` without a concrete cross-site requirement.
- Deriving WebAuthn RP ID/origins from request headers.
- Returning early on unknown usernames (timing oracle).
- Distinct error messages for "no such user" vs "wrong password".
- Per-handler role checks instead of one enforcement point.
- New endpoints without role-matrix entries (silently public or blocked).
- Trusting `X-Forwarded-For` without a trusted-proxy config.
- Password composition rules or forced rotation (NIST 800-63B).
- TOTP alongside passkeys (doubled ceremony surface, weaker properties).
