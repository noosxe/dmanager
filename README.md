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
- **Embedded Tailscale Node** — optional built-in tailnet access (`TAILSCALE_AUTHKEY`): manage dmanager from anywhere in your tailnet with no published ports and no sidecar container; optional tailnet HTTPS (`:443`, Tailscale-issued certificates — passkeys included) served over HTTP/2, with node status visible in the Admin UI
- **System Logs** — browse structured backend logs directly in the UI
- **Authentication & Passkeys** — secure session-based authentication with role-based access control (admin / viewer), discoverable WebAuthn passkeys (Touch ID, Windows Hello, Face ID, hardware security keys), NIST password policy, login rate limiting, session management, and auth audit logging
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

With the embedded Tailscale node enabled, the UI is also reachable from your tailnet at `http(s)://<hostname>.<tailnet>.ts.net` — no published ports required.

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

docker:
  host: "unix:///var/run/docker.sock"

scheduler:
  interval_minutes: 60

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

# Embedded Tailscale node (optional — inert without an auth key; see docs/tailscale.md)
# tailscale:
#   auth_key: "tskey-auth-..."   # tagged, reusable key; only needed on first start
#   hostname: "dmanager"         # node name in the tailnet
#   port: 80                     # tailnet serving port when HTTPS is disabled
#   https_enabled: false         # true: HTTPS on tailnet :443 with Tailscale-issued certs
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
| `server.db_path` | `string` | `"dmanager.db"` | Path to the SQLite database file. Inside Docker this should be on a persistent volume (e.g. `/var/lib/dmanager/dmanager.db`). |
| `server.allowed_origins` | `string[]` | `[]` | List of allowed CORS origins. Leave empty to disallow cross-origin requests. Use `["*"]` to allow all origins. |
| `server.trusted_proxy` | `bool` | `false` | When `true`, trusts `X-Forwarded-For` header from reverse proxies for client IP extraction and rate limiting. |

#### `docker` — Docker daemon connection

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `docker.host` | `string` | `"unix:///var/run/docker.sock"` | Docker daemon endpoint. Typically the Unix socket path. |

