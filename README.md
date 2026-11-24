# dmanager

A self-contained Docker Container Manager web application that discovers local containers, allows start/stop operations, and conducts scheduled image update checks.

## Disclosure

Written by AI, tested and used by humans.

## Features

- **Container Discovery** — automatically discovers and lists all Docker containers on the host
- **Container Management** — start, stop, and upgrade containers from a modern web UI
- **Image Update Checks** — scheduled background checks for newer image versions across registries
- **Auto-Update** — optional per-container automatic re-deployment preserving all configuration
- **Private Registry Support** — authenticate against private registries (GHCR, Docker Hub, etc.)
- **Gotify Notifications** — receive push notifications for update events and failures
- **Embedded Tailscale Node** — optional built-in tailnet access (`TAILSCALE_ENABLED=true` plus `TAILSCALE_AUTHKEY`): manage dmanager from anywhere in your tailnet with no published ports and no sidecar container; optional tailnet HTTPS mode (`:443` instead of HTTP, Tailscale-issued certificates — passkeys included) served over HTTP/2, with node status visible in the Admin UI
- **System Logs** — browse structured backend logs directly in the UI
- **Authentication & Passkeys** — secure session-based authentication with role-based access control (admin / viewer), discoverable WebAuthn passkeys (Touch ID, Windows Hello, Face ID, hardware security keys), NIST password policy, login rate limiting (separate lockout counters for password and passkey sign-in), session management, and auth audit logging
- **Audit Logs** — every mutation and system action is recorded to an audit trail with days-based retention, manageable from the Admin UI
- **System Email** — optional SMTP delivery for system notifications via a relay (off and fully inert unless configured; verify with `dmanager smtp test`)
---

