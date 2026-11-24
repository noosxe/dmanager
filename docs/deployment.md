# Production Deployment Guide & Checklist

This document details the procedure and requirements to deploy the `dmanager` application in production environments using Docker and Docker Compose.

---

## 1. System Requirements

* **Docker Engine:** Version 20.10.0 or higher.
* **Docker Compose:** Version 2.0.0 or higher.
* **Unix Socket Access:** The host system's `/var/run/docker.sock` must be mountable inside the container. The container process requires read/write access to this socket to discover and control containers on the host.

---

## 2. Configuration Options

`dmanager` is configured via a YAML file (default: `/etc/dmanager/config.yaml`) and/or environment variables prefixed with `DMANAGER_`.

### 2.1. Configuration Keys Reference

| YAML Key | Environment Override | Default Value | Description |
| :--- | :--- | :--- | :--- |
| `server.port` | `DMANAGER_SERVER_PORT` | `9283` | Port for the HTTP/ConnectRPC server. |
| `server.listen_address` | `DMANAGER_SERVER_LISTEN_ADDRESS` | *(empty: all interfaces)* | IP address or host name the plain HTTP listener binds to (no port). |
| `server.http.enabled` | `DMANAGER_SERVER_HTTP_ENABLED` | `true` | Serve the plain HTTP listener. May be `false` only when `tailscale.enabled` is `true`; otherwise startup fails because no listener would be up. |
| `server.db_path` | `DMANAGER_SERVER_DB_PATH` | `/var/lib/dmanager/dmanager.db` | Persistent SQLite path. |
| `server.allowed_origins` | `DMANAGER_SERVER_ALLOWED_ORIGINS` | `[]` | CORS comma-separated allowed origins list. |
| `server.trusted_proxy` | `DMANAGER_SERVER_TRUSTED_PROXY` | `false` | When true, trusts `X-Forwarded-For` header for client IP extraction. Set it when running behind a reverse proxy: otherwise every client shares the proxy's address, so login rate limits (including the 20-per-minute passkey sign-in begin limit) apply to all clients together. |
| `docker.host` | `DMANAGER_DOCKER_HOST` | `unix:///var/run/docker.sock` | Path to Docker unix socket. |
| `docker.external_label_selectors` | `DMANAGER_DOCKER_EXTERNAL_LABEL_SELECTORS` | `[]` | Label selectors (`key=value` or bare `key`; comma-separated in the env var) that mark containers as externally managed, in addition to the `dmanager.managed-by` label. Such containers are never upgraded by dmanager. |
| `scheduler.interval_minutes` | `DMANAGER_SCHEDULER_INTERVAL_MINUTES` | `60` | Schedule frequency for registry update checks. |
| `scheduler.notify_external_updates` | `DMANAGER_SCHEDULER_NOTIFY_EXTERNAL_UPDATES` | `false` | Send update-available notifications for externally managed containers too. |
| `auth.session_idle_timeout` | `DMANAGER_AUTH_SESSION_IDLE_TIMEOUT` | `168h` | Sliding idle timeout for standard sessions (7 days). |
| `auth.session_absolute_timeout` | `DMANAGER_AUTH_SESSION_ABSOLUTE_TIMEOUT` | `720h` | Absolute lifetime cap for standard sessions (30 days). |
| `auth.remember_me_idle_timeout` | `DMANAGER_AUTH_REMEMBER_ME_IDLE_TIMEOUT` | `720h` | Sliding idle timeout for "Remember me" sessions (30 days). |
| `auth.remember_me_absolute_timeout` | `DMANAGER_AUTH_REMEMBER_ME_ABSOLUTE_TIMEOUT` | `2160h` | Absolute lifetime cap for "Remember me" sessions (90 days). |
| `auth.secure_cookies` | `DMANAGER_AUTH_SECURE_COOKIES` | `auto` | Cookie `Secure` attribute mode (`auto`, `always`, `never`). |
| `auth.bcrypt_cost` | `DMANAGER_AUTH_BCRYPT_COST` | `12` | Bcrypt hashing work factor for new passwords (min 4, max 31). |
| `auth.breached_password_check` | `DMANAGER_AUTH_BREACHED_PASSWORD_CHECK` | `false` | Enable HIBP k-anonymity breached password verification on setup. |
| `webauthn.rp_id` | `DMANAGER_WEBAUTHN_RP_ID` | `""` | Relying Party ID for passkeys (domain without port). |
| `webauthn.origins` | `DMANAGER_WEBAUTHN_ORIGINS` | `[]` | Allowed origins (scheme+host+port) authorized for passkeys. |
| `webauthn.require_user_verification` | `DMANAGER_WEBAUTHN_REQUIRE_USER_VERIFICATION` | `preferred` | User verification policy: `preferred`, `required` or `discouraged`. `required` makes every passkey sign-in ask for a PIN or biometric; `preferred` lets the authenticator decide, so a PIN-less key can sign in with one touch. See [Security](security.md#25-webauthn--passkey-security-model). |
| `tailscale.enabled` | `DMANAGER_TAILSCALE_ENABLED` / `TAILSCALE_ENABLED` | `false` | Switches the embedded tailnet node on; the only switch. Requires `tailscale.auth_key`. |
| `tailscale.auth_key` | `DMANAGER_TAILSCALE_AUTHKEY` / `TAILSCALE_AUTHKEY` | `""` | Tailscale auth key. Required when `tailscale.enabled` is `true` (startup fails if empty); ignored when disabled. |
| `tailscale.auth_key_file` | `DMANAGER_TAILSCALE_AUTHKEY_FILE` / `TAILSCALE_AUTHKEY_FILE` | `""` | Read the auth key from this file instead (see [Secrets from files](#secrets-from-files)). |
| `tailscale.hostname` | `DMANAGER_TAILSCALE_HOSTNAME` / `TAILSCALE_HOSTNAME` | `dmanager` | MagicDNS hostname on the tailnet. |
| `tailscale.state_dir` | `DMANAGER_TAILSCALE_STATE_DIR` / `TAILSCALE_STATE_DIR` | `<dirname of server.db_path>/tailscale` | Persistent node state directory (created `0700`). |
| `tailscale.port` | `DMANAGER_TAILSCALE_PORT` / `TAILSCALE_PORT` | `80` | Tailnet-side port the web app is served on in HTTP mode. Ignored when `https_enabled` is `true` (HTTPS uses 443). |
| `tailscale.https_enabled` | `DMANAGER_TAILSCALE_HTTPS_ENABLED` / `TAILSCALE_HTTPS_ENABLED` | `false` | Serve HTTPS **instead of** HTTP, on tailnet port 443, with an automatic `*.ts.net` certificate. Requires HTTPS Certificates + MagicDNS on the tailnet (see §2.3). |

### 2.2. Private Registry Configuration

Credentials for private registry checks go in the `registries` list. Supply the token from a file
rather than as a plain value (the file variant is `password_file`, or `DMANAGER_REGISTRIES_<N>_PASSWORD_FILE`
as an environment variable; see [Secrets from files](#secrets-from-files) for a Docker Compose example):

```yaml
registries:
  - host: "ghcr.io"
    username: "example-user"
    password_file: "/run/secrets/ghcr_token"
```

A plain `password` (or `DMANAGER_REGISTRIES_<N>_PASSWORD`) still works, but then the token sits in the
config file or the container environment.

### 2.3. Embedded Tailscale Node

dmanager can embed a [Tailscale](https://tailscale.com) node (via `tsnet`) directly in the
server process, making the web app reachable from your tailnet without published ports or a
sidecar container. The feature is inert unless `tailscale.enabled` is `true` (an auth key alone does not enable it).
Full design: [docs/tailscale.md](tailscale.md).

> **Upgrade note (breaking change):** a deployment that only sets the auth key no longer joins the
> tailnet until `TAILSCALE_ENABLED=true` is set, and HTTPS mode no longer also serves plain HTTP.
> See [Upgrading](#upgrading).

```bash
TAILSCALE_ENABLED=true
TAILSCALE_AUTHKEY=tskey-auth-xxxxxxxxxxxx-xxxxxxxxxxxxx
# optional:
TAILSCALE_HOSTNAME=dmanager
TAILSCALE_STATE_DIR=/var/lib/dmanager/tailscale
TAILSCALE_HTTPS_ENABLED=true
```

Operational notes:

- **Auth key lifecycle:** the key is consumed only on first registration. Once node state exists
  in `state_dir`, restarts authenticate from persisted state — the env var may be removed after a
  successful first start. Losing the state directory re-triggers registration and requires a
  fresh key. An expired key plus existing state still boots.
- **Persistence:** keep `state_dir` on the same volume as the SQLite database (the default derives it
  from `server.db_path`, landing under `/var/lib/dmanager` in the standard Docker setup).
- **Failure behavior:** if the node cannot start (invalid/expired key, control plane unreachable),
  dmanager logs an error and keeps serving on the LAN — tailnet problems never block local access.
- **Network requirements:** outbound HTTPS (control plane/DERP) and outbound UDP for direct
  connections. No inbound ports, no `/dev/net/tun`, no extra container capabilities.
- **HTTPS on the tailnet (optional):** set `tailscale.https_enabled: true` (or
  `TAILSCALE_HTTPS_ENABLED=1`) to serve HTTPS on tailnet port 443 with an automatic
  Let's Encrypt certificate for `<hostname>.<tailnet>.ts.net` — see below. This replaces the
  plain-HTTP tailnet listener (`tailscale.port` is then unused); use the `https://` URL in
  browsers for passkey support.
- The state directory holds the node's private keys: treat it like the database (mounted volume,
  `0700`).

**HTTPS on the tailnet** (`https_enabled`):

Prerequisites (tailnet-side, in the Tailscale admin console):

1. **MagicDNS** enabled on the tailnet (so the node has a `<hostname>.<tailnet>.ts.net` DNS name).
2. **HTTPS Certificates** enabled on the tailnet (provisions Let's Encrypt certificates for
   `*.ts.net` names).

Find your node's DNS name in the startup log (`module=tailscale ... dns_name=<name>`) or via
`tailscale status` on any node. With both prerequisites met, `https_enabled: true` serves
dmanager at `https://<hostname>.<tailnet>.ts.net`. The first request may pause briefly while
the certificate is provisioned. Without the prerequisites, no tailnet listener runs at all (there is
no fallback to plain HTTP): the log says HTTPS mode needs MagicDNS and HTTPS certificates enabled
in the tailnet, and the same error shows on the status card. The LAN listener keeps working.

The node's live status — state, DNS name, tailnet IPs, access URLs, and key-expiry warnings —
is also visible in the web UI (Administration page, top card) via `AdminService.GetTailscaleStatus`,
so day-to-day checks do not require reading server logs.

Effects:

- **Secure session cookies:** the HTTPS listener injects `X-Forwarded-Proto: https`
  in-process, so `auth.secure_cookies: auto` sets the `Secure` cookie attribute there.
  The header is forced server-side and cannot be spoofed by tailnet peers.
- **Passkeys over the tailnet:** `https://<hostname>.<tailnet>.ts.net` is a secure context, so
  WebAuthn works. Configure the webauthn section so the tailnet origin is accepted:

  ```yaml
  webauthn:
    rp_id: "<tailnet>.ts.net"        # e.g. tail1234.ts.net — must be a suffix of the origin host
    origins:
      - "https://<hostname>.<tailnet>.ts.net"
      # add any other origins you use (e.g. an https LAN reverse proxy)
  ```

  Note: changing `webauthn.rp_id` invalidates passkeys registered under a previous RPID —
  existing users may need to re-register their passkeys.
---

## 3. Deployment Methods

### 3.1. Method A: Docker Compose (Recommended)

1. Save the following content to [docker-compose.yml](file:///home/mechsoull/Projects/dmanager/docker-compose.yml):
   ```yaml
   services:
     dmanager:
       image: dmanager:latest
       build:
         context: .
         dockerfile: Dockerfile
       container_name: dmanager
       restart: unless-stopped
       ports:
         - "${DMANAGER_PORT:-9283}:9283"
       volumes:
         - /var/run/docker.sock:/var/run/docker.sock
         - dmanager-data:/var/lib/dmanager
       environment:
         - DMANAGER_SERVER_PORT=9283
         - DMANAGER_SCHEDULER_INTERVAL_MINUTES=${DMANAGER_SCHEDULER_INTERVAL_MINUTES:-60}

   volumes:
     dmanager-data:
   ```
2. Build and start the service:
   ```bash
   docker compose up -d --build
   ```

### 3.2. Method B: Docker Run Command

Run the container inline passing parameters:

```bash
docker run -d \
  --name dmanager \
  --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v dmanager-data:/var/lib/dmanager \
  -p 9283:9283 \
  dmanager:latest
```

---

## 4. Verification & Diagnostics Checklist

Following container startup, execute these diagnostics steps to verify correct application health:

- [ ] **Check Container Execution State:**
  Verify that the container has initialized and is running:
  ```bash
  docker ps -f name=dmanager
  ```
- [ ] **Inspect s6-overlay Initialization Logs:**
  Verify that s6-overlay successfully booted both the environment and the daemon service:
  ```bash
  docker logs dmanager
  ```
  *Expected Output snippet:*
  ```
  s6-rc: info: service dmanager successfully started
  2026/07/04 12:35:12 goose: successfully migrated database to version: 1
  2026/07/04 12:35:12 Starting dmanager server on port 9283...
  ```
- [ ] **Verify SQLite DB Creation:**
  Confirm that the database file is generated in the volume mount path:
  ```bash
  docker exec dmanager ls -lh /var/lib/dmanager/dmanager.db
  ```
- [ ] **Onboard On Browser:**
  Open a browser tab to `http://localhost:9283` (or host IP if remote). Verify you are redirected to `/setup` to configure the primary administrator account (representing correct empty database verification).

## Secrets from files

Service managers and secret tools usually hand secrets over as files. The Tailscale auth key, the SMTP
password and registry passwords each have a file variant, as a config key and as an environment variable:

| Secret | Config key | Environment variable |
| :--- | :--- | :--- |
| Tailscale auth key | `tailscale.auth_key_file` | `DMANAGER_TAILSCALE_AUTHKEY_FILE` (or `TAILSCALE_AUTHKEY_FILE`) |
| SMTP password | `smtp.password_file` | `DMANAGER_SMTP_PASSWORD_FILE` |
| Registry password | `registries[N].password_file` | `DMANAGER_REGISTRIES_<N>_PASSWORD_FILE` |

- Setting both the value and the file for the same secret is a startup error; use one.
- Trailing newlines (LF or CRLF) are trimmed; other whitespace is kept.
- A file that cannot be read fails startup. The error names the path, never the contents.
- An empty file counts as an empty secret, so `tailscale.enabled` with an empty key file still fails validation.

### Docker Compose with `secrets:`

Compose top-level `secrets:` backed by files mount each secret read-only at `/run/secrets/<name>`. Point the
`*_FILE` variables at those paths:

```yaml
services:
  dmanager:
    image: ghcr.io/noosxe/dmanager:latest
    secrets:
      - tailscale_authkey
      - ghcr_token
    environment:
      - DMANAGER_TAILSCALE_ENABLED=true
      - DMANAGER_TAILSCALE_AUTHKEY_FILE=/run/secrets/tailscale_authkey
      - DMANAGER_REGISTRIES_0_HOST=ghcr.io
      - DMANAGER_REGISTRIES_0_USERNAME=example-user
      - DMANAGER_REGISTRIES_0_PASSWORD_FILE=/run/secrets/ghcr_token

secrets:
  tailscale_authkey:
    file: ./secrets/tailscale_authkey
  ghcr_token:
    file: ./secrets/ghcr_token
```

Keep the `./secrets/` files readable only by you (for example `chmod 600`) and out of version control.

The same in the YAML config file:

```yaml
tailscale:
  enabled: true
  auth_key_file: /run/secrets/tailscale_authkey
registries:
  - host: ghcr.io
    username: example-user
    password_file: /run/secrets/ghcr_token
```

When switching from a plain value, remove the plain value (`tailscale.auth_key`, `TAILSCALE_AUTHKEY`,
`DMANAGER_TAILSCALE_AUTHKEY`, `registries[N].password`, `DMANAGER_REGISTRIES_<N>_PASSWORD`) from the config
file, the Compose `environment:` and any `.env` file. A value and its file set together is a startup error.
An empty-string variable counts as unset, so `TAILSCALE_AUTHKEY=${TAILSCALE_AUTHKEY:-}` in a Compose file
does not conflict with a key file.

---

## Upgrading

### To v0.10.0

**Breaking changes**

- **`tailscale.enabled` is the only switch for the embedded Tailscale node.** An auth key alone no longer
  starts the node (it is ignored, with one info log line). Existing tailnet deployments must set
  `tailscale.enabled: true` (`DMANAGER_TAILSCALE_ENABLED` / `TAILSCALE_ENABLED`). With `enabled: true` an empty
  auth key fails startup.
- **`tailscale.https_enabled` serves HTTPS instead of HTTP, not both.** `false` serves plain HTTP on
  `tailscale.port`; `true` serves HTTPS on tailnet port 443 only, and `tailscale.port` is unused. If the
  tailnet lacks MagicDNS or HTTPS certificates, no tailnet listener runs and the error shows in the status
  card (there is no fallback to HTTP). Update bookmarks, `webauthn.origins` and any reverse proxy that relied
  on the plain-HTTP tailnet URL.
- **Externally managed containers are locked server-side.** Containers carrying the `dmanager.managed-by`
  label, or matching `docker.external_label_selectors`, cannot be upgraded or have auto-update enabled (the
  request is rejected), the scheduler never auto-updates them, and update notifications are skipped unless
  `scheduler.notify_external_updates` is `true`. The UI shows them with a badge and disabled controls. See
  [Externally managed containers](../README.md#externally-managed-containers).

**New, non-breaking options**

- `server.http.enabled` and `server.listen_address`: turn the plain HTTP listener off (tailnet-only
  deployments) or bind it to one address. See §2.1.
- `*_file` variants for the Tailscale auth key, SMTP password and registry passwords:
  see [Secrets from files](#secrets-from-files).
- A Nix flake package and a NixOS module (`services.dmanager`): see
  [NixOS module](../README.md#nixos-module).
