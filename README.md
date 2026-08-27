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
- **System Logs** — browse structured backend logs directly in the UI
- **Authentication & Passkeys** — secure session-based authentication with role-based access control (admin / viewer), discoverable WebAuthn passkeys (Touch ID, Windows Hello, Face ID, hardware security keys), NIST password policy, login rate limiting, session management, and auth audit logging

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
    ports:
      - "9283:9283"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - dmanager-data:/var/lib/dmanager
      # Optional: mount a custom config file
      # - ./config.yaml:/etc/dmanager/config.yaml:ro
    environment:
      - DMANAGER_SERVER_PORT=9283
      - DMANAGER_SCHEDULER_INTERVAL_MINUTES=60

volumes:
  dmanager-data:
```

### 2. Start the application

```bash
docker compose up -d
```

### 3. Open the web UI

Navigate to [http://localhost:9283](http://localhost:9283) in your browser.

On first launch you will be prompted to create an administrator account.

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
| `/var/lib/dmanager` | Persistent storage for the SQLite database. |
| `/etc/dmanager/config.yaml` | Optional custom configuration file (mount read-only). |

---

## Ports

| Container port | Protocol | Description |
|----------------|----------|-------------|
| `9283` | HTTP | Web UI and ConnectRPC API. |

---

## Building from source

dmanager uses a multi-stage Dockerfile that compiles both the React frontend and Go backend into a single static binary:

```bash
# Build the image locally
docker compose build

# Or build directly, injecting version metadata from git
docker build -t dmanager:local \
  --build-arg VERSION=$(git describe --tags --always --dirty) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  .
```

### Running the binary directly

If you prefer to run outside Docker (requires Go 1.27+, Node.js 24+, and pnpm):

```bash
# Build frontend
cd frontend && pnpm install && pnpm build && cd ..

# Build backend (embeds frontend assets; version metadata comes from git)
go build -o dmanager \
  -ldflags="-X dmanager/cmd.version=$(git describe --tags --always --dirty) \
            -X dmanager/cmd.commit=$(git rev-parse --short HEAD) \
            -X dmanager/cmd.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" .

# Run
./dmanager serve --config config.yaml
```

#### CLI flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | `-c` | *(auto-detect)* | Path to the YAML configuration file. |
| `--port` | `-p` | `9283` | Port to listen on (overrides config). |
| `--db` | `-d` | `dmanager.db` | Path to SQLite database file (overrides config). |

---

## License

See [LICENSE](LICENSE) for details.