## Quick Start with Docker Compose

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) (v20.10+)
- [Docker Compose](https://docs.docker.com/compose/install/) (v2+)

### 1. Create a `docker-compose.yml`

```yaml
services:
  dmanager:
    image: ghcr.io/noosxe/dmanager:latest
    container_name: dmanager
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:9283/"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 30s
    ports:
      - "${DMANAGER_PORT:-9283}:9283"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - dmanager-data:/var/lib/dmanager
      # Optional: mount a custom config file
      # - ./config.yaml:/etc/dmanager/config.yaml:ro
    environment:
      # Values interpolate from the environment or a .env file (see .env.example)
      - DMANAGER_SERVER_PORT=9283
      - DMANAGER_SCHEDULER_INTERVAL_MINUTES=${DMANAGER_SCHEDULER_INTERVAL_MINUTES:-60}
      # Passkeys work on http://localhost:9283 out of the box; override both
      # for a real domain (a stable hostname — browsers refuse WebAuthn on
      # IP origins):
      - DMANAGER_WEBAUTHN_RP_ID=${DMANAGER_WEBAUTHN_RP_ID:-localhost}
      - DMANAGER_WEBAUTHN_ORIGINS=${DMANAGER_WEBAUTHN_ORIGINS:-http://localhost:9283}

volumes:
  dmanager-data:
```

Optional: create a `.env` file next to `docker-compose.yml` to override any of the settings above — [`.env.example`](.env.example) in the repository is a documented template. Compose reads it automatically, and every variable has a safe fallback, so the stack also runs without one.

### 2. Start the application

```bash
docker compose up -d
```

### 3. Open the web UI

Navigate to [http://localhost:9283](http://localhost:9283) in your browser.

On first launch you will be prompted to create an administrator account.

With the embedded Tailscale node enabled, the UI is also reachable from your tailnet at `http://<hostname>.<tailnet>.ts.net:<port>` (HTTP mode, default) or `https://<hostname>.<tailnet>.ts.net` (HTTPS mode) — no published ports required.

### Stopping the application

```bash
docker compose down
```

> [!IMPORTANT]
> The Docker socket (`/var/run/docker.sock`) **must** be mounted into the container so dmanager can discover and manage containers on the host.

---

## Configuration

dmanager loads configuration from multiple sources, merged in the following order of precedence (later overrides earlier):

1. **Built-in defaults** (hardcoded)
2. **YAML configuration file**
3. **Environment variables** (prefixed with `DMANAGER_`)
4. **CLI flags** (when running the binary directly)

### Configuration file

When running inside Docker the config file is read from `/etc/dmanager/config.yaml`. To customise it, create a `config.yaml` on the host and bind-mount it:

```yaml
# docker-compose.yml (excerpt)
volumes:
  - ./config.yaml:/etc/dmanager/config.yaml:ro
```

Without a custom mount the built-in defaults are used.

When running the binary directly the following paths are searched in order:

1. Path specified via `--config` / `-c` flag
2. `/etc/dmanager/config.yaml`
3. `./config.yaml` (current working directory)

#### Example `config.yaml`

```yaml
server:
  port: "9283"
  db_path: "/var/lib/dmanager/dmanager.db"
  allowed_origins: []
  trusted_proxy: false
  trusted_proxy_hops: 1

docker:
  host: "unix:///var/run/docker.sock"
  # Extra "key=value" (or bare "key") label selectors for containers owned by
  # another tool; see "Externally managed containers".
  external_label_selectors: []

scheduler:
  interval_minutes: 60
  notify_external_updates: false

auth:
  session_idle_timeout: 168h
  session_absolute_timeout: 720h
  remember_me_idle_timeout: 720h
  remember_me_absolute_timeout: 2160h
  secure_cookies: auto
  bcrypt_cost: 12
  breached_password_check: false

webauthn:
  rp_id: "dmanager.example.com"
  origins:
    - "https://dmanager.example.com"
  require_user_verification: preferred

# Embedded Tailscale node (optional — off unless enabled is true; see docs/tailscale.md)
# tailscale:
#   enabled: false               # the only switch; true requires auth_key (startup fails otherwise)
#   auth_key: "tskey-auth-..."   # tagged, reusable key; only needed on first start
#   hostname: "dmanager"         # node name in the tailnet
#   port: 80                     # HTTP-mode tailnet port; ignored when https_enabled is true
#   https_enabled: false         # false: HTTP on port; true: HTTPS only, on tailnet :443 (no HTTP)
#   state_dir: ""                # defaults to <db_dir>/tailscale

# System email (optional). Mail goes through an SMTP relay for system
# purposes only — there is no user-facing send feature.
smtp:
  enabled: false
  # host: "postfix.relay.internal"
  # port: "25"
  # username: ""
  # password: ""
  # from_email: "noreply@example.com"
  # from_name: "dmanager"
  # tls_mode: "none"          # none | starttls | tls
  # timeout_seconds: 15

registries: []
```

### Configuration file reference

#### `server` — HTTP server settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server.port` | `string` | `"9283"` | Port the HTTP server listens on. |
| `server.listen_address` | `string` | `""` | IP address or host name the plain HTTP listener binds to (no port). Empty binds all interfaces. |
| `server.http.enabled` | `bool` | `true` | Serve the plain HTTP listener. May be `false` only when `tailscale.enabled` is `true`; see [Tailnet-only deployment](docs/tailscale.md#10-tailnet-only-deployment-no-plain-http-listener). |
| `server.db_path` | `string` | `"dmanager.db"` | Path to the SQLite database file. Inside Docker this should be on a persistent volume (e.g. `/var/lib/dmanager/dmanager.db`). |
| `server.allowed_origins` | `string[]` | `[]` | List of allowed CORS origins. Leave empty to disallow cross-origin requests. Use `["*"]` to allow all origins. |
| `server.trusted_proxy` | `bool` | `false` | When `true`, takes the client IP for rate limiting and the audit log from `X-Forwarded-For`, read from the right (see `trusted_proxy_hops`). Set it behind a reverse proxy; otherwise all clients share the proxy's address and login rate limits apply to them together. The proxy must append to `X-Forwarded-For` rather than replace it (nginx `$proxy_add_x_forwarded_for`; Caddy and Traefik do by default). |
| `server.trusted_proxy_hops` | `int` | `1` | Number of reverse proxies in front of dmanager that append to `X-Forwarded-For`. The client IP is the entry that many positions from the right, so `1` uses the rightmost entry (the address the nearest proxy saw) and `2` suits Cloudflare in front of nginx. Entries further left are client-controlled and ignored. Must be at least `1`; only used when `trusted_proxy` is `true`. |

#### `docker` — Docker daemon connection

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `docker.host` | `string` | `"unix:///var/run/docker.sock"` | Docker daemon endpoint. Typically the Unix socket path. |
| `docker.external_label_selectors` | `string[]` | `[]` | Label selectors that mark a container as [externally managed](#externally-managed-containers). Each entry is `key=value` (label has exactly that value) or a bare `key` (label is present). A malformed entry fails startup. |

#### `scheduler` — Background update checker

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scheduler.interval_minutes` | `int` | `60` | Interval in minutes between automatic image update checks. |
| `scheduler.notify_external_updates` | `bool` | `false` | Also send "update available" notifications for externally managed containers. |

#### `auth` — Authentication & session timeouts

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `auth.session_idle_timeout` | `duration` | `168h` (7d) | Sliding idle timeout for standard user sessions. |
| `auth.session_absolute_timeout` | `duration` | `720h` (30d) | Maximum absolute lifetime cap for standard sessions. |
| `auth.remember_me_idle_timeout` | `duration` | `720h` (30d) | Sliding idle timeout for "Remember me" sessions. |
| `auth.remember_me_absolute_timeout` | `duration` | `2160h` (90d) | Maximum absolute lifetime cap for "Remember me" sessions. |
| `auth.secure_cookies` | `string` | `"auto"` | Cookie `Secure` attribute mode (`"auto"`, `"always"`, or `"never"`). |
| `auth.bcrypt_cost` | `int` | `12` | Bcrypt hashing work factor for new passwords (min 4, max 31). |
| `auth.breached_password_check` | `bool` | `false` | When `true`, checks new passwords against HaveIBeenPwned API via k-anonymity. |

#### `webauthn` — Passkeys & WebAuthn configuration

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `webauthn.rp_id` | `string` | `""` | Relying Party ID for passkeys (effective domain without port, e.g. `"dmanager.example.com"` or `"localhost"`). Passkeys are disabled unless both `rp_id` and `origins` are set. |
| `webauthn.origins` | `string[]` | `[]` | Fully-qualified origins allowed for passkey ceremonies (e.g. `["https://dmanager.example.com"]`). |
| `webauthn.require_user_verification` | `string` | `"preferred"` | User verification policy: `preferred` (default), `required` or `discouraged`. With `required`, every passkey sign-in and registration asks for the authenticator's PIN or biometric (for a password manager, its unlock check) and credentials without it are rejected. With `preferred`, the authenticator decides: a PIN-less security key or an already-unlocked password manager can sign in with one confirmation. `discouraged` does not ask for it. A change applies to new ceremonies; existing passkeys are kept. See [Passkey user verification](docs/security.md#25-webauthn--passkey-security-model). |

#### `tailscale` — embedded tailnet node (optional)

The section is inert unless `tailscale.enabled` is `true`; an auth key alone does nothing (a key set while disabled is ignored). **Breaking change:** deployments that only set the auth key must now also set `tailscale.enabled: true`, and `https_enabled: true` now serves HTTPS *instead of* HTTP. See [docs/tailscale.md](docs/tailscale.md) for the full setup walkthrough.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `tailscale.enabled` | `bool` | `false` | The only switch for the embedded node. `true` requires `tailscale.auth_key`; startup fails if it is empty. |
| `tailscale.auth_key` | `string` | `""` | Tailscale auth key (a tagged, reusable key is recommended). Node state persists in `tailscale.state_dir`, so the key is only needed on first start. |
| `tailscale.auth_key_file` | `string` | `""` | Path of a file holding the auth key; mutually exclusive with `tailscale.auth_key`. |
| `tailscale.hostname` | `string` | `"dmanager"` | Node name in the tailnet (lowercase letters, digits, hyphens; max 63 chars). Served at `<hostname>.<tailnet>.ts.net`. |
| `tailscale.port` | `int` | `80` | Tailnet serving port in HTTP mode only; ignored when `https_enabled` is `true`. |
| `tailscale.https_enabled` | `bool` | `false` | Selects the protocol. `false`: plain HTTP on `tailscale.port`. `true`: HTTPS only, on tailnet `:443` using Tailscale-issued certificates (valid TLS that passkeys accept, served over HTTP/2), with no plain-HTTP tailnet listener. Needs MagicDNS and HTTPS certificates on the tailnet; without them no tailnet listener runs. |
| `tailscale.state_dir` | `string` | `<db_dir>/tailscale` | tsnet state directory; must be a dedicated directory when set explicitly. |

#### `smtp` — system email relay (optional)

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `smtp.enabled` | `bool` | `false` | Master switch. When `false`, the whole section is inert — email-consuming features degrade to logged no-ops. |
| `smtp.host` | `string` | `""` | SMTP relay host (e.g. a postfix container on the Docker network). Required when enabled. |
| `smtp.port` | `string` | `"25"` | Relay port. Use `"587"` for submission-style relays. |
| `smtp.username` / `smtp.password` | `string` | `""` | Optional SMTP AUTH (PLAIN). Leave empty for relays that trust the internal network. Prefer `DMANAGER_SMTP_PASSWORD` env for the secret. |
| `smtp.password_file` | `string` | `""` | Path of a file holding the SMTP password; mutually exclusive with `smtp.password`. |
| `smtp.from_email` | `string` | `""` | Sender address, required when enabled. Must sit on the upstream provider’s verified domain (e.g. Resend) or upstream delivery fails. |
| `smtp.from_name` | `string` | `""` | Optional display name, rendered as `From: "dmanager" <noreply@example.com>`. |
| `smtp.tls_mode` | `string` | `"none"` | `none` (plaintext — internal networks only, the relay must be restricted to them), `starttls` (STARTTLS is mandatory: if the relay does not offer it or the upgrade fails, nothing is sent and no credentials are transmitted), or `tls` (implicit TLS, port 465 style). Certificates are always verified: an internal relay with a self-signed or otherwise untrusted certificate must present a trusted one, or use `none` (plaintext, internal networks only). |
| `smtp.timeout_seconds` | `int` | `15` | Dial + send budget per message (1–120). |

Email is sent by system flows only; there is no API or UI send path. Verify the setup from the deployment with `dmanager smtp test --to=you@example.com`.
#### `registries` — Private registry credentials

A list of registry credential entries. Each entry supports the following fields:

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `registries[].host` | `string` | — | Registry hostname (e.g. `ghcr.io`, `registry.example.com`). |
| `registries[].username` | `string` | — | Authentication username. |
| `registries[].password` | `string` | — | Authentication password or access token. |
| `registries[].password_file` | `string` | — | Path of a file holding the password or token; mutually exclusive with `password`. Preferred, see [Secrets from files](docs/deployment.md#secrets-from-files). |

**Example with private registries:**

```yaml
registries:
  - host: ghcr.io
    username: myuser
    password: ghp_xxxxxxxxxxxxxxxxxxxx
  - host: registry.example.com
    username: deploy
    password: s3cret
```

---

## Externally managed containers

Some containers belong to another tool (a NixOS service, a CI runner pool) and must not be recreated by dmanager. A container is **externally managed** when

- it carries the label `dmanager.managed-by=<value>` with any non-empty value (the value names the owner, for example `nixos`), or
- it matches one of the `docker.external_label_selectors`, for example `com.example.pool=true`.

Labels are read from Docker and re-evaluated on every request, so a label added by recreating a container, or a selector added to the config, takes effect immediately.

For these containers dmanager still checks for newer images and shows that an update is available, but:

- `UpgradeContainer` and enabling auto-update are rejected with a `FailedPrecondition` error;
- the scheduler never applies an auto-update, even if the flag was stored before the label appeared;
- no "update available" notification is sent unless `scheduler.notify_external_updates` is `true`;
- the UI shows a "Managed by <value>" badge (for a selector match, the selector itself) and disables the Upgrade button and the auto-update toggle.

NixOS `virtualisation.oci-containers` with the Docker backend adds no marker label of its own. Set the label in the host configuration on each container dmanager should leave alone:

```nix
virtualisation.oci-containers.containers.<name> = {
  image = "...";
  labels."dmanager.managed-by" = "nixos";
};
```

## Environment variables

All configuration values can be overridden via environment variables prefixed with `DMANAGER_`. Underscores map to the nested YAML structure (e.g. `server.port` → `DMANAGER_SERVER_PORT`).

| Variable | Maps to | Default | Description |
|----------|---------|---------|-------------|
| `DMANAGER_SERVER_PORT` | `server.port` | `9283` | Port the HTTP server listens on. |
| `DMANAGER_SERVER_LISTEN_ADDRESS` | `server.listen_address` | *(empty)* | Bind address of the plain HTTP listener. |
| `DMANAGER_SERVER_HTTP_ENABLED` | `server.http.enabled` | `true` | Enable the plain HTTP listener. |
| `DMANAGER_SERVER_DB_PATH` | `server.db_path` | `dmanager.db` | Path to the SQLite database file. |
| `DMANAGER_SERVER_ALLOWED_ORIGINS` | `server.allowed_origins` | *(empty)* | Comma-separated list of allowed CORS origins. |
| `DMANAGER_SERVER_TRUSTED_PROXY` | `server.trusted_proxy` | `false` | Trust `X-Forwarded-For` header for client IP extraction. |
| `DMANAGER_SERVER_TRUSTED_PROXY_HOPS` | `server.trusted_proxy_hops` | `1` | Proxies appending to `X-Forwarded-For`; the client IP is that many entries from the right. |
| `DMANAGER_DOCKER_HOST` | `docker.host` | `unix:///var/run/docker.sock` | Docker daemon endpoint. |
| `DMANAGER_DOCKER_EXTERNAL_LABEL_SELECTORS` | `docker.external_label_selectors` | *(empty)* | Comma-separated label selectors (`key=value` or `key`) marking containers as externally managed. |
| `DMANAGER_SCHEDULER_INTERVAL_MINUTES` | `scheduler.interval_minutes` | `60` | Minutes between automatic image update checks. |
| `DMANAGER_SCHEDULER_NOTIFY_EXTERNAL_UPDATES` | `scheduler.notify_external_updates` | `false` | Send update notifications for externally managed containers too. |
| `DMANAGER_AUTH_SESSION_IDLE_TIMEOUT` | `auth.session_idle_timeout` | `168h` | Sliding idle timeout for standard sessions. |
| `DMANAGER_AUTH_SESSION_ABSOLUTE_TIMEOUT` | `auth.session_absolute_timeout` | `720h` | Absolute lifetime cap for standard sessions. |
| `DMANAGER_AUTH_REMEMBER_ME_IDLE_TIMEOUT` | `auth.remember_me_idle_timeout` | `720h` | Sliding idle timeout for "Remember me" sessions. |
| `DMANAGER_AUTH_REMEMBER_ME_ABSOLUTE_TIMEOUT` | `auth.remember_me_absolute_timeout` | `2160h` | Absolute lifetime cap for "Remember me" sessions. |
| `DMANAGER_AUTH_SECURE_COOKIES` | `auth.secure_cookies` | `auto` | Cookie `Secure` attribute mode (`auto`, `always`, `never`). |
| `DMANAGER_AUTH_BCRYPT_COST` | `auth.bcrypt_cost` | `12` | Bcrypt hashing work factor for new passwords. |
| `DMANAGER_AUTH_BREACHED_PASSWORD_CHECK` | `auth.breached_password_check` | `false` | Enable HaveIBeenPwned password breach checking. |
| `DMANAGER_WEBAUTHN_RP_ID` | `webauthn.rp_id` | *(empty)* | Relying Party ID for passkey authentication. |
| `DMANAGER_WEBAUTHN_ORIGINS` | `webauthn.origins` | *(empty)* | Comma-separated list of allowed WebAuthn origins. |
| `DMANAGER_WEBAUTHN_REQUIRE_USER_VERIFICATION` | `webauthn.require_user_verification` | `preferred` | Passkey user verification policy (`preferred`, `required`, `discouraged`); see [`webauthn`](#webauthn--passkeys--webauthn-configuration). |
| `DMANAGER_TAILSCALE_ENABLED` | `tailscale.enabled` | `false` | Switches the embedded Tailscale node on. Requires an auth key. |
| `DMANAGER_TAILSCALE_AUTHKEY` | `tailscale.auth_key` | *(empty)* | Embedded Tailscale node auth key — required when enabled; ignored when disabled. |
| `DMANAGER_TAILSCALE_AUTHKEY_FILE` (also bare `TAILSCALE_AUTHKEY_FILE`) | `tailscale.auth_key_file` | *(empty)* | File holding the Tailscale auth key. |
| `DMANAGER_TAILSCALE_HOSTNAME` | `tailscale.hostname` | `dmanager` | Node name in the tailnet. |
| `DMANAGER_TAILSCALE_PORT` | `tailscale.port` | `80` | Tailnet serving port in HTTP mode only. |
| `DMANAGER_TAILSCALE_HTTPS_ENABLED` | `tailscale.https_enabled` | `false` | Serve HTTPS only, on tailnet `:443`, instead of HTTP. |
| `DMANAGER_TAILSCALE_STATE_DIR` | `tailscale.state_dir` | `<db_dir>/tailscale` | tsnet state directory. |
| `TAILSCALE_ENABLED`, `TAILSCALE_AUTHKEY`, `TAILSCALE_HOSTNAME`, `TAILSCALE_PORT`, `TAILSCALE_HTTPS_ENABLED`, `TAILSCALE_STATE_DIR` | *(bare aliases)* | — | Tailscale-style aliases; apply only when the `DMANAGER_TAILSCALE_*` counterpart is unset (precedence: prefixed > bare > YAML > defaults). |
| `DMANAGER_SMTP_ENABLED` | `smtp.enabled` | `false` | Master switch for system email. |
| `DMANAGER_SMTP_HOST` / `DMANAGER_SMTP_PORT` | `smtp.host` / `smtp.port` | `""` / `25` | SMTP relay endpoint (both required when enabled). |
| `DMANAGER_SMTP_USERNAME` / `DMANAGER_SMTP_PASSWORD` | `smtp.username` / `smtp.password` | *(empty)* | Optional SMTP AUTH (PLAIN); prefer the env var for the password. |
| `DMANAGER_SMTP_PASSWORD_FILE` | `smtp.password_file` | *(empty)* | File holding the SMTP password. |
| `DMANAGER_SMTP_FROM_EMAIL` / `DMANAGER_SMTP_FROM_NAME` | `smtp.from_email` / `smtp.from_name` | *(empty)* | Sender address (required when enabled) and optional display name. |
| `DMANAGER_SMTP_TLS_MODE` | `smtp.tls_mode` | `none` | `none`, `starttls`, or `tls`. |
| `DMANAGER_SMTP_TIMEOUT_SECONDS` | `smtp.timeout_seconds` | `15` | Dial + send budget per message (1–120). |
| `DMANAGER_REGISTRIES_<N>_HOST` | `registries[N].host` | — | Hostname of the Nth registry (0-indexed). |
| `DMANAGER_REGISTRIES_<N>_USERNAME` | `registries[N].username` | — | Username for the Nth registry. |
| `DMANAGER_REGISTRIES_<N>_PASSWORD` | `registries[N].password` | — | Password / token for the Nth registry. |
| `DMANAGER_REGISTRIES_<N>_PASSWORD_FILE` | `registries[N].password_file` | — | File holding the password / token for the Nth registry. |
| `DMANAGER_ENV` | *(logging mode)* | *(not set)* | Set to `production` to switch log output to JSON format. |
| `APP_ENV` | *(logging mode)* | *(not set)* | Alternative to `DMANAGER_ENV`. Set to `production` for JSON logs. |

**Registry credentials example (Docker Compose):**

```yaml
environment:
  - DMANAGER_REGISTRIES_0_HOST=ghcr.io
  - DMANAGER_REGISTRIES_0_USERNAME=myuser
  - DMANAGER_REGISTRIES_0_PASSWORD=ghp_xxxxxxxxxxxxxxxxxxxx
  - DMANAGER_REGISTRIES_1_HOST=registry.example.com
  - DMANAGER_REGISTRIES_1_USERNAME=deploy
  - DMANAGER_REGISTRIES_1_PASSWORD=s3cret
```

---

## Volumes

| Container path | Purpose |
|----------------|---------|
| `/var/run/docker.sock` | **Required.** Host Docker socket for container management. |
| `/var/lib/dmanager` | Persistent storage for the SQLite database and the embedded Tailscale node state (`tailscale/` subdirectory). |
| `/etc/dmanager/config.yaml` | Optional custom configuration file (mount read-only). |

---

## Ports

| Container port | Protocol | Description |
|----------------|----------|-------------|
| `9283` | HTTP | Web UI and ConnectRPC API. Fixed internal container port — publish it on any host port (e.g. `${DMANAGER_PORT:-9283}:9283`). |
| `80` / `443` | HTTP / HTTPS | Tailnet-only listeners when the embedded Tailscale node is enabled — reachable via MagicDNS (`<hostname>.<tailnet>.ts.net`), never published to the host. Exactly one is served: `443` (HTTPS with Tailscale-issued certificates and HTTP/2) when `tailscale.https_enabled: true`, otherwise `tailscale.port` (HTTP, default `80`). |

---

## Make targets

Working from a repository checkout, `make` wraps the common compose workflows (the stack is `docker-compose.yml` + optional `.env`):

| Target | Action |
|--------|--------|
| `make launch` | Start the stack in the background |
| `make stop` | Stop and remove the stack |
| `make restart` | Stop, then launch |
| `make build` | Build the local image (compose `build:` section) |
| `make pull` | Pull the images referenced by the stack |
| `make logs` | Follow stack logs |
| `make status` | Show container status |
| `make smtp-test TO=you@example.com` | Send a test email through the configured relay |

| `make lint` | Run golangci-lint with the repository config (same check as CI's lint job) |
| `make lint-install` | Install the CI-pinned golangci-lint binary into `$(go env GOPATH)/bin` |
| `make nix-hashes` | Refresh the Nix package's pnpm and Go dependency hashes |

Backend changes are linted in CI with golangci-lint (`Golangci-lint Check` job). Run the same check locally with `make lint`; if the binary is missing or a different version, `make lint-install` fetches exactly the pinned build. The pin matters: older golangci-lint releases fail against Go 1.27 stdlib export data — that mismatch, not a toolchain incompatibility, is what made local lint appear broken ([#311](https://github.com/noosxe/dmanager/issues/311)).

## Nix package

The flake builds dmanager without the container image: `nix build .#default` produces `result/bin/dmanager`, and `overlays.default` adds `pkgs.dmanager` (x86_64-linux and aarch64-linux). `nix develop` provides the dev shell. The definitions live in `nix/`.

The package fetches dependencies as fixed-output derivations whose hashes are pinned in `nix/hashes.json`. Any change to `frontend/pnpm-lock.yaml`, `go.mod` or `go.sum` (including Dependabot bumps) invalidates them and the Nix build fails with a hash mismatch. Run `make nix-hashes` (or `nix/update-hashes.sh`) and commit the updated `nix/hashes.json`. The Go version is read from `go.mod` and must match the locked nixpkgs `go_1_27`.


### NixOS module

`nixosModules.default` runs dmanager as a plain systemd service (no container image). Add the flake as an input and enable it:

```nix
# flake.nix
inputs.dmanager.url = "github:noosxe/dmanager";

# configuration.nix (with dmanager.nixosModules.default in the host's modules)
virtualisation.docker.enable = true;
services.dmanager = {
  enable = true;
  # Plain HTTP on :9283; open the port only if the host is not behind another listener.
  openFirewall = true;
  # Or tailnet-only: http.enable = false plus the Tailscale node.
  # http.enable = false;
  # tailscale = {
  #   enable = true;
  #   hostname = "dmanager";
  #   authKeyFile = "/run/secrets/dmanager-tailscale-key";
  # };
  # webauthn = {
  #   rpId = "dmanager.<tailnet>.ts.net";
  #   origins = [ "https://dmanager.<tailnet>.ts.net" ];
  # };
};
```

`inputs.dmanager.inputs.nixpkgs.follows = "nixpkgs"` is supported as long as the host's nixpkgs has a Go at least as new as `go.mod` requires; evaluation fails with a clear message otherwise. Without `follows`, dmanager uses its own locked nixpkgs.

The module options map to the config keys: `port`, `listenAddress`, `openFirewall` (opens `port` only while `http.enable` is true), `http.enable`, `webauthn.{rpId,origins}`, `tailscale.{enable,hostname,httpsEnabled,authKeyFile}`, `smtp.{enable,host,port,username,passwordFile,fromEmail,fromName,tlsMode}`, `registries` (`host`, `username`, `passwordFile`), `externalLabelSelectors` and `notifyExternalUpdates`. Anything else goes into the freeform `settings` attribute set (the YAML structure of the config file); a value set there wins over the typed options. `environmentFile` points at a dotenv file for secrets given as `DMANAGER_*` variables.

The config is rendered into the world-readable Nix store, so secrets must come from the `*File` options (only the file path is written) or from `environmentFile`; the module refuses plain `password`/`auth_key` values in `settings`.

The service runs as the `dmanager` user, which gets the `docker` group for the unit only. Docker socket access is root-equivalent: anyone who can drive dmanager can start a privileged container. The unit is sandboxed (read-only file system except the state directory, no capabilities, restricted syscalls and address families), but that cannot limit what dmanager does through Docker.

The state directory `/var/lib/dmanager` (mode 0700) holds the database and the Tailscale node state. On a host with an ephemeral root (impermanence), persist it, or the admin account and the tailnet identity are lost on every reboot.

Containers declared with `virtualisation.oci-containers` that dmanager should only observe get the label described under [Externally managed containers](#externally-managed-containers):

```nix
virtualisation.oci-containers.containers.<name>.labels."dmanager.managed-by" = "nixos";
```

`nix flake check` builds the package and boots a NixOS VM test (x86_64-linux, needs KVM) that runs the service next to Docker and checks that the UI and API answer and that a labelled container is reported as externally managed.

## Documentation

- [docs/tailscale.md](docs/tailscale.md) — embedded Tailscale node: setup, HTTPS, passkeys over the tailnet
- [docs/deployment.md](docs/deployment.md) — deployment guide, secrets from files, [upgrade notes](docs/deployment.md#upgrading)
- [docs/security.md](docs/security.md) — security model
- [docs/passkeys.md](docs/passkeys.md) — passkey requirements, what the tests pin, client notes and how to report a problem
- [docs/design.md](docs/design.md) — architecture and design decisions

## Roadmap

- **v0.10.0 — NixOS and secrets** (this release): native NixOS install (flake package and module), externally managed containers locked against upgrades, secrets read from files, and a tailnet-only setup with the plain HTTP listener optional. Breaking changes are listed in the [upgrade notes](docs/deployment.md#to-v0100).
- **v0.11.0 — shadcn/ui migration** ([#299](https://github.com/noosxe/dmanager/issues/299)): migrate the frontend onto shadcn/ui components over Tailwind v4 — accessible primitives replacing hand-rolled ones, the current palette as a token theme, and an in-app light/dark/system toggle, converted page by page. Also passkey interop hardening ([#306](https://github.com/noosxe/dmanager/issues/306)).
- **v0.11.0 — MCP server for agent access** ([#301](https://github.com/noosxe/dmanager/issues/301)): expose dmanager to AI agents via a streamable-HTTP Model Context Protocol server — container status, logs, and lifecycle actions as tools, gated by the existing RBAC model and audit-logged. Design story to define auth, SDK choice, and tool surface. Pairs naturally with the embedded Tailscale node for private agent access.
- **v0.12.0 — reliable self-update** ([#302](https://github.com/noosxe/dmanager/issues/302)): 1-click "Update dmanager" plus optional scheduled automatic mode — a mechanism that survives the manager's own death (helper-container recreate as the default candidate), with pre-update DB backup, health-gate verification, and automatic rollback.

## Security notes

### Why the container runs as root

The `dmanager` service runs as root inside the container because it needs
read/write access to the host's Docker socket (`/var/run/docker.sock`),
which is typically owned by `root:docker` with mode `660`. Mapping that
group to an unprivileged in-container user is host-specific (the `docker`
group GID varies between hosts), and Docker socket access is equivalent to
root on the host regardless of the UID inside the container — so dropping
privileges in-container adds little real isolation while the socket is
mounted.

If you run under rootless Docker or Podman where the socket is already
user-accessible, the standard [s6-overlay privilege-drop
pattern](https://github.com/just-containers/s6-overlay#dropping-privileges)
applies — create a `dmanager` user in the image, `chown /var/lib/dmanager`
before startup, and prefix the exec line in
`rootfs/etc/s6-overlay/s6-rc.d/dmanager/run` with `s6-setuidgid dmanager`.