#### `scheduler` — Background update checker

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scheduler.interval_minutes` | `int` | `60` | Interval in minutes between automatic image update checks. |

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
| `webauthn.rp_id` | `string` | `""` | Relying Party ID for passkeys (effective domain without port, e.g. `"dmanager.example.com"` or `"localhost"`). |
| `webauthn.origins` | `string[]` | `[]` | Fully-qualified origins allowed for passkey ceremonies (e.g. `["https://dmanager.example.com"]`). |
| `webauthn.require_user_verification` | `string` | `"preferred"` | User verification requirement (`"preferred"`, `"required"`, or `"discouraged"`). |

#### `tailscale` — embedded tailnet node (optional)

The section is inert until an auth key is configured. See [docs/tailscale.md](docs/tailscale.md) for the full setup walkthrough.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `tailscale.auth_key` | `string` | `""` | Tailscale auth key (a tagged, reusable key is recommended). Node state persists in `tailscale.state_dir`, so the key is only needed on first start. |
| `tailscale.hostname` | `string` | `"dmanager"` | Node name in the tailnet (lowercase letters, digits, hyphens; max 63 chars). Served at `<hostname>.<tailnet>.ts.net`. |
| `tailscale.port` | `int` | `80` | Tailnet serving port when HTTPS is disabled. |
| `tailscale.https_enabled` | `bool` | `false` | Serve HTTPS on tailnet `:443` using Tailscale-issued certificates — valid TLS that passkeys accept, served over HTTP/2. |
| `tailscale.state_dir` | `string` | `<db_dir>/tailscale` | tsnet state directory; must be a dedicated directory when set explicitly. |

#### `smtp` — system email relay (optional)

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `smtp.enabled` | `bool` | `false` | Master switch. When `false`, the whole section is inert — email-consuming features degrade to logged no-ops. |
| `smtp.host` | `string` | `""` | SMTP relay host (e.g. a postfix container on the Docker network). Required when enabled. |
| `smtp.port` | `string` | `"25"` | Relay port. Use `"587"` for submission-style relays. |
| `smtp.username` / `smtp.password` | `string` | `""` | Optional SMTP AUTH (PLAIN). Leave empty for relays that trust the internal network. Prefer `DMANAGER_SMTP_PASSWORD` env for the secret. |
| `smtp.from_email` | `string` | `""` | Sender address, required when enabled. Must sit on the upstream provider’s verified domain (e.g. Resend) or upstream delivery fails. |
| `smtp.from_name` | `string` | `""` | Optional display name, rendered as `From: "dmanager" <noreply@example.com>`. |
| `smtp.tls_mode` | `string` | `"none"` | `none` (plaintext — internal networks only, the relay must be restricted to them), `starttls`, or `tls` (implicit TLS, port 465 style). |
| `smtp.timeout_seconds` | `int` | `15` | Dial + send budget per message (1–120). |

Email is sent by system flows only; there is no API or UI send path. Verify the setup from the deployment with `dmanager smtp test --to=you@example.com`.
#### `registries` — Private registry credentials

A list of registry credential entries. Each entry supports the following fields:

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `registries[].host` | `string` | — | Registry hostname (e.g. `ghcr.io`, `registry.example.com`). |
| `registries[].username` | `string` | — | Authentication username. |
| `registries[].password` | `string` | — | Authentication password or access token. |

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

## Environment variables

All configuration values can be overridden via environment variables prefixed with `DMANAGER_`. Underscores map to the nested YAML structure (e.g. `server.port` → `DMANAGER_SERVER_PORT`).

| Variable | Maps to | Default | Description |
|----------|---------|---------|-------------|
| `DMANAGER_SERVER_PORT` | `server.port` | `9283` | Port the HTTP server listens on. |
| `DMANAGER_SERVER_DB_PATH` | `server.db_path` | `dmanager.db` | Path to the SQLite database file. |
| `DMANAGER_SERVER_ALLOWED_ORIGINS` | `server.allowed_origins` | *(empty)* | Comma-separated list of allowed CORS origins. |
| `DMANAGER_SERVER_TRUSTED_PROXY` | `server.trusted_proxy` | `false` | Trust `X-Forwarded-For` header for client IP extraction. |
| `DMANAGER_DOCKER_HOST` | `docker.host` | `unix:///var/run/docker.sock` | Docker daemon endpoint. |
| `DMANAGER_SCHEDULER_INTERVAL_MINUTES` | `scheduler.interval_minutes` | `60` | Minutes between automatic image update checks. |
| `DMANAGER_AUTH_SESSION_IDLE_TIMEOUT` | `auth.session_idle_timeout` | `168h` | Sliding idle timeout for standard sessions. |
| `DMANAGER_AUTH_SESSION_ABSOLUTE_TIMEOUT` | `auth.session_absolute_timeout` | `720h` | Absolute lifetime cap for standard sessions. |
| `DMANAGER_AUTH_REMEMBER_ME_IDLE_TIMEOUT` | `auth.remember_me_idle_timeout` | `720h` | Sliding idle timeout for "Remember me" sessions. |
| `DMANAGER_AUTH_REMEMBER_ME_ABSOLUTE_TIMEOUT` | `auth.remember_me_absolute_timeout` | `2160h` | Absolute lifetime cap for "Remember me" sessions. |
| `DMANAGER_AUTH_SECURE_COOKIES` | `auth.secure_cookies` | `auto` | Cookie `Secure` attribute mode (`auto`, `always`, `never`). |
| `DMANAGER_AUTH_BCRYPT_COST` | `auth.bcrypt_cost` | `12` | Bcrypt hashing work factor for new passwords. |
| `DMANAGER_AUTH_BREACHED_PASSWORD_CHECK` | `auth.breached_password_check` | `false` | Enable HaveIBeenPwned password breach checking. |
| `DMANAGER_WEBAUTHN_RP_ID` | `webauthn.rp_id` | *(empty)* | Relying Party ID for passkey authentication. |
| `DMANAGER_WEBAUTHN_ORIGINS` | `webauthn.origins` | *(empty)* | Comma-separated list of allowed WebAuthn origins. |
| `DMANAGER_WEBAUTHN_REQUIRE_USER_VERIFICATION` | `webauthn.require_user_verification` | `preferred` | Passkey user verification policy (`preferred`, `required`, `discouraged`). |
| `DMANAGER_TAILSCALE_AUTHKEY` | `tailscale.auth_key` | *(empty)* | Embedded Tailscale node auth key — the node is off unless set. |
| `DMANAGER_TAILSCALE_HOSTNAME` | `tailscale.hostname` | `dmanager` | Node name in the tailnet. |
| `DMANAGER_TAILSCALE_PORT` | `tailscale.port` | `80` | Tailnet serving port when HTTPS is disabled. |
| `DMANAGER_TAILSCALE_HTTPS_ENABLED` | `tailscale.https_enabled` | `false` | Serve HTTPS on tailnet `:443` with Tailscale-issued certificates. |
| `DMANAGER_TAILSCALE_STATE_DIR` | `tailscale.state_dir` | `<db_dir>/tailscale` | tsnet state directory. |
| `TAILSCALE_AUTHKEY`, `TAILSCALE_HOSTNAME`, `TAILSCALE_PORT`, `TAILSCALE_HTTPS_ENABLED`, `TAILSCALE_STATE_DIR` | *(bare aliases)* | — | Tailscale-style aliases; apply only when the `DMANAGER_TAILSCALE_*` counterpart is unset (precedence: prefixed > bare > YAML > defaults). |
| `DMANAGER_SMTP_ENABLED` | `smtp.enabled` | `false` | Master switch for system email. |
| `DMANAGER_SMTP_HOST` / `DMANAGER_SMTP_PORT` | `smtp.host` / `smtp.port` | `""` / `25` | SMTP relay endpoint (both required when enabled). |
| `DMANAGER_SMTP_USERNAME` / `DMANAGER_SMTP_PASSWORD` | `smtp.username` / `smtp.password` | *(empty)* | Optional SMTP AUTH (PLAIN); prefer the env var for the password. |
| `DMANAGER_SMTP_FROM_EMAIL` / `DMANAGER_SMTP_FROM_NAME` | `smtp.from_email` / `smtp.from_name` | *(empty)* | Sender address (required when enabled) and optional display name. |
| `DMANAGER_SMTP_TLS_MODE` | `smtp.tls_mode` | `none` | `none`, `starttls`, or `tls`. |
| `DMANAGER_SMTP_TIMEOUT_SECONDS` | `smtp.timeout_seconds` | `15` | Dial + send budget per message (1–120). |
| `DMANAGER_REGISTRIES_<N>_HOST` | `registries[N].host` | — | Hostname of the Nth registry (0-indexed). |
| `DMANAGER_REGISTRIES_<N>_USERNAME` | `registries[N].username` | — | Username for the Nth registry. |
| `DMANAGER_REGISTRIES_<N>_PASSWORD` | `registries[N].password` | — | Password / token for the Nth registry. |
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
| `80` / `443` | HTTP / HTTPS | Tailnet-only listeners when the embedded Tailscale node is enabled — reachable via MagicDNS (`<hostname>.<tailnet>.ts.net`), never published to the host. `443` serves HTTPS with Tailscale-issued certificates and HTTP/2 when `tailscale.https_enabled: true`. |

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

Backend changes are linted in CI with golangci-lint (`Golangci-lint Check` job). Run the same check locally with `make lint`; if the binary is missing or a different version, `make lint-install` fetches exactly the pinned build. The pin matters: older golangci-lint releases fail against Go 1.27 stdlib export data — that mismatch, not a toolchain incompatibility, is what made local lint appear broken ([#311](https://github.com/noosxe/dmanager/issues/311)).

## Documentation

- [docs/tailscale.md](docs/tailscale.md) — embedded Tailscale node: setup, HTTPS, passkeys over the tailnet
- [docs/deployment.md](docs/deployment.md) — deployment guide
- [docs/security.md](docs/security.md) — security model
- [docs/design.md](docs/design.md) — architecture and design decisions

## Roadmap

- **v0.10.0 — shadcn/ui migration** ([#299](https://github.com/noosxe/dmanager/issues/299)): migrate the frontend onto shadcn/ui components over Tailwind v4 — accessible primitives (dialogs, tabs, dropdowns, toasts) replacing hand-rolled ones, the current palette carried over as a token theme, and an in-app light/dark/system theme toggle. Incremental page-by-page conversion — no big-bang rewrite. See the issue for the full audit and migration sketch.
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
