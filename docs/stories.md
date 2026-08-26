# AI Agent Feature Stories Roadmap

This document outlines the development plan for the `dmanager` application. The stories are designed to be bite-sized (typically < 300 lines of code changed per PR) and logically sequenced so that dependencies are met first. Each story includes strict validation checks to verify implementation correctness.

```mermaid
graph TD
    S1["STORY-001: DB Schema & Migrations (DONE)"] --> S2["STORY-002: Backend Authentication (DONE)"]
    S2 --> S3["STORY-003: CLI Serve Command (DONE)"]
    S3 --> S4["STORY-004: Docker Client & Container Discovery (DONE)"]
    S4 --> S5["STORY-005: Event Monitor & DB Sync Daemon (DONE)"]
    S5 --> S6["STORY-006: Container ConnectRPC Sync Stream (DONE)"]
    S6 --> S7["STORY-007: Container Start/Stop API (DONE)"]
    S7 --> S8["STORY-008: Container Re-Creation Upgrade API (DONE)"]
    S8 --> S9["STORY-009: Log Streaming API (DONE)"]
    S2 --> F1["STORY-010: Frontend Routing & Auth Page (DONE)"]
    F1 --> F2["STORY-011: Frontend Dashboard & Grid (DONE)"]
    F2 --> F3["STORY-012: Frontend Actions & Status Stream (DONE)"]
    F3 --> F4["STORY-013: Frontend Logs Console xterm.js (DONE)"]
    S9 --> S14["STORY-014: Dockerfile & Containerization (DONE)"]
    F4 --> S14
    F4 --> F5["STORY-015: Frontend Test Suite Setup (DONE)"]
    S14 --> S16["STORY-016: Production Compose & Deployment (DONE)"]
    F5 --> S16
    S16 --> F17["STORY-017: Remove Google Fonts Dependency (DONE)"]
    F17 --> S18["STORY-018: Docker Build CI Workflow (DONE)"]
    S18 --> S19["STORY-019: Upgrade GitHub Actions to Latest Versions (DONE)"]
    S19 --> S20["STORY-020: Structured Logging Migration (DONE)"]
    S20 --> S21["STORY-021: Version Tag Release Workflow (DONE)"]
    S21 --> S22["STORY-022: Implement Container Auto-Update and Check Updates RPCs (DONE)"]
    S22 --> S23["STORY-023: Periodic Registry Update Checker (DONE)"]
    S23 --> S24["STORY-024: Automated Container Re-Deployment Workflow (DONE)"]
    S24 --> S25["STORY-025: Improve Request and Action Logging (DONE)"]
    S25 --> S26["STORY-026: Enable Client-Server Binary Communication (DONE)"]
    S26 --> S27["STORY-027: Implement LogServiceHandler (DONE)"]
    S27 --> F28["STORY-028: Frontend Client-Side Logging Framework (DONE)"]
    F28 --> F29["STORY-029: System Logs Page Design and Implementation (DONE)"]
    F29 --> F30["STORY-030: Migrate Sidebar Logo to SVG Asset (DONE)"]
    F30 --> F31["STORY-031: Configure Build Pipeline Favicon Generation (DONE)"]
    F31 --> S32["STORY-032: DB Settings Migration, Repo & RPCs (DONE)"]
    S32 --> S33["STORY-033: Gotify Notification Dispatcher (DONE)"]
    S32 --> F34["STORY-034: Frontend Settings Web UI (DONE)"]
    F34 --> F35["STORY-035: Configure Frontend minimumReleaseAge (DONE)"]
    F35 --> F36["STORY-036: Configure Dependabot npm Cooldown (DONE)"]
    F36 --> S37["STORY-037: Configure Dependabot for GitHub Actions (DONE)"]
    S37 --> F38["STORY-038: Frontend Toast Notification System (DONE)"]
    F38 --> F39["STORY-039: Private Registry Status Monitoring (DONE)"]
    F39 --> F40["STORY-040: Frontend Table View and View Switcher (DONE)"]
    F40 --> S41["STORY-041: Eliminate QEMU from Multi-Architecture Docker Builds (DONE)"]
    S41 --> S42["STORY-042: Two-Clock Session Model & Sliding Renewal"]
    S42 --> S43["STORY-043: Auth Configuration & Cookie Hardening"]
    S42 --> S44["STORY-044: Background Purge Job for Expired Sessions"]
```








---

## Backend Stories

