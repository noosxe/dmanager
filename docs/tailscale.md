# Embedded Tailscale Node — Feature Design
Status: **APPROVED — v1 scope per §9 decisions; implemented by STORY-074–076**


---

## 1. Overview & Goals

dmanager is typically deployed on a single host behind a published port (`9283`). This feature embeds a
[Tailscale](https://tailscale.com) node directly into the dmanager process, so a running instance becomes
reachable from the operator's tailnet **without publishing ports, exposing the LAN, or running a sidecar
container**.

Implementation vehicle: [`tailscale.com/tsnet`](https://pkg.go.dev/tailscale.com/tsnet) — Tailscale's official
library for embedding a full userspace tailscaled (WireGuard, DERP, MagicDNS client) inside a Go binary.
tsnet is pure Go (works with the project's `CGO_ENABLED=0` static build), needs **no `/dev/net/tun`**, no
extra capabilities, and only requires outbound network access (HTTPS to the control plane / DERP, UDP for
direct connections when possible).

**Goals**

- Feature is fully inert unless an auth key is configured — zero behavior change for existing deployments.
- The same HTTP handler tree (ConnectRPC services + SPA + CORS middleware + auth interceptors) is served on
  the tailnet listener; **all existing authentication, sessions, RBAC and rate limiting still apply**.
- Persistent node identity in a state directory so restarts reuse the registered node instead of burning
  auth keys and churning tailnet node entries.
- Clean lifecycle integration: node starts after config validation, shuts down during graceful shutdown.
- Docker deployments work with the existing `dmanager-data` volume and no additional capabilities.

**Non-goals (v1)**

- Tailscale Funnel (public-internet exposure) — explicitly out of scope.
- Acting as an exit node or subnet router.
- Runtime/UI-driven configuration of the tailnet node (static config only, like `smtp`/`webauthn`).
- Multi-node / multi-tailnet support.

---

## 2. Configuration Design

### 2.1. Keys

| YAML key | Environment variable | Default | Description |
|---|---|---|---|
| `tailscale.auth_key` | `TAILSCALE_AUTHKEY` (see §9 Q1) | `""` | **Gate.** Feature enabled iff non-empty after trim. Tailscale auth key (`tskey-auth-…`). |
| `tailscale.hostname` | `TAILSCALE_HOSTNAME` | `"dmanager"` | MagicDNS hostname on the tailnet. |
| `tailscale.state_dir` | `TAILSCALE_STATE_DIR` | `<dirname(db_path)>/tailscale` (see §9 Q9) | Directory for persistent node state (created `0700` if missing). |
| `tailscale.port` | `TAILSCALE_PORT` | `80` (see §9 Q3) | Tailnet-side listen port for the HTTP server. |

Gating rule (single switch, no separate `enabled` flag — mirrors `smtp.enabled`-free design of the auth key
itself): **the node starts iff `tailscale.auth_key` resolves non-empty.**

### 2.2. Layering & precedence

Koanf layering is preserved (later wins):

1. Hardcoded defaults (`hostname=dmanager`, `port=80`, `state_dir` derived at load time).
2. YAML config file (`tailscale:` section).
3. Environment variables.

Environment mapping follows the existing pattern in `internal/config/config.go`:

- `DMANAGER_TAILSCALE_*` variables flow through the koanf `env.Provider` transform
  (`DMANAGER_TAILSCALE_AUTHKEY` → `tailscale.auth_key`, etc.), consistent with every other section.
- The **bare** names the feature is specified with (`TAILSCALE_AUTHKEY`, `TAILSCALE_HOSTNAME`,
  `TAILSCALE_STATE_DIR`) are handled in the manual post-processing step (same mechanism as
  `DMANAGER_REGISTRIES_*`), applied only when the prefixed variable did not set the key — so
  `DMANAGER_TAILSCALE_*` > `TAILSCALE_*` > YAML > defaults.
- The `AuthKey` field is set **explicitly** on the `tsnet.Server` so tsnet's implicit
  `TS_AUTHKEY` env fallback never activates implicitly (see §9 Q10).

### 2.3. Struct & validation

```go
// internal/config/config.go
type TailscaleConfig struct {
    AuthKey   string `koanf:"auth_key"`
    Hostname  string `koanf:"hostname"`
    StateDir  string `koanf:"state_dir"`
    Port      int    `koanf:"port"`
}
```

`Config.Validate()` additions (only when `auth_key != ""` — the whole section is inert while disabled, same
principle as SMTP):

- `hostname` must match Tailscale hostname rules: lowercase letters, digits, and hyphens
  (`^[a-z0-9][a-z0-9-]*$`, no trailing hyphen, ≤63 chars).
- `port` in 1–65535.
- `state_dir` must not be `/` and must not equal the directory holding the SQLite DB itself (it becomes a
  sibling by default, which is fine — only direct collision is rejected).

The resolved auth key is **never logged** and never returned in any RPC.

---

## 3. Architecture

### 3.1. New package: `internal/tailscale`

A thin, testable wrapper around `tsnet.Server`:

```go
package tailscale

// Node owns the embedded tailscale lifecycle for one dmanager process.
type Node struct {
    srv    *tsnet.Server
    logger *slog.Logger
}

func New(cfg config.TailscaleConfig, logger *slog.Logger) *Node

// Start brings the node Up (blocks until connected or ctx deadline),
// logs the tailnet identity (IPs, DNS name) at info level.
func (n *Node) Start(ctx context.Context) error

// Serve listens on the tailnet (":<port>") and serves h until the listener
// closes or the serve loop errors. Intended to run in its own goroutine.
func (n *Node) Serve(h http.Handler) error

// Close stops the node and releases all resources (idempotent).
func (n *Node) Close() error
```

Responsibilities:

- `MkdirAll(stateDir, 0700)` before `Up` (tsnet requires the directory to exist).
- Map `tsnet.Server.Logf` output into the structured logger at debug level under
  `logger.With("module", "tailscale")`.
- Construct `tsnet.Server{AuthKey, Hostname, Dir}` with explicit values only.
- After `Up`, fetch `Status()` once and log: `tailscale node online` with `ips`, `dns_name`
  (`<hostname>.<tailnet>.ts.net`), and `backend` (DERP vs direct). This is the operator's primary
  confirmation the feature works.

### 3.2. Integration point: `cmd/serve.go`

Inserted after the HTTP handler tree is fully built (after `handler := withCORS(...)`) and before
`ListenAndServe`, so the same `handler` value serves both listeners:

```go
if cfg.Tailscale.AuthKey != "" {
    node := tailscale.New(cfg.Tailscale, logger.With("module", "tailscale"))
    go func() {
        upCtx, cancel := context.WithTimeout(srvCtx, 60*time.Second)
        defer cancel()
        if err := node.Start(upCtx); err != nil {
            cmdLogger.Error("tailscale node failed to start; tailnet access disabled",
                "error", err) // degraded mode — see §9 Q2
            return
        }
        if err := node.Serve(handler); err != nil {
            cmdLogger.Error("tailscale serve loop exited", "error", err)
        }
    }()
    defer func() { _ = node.Close() }() // ordered after HTTP drain — see §3.3
}
```

The main `ListenAndServe` on `:9283` is unchanged. Startup ordering rationale: the tsnet `Up` handshake
(network + control plane round-trips) runs concurrently with normal boot so LAN availability never depends
on tailnet health.

### 3.3. Shutdown ordering

```
SIGTERM → srvCancel() (background jobs)  → server.Shutdown (HTTP drain, ≤10s)
        → node.Close() (tsnet listeners closed, node goes offline cleanly)
```

`node.Close()` runs after the HTTP drain via the existing `defer` stack (registered after the shutdown
goroutine's resources, so it fires last). Clean offline transition matters for fast reconnects and for
ephemeral node reaping if §9 Q5 is later enabled.

---

## 4. Serving & TLS Design (v1)

v1 serves **plain HTTP on the tailnet listener** (`tsnet.Server.Listen("tcp", ":"+port)`, default port 80).
Traffic is WireGuard-encrypted node-to-node; plain HTTP inside the tailnet is equivalent in trust level to
the current plain-HTTP LAN deployment.

Two documented consequences (both already true for plain-HTTP LAN deployments today — this feature does not
regress them):

1. **Passkeys (WebAuthn) require a secure context.** Browsers refuse `navigator.credentials` on
   `http://dmanager.<tailnet>.ts.net` (it is not `localhost`). Password login works fine over tailnet HTTP;
   passkey registration/use does not.
2. **`auth.secure_cookies: auto`** only sets the `Secure` cookie attribute when
   `X-Forwarded-Proto: https` is present (see `internal/auth/service.go`). Plain tailnet HTTP therefore
   yields non-Secure session cookies — same as today's HTTP LAN mode.

**HTTPS follow-up (§9 Q4):** `tsnet.Server.ListenTLS("tcp", ":443")` provisions a Let's Encrypt cert for
`<hostname>.<tailnet>.ts.net` (requires HTTPS certificates + MagicDNS enabled on the tailnet). Because tsnet
terminates TLS in-process, requests arrive without `X-Forwarded-Proto`; the design for that story includes a
small middleware on the TLS listener that sets `X-Forwarded-Proto: https` before the CORS/auth stack, plus
`webauthn.rp_id` / `webauthn.origins` guidance for the `*.ts.net` domain. Deferred out of v1 to keep the
first PR small; no v1 decision blocks it.

---

## 5. Security Considerations

Additions for `docs/security.md` when implemented:

- **No auth bypass.** The tailnet listener serves the identical handler tree: session auth interceptor,
  RBAC (admin/viewer), rate limiting, audit logging all enforced. Tailnet identity is a **network-layer**
  restriction (who can reach the port), not an application-layer one.
- **Auth key handling.** The auth key is a credential equivalent to "invite a node to the tailnet". It is
  read from env/YAML, held in memory, never logged, never exposed via RPC or the settings service. YAML
  storage is equally sensitive to `registries[].password` (already accepted precedent) — env vars
  recommended in compose docs.
- **State directory.** Contains the node's WireGuard private key and machine identity. Created `0700`;
  documented as host-sensitive, same class as the SQLite DB. In Docker it lives under the existing
  `dmanager-data` volume.
- **Key lifecycle.** Auth keys are only consumed on first registration. Once state exists in
  `state_dir`, restarts authenticate from persisted state and the key is ignored — an expired key plus
  existing state still boots. The deployment doc will call out that operators may remove the env var after
  first successful start (and that state-dir loss re-triggers registration, requiring a fresh key).
- **Funnel.** Explicitly not wired. No code path can expose the listener to the public internet.
- **Supply chain.** Adds `tailscale.com` (BSD-3-Clause) as a direct dependency — a large module.
  Expected impact: binary size +~15–25 MB, idle RSS +~20–40 MB when enabled; disabled deployments still pay
  the binary-size cost but none of the runtime cost. Pinned via go.sum as usual.
- **Outbound network.** Control plane + DERP over 443/TCP, direct paths over UDP when traversable. No
  inbound ports, no `NET_ADMIN`, no TUN device — verified compatible with the existing unprivileged
  alpine runtime image.

---

## 6. Container / Deployment Impact

- `Dockerfile`: **no changes** (tsnet compiles into the existing static binary; no extra packages).
- `docker-compose.yml` docs example:

  ```yaml
  environment:
    - TAILSCALE_AUTHKEY=tskey-auth-xxxxxxxxxxxx-xxxxxxxxxxxxx
    # optional:
    # - TAILSCALE_HOSTNAME=dmanager
    # - TAILSCALE_STATE_DIR=/var/lib/dmanager/tailscale
  ```

  The default state dir resolves under `/var/lib/dmanager/…` (the existing volume) automatically because it
  derives from `db_path`.
- `rootfs/etc/dmanager/config.yaml`: add an empty `tailscale: {}` section for discoverability.
- `docs/deployment.md`: new config table rows + a "Tailscale access" section (key lifecycle notes from §5).

---

## 7. Testing Strategy

- **Config unit tests** (`internal/config/config_test.go`): gating off by default; bare-name env mapping;
  `DMANAGER_`-prefixed precedence over bare names; hostname/port validation matrix; inert-section rule
  (invalid hostname with empty auth key does not error).
- **Node unit tests** (`internal/tailscale`): the package is kept thin; `tsnet.Server` construction fields
  (authkey/hostname/dir) asserted via a small seam — `New` returns the node with its `tsnet.Server`
  configured but not started, making assertions trivial without network. Serve-loop error propagation tested
  with a stub listener.
- **No CI integration against a real tailnet** (needs account secrets). Manual validation checklist in the
  story: ephemeral auth key → boot → confirm `tailscale status` shows the node → HTTP reachability at
  `http://<hostname>:<port>` → graceful shutdown takes node offline.

---

## 8. Implementation Stories (proposed)

Following the repo's bite-sized story convention (next free number: 074):

- **STORY-074 — Tailscale config plumbing (DONE).** `TailscaleConfig` struct, koanf defaults, env transform
  (`DMANAGER_TAILSCALE_*` + bare-name post-processing), validation, unit tests, `docs/deployment.md` table
  rows.
- **STORY-075 — Embedded node lifecycle (DONE).** `internal/tailscale` package (`New`/`Start`/`Serve`/`Close`),
  serve.go wiring, degraded-mode logging, shutdown ordering, unit tests, `go.mod` addition.
- **STORY-076 — Deployment & security docs (DONE).** Compose example, rootfs config section, security doc
  additions (§5), README feature bullet, manual validation checklist.
- **STORY-077 — Tailnet HTTPS (DONE).** `ListenTLS` listener (`tailscale.https_enabled`, opt-in,
  plain HTTP keeps running), `X-Forwarded-Proto` middleware (forced server-side, `Set` not `Add`),
  webauthn origins guidance in `docs/deployment.md` §2.3.
- **STORY-078 (follow-up, gated on §9 Q8) — Node status in Admin API/UI.** Surface IPs, DNS name,
  connection state via AdminService.

---

## 9. Decisions (resolved)

All open questions from the initial draft have been resolved; recommendations adopted:

| # | Question | Decision |
|---|---|---|
| Q1 | Env var naming | `DMANAGER_TAILSCALE_*` canonical; bare `TAILSCALE_AUTHKEY` / `TAILSCALE_HOSTNAME` / `TAILSCALE_STATE_DIR` / `TAILSCALE_PORT` accepted as aliases. Prefixed wins over bare, bare wins over YAML. |
| Q2 | Startup failure policy | Degraded mode: single attempt, error log, LAN-only operation continues. No retry loop in v1. |
| Q3 | Tailnet listen port | Default `80`, configurable via `tailscale.port`. |
| Q4 | HTTPS on tailnet | Shipped as STORY-077: opt-in `tailscale.https_enabled` serves HTTPS on 443 next to plain HTTP; passkeys work over the `https://*.ts.net` origin. |
| Q5 | Ephemeral node mode | Not supported in v1. |
| Q6 | Headscale support | Defer; no `control_url` in v1. |
| Q7 | Tailnet-only hardening | Defer; operators unpublish the Docker port if desired. |
| Q8 | Node status visibility | Startup logs only in v1; UI/Admin surface is STORY-078. |
| Q9 | Default state dir | Derived from `db_path` dirname at config load time (`<dirname>/tailscale`). |
| Q10 | `TS_AUTHKEY` alias | Not accepted; `AuthKey` set explicitly so tsnet never reads env implicitly. |
| Q11 | Key-expiry warning UX | Folded into STORY-078. |
