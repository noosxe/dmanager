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
| `server.db_path` | `DMANAGER_SERVER_DB_PATH` | `/var/lib/dmanager/dmanager.db` | Persistent SQLite path. |
| `server.allowed_origins` | `DMANAGER_SERVER_ALLOWED_ORIGINS` | `[]` | CORS comma-separated allowed origins list. |
| `server.trusted_proxy` | `DMANAGER_SERVER_TRUSTED_PROXY` | `false` | When true, trusts `X-Forwarded-For` header for client IP extraction. |
| `docker.host` | `DMANAGER_DOCKER_HOST` | `unix:///var/run/docker.sock` | Path to Docker unix socket. |
| `scheduler.interval_minutes` | `DMANAGER_SCHEDULER_INTERVAL_MINUTES` | `60` | Schedule frequency for registry update checks. |
| `auth.session_idle_timeout` | `DMANAGER_AUTH_SESSION_IDLE_TIMEOUT` | `168h` | Sliding idle timeout for standard sessions (7 days). |
| `auth.session_absolute_timeout` | `DMANAGER_AUTH_SESSION_ABSOLUTE_TIMEOUT` | `720h` | Absolute lifetime cap for standard sessions (30 days). |
| `auth.remember_me_idle_timeout` | `DMANAGER_AUTH_REMEMBER_ME_IDLE_TIMEOUT` | `720h` | Sliding idle timeout for "Remember me" sessions (30 days). |
| `auth.remember_me_absolute_timeout` | `DMANAGER_AUTH_REMEMBER_ME_ABSOLUTE_TIMEOUT` | `2160h` | Absolute lifetime cap for "Remember me" sessions (90 days). |
| `auth.secure_cookies` | `DMANAGER_AUTH_SECURE_COOKIES` | `auto` | Cookie `Secure` attribute mode (`auto`, `always`, `never`). |
| `auth.bcrypt_cost` | `DMANAGER_AUTH_BCRYPT_COST` | `12` | Bcrypt hashing work factor for new passwords (min 4, max 31). |
| `auth.breached_password_check` | `DMANAGER_AUTH_BREACHED_PASSWORD_CHECK` | `false` | Enable HIBP k-anonymity breached password verification on setup. |
| `webauthn.rp_id` | `DMANAGER_WEBAUTHN_RP_ID` | `""` | Relying Party ID for passkeys (domain without port). |
| `webauthn.origins` | `DMANAGER_WEBAUTHN_ORIGINS` | `[]` | Allowed origins (scheme+host+port) authorized for passkeys. |
| `webauthn.require_user_verification` | `DMANAGER_WEBAUTHN_REQUIRE_USER_VERIFICATION` | `preferred` | User verification policy (`preferred` or `required`). |

### 2.2. Private Registry Configuration

To configure credentials for private registry checks, list them inside the YAML configuration:

```yaml
registries:
  - host: "ghcr.io"
    username: "my-github-user"
    password: "ghp_securepersonaltoken"
```

Or configure them via indexed environment variables:

```bash
DMANAGER_REGISTRIES_0_HOST=ghcr.io
DMANAGER_REGISTRIES_0_USERNAME=my-github-user
DMANAGER_REGISTRIES_0_PASSWORD=ghp_securepersonaltoken
```

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
         - "9283:9283"
       volumes:
         - /var/run/docker.sock:/var/run/docker.sock
         - dmanager-data:/var/lib/dmanager
       environment:
         - DMANAGER_SERVER_PORT=9283
         - DMANAGER_SCHEDULER_INTERVAL_MINUTES=60

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
