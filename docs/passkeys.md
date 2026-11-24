# Passkeys: requirements and client notes

What dmanager requires from a passkey and its client, what the tests pin, and
what has not been tested against real clients. For the design see
[authentication.md](authentication.md) (§4); for the security model see
[security.md](security.md) (§2.5); for the settings see
[deployment.md](deployment.md).

## What dmanager requires

| Area | Requirement |
| :--- | :--- |
| Configuration | `webauthn.rp_id` **and** `webauthn.origins` must both be set. If either is missing, passkeys are disabled (the server logs it and the UI hides sign-in). There is no default derived from the request. |
| Page | A secure context: HTTPS, or `localhost`. Browsers do not expose WebAuthn on plain HTTP over a LAN address; the UI then disables the passkey buttons and says why. The page origin must be listed in `webauthn.origins` and match `rp_id`. |
| Discoverable credentials | Sign-in has no username step, so only discoverable (resident) credentials work. Registration asks for `residentKey: required`. If the client reports `credProps.rk = false`, registration is rejected. If the client omits `credProps`, the credential is accepted: the authenticator was asked for a resident key and nothing says otherwise. A non-discoverable credential that slips through would register but never sign in. |
| User verification | `webauthn.require_user_verification`: `preferred` (default), `required` or `discouraged`. Under `required`, registration and sign-in assertions without the UV flag are rejected. The registration's UV flag is stored per passkey and shown in Settings. |
| Attestation | None requested; the authenticator model comes from the AAGUID and is only a display label. |
| User handle | Opaque per-user value: 32 random bytes for users that had no passkeys when the handle column was introduced (chosen when they first need it), the legacy 8-byte row-ID value for users that already had passkeys. On sign-in the WebAuthn library checks the returned handle against the credential owner's stored handle. |
| Counter and flags | A signature counter that does not increase (including N to 0) logs the user in and flags the passkey with a warning badge. Counters that stay at 0 are normal and never flagged. The backup-eligible flag must not change after registration; backup state may change. |
| Challenges | Single use, 120 seconds, stored server side. |
| Rate limits | Failed sign-ins are limited per source IP (shared by password and passkey) and per username and method. `BeginPasskeyLogin` has its own per-IP limit. Behind a reverse proxy, set `server.trusted_proxy` (and `server.trusted_proxy_hops` if more than one proxy is in front). |

## What the tests pin

All of these run against a synthetic authenticator that speaks the real wire
format (`internal/auth/webauthn_fake_test.go`); none uses captures from a real
browser or device.

- Replay, expiry, unknown and duplicate credentials, wrong origin and RP ID hash.
- Backup-eligible and backup-state lifecycle, counter policy (0 to 0, increasing, regression, N to 0), regression not counting as a failed login.
- `credProps` handling and unsolicited extension outputs.
- User verification under `preferred` and `required`.
- Exclusion of existing credentials, challenge binding to the user, concurrent finish and registration.
- The begin throttle and its memory and audit caps.
- User handle rules: random for new users, legacy kept, foreign or unknown handle rejected.
- One generic sign-in failure message, with the reason in the audit event.

## Clients

No row below is backed by a test against the real client. They describe what
is expected from the specification and from how dmanager is configured; treat
them as untested until someone reports otherwise.

| Client | Expected | Status |
| :--- | :--- | :--- |
| Platform passkeys (Touch ID, Windows Hello, Android) | Discoverable, user verified, usually synced (counter 0) | Untested |
| Password managers as passkey providers | Discoverable, synced, UV by unlock | Untested |
| FIDO2 security keys with a PIN | Discoverable if the key has resident-key slots; counter increases | Untested |
| Older security keys without resident-key support | Cannot create a discoverable credential; registration fails or is rejected | Untested |
| Cross-device (phone via QR code) | Works when the browser supports hybrid transport | Untested |
| Browsers that omit `credProps` | Accepted without proof of discoverability | Untested |

Real client captures are tracked in [#306](https://github.com/noosxe/dmanager/issues/306).

## Reporting a client problem

Open an issue with:

- Browser and version, operating system, and the authenticator (platform, password manager name, or security key model).
- Whether the page is HTTPS or localhost, and the value of `webauthn.require_user_verification`. Describe the origin and RP ID in general terms rather than pasting real hostnames.
- The step that failed (registration or sign-in) and the message shown.
- The `login_failed` or registration error in the audit log for that attempt: it carries the specific reason that the browser is not shown.
- The browser console error name (for example `NotAllowedError`, `InvalidStateError`, `NotSupportedError`).

Do not include credentials, session cookies, or the raw WebAuthn payloads.