### STORY-001: DB Schema, Migration Setup & SQLC CodeGen [DONE]
- **Scope:** Backend Database Infrastructure
- **Estimated Size:** Small (~100 LOC)
- **Dependencies:** None
- **Token Estimate:** Input: ~50k | Output: ~3k | Total: ~53k (Actual: ~52k)
- **Goal:** Set up the database migrations framework and generate initial SQL query wrappers using goose and SQLC.
- **Tasks:**
  1. Create database migration script `internal/db/migrations/00001_init.sql` based on [docs/schema.md](file:///home/mechsoull/Projects/dmanager/docs/schema.md).
  2. Configure SQLC via `sqlc.yaml` in the root (matching SQLite driver rules).
  3. Create SQL queries in `internal/db/queries/` (`users.sql`, `sessions.sql`, `containers.sql`).
  4. Generate Go queries code using `sqlc generate`.
- **Files Affected:**
  - `internal/db/migrations/00001_init.sql` (new)
  - `sqlc.yaml` (new)
  - `internal/db/queries/users.sql`, `internal/db/queries/sessions.sql`, `internal/db/queries/containers.sql` (new)
  - `internal/db/db.go` (new wrapper to open SQLite with CGO-free driver `ncruces/go-sqlite3`)
- **Validation Check:**
  - Run `go generate ./...` or `sqlc generate`.
  - Validate that the generated files compile successfully.
  - Verify formatting and linting: `golangci-lint run`.

---

### STORY-002: Backend Authentication Services [DONE]
- **Scope:** Backend Security & DB
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-001`
- **Token Estimate:** Input: ~90k | Output: ~6k | Total: ~96k (Actual: ~95k)
- **Goal:** Implement secure session-based authentication handlers using ConnectRPC.
- **Tasks:**
  1. Implement `AuthService` interface defined in [docs/protocol.md](file:///home/mechsoull/Projects/dmanager/docs/protocol.md).
  2. Implement `Login` (verify credentials, hash checks with bcrypt, store session in DB, return token).
  3. Implement `GetCurrentUser` (parse Authorization token, fetch session, return user payload).
  4. Implement `Logout` (invalidate session token in DB).
  5. Set up ConnectRPC auth interceptor/middleware.
- **Files Affected:**
  - `internal/auth/service.go` (new)
  - `internal/auth/interceptor.go` (new middleware)
  - `internal/auth/service_test.go` (new unit tests)
- **Validation Check:**
  - Run unit tests: `go test -v ./internal/auth/...`
  - Verify zero linter warnings: `golangci-lint run`.

---

### STORY-003: Cobra Serve Command & Server Bootstrap [DONE]
- **Scope:** CLI & Server Setup
- **Estimated Size:** Small (~150 LOC)
- **Dependencies:** `STORY-002`
- **Token Estimate:** Input: ~45k - 60k | Output: ~2k - 3k | Total: ~47k - 63k
- **Goal:** Add the `serve` command to the Cobra CLI, parsing configuration and starting the HTTP/gRPC-web server.
- **Tasks:**
  1. Create a `cmd/serve.go` sub-command under cobra.
  2. Initialize Koanf configuration parser (reading configs from `/etc/dmanager/config.yaml` or env).
  3. Bootstrap Echo v5 (or standard multiplexer) listening on configured port.
  4. Register ConnectRPC handlers (`AuthService`).
- **Files Affected:**
  - `cmd/serve.go` (new)
  - `internal/config/config.go` (new config manager)
- **Validation Check:**
  - Compile application: `go build -o dmanager .`
  - Start daemon locally: `./dmanager serve --help`
  - Run linter verification.

---

### STORY-004: Docker Client Setup & Basic Container Listing [DONE]
- **Scope:** Docker Integration
- **Estimated Size:** Small (~150 LOC)
- **Dependencies:** `STORY-003`
- **Token Estimate:** Input: ~50k - 70k | Output: ~3k - 4k | Total: ~53k - 74k
- **Goal:** Initialize the standard Docker SDK client and provide a simple non-streaming API to query local container metadata.
- **Tasks:**
  1. Add Docker SDK Go package dependency.
  2. Initialize Docker Client inside background context (listening on local unix socket).
  3. Implement the backend container listing RPC (`ListContainers`) to return discovered local Docker containers.
- **Files Affected:**
  - `internal/docker/client.go` (new Docker wrapper)
  - `internal/container/service.go` (implement list container RPC)
- **Validation Check:**
  - Mock docker tests: `go test -v ./internal/container/...`
  - Ensure `go build` passes.

---

### STORY-005: Docker Event Monitor & Database Sync Daemon [DONE]
- **Scope:** Daemon / Synchronization
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-004`
- **Token Estimate:** Input: ~75k - 100k | Output: ~5k - 7k | Total: ~80k - 107k
- **Goal:** Implement a long-running background monitor that listens to Docker events API, processes container changes, and syncs container states into the SQLite database.
- **Tasks:**
  1. Implement background daemon thread initialized at server startup.
  2. Listen to Docker Events stream (`Events(ctx, ...)`).
  3. On container create, start, stop, destroy, or die events, query container details and write updates to the `containers` database table.
- **Files Affected:**
  - `internal/docker/monitor.go` (new background sync daemon)
- **Validation Check:**
  - Run unit and mock database transaction tests.
  - Compile and verify formatting.

---

### STORY-006: Container ConnectRPC Sync Stream [DONE]
- **Scope:** API / Real-Time Streaming
- **Estimated Size:** Medium (~200 LOC)
- **Dependencies:** `STORY-005`
- **Token Estimate:** Input: ~80k - 110k | Output: ~5k - 8k | Total: ~85k - 118k
- **Goal:** Expose the real-time container states stream via ConnectRPC server streaming.
- **Tasks:**
  1. Implement `StreamContainers` server stream handler.
  2. Set up internal Pub/Sub pattern (or channels) where the background Docker monitor publishes updates, and the streaming handler pushes events to active clients.
- **Files Affected:**
  - `internal/container/stream.go` (stream endpoint logic)
- **Validation Check:**
  - Test streaming channels with mock events.
  - Check with `golangci-lint run`.

---

### STORY-007: Container Start/Stop API Operations [DONE]
- **Scope:** Container Operations
- **Estimated Size:** Small (~150 LOC)
- **Dependencies:** `STORY-004`
- **Token Estimate:** Input: ~60k - 80k | Output: ~3k - 5k | Total: ~63k - 85k
- **Goal:** Support remote command actions to start or stop targeted containers using the Docker SDK.
- **Tasks:**
  1. Implement `StartContainer` ConnectRPC handler invoking Docker API.
  2. Implement `StopContainer` ConnectRPC handler invoking Docker API (with timeout parameters).
  3. Handle errors securely and verify authorization rules.
- **Files Affected:**
  - `internal/container/actions.go` (start/stop methods)
- **Validation Check:**
  - Run action mock unit tests.

---

### STORY-008: Container Re-Creation Upgrade API [DONE]
- **Scope:** Docker Integration
- **Estimated Size:** Large (~300 LOC)
- **Dependencies:** `STORY-007`
- **Token Estimate:** Input: ~120k - 160k | Output: ~8k - 12k | Total: ~128k - 172k
- **Goal:** Implement the "Upgrade Container" operation, pulling the latest tag digest and re-creating the container with exact parameters preserved.
- **Tasks:**
  1. Implement `UpgradeContainer` RPC.
  2. Query existing container settings via `ContainerInspect` (preserve Envs, Ports, Binds, Volumes, Labels, Networks).
  3. Pull latest image tag digest (`ImagePull`).
  4. Stop and delete the old container.
  5. Re-create using the stored parameters and start the new instance.
- **Files Affected:**
  - `internal/container/upgrade.go` (re-create logic)
- **Validation Check:**
  - Unit tests verifying recreation parameter preservation mapping.

---

### STORY-009: Log Streaming & Tailing API [DONE]
- **Scope:** Real-Time Streams
- **Estimated Size:** Medium (~200 LOC)
- **Dependencies:** `STORY-004`
- **Token Estimate:** Input: ~70k - 90k | Output: ~4k - 6k | Total: ~74k - 96k
- **Goal:** Establish a streaming handler that yields stdout/stderr logs of a container over ConnectRPC.
- **Tasks:**
  1. Implement `StreamLogs` server stream.
  2. Call Docker SDK `ContainerLogs` requesting tail values and active stdout/stderr streams.
  3. Map chunk bytes to Protobuf stream responses and stream back to client.
- **Files Affected:**
  - `internal/container/logs.go` (log streaming endpoint)
- **Validation Check:**
  - Unit tests running reader mock buffers.

---

## Frontend Stories

### STORY-010: Frontend Routing & Login Page [DONE]
- **Scope:** Frontend Auth
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-002`
- **Token Estimate:** Input: ~80k - 120k | Output: ~6k - 9k | Total: ~86k - 129k
- **Goal:** Initialize React Router, design a premium glassmorphic login interface, and store the authentication token.
- **Tasks:**
  1. Set up TanStack Router rules (protected dashboard routes vs. public login route).
  2. Implement premium, clean Login component with fields validation.
  3. Add auth hooks to manage API credentials and store token in localStorage.
- **Files Affected:**
  - `frontend/src/routes/router.tsx` (navigation routes configuration)
  - `frontend/src/components/Login.tsx` (new premium login view)
  - `frontend/src/components/Setup.tsx` (new premium onboarding setup view)
  - `frontend/src/hooks/useAuth.tsx` (state management)
- **Validation Check:**
  - Run Biome linters and formatting checks: `pnpm biome check .`
  - Compile React build: `pnpm build`


---

### STORY-011: Frontend Dashboard Layout & Container Grid [DONE]
- **Scope:** Frontend UI
- **Estimated Size:** Large (~350 LOC)
- **Dependencies:** `STORY-010`
- **Token Estimate:** Input: ~140k - 180k | Output: ~9k - 13k | Total: ~149k - 193k
- **Goal:** Build the main dashboard framework displaying a premium grid view of discovered containers with status cards.
- **Tasks:**
  1. Create navigation sidebar (dashboard links, user actions, profile card).
  2. Create Container Grid displaying container name, image tag, status badges (running/stopped/updating), and action buttons.
  3. Implement local filtering and search functionality.
- **Files Affected:**
  - `frontend/src/components/DashboardLayout.tsx` (main shell)
  - `frontend/src/components/ContainerGrid.tsx` (grid rendering)
- **Validation Check:**
  - Biome checks and Vite production build compilation.

---

### STORY-012: Frontend Actions & Real-time Status Stream Integration [DONE]
- **Scope:** Frontend State
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-006`, `STORY-007`
- **Token Estimate:** Input: ~90k - 130k | Output: ~5k - 8k | Total: ~95k - 138k
- **Goal:** Integrate ConnectRPC clients to stream real-time updates and trigger container actions (start/stop).
- **Tasks:**
  1. Add `@connectrpc/connect` and transport configs.
  2. Setup hook to subscribe to `StreamContainers` and patch the React state when updates arrive.
  3. Connect action buttons to trigger `StartContainer` / `StopContainer` endpoints.
- **Files Affected:**
  - `frontend/src/hooks/useContainers.ts` (stream binding)
- **Validation Check:**
  - Verification using Vitest + Mock Service Worker (MSW) or stubbed RPC mock responses.

---

### STORY-013: Frontend Logs Terminal Console [DONE]
- **Scope:** Frontend UI
- **Estimated Size:** Medium (~200 LOC)
- **Dependencies:** `STORY-009`
- **Token Estimate:** Input: ~80k - 110k | Output: ~4k - 7k | Total: ~84k - 117k
- **Goal:** Build a container logs console drawer using `xterm.js` to render streamed terminal outputs.
- **Tasks:**
  1. Install `xterm` and `@xterm/addon-fit`.
  2. Create a logs drawer/modal component with a black, monospaced terminal environment.
  3. Bind `StreamLogs` RPC stream directly to write incoming chunks to the xterm buffer.
- **Files Affected:**
  - `frontend/src/components/LogsDrawer.tsx` (terminal UI wrapper)
- **Validation Check:**
  - Compile frontend cleanly, verify Biome check passes.

---

### STORY-014: Dockerfile & Containerization [DONE]
- **Scope:** Deployment & Process Control
- **Estimated Size:** Small (~100 LOC)
- **Dependencies:** `STORY-009`, `STORY-013`
- **Token Estimate:** Input: ~50k | Output: ~3k | Total: ~53k
- **Goal:** Set up a multi-stage Dockerfile to build the frontend and backend, package them into a lightweight Alpine image with s6-overlay, and verify execution.
- **Tasks:**
  1. Create a root `Dockerfile` using multi-stage builds (`node:24-alpine`, `golang:alpine`, and `alpine:latest`).
  2. Configure `s6-overlay` in the runtime image to supervise the `dmanager` process using the embedded configuration in `rootfs/`.
  3. Verify local build works by compiling and starting the container with the docker socket mounted.
- **Files Affected:**
  - `Dockerfile` (new)
- **Validation Check:**
  - Build the docker image locally.
  - Run the built image checking s6-overlay starts up `dmanager` successfully.

---

### STORY-015: Frontend Test Suite Setup & Component Testing [DONE]
- **Scope:** Frontend Testing
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-011`, `STORY-013`
- **Token Estimate:** Input: ~60k | Output: ~4k | Total: ~64k
- **Goal:** Set up Vitest, React Testing Library, and MSW for the React frontend, and write component/integration tests for critical views.
- **Tasks:**
  1. Add devDependencies for `vitest`, `@testing-library/react`, `@testing-library/jest-dom`, `@testing-library/user-event`, `jsdom`, and `msw` in `frontend/package.json`.
  2. Configure Vitest in `frontend/vite.config.ts` (using `jsdom` environment and global test setups).
  3. Create test files for critical components (e.g. `Login.test.tsx` and custom hook tests or a setup file) asserting basic UI functionality, interactions, and mocked responses.
- **Files Affected:**
  - `frontend/package.json` (modified)
  - `frontend/vite.config.ts` (modified)
  - `frontend/src/setupTests.ts` (new setup file)
  - `frontend/src/components/Login.test.tsx` (new test)
- **Validation Check:**
  - Run `pnpm test` (or `vitest run`) inside the frontend directory, ensuring all tests pass successfully.

---

### STORY-016: Production Compose & Deployment [DONE]
- **Scope:** Deployment & Operations
- **Estimated Size:** Small (~100 LOC)
- **Dependencies:** `STORY-014`, `STORY-015`
- **Token Estimate:** Input: ~40k | Output: ~2k | Total: ~42k
- **Goal:** Set up a production Docker Compose configuration file and draft a clear deployment verification checklist document.
- **Tasks:**
  1. Create a root-level `docker-compose.yml` supporting restart policies, persistent volumes, environment overrides, and the unix socket bridge.
  2. Create a `docs/deployment.md` document outlining requirements, configurations, and docker run commands.
- **Files Affected:**
  - `docker-compose.yml` (new)
  - `docs/deployment.md` (new)
- **Validation Check:**
  - Verify that `docker compose config` passes successfully.

---

### STORY-017: Remove Google Fonts Dependency [DONE]
- **Scope:** Frontend Assets & Styling
- **Estimated Size:** Small (~50 LOC)
- **Dependencies:** `STORY-015`
- **Token Estimate:** Input: ~30k | Output: ~2k | Total: ~32k
- **Goal:** Remove Google Fonts external runtime dependency by packaging and self-hosting Plus Jakarta Sans and JetBrains Mono fonts locally.
- **Tasks:**
  1. Install Fontsource npm packages for Plus Jakarta Sans and JetBrains Mono in the frontend directory.
  2. Update `frontend/src/index.css` to import local stylesheets instead of fetching fonts from `fonts.googleapis.com`.
  3. Validate frontend builds and test execution.
- **Files Affected:**
  - `frontend/package.json` (modified)
  - `frontend/src/index.css` (modified)
- **Validation Check:**
  - Run Biome linters: `pnpm biome check .`
  - Compile frontend build: `pnpm build`
  - Run frontend test suite: `pnpm test`

---

### STORY-018: Docker Build CI Workflow [DONE]
- **Scope:** CI/CD & Infrastructure
- **Estimated Size:** Small (~50 LOC)
- **Dependencies:** `STORY-016`, `STORY-017`
- **Token Estimate:** Input: ~30k | Output: ~2k | Total: ~32k
- **Goal:** Add a new CI workflow "docker" triggered on pushes/pull requests to main to build the Docker image for both amd64 and arm64 platforms.
- **Tasks:**
  1. Add `.github/workflows/docker.yml` workflow triggered on push/PR to `main` branch.
  2. Implement build strategy using a platform matrix for `linux/amd64` and `linux/arm64`.
  3. Configure QEMU support for the `linux/arm64` platform matrix run.
  4. Use Docker build-push action to build the image (without pushing).
- **Files Affected:**
  - `.github/workflows/docker.yml` (new)
- **Validation Check:**
  - Verify workflow YAML structure is correct.

---

### STORY-019: Upgrade GitHub Actions to Latest Versions [DONE]
- **Scope:** CI/CD & Infrastructure
- **Estimated Size:** Small (~50 LOC)
- **Dependencies:** `STORY-018`
- **Token Estimate:** Input: ~30k | Output: ~2k | Total: ~32k
- **Goal:** Upgrade all GitHub Actions used in workflows to their latest stable major versions (checkout to v7, setup-qemu-action to v4, setup-buildx-action to v4, build-push-action to v7, action-setup to v6) and search web to verify compatibility.
- **Tasks:**
  1. Update `.github/workflows/backend.yml` (upgrade actions/checkout to v7).
  2. Update `.github/workflows/docker.yml` (upgrade actions/checkout to v7, setup-qemu-action to v4, setup-buildx-action to v4, build-push-action to v7).
  3. Update `.github/workflows/frontend.yml` (upgrade actions/checkout to v7, action-setup to v6).
- **Files Affected:**
  - `.github/workflows/backend.yml` (modified)
  - `.github/workflows/docker.yml` (modified)
  - `.github/workflows/frontend.yml` (modified)
- **Validation Check:**
  - Verify all GitHub workflows pass parsing/validation check.

---

### STORY-020: Structured Logging Migration [DONE]
- **Scope:** Backend Infrastructure
- **Estimated Size:** Medium (~150 LOC)
- **Dependencies:** `STORY-003`, `STORY-005`
- **Token Estimate:** Input: ~35k | Output: ~2k | Total: ~37k
- **Goal:** Migrate the backend from standard `log` library to structured logging using `log/slog` with module-scoped attributes.
- **Tasks:**
  1. Initialize the global `slog` handler in `cmd/serve.go`.
  2. Replace standard `log` library print usages in `cmd/serve.go` and `internal/docker/monitor.go` with contextual `slog` methods (e.g. `slog.Info`, `slog.Error`).
  3. Ensure module-scoped loggers (e.g., using `logger.With("module", "module_name")`) are instantiated and passed to backend components.
- **Files Affected:**
  - `cmd/serve.go` (modified)
  - `internal/docker/monitor.go` (modified)
- **Validation Check:**
  - Run compiler checking: `go build` / `go vet ./...`
  - Verify formatting and linting: `golangci-lint run`
  - Run all Go tests to verify everything compiles and runs successfully.

---

### STORY-021: Version Tag Release Workflow [DONE]
- **Scope:** CI/CD & Release Infrastructure
- **Estimated Size:** Small (~100 LOC)
- **Dependencies:** `STORY-020`
- **Token Estimate:** Input: ~30k | Output: ~2k | Total: ~32k
- **Goal:** Add a release workflow that builds Docker images for both `amd64` and `arm64`, pushes them to GHCR, and creates a GitHub Release referencing those images with auto-generated release notes.
- **Tasks:**
  1. Create `.github/workflows/release.yml` triggered on push of tags matching `v*`.
  2. Implement registry login step using `GITHUB_TOKEN` to GHCR.
  3. Configure Docker build-push-action to build multi-platform (`linux/amd64`, `linux/arm64`) images and push them to GHCR.
  4. Tag images as both `latest` and with the exact tag format (e.g. `vX.Y.Z`).
  5. Add steps to generate automatic release notes and create an official GitHub release with those notes.
- **Files Affected:**
  - `.github/workflows/release.yml` (new)
- **Validation Check:**
  - Verify syntax and schema correctness of `.github/workflows/release.yml`.

---

### STORY-022: Implement Container Auto-Update and Check Updates RPCs [DONE]
- **Scope:** Backend Container Service & Registry Integration
- **Estimated Size:** Medium (~200 LOC)
- **Dependencies:** `STORY-021`
- **Goal:** Implement `SetContainerAutoUpdate` and `CheckContainerUpdates` RPC handlers in `ContainerServiceHandler`.
- **Tasks:**
  1. Add registries slice configuration to `Service` struct to handle credentials matching.
  2. Implement `SetContainerAutoUpdate` to persist setting in SQLite DB and publish event via broker.
  3. Implement `CheckContainerUpdates` to perform remote Docker Registry check via `DistributionInspect` with appropriate credential matching, compare digests, update SQLite DB, and broadcast the change.
- **Files Affected:**
  - `internal/container/service.go` (modified)
  - `internal/container/actions.go` (new RPC implementations or helper additions)
  - `internal/container/service_test.go` (new unit tests)
  - `cmd/serve.go` (modified constructor call)
- **Validation Check:**
  - Run all unit and integration tests inside container service: `go test -v ./internal/container/...`
  - Verify zero linter warnings: `golangci-lint run`

---

### STORY-023: Periodic Registry Update Checker [DONE]
- **Scope:** Backend Background Daemon
- **Estimated Size:** Medium (~150 LOC)
- **Dependencies:** `STORY-022`
- **Goal:** Implement a background loop utilizing `time.Ticker` configured by `scheduler.interval_minutes` to check container images for registry digest changes, save status, and broadcast to clients.
- **Tasks:**
  1. Create a new scheduler/daemon service in `internal/container/scheduler.go` that loops with a ticker.
  2. Retrieve container details, query registry tag digests with credential resolution.
  3. Compare registry digests with local images.
  4. Write status update to SQLite database and publish to the events broker.
- **Files Affected:**
  - `internal/container/scheduler.go` (new)
  - `cmd/serve.go` (modified to initialize background worker)
- **Validation Check:**
  - Build checks and linter validation.
  - Verification tests for periodic checks logic.

---

### STORY-024: Automated Container Re-Deployment Workflow [DONE]
- **Scope:** Backend Background Daemon / Docker Re-deployment
- **Estimated Size:** Medium (~150 LOC)
- **Dependencies:** `STORY-023`
- **Goal:** Implement the automated pull-stop-remove-recreate-start workflow for containers with auto-update enabled when a newer registry digest is confirmed.
- **Tasks:**
  1. Refactor container recreation/upgrade logic in `internal/container/upgrade.go` to expose an internal/unauthenticated method.
  2. When the periodic scheduler detects a newer digest for a container with `auto_update` enabled, call the internal upgrade workflow.
  3. Verify parameters preservation (environment, networking, ports, volumes).
  4. Implement tests ensuring auto-update flow triggers container recreation successfully.
- **Files Affected:**
  - `internal/container/upgrade.go` (modified)
  - `internal/container/scheduler.go` (modified)
  - `internal/container/service_test.go` (modified/extended)
- **Validation Check:**
  - Run `go test -v ./internal/container/...`
  - Compile the server successfully and check linter rules.

---

### STORY-025: Improve Request and Action Logging [DONE]
- **Scope:** Backend Logging & Operations
- **Estimated Size:** Small (~100 LOC)
- **Dependencies:** `STORY-024`
- **Goal:** Improve logging at `Info` level for client requests and daemon actions (start, stop, upgrade, auto-update setting, update check) to clarify backend operations.
- **Tasks:**
  1. Add unary and streaming request logging to the auth interceptor (`internal/auth/interceptor.go`) to log request procedures, users, status, and duration at `Info` level.
  2. Implement `Info` level logging inside `internal/container/actions.go` for container start, stop, auto-update modifications, and image update checks.
  3. Implement `Info` level logging in `internal/container/upgrade.go` for container image upgrades.
- **Files Affected:**
  - `internal/auth/interceptor.go` (modified)
  - `internal/container/actions.go` (modified)
  - `internal/container/upgrade.go` (modified)
- **Validation Check:**
  - Verify that backend compile check and tests pass successfully: `go test -v ./...`
  - Ensure zero linter warnings: `golangci-lint run`

---

### STORY-026: Enable Client-Server Binary Communication [DONE]
- **Scope:** Communication Protocol / ConnectRPC Transport
- **Estimated Size:** Small (~10 LOC)
- **Dependencies:** `STORY-025`
- **Goal:** Enable binary wire format for ConnectRPC calls on the client transport.
- **Tasks:**
  1. Update `frontend/src/client.ts` to add `useBinaryFormat: true` to `createConnectTransport`.
- **Files Affected:**
  - `frontend/src/client.ts` (modified)
- **Validation Check:**
  - Run Biome linters: `pnpm biome check .`
  - Compile frontend build: `pnpm build`
  - Run frontend test suite: `pnpm test`

---

### STORY-027: Implement LogServiceHandler [DONE]
- **Scope:** Backend Logging / ConnectRPC
- **Estimated Size:** Small (~100 LOC)
- **Dependencies:** `STORY-026`
- **Goal:** Implement the backend LogServiceHandler to process and format frontend logs.
- **Tasks:**
  1. Create package `internal/logging` and implement `LogServiceHandler` service handler with `SyncLogs` in `internal/logging/service.go`.
  2. Implement backend unit tests for `SyncLogs` in `internal/logging/service_test.go`.
  3. Register `LogService` via `NewLogServiceHandler` in `cmd/serve.go`.
- **Files Affected:**
  - `internal/logging/service.go` (new)
  - `internal/logging/service_test.go` (new)
  - `cmd/serve.go` (modified)
- **Validation Check:**
  - Compile the server successfully: `go build -o /dev/null ./...`
  - Run all Go tests: `go test -v ./internal/logging/...`
  - Ensure zero linter warnings: `golangci-lint run`

---

### STORY-028: Frontend Client-Side Logging Framework [DONE]
- **Scope:** Frontend Logging & Local Storage
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-027`
- **Goal:** Set up Dexie.js in the frontend, buffering logs in IndexedDB, and synchronize them to the Go backend during browser idle periods.
- **Tasks:**
  1. Configure IndexedDB via Dexie.js to store client logs (level, message, timestamp, component, metadata).
  2. Implement global interceptors or wraps to capture warnings, errors, uncaught exceptions, and user actions, storing them in IndexedDB.
  3. Implement an idle-time sync mechanism utilizing `requestIdleCallback` (with custom timeout fallbacks) to send buffered logs in batches using the ConnectRPC LogService client.
  4. Ensure synced logs are pruned or deleted from the IndexedDB buffer upon successful synchronization.
- **Files Affected:**
  - `frontend/src/client.ts` (modified to export log client)
  - `frontend/src/services/logger.ts` (new)
  - `frontend/src/services/syncer.ts` (new)
  - `frontend/src/main.tsx` (modified to initialize logging and syncing)
- **Validation Check:**
  - Run frontend test suite: `pnpm test`
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`

---

### STORY-029: System Logs Page Design and Implementation [DONE]
- **Scope:** Central Logging & Frontend Interface
- **Estimated Size:** Medium (~350 LOC)
- **Dependencies:** `STORY-028`
- **Goal:** Design and implement an in-memory structured log ring-buffer on the backend, expose a `GetSystemLogs` RPC endpoint, and implement a responsive logs dashboard on the frontend with severity filters and text searching.
- **Tasks:**
  1. Add `GetSystemLogs` RPC definition to `proto/dmanager/v1/log.proto` and regenerate protobuf files.
  2. Implement `RingBuffer` and `InterceptHandler` in the backend to capture all slog structured log events.
  3. Register the new intercept handler wrapper on the main slog logger in `cmd/serve.go`.
  4. Implement `GetSystemLogs` in `internal/logging/service.go` and add unit tests to `internal/logging/service_test.go`.
  5. Enable the "System Logs" sidebar navigation button in `DashboardLayout.tsx` and register a `/logs` route in `frontend/src/routes/router.tsx`.
  6. Create the `SystemLogs.tsx` frontend page displaying the log stream with level filtering, query filtering, and auto-refresh.
- **Files Affected:**
  - `proto/dmanager/v1/log.proto` (modified)
  - `internal/logging/buffer.go` (new)
  - `internal/logging/service.go` (modified)
  - `internal/logging/service_test.go` (modified)
  - `cmd/serve.go` (modified)
  - `frontend/src/routes/router.tsx` (modified)
  - `frontend/src/components/DashboardLayout.tsx` (modified)
  - `frontend/src/components/SystemLogs.tsx` (new)
- **Validation Check:**
  - Compile the server successfully: `go build -o /dev/null ./...`
  - Run all Go tests: `go test -v ./internal/logging/...`
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`

---

### STORY-030: Migrate Sidebar Logo to SVG Asset [DONE]
- **Scope:** Frontend Assets & Clean Up
- **Estimated Size:** Small (~50 LOC)
- **Dependencies:** `STORY-029`
- **Goal:** Extract `.sidebar-logo` with its gradient background and terminal icon into a standalone `logo.svg` asset, update the frontend components to reference this asset, and clean up the styling.
- **Tasks:**
  1. Create `frontend/public/logo.svg` containing the SVG definition of the sidebar logo (including gradient, shape, and terminal icon).
  2. Modify `frontend/src/components/DashboardLayout.tsx` to use the new `logo.svg` for both mobile and desktop sidebar logos.
  3. Clean up the unused css styles in `frontend/src/index.css`.
- **Files Affected:**
  - `frontend/public/logo.svg` (new)
  - `frontend/src/components/DashboardLayout.tsx` (modified)
  - `frontend/src/index.css` (modified)
- **Validation Check:**
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`

---

### STORY-031: Configure Build Pipeline Favicon Generation [DONE]
- **Scope:** Frontend Build Pipeline
- **Estimated Size:** Small (~30 LOC)
- **Dependencies:** `STORY-030`
- **Goal:** Set up automated favicon asset generation in the Vite build pipeline using `@vite-pwa/assets-generator`.
- **Tasks:**
  1. Add `@vite-pwa/assets-generator` as a devDependency in the frontend package.
  2. Configure `generate-assets` script in `frontend/package.json` to generate favicon assets from `logo.svg`.
  3. Prepend `pnpm generate-assets` to the `build` script in `frontend/package.json`.
  4. Delete the old `favicon.svg` file from the repository.
  5. Update `frontend/index.html` to reference the generated favicon assets (`favicon.ico`, `logo.svg`, `apple-touch-icon-180x180.png`).
- **Files Affected:**
  - `frontend/package.json` (modified)
  - `frontend/index.html` (modified)
  - `frontend/public/favicon.svg` (deleted)
  - `docs/stories.md` (modified)
- **Validation Check:**
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build` (which generates the assets and runs vite build)

---

### STORY-032: Database Settings Migration, Repository & RPCs [DONE]
- **Scope:** Backend Settings Infrastructure
- **Estimated Size:** Medium (~200 LOC)
- **Dependencies:** `STORY-031`
- **Goal:** Set up database settings table migrations, SQLC query generation, settings service protobuf schema definitions, and RPC logic to get, update, and test settings.
- **Tasks:**
  1. Create database migration script `internal/db/migrations/00002_add_settings.sql` matching [docs/schema.md](file:///home/mechsoull/Projects/dmanager/docs/schema.md).
  2. Create SQL query templates in `internal/db/queries/settings.sql` and run `sqlc generate`.
  3. Define settings RPC protobuf endpoints in `proto/dmanager/v1/settings.proto` and run code generation scripts.
  4. Implement `SettingsService` handler handling `GetSettings` and `UpdateSettings` in Go.
  5. Implement `TestGotifyNotification` handler to dispatch a test payload to a target Gotify server.
- **Files Affected:**
  - `internal/db/migrations/00002_add_settings.sql` (new)
  - `internal/db/queries/settings.sql` (new)
  - `proto/dmanager/v1/settings.proto` (new)
  - `internal/settings/service.go` (new)
  - `internal/settings/service_test.go` (new)
  - `cmd/serve.go` (modified)
- **Validation Check:**
  - Verify compiler checking and tests pass: `go test -v ./internal/settings/...`
  - Ensure zero linter warnings: `golangci-lint run`.

---

### STORY-033: Gotify Notification Dispatcher [DONE]
- **Scope:** Backend Background Notification Dispatch
- **Estimated Size:** Medium (~150 LOC)
- **Dependencies:** `STORY-032`
- **Goal:** Implement a notification dispatcher that subscribes to container events/triggers and dispatches Gotify messages for image updates found, check failures, and auto-update outcomes.
- **Tasks:**
  1. Implement a notification package/service handling POST requests to Gotify server.
  2. Hook the dispatcher to notify when newer container image updates are found.
  3. Hook the dispatcher to notify on failures to check for container image updates.
  4. Hook the dispatcher to notify on both success and failure cases of automatic container updates/re-deployments.
- **Files Affected:**
  - `internal/notification/gotify.go` (new)
  - `internal/container/scheduler.go` (modified)
  - `internal/notification/gotify_test.go` (new)
- **Validation Check:**
  - Verify dispatcher tests pass: `go test -v ./internal/notification/...`
  - Verify overall Go build and lints pass.

---

### STORY-034: Frontend Settings View and Gotify Form [DONE]
- **Scope:** Frontend Settings Panel
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-032`
- **Goal:** Enable Settings sidebar item, implement the routing structure for `/settings`, and build the form to update and test Gotify notification configurations.
- **Tasks:**
  1. Add settings route in `frontend/src/routes/router.tsx`.
  2. Update `DashboardLayout.tsx` to enable the Settings navigation link.
  3. Build a beautiful glassmorphic `Settings.tsx` containing the form for Gotify URL and Application Token.
  4. Connect saving actions to `UpdateSettings` and testing connections to `TestGotifyNotification`.
- **Files Affected:**
  - `frontend/src/routes/router.tsx` (modified)
  - `frontend/src/components/DashboardLayout.tsx` (modified)
  - `frontend/src/components/Settings.tsx` (new)
- **Validation Check:**
  - Run Biome checks: `pnpm biome check .`
  - Compile React build: `pnpm build`
  - Run frontend test suite.

---

### STORY-035: Configure Frontend minimumReleaseAge [DONE]
- **Scope:** Frontend Package Configuration
- **Estimated Size:** Small (~5 LOC)
- **Dependencies:** `STORY-034`
- **Goal:** Configure `minimumReleaseAge: 1440` in `frontend/pnpm-workspace.yaml` to ensure package installation delay and security.
- **Tasks:**
  1. Add `minimumReleaseAge: 1440` configuration to `frontend/pnpm-workspace.yaml`.
- **Files Affected:**
  - `frontend/pnpm-workspace.yaml` (modified)
- **Validation Check:**
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`
  - Run frontend test suite: `pnpm test`

---

### STORY-036: Configure Dependabot npm Cooldown [DONE]
- **Scope:** CI Configuration
- **Estimated Size:** Small (~5 LOC)
- **Dependencies:** `STORY-035`
- **Goal:** Configure a 1-day cooldown in `.github/dependabot.yml` for npm updates to match pnpm's `minimumReleaseAge` configuration.
- **Tasks:**
  1. Add `cooldown: default-days: 1` to `.github/dependabot.yml` in the `npm` ecosystem block.
- **Files Affected:**
  - `.github/dependabot.yml` (modified)
- **Validation Check:**
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`
  - Run frontend test suite: `pnpm test`

---

### STORY-037: Configure Dependabot for GitHub Actions [DONE]
- **Scope:** CI Configuration
- **Estimated Size:** Small (~5 LOC)
- **Dependencies:** `STORY-036`
- **Goal:** Configure Dependabot in `.github/dependabot.yml` to perform weekly checks for `github-actions` updates.
- **Tasks:**
  1. Add a new package-ecosystem block for `github-actions` in `.github/dependabot.yml`.
- **Files Affected:**
  - `.github/dependabot.yml` (modified)
- **Validation Check:**
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`
  - Run frontend test suite: `pnpm test`

---

### STORY-038: Frontend Toast Notification System [DONE]
- **Scope:** Frontend UI & UX Feedback
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-037`
- **Goal:** Implement a responsive, beautiful glassmorphic toast notification system in the frontend, providing clear success, error, warning, and info feedback to user actions.
- **Tasks:**
  1. Implement a global `ToastContext` and `ToastProvider` to manage toast items.
  2. Implement a `ToastContainer` and animated `Toast` components with elegant visual styles.
  3. Integrate the toast system into `useContainers` hook (start/stop actions, update check, auto-update, upgrade).
  4. Integrate the toast system into `Settings.tsx` (saving settings, testing connection).
- **Files Affected:**
  - `frontend/src/context/ToastContext.tsx` (new)
  - `frontend/src/components/ToastContainer.tsx` (new)
  - `frontend/src/hooks/useContainers.ts` (modified)
  - `frontend/src/components/Settings.tsx` (modified)
  - `frontend/src/components/DashboardLayout.tsx` (modified)
- **Validation Check:**
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`
  - Run frontend test suite: `pnpm test`

---

### STORY-039: Private Registry Status Monitoring [DONE]
- **Scope:** Settings Web UI & Private Registry Integration
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-038`
- **Goal:** Implement the backend RPC `GetRegistryStatus` to verify registry credentials and connections, and display these statuses in a dedicated settings section in the Web UI.
- **Tasks:**
  1. Define the `GetRegistryStatus` RPC in `proto/dmanager/v1/settings.proto` and run code generation scripts.
  2. Implement the `GetRegistryStatus` handler in the backend `settings.Service` by passing the config's `Registries` and the `dockerClient` to the service, and calling `RegistryLogin` to verify status.
  3. Create the `RegistryStatus` UI card components and embed them in `Settings.tsx` settings view.
  4. Connect the frontend card list to the `GetRegistryStatus` RPC and implement manual refresh and automatic loading state.
- **Files Affected:**
  - `proto/dmanager/v1/settings.proto` (modified)
  - `internal/settings/service.go` (modified)
  - `internal/settings/service_test.go` (modified)
  - `cmd/serve.go` (modified)
  - `frontend/src/components/Settings.tsx` (modified)
- **Validation Check:**
  - Compile the server successfully: `go build -o /dev/null ./...`
  - Run all Go tests: `go test -v ./internal/settings/...`
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`

---

### STORY-040: Frontend Table View and View Switcher [DONE]
- **Scope:** Frontend Web UI
- **Estimated Size:** Medium (~300 LOC)
- **Dependencies:** `STORY-039`
- **Goal:** Implement a view switcher and a premium TanStack-based table view for containers on the main page of the Web UI, persisting the user's view preference.
- **Tasks:**
  1. Install `@tanstack/react-table` package.
  2. Implement `ContainerTable.tsx` using `@tanstack/react-table` for displaying containers list.
  3. Implement view switcher in `ContainerGrid.tsx` with layout/list toggle buttons.
  4. Save/load the selected view mode to/from `localStorage`.
  5. Add styles for the table headers, rows, active updates, status colors, and buttons to `index.css`.
- **Files Affected:**
  - `frontend/package.json` (modified)
  - `frontend/src/components/ContainerGrid.tsx` (modified)
  - `frontend/src/components/ContainerTable.tsx` (new)
  - `frontend/src/index.css` (modified)
- **Validation Check:**
  - Run Biome check: `pnpm biome check .`
  - Compile frontend: `pnpm build`

---

### STORY-041: Eliminate QEMU from Multi-Architecture Docker Builds [DONE]
- **Scope:** Container CI/CD & Build Infrastructure
- **Estimated Size:** Small (~100 LOC)
- **Dependencies:** `STORY-040`
- **Goal:** Refactor the Docker build setup to eliminate the dependency on QEMU, enabling native-speed multi-architecture container image builds.
- **Tasks:**
  1. Refactor the root `Dockerfile` to download and extract `s6-overlay` in a native `$BUILDPLATFORM` stage and copy it to a no-RUN final runtime stage.
  2. Remove the `Set up QEMU` action from `.github/workflows/release.yml`.
  3. Remove the `Set up QEMU` action from `.github/workflows/docker.yml`.
- **Files Affected:**
  - `Dockerfile` (modified)
  - `.github/workflows/release.yml` (modified)
  - `.github/workflows/docker.yml` (modified)
- **Validation Check:**
  - Verify that the Dockerfile build compiles for both `linux/amd64` and `linux/arm64` locally.

---

### STORY-042: Two-Clock Session Model & Sliding Renewal
- **Scope:** Backend Database & Authentication Interceptor
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-041`
- **Goal:** Implement the OWASP-standard two-clock session lifecycle model with sliding idle expiration and an absolute ceiling cap, including schema migration and lazy renewal in the ConnectRPC interceptor.
- **Tasks:**
  1. Create migration `00003_session_clocks.sql` to add `last_seen_at` and `absolute_expires_at` columns, backfill existing records, and create an index on `last_seen_at`.
  2. Update SQLC queries in `internal/db/queries/sessions.sql` (`TouchSession`, `ListSessionsByUser`, `DeleteSessionsByUser`, `CreateSession`, `PurgeExpiredSessions`).
  3. Update `internal/auth/interceptor.go` to reject expired sessions and slide `expires_at` only after half the idle timeout has elapsed, clamped to `absolute_expires_at`.
- **Files Affected:**
  - `internal/db/migrations/00003_session_clocks.sql` (new)
  - `internal/db/queries/sessions.sql` (modified)
  - `internal/auth/interceptor.go` (modified)
- **Validation Check:**
  - Run `sqlc generate`.
  - Run database migration and session interceptor unit tests.

---

### STORY-043: Auth Configuration & Cookie Hardening
- **Scope:** Backend Configuration, Cookies & Frontend Login Form
- **Estimated Size:** Medium (~250 LOC)
- **Dependencies:** `STORY-042`
- **Goal:** Introduce `auth.*` configuration section in koanf, enforce cookie `Secure` and `Max-Age` headers, upgrade bcrypt cost to 12, and support the "Remember me" option on backend and frontend.
- **Tasks:**
  1. Add `AuthConfig` to `internal/config/config.go` with environment variable mappings and strict validation.
  2. Update `proto/dmanager/v1/auth.proto` to add `remember_me` in `LoginRequest` and regenerate code.
  3. Implement `issueSession` in `internal/auth/service.go` setting `Max-Age` and proper `Secure` mode (`auto`, `always`, `never`).
  4. Use `cfg.Auth.BcryptCost` (default 12) for `SetupAdmin`.
  5. Add "Remember me" checkbox to `frontend/src/components/Login.tsx` and pass parameter through `useAuth.tsx`.
- **Files Affected:**
  - `internal/config/config.go` (modified)
  - `proto/dmanager/v1/auth.proto` (modified)
  - `internal/auth/service.go` (modified)
  - `frontend/src/components/Login.tsx` (modified)
  - `frontend/src/hooks/useAuth.tsx` (modified)
- **Validation Check:**
  - Run `go test ./internal/config/... ./internal/auth/...`
  - Run Biome check and React tests.

---

### STORY-044: Background Purge Job for Expired Sessions
- **Scope:** Background Daemon Maintenance
- **Estimated Size:** Small (~150 LOC)
- **Dependencies:** `STORY-042`
- **Goal:** Run an extensible background purge job on an hourly ticker to remove sessions expired by either idle or absolute clocks.
- **Tasks:**
  1. Update `PurgeExpiredSessions` query to delete sessions where `expires_at < ? OR absolute_expires_at < ?`.
  2. Implement `StartPurgeJob` in `internal/auth/purge.go` supporting multiple purge handlers with error logging.
  3. Wire the purge job into `cmd/serve.go` with graceful shutdown context.
- **Files Affected:**
  - `internal/auth/purge.go` (new)
  - `internal/auth/purge_test.go` (new)
  - `cmd/serve.go` (modified)
- **Validation Check:**
  - Run unit and integration tests for purge job with SQLite.
  - Verify zero linter issues (`golangci-lint run`).







