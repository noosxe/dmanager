# System Architecture & Technical Design Document

This document outlines the technical design and system architecture for the Docker Container Manager web application.

---

## 1. System Architecture Overview

The system follows a classic client-server model, optimized for low footprint and zero-dependency deployment. 

```mermaid
graph TD
    subgraph Client [Vite / React Frontend]
        Router[TanStack Router]
        Query[TanStack Query]
        Table[TanStack Table]
        Form[TanStack Form]
        Hotkeys[TanStack Hotkeys]
        ClientRPC[ConnectRPC Client]
        
        Router --> UI[UI Components]
        Query --> ClientRPC
        Table --> UI
        Form --> UI
        Hotkeys --> UI
    end

    subgraph Server [Go Backend]
        Cobra[Cobra CLI Wrapper]
        Koanf[Koanf Configuration]
        Echo[Echo v5 Web Framework]
        ConnectRPC[ConnectRPC Handlers]
        Embed[go:embed FS]
        Goose[Goose Migrations]
        SQLC[SQLC Queries]
        Resty[Resty v3 API Client]
        DockerAPI[Docker Engine API Client]

        Cobra --> Koanf
        Echo --> ConnectRPC
        Echo --> Embed
        ConnectRPC --> DB[(SQLite Database)]
        Goose --> DB
        SQLC --> DB
        ConnectRPC --> DockerAPI
        ConnectRPC --> Resty
    end

    ClientRPC -- "ConnectRPC Protocol (gRPC-Web / HTTP)" --> Echo
    DockerAPI -- "Unix Socket / Named Pipe" --> Docker[Docker Daemon]
    Resty -- "HTTPS" --> Registry[Docker Registry API]
```

---

## 2. Backend Design

The backend is built in Go, prioritizing performance, strict type-safety, and minimal resource usage.

### 2.1. Command Line Interface (Cobra)
* **Cobra CLI Wrapper:** The application binary exposes commands via `spf13/cobra`.
* **Commands:**
  * `serve`: Launches the Echo web server and starts the daemon loops.
  * `migrate`: Explicitly runs or reverts database migrations.
  * `version`: Prints build details.

### 2.2. Configuration Management (Koanf)
* **Koanf Config Manager:** Application config is managed using `koanf`.
* **Sources & Precedence:** Configuration values are merged in order of increasing priority (later overrides earlier):
  1. Defaults (hardcoded)
  2. TOML Configuration File (e.g., `config.toml`)
  3. Environment variables (prefixed with `DMAN_`)
  4. Command-line flags (parsed via Cobra/pflag)
* **Structure:** Configures system parameters (host, port, SQLite path, global schedule intervals).
* **Registry Credentials Block:** The configuration structure includes a `registries` list, allowing operators to define server locations and credentials (host URL, username, password/token) for private registries (such as `ghcr.io` or private Docker Hub spaces) to perform authenticated pulls and check tag digests.

### 2.3. Web Framework (Echo v5), ConnectRPC & Protobuf Layout
* **Echo v5 Framework:** Used as the base HTTP router.
* **ConnectRPC Integration:** 
  * Services are defined using standard Protocol Buffers (`.proto`).
  * ConnectRPC generates standard Go HTTP handlers.
  * These handlers are mounted cleanly as sub-routes on Echo's routing engine using standard Go `http.Handler` routing compatibility.
  * JSON and traditional REST endpoints are not exposed; all interactions occur over the Connect protocol (using binary Protobuf payload formats for efficiency and type safety).
* **Protobuf Directory & Generation Schema (Buf):**
  * Protocol Buffer definitions reside under `proto/dmanager/v1/`.
  * The project utilizes **`buf`** (instead of raw `protoc`) to manage dependencies, lint proto files, and generate code.
  * Generated Go artifacts are output to `internal/gen/dmanager/v1/`.
  * Generated TypeScript/React clients are output to `frontend/src/gen/dmanager/v1/`.

### 2.4. Data Storage & Migrations (SQLite, SQLC, Goose)
* **Database & Driver (CGO-free):** To ensure a single, statically compiled binary that is easy to cross-compile for multiple architectures, **CGO is fully disabled (`CGO_ENABLED=0`)**.
  * The database engine selected is SQLite.
  * The driver utilized is **`ncruces/go-sqlite3`**, a modern, high-performance pure Go SQLite driver. It compiles the official SQLite library into WASM and executes it via `wasm2go`, offering superior performance and compatibility over other transpilation-based alternatives (like `modernc.org/sqlite`) while maintaining 100% compliance with Go's `database/sql` interface.
  * The database runs in WAL (Write-Ahead Logging) mode to maximize write concurrency and read performance.
* **Concurrency & Lock Mitigation:**
  * To prevent transient SQLite `database is locked` errors during concurrent operations (e.g., background schedulers updating container statuses while users access auth/logs):
    * The write connection pool size is capped at `MaxOpenConns = 1`.
    * A default connection initialization queries `PRAGMA busy_timeout = 5000;` on startup to instruct SQLite to wait up to 5 seconds for locked operations before failing.
* **Migrations (Goose):** Database schema changes are version-controlled with `.sql` files and managed using `pressly/goose`. Migrations are embedded using `go:embed` and executed automatically at application startup.
* **SQLC for Queries:** Type-safe database queries are generated at compile-time using SQLC. SQLC reads the migration schemas and user-defined SQL queries, generating high-performance Go structures and execution methods without ORM overhead.

### 2.5. 3rd-Party Communications (Resty v3)
* **Resty v3 HTTP Client:** Used for external registry queries (such as querying registry APIs to fetch image manifests and tags).
* **Registry Interactions:** Fetches image metadata from registries (e.g., Docker Hub, GHCR) to determine if a newer version of a container image exists.

### 2.6. Embedded Production Build (`go:embed`)
* **Single Binary Output:** In production, Vite's build artifacts (`dist/` containing HTML, JS, CSS, assets) are compiled into the Go binary using `go:embed`.
* **Routing Fallback:** Echo serves these static assets directly from memory, mapping unknown client-side routes back to `index.html` to allow TanStack Router to handle SPA routing.

### 2.7. Code Quality & Standards (golangci-lint)
* **Linting & Quality Control:** Go backend code standards are enforced via `golangci-lint`.
* **Version Pinning & Reproducibility:** To ensure identical results between local development and CI pipelines, we pin the linter to a specific version: **`v2.13.1`**.
  * **Local Environment:** The Nix `flake.nix` dev shell is configured to provide `golangci-lint` at version `v2.13.1`.
  * **CI/CD Integration:** The CI pipeline (e.g., GitHub Actions) runs natively without Nix. The workflow configuration explicitly specifies the version input parameter (for example, utilizing `golangci/golangci-lint-action` with `version: v2.13.1`). This alignment guarantees that the exact same rule configurations and parsing engine version are applied across all environments.
* **Linter Profiles:** Enables standard linters including `govet`, `staticcheck`, `errcheck`, `unused`, and `gosec` for security auditing.
* **Configuration:** Standardized in a `.golangci.yml` configuration file at the repository root, ensuring consistent code analysis locally and in CI pipelines.

### 2.8. Module Structure, Dependency Injection & Logging
* **Domain-Driven Packaging:** The backend is divided into discrete, domain-specific packages (e.g., `internal/container`, `internal/image`, `internal/config`, `internal/database`).
* **Explicit Dependency Injection (DI):**
  * Heavy DI frameworks are avoided to keep binary startup time fast and dependencies transparent.
  * Construct-based manual injection is used. Each service defines its external requirements using interfaces (e.g., a `ContainerService` requiring a `DockerClient` interface).
  * A main bootstrap coordinator in `cmd/serve.go` instantiates concrete dependencies (e.g., the SQLite connection, Docker client) and passes them to constructors (e.g., `NewService(...)`), promoting mock-based unit testing.
* **Contextual Structured Logging (`log/slog`):**
  * Utilizes Go's official `log/slog` library for performance-oriented structured logging.
  * The root logger is configured at startup (defining output format as JSON in production, and standard log levels).
  * **Module-Scoped Logging:** When instantiating a module, the bootstrap process passes a scoped logger created using `logger.With("module", "module_name")`. All log events triggered within that module will implicitly include the `"module"` attribute, streamlining log aggregation, searchability, and system analysis.
  * **Client Request Logging:** The ConnectRPC auth interceptor logs every incoming unary and streaming client request at `Info` level. The logged fields include the procedure name, the authenticated user (if any), the execution duration, and the resulting error if the request failed.
  * **Daemon Action Logging:** Key application actions and operations (such as container start, container stop, auto-update state modifications, manual and scheduled update checks, and container image upgrades) are logged at `Info` level to track daemon activity clearly without requiring verbose debug logs.
* **Frontend Log Ingestion Endpoint:**
  * The backend exposes a specific ConnectRPC endpoint (e.g., `dmanager.v1.LogService/SyncLogs`) to accept batches of frontend logs.
  * When client logs are received via this RPC, the backend parses each entry and passes it into the standard `slog` stream.
  * Ingested frontend logs will contain attributes like `"source": "frontend"`, `"client_level"`, and `"client_timestamp"`, allowing seamless centralized log analysis.

### 2.10. Authentication & Authorization
* **Data Storage (SQLite):** User accounts and active sessions are stored in the SQLite database (`users` and `sessions` tables).
  * `users` table: `id`, `username`, `password_hash`, and `role` (e.g., `admin`, `viewer`).
  * `sessions` table: `session_id` (a cryptographically secure 32-byte hex-encoded random string), `user_id`, and `expires_at`.
* **Password Hashing (Bcrypt):** Passwords are encrypted on creation and verified during authentication using `golang.org/x/crypto/bcrypt` without external system compiler dependencies.
* **Session Cookies:**
  * Successful authentication yields a session identifier delivered via an HTTP cookie.
  * Cookie properties are set strictly to: `HttpOnly` (XSS prevention), `Secure` (HTTPS transport requirement), and `SameSite=Strict` (CSRF prevention).
* **ConnectRPC Interceptor Middleware:**
  * A server interceptor is registered to run on all ConnectRPC incoming requests.
  * It extracts the session cookie, queries SQLite to validate status and expiration, and binds the authorized user structure (`id`, `username`, `role`) directly to the Go execution context (`context.Context`).
  * All endpoint handlers retrieve authorization data from the context. Mutating calls check for the `admin` role, while viewing queries require at least the `viewer` role.
  * Excluded route: The `AuthService/Login` endpoint is explicitly bypassed by the interceptor.

### 2.11. Docker Engine SDK Communication
* **SDK Library:** The backend communicates with the local host container engine using the official Moby Go SDK (`github.com/moby/moby/client`).
* **Connection Channel:** Communication is executed over the standard Unix Socket located at `/var/run/docker.sock`.
* **Context Lifecycles:** All Docker SDK operations are bound to Go's standard `context.Context` (with timeouts) to avoid hanging threads.

### 2.12. Container Log Streaming
* **Real-time Log Channel (`GetContainerLogs`):**
  * The backend provides log streaming via a ConnectRPC server streaming endpoint: `ContainerService/GetContainerLogs`.
  * Minimum privilege role is `viewer`. The `auth.Interceptor` handles authentication, while the handler checks that the authenticated user possesses either `admin` or `viewer` role privileges.
  * The handler queries the Docker SDK `ContainerLogs` API.
  * Since Docker logs are multiplexed with a standard 8-byte header when the container TTY is disabled, or as raw text when TTY is enabled, the handler checks the container configuration (`Config.Tty`) via `ContainerInspect` before streaming.
  * If TTY is enabled, the handler reads lines from the raw stream directly.
  * If TTY is disabled, the handler parses the 8-byte header frame-by-frame to demultiplex the stream into `stdout`/`stderr` payloads.
  * In both cases, the parsed logs are packaged as `GetContainerLogsResponse` protobuf stream payloads and transmitted back to the client.

---


## 3. Frontend Design

The frontend is a modern React SPA optimized for speed, developer efficiency, and strict type consistency.

### 3.1. Package Management & Build (Vite, pnpm)
* **Vite Engine:** Fast bundling using ESBuild.
* **Package Manager:** `pnpm` is utilized for workspace dependency resolution and fast builds.

### 3.2. Code Quality & Standards (Oxlint & Oxfmt)
* **Linter & Formatter:** Oxlint is the linter and Oxfmt the formatter and import sorter, providing a fast, Rust-based toolchain.
* **No ESLint/Prettier:** ESLint and Prettier are excluded entirely from the toolchain in favor of the oxc tools' unified, highly performant workflow.

### 3.3. Styling (TailwindCSS)
* **Utility-First Styling:** TailwindCSS handles styling.
* **Vite Integration:** Configured with TailwindCSS's official Vite plugin for fast build-time compilation.

### 3.4. TanStack Package Suite Integration
* **TanStack Router:** Provides type-safe routing. Route definitions and layouts (dashboard, settings, container details) are strictly typed.
* **TanStack Query (React Query):** Integrated with the generated ConnectRPC client. React Query manages request states, caching, invalidation, and polling.
* **TanStack Table:** Manages tabular data views (e.g., container listing, image status list) supporting sorting, filtering, and row actions.
* **TanStack Form:** Provides type-safe state management for input forms (e.g., configuration parameters, schedule schedules).
* **TanStack Hotkeys:** Standardizes keyboard shortcuts (e.g., `ctrl+p` to search containers, `s` to start selected, `x` to stop, `r` to trigger manually check updates).
* **TanStack Devtools:** Configured for development mode (Router Devtools, Query Devtools) to inspect state visually.

### 3.5. Frontend Logging & Local Storage (Dexie.js)
* **IndexedDB Local Buffer (Dexie.js):** To prevent log loss when the user goes offline or closes their browser, all client-side logs (warnings, errors, uncaught exceptions, and user actions) are immediately saved locally to browser IndexedDB using `dexie.js`.
* **Idle-Time Syncer:** 
  * The frontend initiates a background synchronization task.
  * The syncer monitors browser idle periods using `requestIdleCallback` (with standard timer fallbacks).
  * When the main thread is idle, the task pulls the backlog of unsynced logs from IndexedDB, batches them, and transmits them to the backend using the ConnectRPC logging service client.
  * Upon successful backend ingestion acknowledgement, the logs are flagged as synced or pruned from IndexedDB.

### 3.6. Dashboard Layout & Container Grid Components
* **Dashboard Layout (`DashboardLayout.tsx`):**
  * Provides a consistent, responsive layout shell for all authenticated views.
  * Features a modern, glassmorphic navigation sidebar containing navigation links, application logo, active status badge, user identity card (username, role), and sign-out controls.
  * Adjusts smoothly to mobile viewports with toggleable sidebar navigation drawer.
* **Container Grid (`ContainerGrid.tsx`):**
  * Renders the main dashboard container management area.
  * Features a search input to instantly filter containers by name or image/tag.
  * Supports status filtering buttons (All, Running, Stopped) to narrow down active workloads.
  * Displays visual metrics (e.g. Total, Running, Stopped, and Updates Available container counts).
  * Integrates the View Switcher component to toggle between Card Grid and Table views.
* **Container Table (`ContainerTable.tsx`):**
  * Renders a premium, responsive data table of discovered containers utilizing the TanStack Table (`@tanstack/react-table`) library.
  * Displays container name, state/status badges, image repository and tags, container ID, auto-update toggle status, and actions.
  * Integrates sorting and structured column layouts with sleek hover animations.
* **View Switcher:**
  * A toolbar toggle component (using layout/list icons) to switch views.
  * Stores the user's active view preference (`grid` or `table`) in `localStorage` for cross-session persistence.

### 3.7. Container State Synchronizer Hook (`useContainers.ts`)
* **State Management & Subscription:**
  - Encapsulates the container fetching and state synchronization logic in a single, reusable custom Hook.
  - Queries all discovered containers initially using the `listContainers` method.
  - Initiates an asynchronous background subscription to the `StreamContainers` server streaming endpoint.
  - Processes streaming events concurrently:
    - **`save` event:** Inserts new containers or patches existing ones in the local state.
    - **`delete` event:** Filters out and removes the specified container from the local state.
  - Automatically handles connection aborts and cleanup by utilizing an `AbortController` passed to the RPC client, preventing memory leaks when components unmount.
  - Exposes loading flags, error payloads, and unified action executors (`start`, `stop`, `upgrade`, `toggleAutoUpdate`, `checkUpdates`) directly to consumer components.

---

## 4. Lifecycle Workflows

### 4.1. Application Startup Sequence
1. **CLI Execution:** Cobra executes the `serve` command.
2. **Config Load:** Koanf merges TOML configuration, environment variables, and CLI overrides.
3. **Database Setup:** Open SQLite file, enable WAL mode.
4. **Auto-Migrations:** Initialize Pressly Goose and execute pending migrations.
5. **Scheduler Setup:** Launch background tasks handling image update checks using the specified schedules.
6. **Vite Embed:** Mount static files FS handler to Echo.
7. **HTTP Init:** Launch Echo HTTP/2 listener on the configured port.

### 4.2. Container Automatic Update Checks & Parameter Preservation
* **Scheduler Engine:** Background check schedules are managed using standard, lightweight Go tickers (`time.Ticker` in select channel loops), avoiding heavy external scheduling libraries.
* **Image Registry Access:** For private repositories, the scheduler extracts authentication credentials from the Koanf `registries` block, encoding them as standard `X-Registry-Auth` header parameters passed to the Docker SDK pull engine or Resty request hooks.
* **Update Verification Workflow:**
  1. **Trigger:** The ticker triggers the update check sequence at the configured interval.
  2. **Registry Check:** For each container, query its registry (via Resty or Docker SDK with optional registry auth) to check tag digest information.
  3. **Digest Comparison:** If the registry digest is newer than the local container image digest, mark the status as "Update Available" in SQLite.
* **Re-Deployment Workflow (Parameter Preservation):** If automatic updates are enabled on a per-container basis and a newer image is confirmed:
  1. **Configuration Inspection:** The system calls `ContainerInspect` on the target container to retrieve its full execution parameters:
     * **Config:** Environment variables, entrypoints, user accounts, labels, working dir.
     * **HostConfig:** Port bindings, volume/bind mounts, restart policies, resource limits (CPU/Memory), network modes.
     * **NetworkSettings:** Assigned custom networks and network aliases.
  2. **Image Pull:** Pull the new image version from the registry using the Docker SDK.
  3. **Stop & Remove:** Gracefully stop the running container (using a standard timeout of 15 seconds) and remove the container resource.
  4. **Container Re-Creation:** Create the new container utilizing the *exact same configuration templates* extracted in Step 1, updating only the `Image` identifier field.
  5. **Start:** Start the container under the identical name, maintaining all network scopes, volume bindings, and port access points.

### 4.3. Initial Server Bootstrapping & Admin Setup
1. **Status Check:** At launch, the React frontend issues an unauthenticated request to a specific initialization checking endpoint (e.g., `AuthService/GetServerStatus`).
2. **Empty Database Case:** The backend queries the `users` table. If the user count is `0`, it returns `needs_setup: true`.
3. **Setup Redirection:** Upon receiving `needs_setup: true`, TanStack Router intercepts the navigation and redirects the client to the `/setup` onboarding route, bypassing the login form.
4. **Onboarding Submission:** The user fills in credentials for the primary administrator account (username/password) via a TanStack Form.
5. **Initial Admin Creation:** The form submits to `AuthService/SetupAdmin`.
   * **Security Safeguard:** The backend verification block validates that the `users` table is indeed empty. If a user already exists in the database, `SetupAdmin` aborts immediately and returns a `FailedPrecondition` / `PermissionDenied` RPC error to block malicious takeovers.
6. **Initialization Lock:** Once the admin is created successfully, subsequent calls to `GetServerStatus` return `needs_setup: false`, forcing all unauthenticated users to the regular login screen (`/login`).

---

## 5. Containerization & Process Management

To ensure standard delivery, compatibility, and process isolation, the application is packaged as a lightweight Docker container utilizing `s6-overlay` for service control.

### 5.1. Multi-Stage Dockerfile Layout
The build is optimized using a three-stage Dockerfile that separates dependencies and compiler environments, reducing rebuild durations:

* **Stage 1: Frontend Builder (`node:24-alpine`)**
  * Sets up the `pnpm` workspace environment.
  * Copies `package.json`, `pnpm-lock.yaml`, and other workspace dependency definitions.
  * Runs `pnpm install --frozen-lockfile` (utilizing Docker mount caches for the pnpm store to accelerate builds).
  * Copies source files, executes `pnpm build` to compile the optimized production React bundles into the `dist/` directory.
  * Integrates `pnpm check` (Oxlint + Oxfmt) verification to ensure all linting and formatting standards are met before compilation.

* **Stage 2: Backend Builder (`golang:alpine`)**
  * Configures CGO-free build parameters: `CGO_ENABLED=0`.
  * Configures multi-architecture environment parameters (`ARG TARGETOS` and `ARG TARGETARCH`) passed by Docker Buildx.
  * Copies `go.mod` and `go.sum`, executing `go mod download` to fetch and cache dependencies separately from source changes.
  * Copies the React production build (`dist/`) output from Stage 1 into the filesystem to allow embedding.
  * Copies backend source files.
  * Compiles the static, single-binary via:
    ```bash
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o dmanager .
    ```

* **Stage 3: Runtime Image (`alpine:latest`)**
  * Serves as the minimal runtime shell.
  * Installs `s6-overlay` process manager, downloading the release corresponding to the target platform architecture.
  * Installs essential utilities (like `ca-certificates` for registry HTTPS communication).
  * Copies the compiled `/usr/local/bin/dmanager` binary from Stage 2.
  * Copies the local repository's `rootfs/` configuration directory into `/` in the container.
  * Sets the container endpoint to `/init` (s6-overlay initialization framework).

### 5.2. s6-Overlay Process Supervision
* **PID 1 Orchestration:** s6-overlay serves as the init process (`ENTRYPOINT ["/init"]`).
* **Configurations (`rootfs/`):**
  * `rootfs/etc/s6-overlay/s6-rc.d/`: Defines service dependency mappings.
  * `rootfs/etc/s6-overlay/s6-rc.d/dmanager/run`: Script executing `dmanager serve --config /etc/dmanager/config.toml` as a supervised background daemon.
  * `rootfs/etc/s6-overlay/s6-rc.d/dmanager/type`: Set to `longrun`.
  * `rootfs/etc/s6-overlay/s6-rc.d/user/contents.d/dmanager`: Empty file marking `dmanager` as a component of the default user service group.
* **Signal Handling:** s6-overlay intercepts shutdown signals (`SIGTERM`, `SIGINT`) and gracefully terminates database connections and active registry checks before shutting down the container environment.

### 5.3. Deployment Host Access Requirement
* **Docker Socket Mount:** Because the `dmanager` runtime runs inside a container (Stage 3), it requires access to the host's daemon socket to perform container discovery and management.
* **Run Arguments:** The container must be run with the host socket mounted directly:
  ```bash
  docker run -d \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /path/to/data:/var/lib/dmanager \
    -p 9283:9283 \
    dmanager:latest
  ```

### 5.4. Release Distribution Pipeline
* **Trigger Event:** The workflow is triggered automatically on pushes of version tags matching `v*` (specifically semantic versions like `vX.Y.Z`).
* **Docker Buildx & Multi-Arch:** Leverages GitHub Actions with QEMU virtualization to build cross-platform Docker images for `linux/amd64` and `linux/arm64`.
* **GitHub Container Registry (GHCR):** Authenticates automatically using `GITHUB_TOKEN` and publishes the tagged images to `ghcr.io/${{ github.repository }}` under both the exact version tag and `latest`.
* **Automated GitHub Release:** Employs official release utilities (e.g., `softprops/action-gh-release` or similar) to create a GitHub Release. It generates release notes automatically to list changes made since the prior release.


### 6. System Logs Feature Design

#### 6.1. Backend System Log Buffering
* **In-Memory Ring Buffer:** Rather than utilizing slow, high-overhead disk or database writes, the backend records logs in a thread-safe circular in-memory buffer with a capacity of 1000 items.
* **Wrapping Slog Handler (`InterceptHandler`):** A custom logger handler wraps Go's structured `slog.Handler` to intercept every log event dynamically, capture its level, message, timestamp, and optional contextual metadata attributes, mapping them into standard protobuf schemas (`v1.LogEntry`) and adding them to the ring buffer.
* **Service RPC (`GetSystemLogs`):**
  * Implemented inside `LogService` on `/dmanager.v1.LogService/GetSystemLogs`.
  * Accepts `limit`, `level_filter`, and `search_query` parameters.
  * Responds with a chronologically sorted list of matches (newest first).

#### 6.2. Frontend System Logs Interface
* **TanStack Route (`/logs`):** A dedicated protected route rendered inside `DashboardLayout`.
* **Log Level Filtering & Text Search:** Allows filtering logs by severity (DEBUG, INFO, WARN, ERROR) and filtering messages using a text search input field.
* **Auto-refresh and Paging Controls:** Features manual triggers to reload list state and control display density.
* **Structured Context Viewer:** Supports viewing nested serialized metadata parameters (JSON) for complex log events (e.g., CLI operations, container upgrades, and request errors).


### 7. Settings & Gotify Notification Integration Design

#### 7.1. Database Configuration Storage
* **Settings Schema:** A new `settings` table is introduced to store global configuration options as key-value pairs to support flexible configurations.
* **Migration `00002_add_settings.sql`:**
  ```sql
  CREATE TABLE settings (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL,
      updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
  ```
  Default configuration entries (e.g. `gotify_url`, `gotify_token`) are initialized as empty values.

#### 7.2. Settings & Notification Service (ConnectRPC)
* **RPC Protocol Definitions (`SettingsService`):**
  * `GetSettings(GetSettingsRequest) returns (GetSettingsResponse)`: Retrieves the currently configured settings.
  * `UpdateSettings(UpdateSettingsRequest) returns (UpdateSettingsResponse)`: Saves/updates the settings values in the SQLite database.
  * `TestGotifyNotification(TestGotifyNotificationRequest) returns (TestGotifyNotificationResponse)`: Dispatches a mock notification to the specified Gotify server to verify authentication and connection settings.
  * `GetRegistryStatus(GetRegistryStatusRequest) returns (GetRegistryStatusResponse)`: Dynamic health check verifying connectivity and login credentials for all configured private registries.
* **Notification Agent:**
  * A background dispatcher listening to container and scheduler event triggers.
  * Dispatches standard Gotify POST request payloads (`title`, `message`, `priority`) to `<gotify_url>/message?token=<gotify_token>` for the following events:
    * **Image Update Found:** Dispatched at `normal` priority when the checker detects a newer image version in the registry.
    * **Update Check Failure:** Dispatched at `warning` or `high` priority if the scheduler fails to query the registry (e.g., registry down, network issues, or invalid credentials).
    * **Auto-update Success:** Dispatched at `normal` priority when the automatic re-deployment workflow completes successfully.
    * **Auto-update Failure:** Dispatched at `high` priority if the automated re-deployment workflow fails (e.g., container recreation fails, pull error).

#### 7.3. Frontend Settings Dashboard
* **TanStack Route (`/settings`):** Accessible via the sidebar "Settings" navigation button.
* **Settings Panel Form:**
  * Displays text fields for Gotify URL and Application Token.
  * Includes standard URL validation rules.
  * Includes a "Save Settings" button invoking `UpdateSettings` RPC.
  * Includes a "Test Gotify Connection" button invoking `TestGotifyNotification` RPC, rendering status to the user.

#### 7.4. Private Registry Status Panel
* **Registry Status Section:** A section embedded within the `/settings` page.
* **Metadata & Health Indicators:**
  * Renders a card list of all private registries configured on the backend.
  * Displays the host URL and username configuration details.
  * Performs parallel connectivity and authentication checks using the server's Docker Engine API client `RegistryLogin`.
  * Displays a colored visual badge for status (`Healthy` / `Configured / OK` in green, `Error / Unhealthy` in red with the descriptive connection error).
  * Includes a "Refresh Status" button to force-reload status.

#### 7.5. Settings Page Shell & Tabs (#192)
* **Root:** `flex` column with `gap: 24px` inside the intentional 800px form column (`padding: 24px; maxWidth: 800px; margin: 0 auto`) — the same gap-based rhythm as the Containers and Administration pages. The page header (icon + title) is a direct child with **no own bottom margin**; the root gap carries header → tabs → panel spacing.
* **Tabs:** rendered by the shared **`PageTabs`** component (§9.4) — `General` and `Security & Sessions` items against `/settings/$tab`. Optimistic local tab state via the item `onClick`; the route remains the source of truth (`useParams({ strict: false })`).
* **Panels:** the Security panel is a self-contained `flex` column `gap: 24px`; the General panel's cards are direct root children whose separation is the root gap (no inline `marginTop`).
### 8. Frontend Toast Notification System Design

#### 8.1. Toast Core Architecture
* **Toast Types:**
  * `info`: General informational messages (e.g., "Checking for updates...").
  * `success`: Successful action completions (e.g., "Container started successfully").
  * `warning`: Warnings or non-blocking issues (e.g., "No updates found").
  * `error`: Error messages from failed actions (e.g., "Failed to stop container").
* **State Management (Toast Context):** A global React Context (`ToastContext`) and custom provider (`ToastProvider`) managed by a custom hook (`useToast`). This provider maintains a list of active toast items, providing `toast.info`, `toast.success`, `toast.warning`, and `toast.error` methods. Each toast is auto-dismissed after a configurable timeout (default 5000ms).
* **Toast UI Component:**
  * Displays a glassmorphic style with matching theme colors and relevant Lucide icons (`CheckCircle2`, `AlertTriangle`, `Info`, `XCircle`).
  * Features a close button for immediate dismissal.
  * Rendered inside a fixed-position container at the top or bottom right of the page with high z-index.

#### 8.2. Integration & Interactions
* **Container Dashboard actions (`useContainers`):**
  * Start Container: Toasts info on request, success on start, and error on failure.
  * Stop Container: Toasts info on request, success on stop, and error on failure.
  * Upgrade Container: Toasts info/loading when upgrading starts, success on completion, and error on failure.
  * Toggle Auto Update: Toasts success/error when toggling.
  * Check Updates: Toasts info when starting checking, success when checking is complete (informing the user whether a new update was found or if the image is already up-to-date), and error on failure.
* **Settings Panel actions (`Settings.tsx`):**
  * Save Settings: Toasts success on save, error on failure.
  * Test Gotify Connection: Toasts info when starting test, success if connection succeeds, and error if it fails.

## 9. Administration Page Design (Images, Volumes, Networks)

A read-only Administration page exposing Docker host resource inventories. This section is the design source of truth for STORY-054 (backend), STORY-055 (frontend), and STORY-056 (images stat cards & default sorting).

### 9.1. Scope & Explicit Non-Goals
* **In scope:** Three tabs (Images, Volumes, Networks), each rendering a sortable, read-only TanStack table populated from the Docker Engine via the host socket.
* **Out of scope (this phase):** Any mutation — no image pull/remove/prune, no volume create/delete, no network create/disconnect, no per-row action buttons, and no streaming/live updates. Resources are fetched on demand. Actionable operations may be designed in a future phase.

### 9.2. Protocol & Backend Service
* **Proto:** New `proto/dmanager/v1/admin.proto` defining `AdminService` with three unary RPCs — `ListImages`, `ListVolumes`, `ListNetworks` — and their message shapes (full schema in [protocol.md](protocol.md) §3.5).
* **Service implementation:** New `internal/admin/service.go` following the `internal/container/service.go` constructor/DI pattern. The service wraps the shared moby SDK client (from `internal/docker.NewClient`) and performs:
  * `ImageList` with container-count enabled, mapping `image.Summary` → `Image` (id, repo_tags, created_unix, size_bytes, containers_count).
  * `VolumeList` mapping `volume.Volume` → `Volume` (name, driver, mountpoint, created_at, labels).
  * `NetworkList` mapping `network.Summary`/`network.Inspect` → `Network` (id, name, driver, scope, internal, created_at).
* **Registration & auth:** Handler registered in `cmd/serve.go` alongside the other services. All three procedures are classified in the Connect auth interceptor (`internal/auth/interceptor.go`) as **authenticated, any role** — identical policy to `ListContainers`. The interceptor's reflection test must keep 100% procedure coverage.
* **Error mapping:** Docker daemon failures surface as Connect `Unavailable`/`Internal` per the standard error mapping in [protocol.md](protocol.md) §4; no partial results are returned.
* **No persistence:** Results are never written to SQLite; every request queries the live Docker socket.

### 9.3. Frontend Routing & Navigation
* **Routes (`src/routes/router.tsx`):** Mirror the Settings route pair — an index route `/administration` whose `beforeLoad` redirects to `/administration/images`, and a `/administration/$tab` route that validates `tab ∈ {images, volumes, networks}` (invalid values redirect to `images`) with the standard auth/setup guards.
* **Sidebar (`DashboardLayout.tsx`):** New `menu-item` Link placed **between System Logs and Settings**, using the lucide `Boxes` icon, `to="/administration/$tab"` with `params={{ tab: "images" }}`, and `activeOptions={{ exact: false }}` so any tab highlights it. Resulting order: Containers, System Logs, Administration, Settings.

### 9.4. Page Component & Tabs
* **`src/components/Administration.tsx`:** Page shell mirrors the **Containers page** (`ContainerGrid.tsx`) layout vocabulary (#189); the tab bar is the shared `PageTabs` component (#192):
  - **Root:** `flex` column with `gap: 24px`, full width. No outer padding (DashboardLayout's `main-container` already pads — the previous shell double-padded) and no `maxWidth` cap: inventories use the same full-bleed width as the containers dashboard.
  - **Header:** `.dashboard-header` + `.header-title-section` — `h2` "Administration" plus a one-line subtitle describing the page — with the manual re-fetch as an accent `auth-submit-btn` labeled **Sync Now** (same vocabulary and in-flight spinner as the containers page; replaces the former secondary-style Refresh button).
  - **Tab bar:** the shared **`PageTabs`** component (`src/components/PageTabs.tsx`, #192) — an item-array API (`{ to, params, icon, label, active, onClick? }`) rendering TanStack `Link`s with the `page-tabs` / `page-tab` CSS classes (renamed from the Settings-named classes; the bar owns **no margin** — the root gap carries spacing). `active` is driven by the resolved tab, and tab state is synced from `useParams({ strict: false })` so browser back/forward and deep links work. Consumed by both Administration (§9.4) and Settings (§7.5).
  - **Stat cards & banner:** §9.6 cards and the error banner use the shared `stats-grid` / `auth-error-banner` classes with no inline margins; the root gap carries the vertical rhythm, matching the containers page.
* **Data hook:** `src/hooks/useAdminResources.ts` — one hook parameterized by resource kind; fetches on mount and tab activation, exposes `{ data, isLoading, error, refresh }`; the header **Sync Now** button triggers manual re-fetch. No polling/streaming.
* **Client:** `adminClient` added to `src/client.ts` via `createClient(AdminService, transport)`.

### 9.5. Resource Tables
Three table components follow the `ContainerTable.tsx` pattern: `ColumnDef<T>[]` defined outside the component, `useReactTable` with `getCoreRowModel` + `getSortedRowModel`, `SortingState` via `useState`, sortable headers using the `table-sort-btn` class with `ArrowUp`/`ArrowDown`/`ArrowUpDown` lucide icons, and cells rendered through `flexRender`. Consistent loading, empty, and error states across all three tables.

| Table | Columns | Notes |
| --- | --- | --- |
| `ImageTable.tsx` | Repository, Tag, Image ID, Size, Created, In Use | Repository/Tag split from the first `repo_tags` entry (`<none>` for dangling images); ID truncated to 12 chars with `title` tooltip; Size formatted human-readable (e.g. `142 MB`); Created rendered relative; In Use renders `containers_count`; **default sort: Size descending** |
| `VolumeTable.tsx` | Volume, Driver, Mountpoint, Created, Labels | Mountpoint truncated with `title` tooltip; Labels rendered as compact `key=value` chips, omitted when empty |
| `NetworkTable.tsx` | Network, ID, Driver, Scope, Internal, Created | ID truncated to 12 chars with `title` tooltip; Internal rendered as Yes/No; default Docker networks (`bridge`, `host`, `none`) styled like other recognized system rows |

* **No actions:** none of the tables define an actions column or per-row buttons — the read-only constraint is enforced at the component level, not just by omitting RPCs.
* **Testing:** `Administration.test.tsx` covers tab routing/validation, table rendering with mocked `adminClient` responses, column sorting, and empty/error states.
* **Default sort:** `ImageTable` opens sorted by Size **descending** (largest images first — disk-usage triage is the primary images workflow); `VolumeTable` and `NetworkTable` keep name ascending.

### 9.6. Images Summary Stat Cards

The Images tab shows a five-card summary strip between the tab bar and the table (STORY-065 added the last two, issue #200), following the `ContainerGrid.tsx` metrics pattern (derived client-side from the same `ListImages` response — **no protocol, backend, or RPC changes**).

| Card | Value | Derivation |
| --- | --- | --- |
| Total Space Used | `Σ size_bytes` (all images) | Sum of every image's size as reported by the daemon |
| Freeable Space | `Σ size_bytes` where `containers_count = 0` | Images not used by any container — the theoretical reclaim footprint |
| Images | image count | `images.length` |
| Unused | image count where `containers_count = 0` | Images not used by any container, tagged **and** untagged alike — the exact prune/delete scope (§9.7/§9.8), counted rather than byte-measured |
| Dangling | image count where `repo_tags` is empty **and** `containers_count = 0` | Untagged images **not referenced by any container** — exactly the set the §9.8 dangling-scope prune deletes (STORY-066 tightened this from STORY-065's tag-only derivation; #203) |

* **Unknown usage counts:** images with `containers_count = -1` (daemon did not calculate) are conservatively treated as **in use**, so Freeable Space, the Unused count, and the Dangling count never overstate what could be reclaimed (STORY-066 extended the rule to Dangling; #203).
* **Shared-layer approximation:** `size_bytes` includes layers shared between images, so both sums match summing the SIZE column of `docker image ls` and overstate uniquely-owned disk; the space actually freed by a future prune will be equal or lower. Sourcing unique sizes from `/system/df` may be designed in a later phase.
* **Styling:** reuses the existing dashboard CSS — `stats-grid` / `stat-card` / `stat-icon-wrapper` / `stat-value` / `stat-label` — with color modifiers `total` (blue), `updates` (amber), `stopped` (gray) and lucide icons `HardDrive`, `Recycle`, `Layers`, plus two one-line variants added in STORY-065: `unused` (amber — pairs with Freeable Space, the same scope counted vs. byte-measured, icon `PackageOpen`) and `dangling` (gray — untagged/inert, icon `TagX`; `TagOff` turned out not to exist in the pinned lucide-react version). The grid is `repeat(auto-fit, minmax(180px, 1fr))`, so five cards reflow natively — single row on wide viewports, wrapping below; the containers/logs strips are per-instance and unaffected.
* **States:** card byte values use `formatBytes` with one decimal at every magnitude (e.g. `142.6 MB`; `0 B` when empty); `stat-value` placeholders render `--` while the first fetch is in flight or on error; an empty inventory renders `0 B` / `0 B` / `0` / `0` / `0`. Table cells keep Docker CLI-style integer sizes.
* **Scope:** Images tab only — the Volumes and Networks tables stay unchanged.
* **Testing:** unit tests cover the derived values (tagged + dangling mix), the `containers_count = -1` conservative rule (a tagless `-1` image counts as neither Unused nor Dangling since STORY-066), formatting of large sums, and the loading placeholder.

### 9.7. Image Deletion — Actions Column

The images table gains a trailing **Actions** column with a delete control. This is the Administration surface's first mutating feature and the first admin-gated control on it; Volumes and Networks tables stay action-free.

**Backend.** `AdminService` gains `DeleteImage(DeleteImageRequest) → DeleteImageResponse` (§3.5 of [protocol.md](protocol.md)). The handler calls the moby SDK `ImageRemove(ctx, id, image.RemoveOptions{Force: force})`; the interceptor classifies the procedure as `RoleAdmin` like the other mutating RPCs. Daemon errors map per the existing conventions: unreachable daemon → `CodeUnavailable`, unknown image → `CodeNotFound`, in-use/tag conflict → `CodeFailedPrecondition` with the daemon's message surfaced; a 200 response is an empty payload — the client re-fetches `ListImages` for the authoritative state.

**Frontend gating (who can delete what).**

| Row state | Renders |
| --- | --- |
| `containers_count = 0` | Delete button (lucide `Trash2`, danger styling) |
| `containers_count > 0` | `—` (no control) |
| `containers_count = -1` (daemon did not calculate) | `—` (no control — conservative, consistent with §9.6) |
| viewer-role user | column still visible but buttons disabled with `title="Admin role required"` |

Client-side gating is a UX affordance only; the daemon re-checks at delete time and a conflict (e.g. a container started between list and click) surfaces as a `CodeFailedPrecondition` error.

**Confirmation — `ConfirmDialog` (danger).** Deleting an image is destructive at the daemon level (re-download required to undo), so the trash button opens the reusable `ConfirmDialog` (§11.4): title **Delete image?**, consequence-focused message naming the image (`repo:tag` + short ID + permanence), confirm verb **Delete**, `variant="danger"` so Cancel receives initial focus — Enter never pre-arms the deletion. While the RPC is in flight the dialog is `busy` (spinner on Delete, both buttons disabled, Esc/backdrop suppressed); when the outcome settles the dialog closes, and success/failure feedback flows through the existing toasts with a list re-fetch on success. Originally shipped as a two-step inline confirm (an interim decision from before the dialog system existed); migrated per #177 once §11 landed. The same system gates passkey deletion and other destructive operations (#178).

**In-flight & result UX.** `useAdminResources` grows `deleteImage(id)`: it tracks `deletingId` (one deletion at a time), shows the `Loader2` spinner in the armed button, and on success calls the existing `refresh()` — the table, empty state, and §9.6 stat cards all recompute from the fresh `ListImages` response (no optimistic removal; the daemon is the source of truth). Success and failure both report through the app's toast system (`ToastContext` / `useToast`, the same channel as container start/stop and passkey deletion): `toast.success("Image deleted successfully.")` on success and `toast.error("Failed to delete image: <daemon message>")` on failure — no banner state is kept. Other rows' buttons stay enabled but dispatching while a deletion is in flight is ignored.

**Force flag.** The frontend sends `force: true`: multi-tag unused images otherwise fail with tag-conflict errors for no user-legitimate reason, and the in-use protection the UI actually cares about is enforced by the daemon regardless of `force`.

**Testing.** Backend: httptest fake exercises removal (happy path, not-found, in-use conflict, daemon down) and the reflection test asserts `RoleAdmin` coverage for the new procedure. Frontend: button presence rules (0 / >0 / -1 / viewer), arm-then-confirm flow, arming reset timeout, per-row spinner, error-toast content, success-toast emission, and post-success `refresh()` re-render including stat-card recomputation.


### 9.8. Image Prune — Bulk Reclaim (issue #196)

§9.7 made individual unused images deletable; §9.8 makes the §9.6 **Freeable Space** stat actionable: one daemon call reclaims every image no container references (the daemon's in-use protection applies regardless of what the client sends). Research (#196): the moby SDK supports this natively — `client.ImagePrune(ctx, client.ImagePruneOptions{Filters}) → image.PruneReport{ImagesDeleted []DeleteResponse{Deleted, Untagged}, SpaceReclaimed uint64}` (Engine API `POST /images/prune`). Filter semantics (verified against the daemon source after the initial prune shipped: `GetBoolOrDefault("dangling", true)`): **when the filter is absent the daemon prunes dangling-only**; `dangling=false` = all unused images (what the CLI's `prune -a` sends); `dangling=true` = untagged only (the CLI's default). This story ships the all-unused scope.

**Backend.** `AdminService` gains `PruneImages(PruneImagesRequest) → PruneImagesResponse` (§3.5 of [protocol.md](protocol.md)). The handler always sends the `dangling` filter explicitly — `dangling=false` for the default scope (all unused), `dangling=true` when `dangling_only` is set — because the daemon's absent-filter default is dangling-only, which shipped in STORY-064 and silently narrowed the prune to untagged images. Unlike `DeleteImage`, the response **carries data**: the per-image report and the actual `SpaceReclaimed` — the toast reports the daemon's number, not a client-side estimate. Classified **RoleAdmin** (interceptor, same as `DeleteImage`). Error mapping degenerates: prune has no NotFound/Conflict cases, so daemon failure → `CodeUnavailable` only.

**Frontend — placement & gating.** A slim actions row sits between the §9.6 stats grid and the table (flex, right-aligned, 12px gap) so the cards keep their grid: two danger-styled buttons (lucide `Trash2`) mapping left→right onto the STORY-065 cards. **Prune Unused** (renamed from "Prune Unused Images" in STORY-066) covers the Unused card — `danglingOnly: false`, every image with zero referencing containers, tagged and untagged alike; **Prune Dangling** covers the Dangling card — `danglingOnly: true`, untagged images only. Both wire scopes existed in the RPC since STORY-064 (the UI simply never sent `true` before STORY-066); there are **no protocol or backend changes**. Gating, evaluated per render from the §9.6 stats:

| State | Renders |
| --- | --- |
| unused scope: `freeableBytes > 0`, admin | Enabled danger button |
| unused scope: `freeableBytes = 0` | Disabled, `title="No unused images to prune"` |
| dangling scope: `danglingCount > 0` and `danglingFreeableBytes > 0`, admin | Enabled danger button |
| dangling scope: `danglingCount = 0` | Disabled, `title="No dangling images to prune"` |
| viewer-role user | Both disabled, `title="Admin role required"` (same affordance-only gating as §9.7) |
| prune in flight | Both disabled; the in-flight button shows the `Trash2` spinner + "Pruning…" (one prune at a time) |

* **One set, two views.** Per the STORY-066 correction (#203), "dangling" means **untagged AND unused** everywhere in the product: the Dangling card, the Prune Dangling button, and the daemon's dangling-scope prune all describe the same set — images with no `repo_tags` that no container references. STORY-065 originally derived the card tag-only; STORY-066 tightens it so the card never shows an image the corresponding prune wouldn't delete (an untagged image in use by a container stays out). Uncalculated (`-1`) usage counts are excluded, the same conservative rule as `freeableBytes` — the dialog therefore states exactly what the client can predict, and the daemon's in-use protection is the final authority regardless.

**Confirmation — `ConfirmDialog` (danger).** One scope-driven instance (§11.4 system): the pending scope selects the copy — title **Prune unused images?** with message `Deletes all {unusedCount} unused images, reclaiming up to {freeableBytes}. Images in use are never touched.` or title **Prune dangling images?** with message `Deletes all {danglingCount} dangling images, reclaiming up to {danglingFreeableBytes}. Tagged images are never touched.` The size is an **upper bound**, hence *up to*: `SpaceReclaimed` only counts layer content that drops to zero references, and layers shared with retained images or held by the build cache are not freed (observed live: reproducible SOURCE_DATE_EPOCH rebuilds leave dangling images whose layers are all still referenced, so an honest daemon reports 0 B — STORY-066 field report). Confirm verb **Prune**, `variant="danger"` → Cancel has initial focus; `busy` while in flight (Esc/backdrop suppressed, spinner on Prune); closes on settle.

**Result UX.** `useAdminResources.pruneImages(danglingOnly: boolean)` tracks a `pruning` flag (single-flight — both buttons gate on it) plus a `pruningScope` (`"unused" | "dangling" | null`) naming which button spins: success → `toast.success("Reclaimed {size} from {count} unused images.")` / `…from {count} dangling images.` using the **daemon-reported** `space_reclaimed`; failure → `toast.error("Failed to prune images: <daemon message>")`. Both outcomes call `refresh()` — inventory, empty state, and stat cards recompute from the fresh `ListImages` response (the prune response reports what was deleted, the list remains the source of truth).

**Testing.** Backend: httptest fake asserting the `POST /images/prune` request shape and the report→proto mapping (deleted/untagged entries, space_reclaimed), plus daemon-down → `CodeUnavailable`; interceptor reflection test keeps 100% `RoleAdmin` coverage. Frontend: gating matrix above, confirm flow, busy lockout, toast contents (daemon-reported bytes on success), and post-settle `refresh()` recomputation.

### 9.9. Builder Cache — Tab, Stats & Prune (issue #206)

STORY-066's field verification showed where image-layer bytes actually live: BuildKit. This host's build cache held **33.54 GB across 634 records** while image prunes honestly reclaimed ~0 — the content stays referenced by cache records. §9.9 surfaces builder-owned space as its own Administration view and makes it actionable. Research verified the wire shapes against moby client v0.5.1 / api v1.55.0 and the live daemon.

**Tab.** `adminTabs` gains **Builder** between Images and Volumes (Containers → Images → Builder → Volumes → Networks; routed like the others, shared `PageTabs` bar). The tab mirrors the page dialect: root `flex` column `gap: 24px`, a stats strip, and an actions row — **no table**. Rationale from live data: of 634 records, 157 are 0 B and 262 are <1 MB (66% dust rows), while 64 records hold 90% of the bytes — the actionable unit is the aggregate, not individual records. Per-record deletion (buildkit prune accepts `id`/`until`/`type` filters) and a size-sorted "top offenders" view are the natural v2 drill-downs if wanted.

**Backend.** Two new `AdminService` procedures (§3.5 of [protocol.md](protocol.md)): the read follows the service's read convention — any authenticated role, same as `ListImages` — while the mutation is **RoleAdmin** (implementation correction of the design's initial both-admin draft).

- `GetBuildCacheStats` → `GetBuildCacheStatsResponse{total_bytes, reclaimable_bytes, record_count, active_count}` — proxies `Client.DiskUsage(ctx, DiskUsageOptions{BuildCache: true})` (`GET /system/df?type=build-cache`; the hyphen matters — `type=buildcache` is rejected by the daemon). The daemon supplies the aggregates (`BuildCacheUsage{TotalSize, Reclaimable, TotalCount, ActiveCount}`) — no client-side summation. `Reclaimable` excludes records whose blobs are shared with other records.
- `PruneBuildCache(PruneBuildCacheRequest{all})` → `PruneBuildCacheResponse{caches_deleted, space_reclaimed}` — proxies `Client.BuildCachePrune(ctx, BuildCachePruneOptions{All})` (`POST /build/prune`). `all=false` is the UI's only scope and preserves buildkit-internal cache types (here: 10 of 634 records); records in active use (`InUse`) are never removed, enforced daemon-side. The response carries `space_reclaimed` and the deleted-record count; per-record IDs are opaque buildkit hashes and are not shipped.

**Cards.** A three-card strip, the Containers-page pattern (reusing `stats-grid` / `stat-card` and the existing `total` blue / `updates` amber / `stopped` gray modifiers — zero new CSS): **Build Cache** (`total_bytes`, icon `Database`, blue), **Reclaimable** (`reclaimable_bytes`, icon `Recycle`, amber — the gating number), **Records** (`record_count`, icon `Layers`, gray). Card failure renders `--` independently: builder stats load in their own state slice, and a builder failure must not take the Images tab's banner path down.

**Controls & confirmation.** An actions row (same pattern as §9.8) holds **Prune Build Cache** (danger `Trash2`, `.prune-btn`). Gating: disabled when `!isAdmin` (`title="Admin role required"`), when `reclaimable_bytes = 0` (`title="No build cache to prune"`), or while a prune/deletion is in flight (joins the single-flight rule). The scope-driven `ConfirmDialog` gains its arm: title **Prune build cache?**, message `Deletes {record_count} build cache records, reclaiming up to {reclaimable_bytes}. Future image builds will be slower until the cache is rebuilt.` — the rebuild cost is the real risk and is stated as such; image prune's in-use protection has no analogue here (nothing running uses cache records, which is exactly why they're deletable). Confirm verb **Prune**, Cancel focus, busy lockout — unchanged.

**Result UX.** Success toast: `Reclaimed {size} from {count} cache records.` using the **daemon-reported** `space_reclaimed` (which may exceed `reclaimable_bytes` when a prune frees shared families together — the toast is the truth, the dialog was the estimate); failure mirrors §9.8. Both outcomes re-fetch builder stats and the image inventory.

**Testing.** Backend: httptest fake for `GET /system/df?type=build-cache` (aggregate mapping incl. the zero-records case) and `POST /build/prune` (query `all` flag, report mapping, daemon-down → `CodeUnavailable`); interceptor reflection keeps 100% `RoleAdmin` coverage. Frontend: card rendering + independent failure, gating matrix, confirm flow with the rebuild-cost copy, toast contents, refresh recomputation, and single-flight interaction with the image prunes.


### 9.10. Builder Records Drill-Down — Table & Per-Record Prune (issue #209)

STORY-067 shipped the aggregate; live usage then showed the drill-down is worth its own pass: 64 records hold 90% of the bytes and the top offender is a 4.8 GB `exec.cachemount`. §9.10 adds the size-sorted records view and per-record deletion to the Builder tab — the "top offenders" v2 flagged in §9.9.

**Wire facts** (verified against moby daemon source, v28.5.2 — stable through 29): `/build/prune` validates `id` among its cache filters and converts it to buildkit `id~=<value>` (prefix match) — sending the full record ID targets exactly one record. `all=1` combined with the `id` filter is deterministic: the filter already restricts candidates to that record; `all` only lifts the internal-type restriction so an explicitly targeted `exec.cachemount`/`source.local` record can be deleted. Blast radius stays 1. `InUse` records are daemon-protected — the prune returns an empty report rather than deleting. The response's `CachesDeleted`/`SpaceReclaimed` carry daemon truth.

**Backend.** Two new `AdminService` procedures (§3.5 of [protocol.md](protocol.md)), read any role / mutate admin per the §9.9 split:

- `ListBuildCacheRecords` → `ListBuildCacheRecordsResponse{repeated BuildCacheRecord records}` — the **same daemon call as stats** (`GET /system/df?type=build-cache`, whose `Items []CacheRecord` we already receive) mapped per record: `id`, `type`, `description`, `size_bytes`, `in_use`, `shared`, `usage_count`, `created_at`, optional `last_used_at`. **Sorted server-side by size desc**: dust (66% of records ≤1 MB) sinks naturally and the client renders top-offenders-first without sort controls. The drill-down is the Builder tab's own fetch slice so a records failure never hides the cards.
- `PruneBuildCacheRecord{id}` → `PruneBuildCacheRecordResponse{caches_deleted, space_reclaimed}` — `POST /build/prune` with `all=1` + `filters={"id":[id]}` (rationale above). Empty/blank `id` → `CodeInvalidArgument` before the daemon is touched; daemon-down → `CodeUnavailable`, mirroring §9.9.

**Table.** A records section under the actions row, reusing the admin table vocabulary (`.table`, header row, right-aligned numeric column): **Size** (desc) · **Type** (mono chip) · **Description** (truncated, `title` tooltip — e.g. `exec mount /bin/sh …`) · **Last used** (relative time or `never`) · **Used** (`usage_count`) · chips **In use** / **Shared** · row delete (danger icon `Trash2`). Row identity = full record ID; no pagination (634 rows render fine; size-desc makes the top the story).

**Controls & confirmation.** Row delete is disabled when `!isAdmin` (`title="Admin role required"`), when the record is `in_use` (`title="Record is in use"`), or while any builder prune is in flight. Per-record `ConfirmDialog` (separate state arm from §9.8/§9.9's scope dialog): title **Delete cache record?**, message `Deletes build cache record {shortId} ({size}). Shared blob content may free less. Rebuilding this step will be slower until the cache is rebuilt.` Confirm verb **Delete**, Cancel focus, busy lockout. One destructive builder op at a time: the record prune joins the aggregate prune's single-flight guard.

**Result UX.** Success toast: `Deleted {count} cache records, reclaimed {size}.` (daemon-reported; `count` can be 0 when the daemon protected the record — the toast then says `0 records` honestly). Both stats and records re-fetch on settle.

**Testing.** Backend: records mapping/sort/timestamps + empty-list; prune-record wire assertions (filters JSON `id` value, `all=1`), report mapping, empty-id rejection, daemon-down. Frontend: table render with live-scale rows, gating matrix (viewer / in-use / busy), confirm flow with the short-ID copy, toast, and refresh recomputation.

### 9.11. Volume Usage On Demand — Sizes, Reclaim & Count (issue #212)

The Volumes tab has been read-only since §9.5. Two gaps: volume sizes are invisible (the daemon's volume list carries no size field at all) and unused volumes have no reclaim path. Both fixable in one story — **if** the daemon cost is respected, because volume sizes are uniquely expensive to know.

**Wire facts** (verified against moby daemon source, v28.5.2 — stable through 29): `GET /system/df?type=volumes` computes each local volume's size via `directory.Size` — a **full recursive walk of every volume's directory tree, executed serially** in a loop. There is **no daemon-side cache**: a singleflight guard deduplicates concurrent calls, but every new call walks again. With one large database volume the walk alone can take seconds, which is why the volume list (`GET /volumes`) ships metadata only. Two consequences drive the design: (1) sizes must never be fetched on the tab-open path, and (2) `RefCount` — which is cheap (in-memory container store counting config references from running **and** stopped containers) — is only available inside the same df response, next to each size. Sizes reported `-1` mean the walk failed for that volume. `/volumes/prune` removes only volumes **not referenced by any container config at prune time** (local driver, no mount options) and answers `PruneReport{VolumesDeleted []string, SpaceReclaimed uint64}` — daemon-protected volumes are simply absent from the report.

**Principle: measurement is strictly opt-in.** Nothing in this story fetches usage automatically — not on tab open, not on refresh, not in any auto-effect. The tab-open cost profile is unchanged from §9.5.

**Backend.** Two new `AdminService` procedures (§3.5 of [protocol.md](protocol.md)), read any role / mutate admin per the §9.9 split:

- `GetVolumeUsage` → `GetVolumeUsageResponse{repeated VolumeUsage volumes, total_size_bytes, reclaimable_bytes, unused_count}` with `VolumeUsage{name, size_bytes, ref_count}` — one `DiskUsage{Volumes: true}` daemon call, mapped by volume name; `size_bytes: -1` passes through (the client renders an em dash) and a nil `UsageData` maps as unknown. The aggregates are computed server-side from the same response (`reclaimable_bytes` = sum of sizes where `ref_count == 0`; `unused_count` = that set's cardinality) so clients never re-derive them. The walk failure mode is per-volume (`-1`), not per-request: one unreadable volume does not fail the RPC.
- `PruneVolumes` → `PruneVolumesResponse{volumes_deleted, repeated string names, space_reclaimed}` — one `VolumesPrune` daemon call, no filters (the daemon's unused scope is fixed: unreferenced at prune time). Daemon-down → `CodeUnavailable`, per convention.

**UI.** The Volumes tab gains a stats row, an actions row, and a Size column — but only the card and the buttons exist from the start:

- **Stats card:** `Volumes` — total count from the existing cheap list (§9.5). No usage data involved.
- **Actions row, two buttons:**
  - **Calculate sizes** (any role — a read, `ListImages` precedent): fires `GetVolumeUsage` once per click; spinner in-button while measuring; the row it populates is the table's Size column.
  - **Reclaim space** (admin, `Trash2` affordance parity with §9.8): arms the volume-prune `ConfirmDialog`. Disabled only while a prune or measurement is in flight — without measurement the scope is genuinely unknown, and the daemon protects referenced volumes regardless, so the button stays enabled; the dialog copy carries the honesty instead.
- **Size column:** `—` until measured; `formatBytes` once measured; `—` for `-1` (walk failure). No sort integration — sizes are a snapshot, and sorting an unmeasured column is incoherent; rows keep the list order (§9.5).
- **ConfirmDialog** (the scope dialog's fourth arm): title **Delete unused volumes?**, message when measured: `Deletes {unused_count} unused volumes, reclaiming up to {reclaimable_bytes}. A volume is unused only when no container — running or stopped — references it. This cannot be undone.` — and when not measured: `Size has not been calculated yet — use Calculate sizes for a preview. Deletes all unused volumes. A volume is unused only when no container — running or stopped — references it. This cannot be undone.` Confirm verb **Delete** (more honest than "Prune" for data-bearing volumes), danger variant, Cancel focus, busy lockout.
- **Result UX:** toast `Reclaimed {size} from {count} unused volumes.` — daemon-reported (`PruneReport`), never the preview. **If sizes had been calculated before the prune, `GetVolumeUsage` re-runs automatically** on settle (the user opted into measurement once; the second walk validates the reclaim against reality). If they had not, nothing re-fetches.

**Testing.** Backend: mapping (size, `-1` passthrough, nil `UsageData`, ref counts), aggregate computation, `type=volumes` query assertion on the fake daemon, prune wire call + report mapping (names preserved), daemon-down. Frontend: tab open fires no usage RPC (regression guard); Calculate sizes populates the column (`—` before, bytes after, `—` on `-1`); viewer can measure but not reclaim (`title="Admin role required"`); dialog copy switches between measured/not-measured arms; confirm flow (`{}` on the wire), toast contents, post-prune re-measure only-when-previously-measured; the count card renders from the list without any usage call.

---

## 10. Engine Status Indicator (Sidebar, issue #180)

### 10.1. Scope & Problem

The sidebar footer renders a `server-status-pill` that is hardcoded markup: a `status-dot` span and the literal text "Engine online" (`DashboardLayout.tsx`). It never reflects reality. This design replaces it with a real connectivity indicator driven by a new lightweight health RPC.

**Non-goals:** no retry/health logic beyond the poll, no per-page banners (the Administration list banner already covers page-level errors), no toasts on transitions (background noise), no WebSocket/streaming push — a 30 s poll is plenty for a status dot.

### 10.2. Protocol & Backend

New procedure in `AdminService` (the established home for daemon-adjacent read procedures; a separate health service is not warranted for one RPC):

```protobuf
rpc CheckEngine(CheckEngineRequest) returns (CheckEngineResponse);

message CheckEngineRequest {}
message CheckEngineResponse {
  bool connected = 1;    // true when the daemon answered the ping
  string api_version = 2; // e.g. "1.51" — daemon API version from ping headers
  string error = 3;       // short reason when connected is false, empty otherwise
}
```

**Status, not error.** The handler calls moby `client.Ping` (`GET /_ping`) with a ~5 s context timeout. On success it returns `connected: true` plus the API version the daemon advertised. On **any** daemon failure it still succeeds — `connected: false` with the daemon error's short message — because the outage is the answer, not an RPC failure. This is a deliberate deviation from the daemon-error → `CodeUnavailable` convention used by every other procedure, and it is what lets the frontend distinguish the two offline causes:

| Observation | Pill | Tooltip |
| --- | --- | --- |
| `connected: true` | online (green, current look) | "Docker Engine API v{api_version}" |
| `connected: false` | no connection (red) | daemon error message |
| Connect transport error | no connection (red) | "Backend unreachable" |

**Role:** `RoleViewer` (authenticated, any role) — same tier as the list procedures. The pill only exists inside the authenticated layout, so nothing polls pre-auth; extending the unauthenticated `GetServerStatus` was rejected because it would leak daemon connectivity to anonymous callers with no benefit.

### 10.3. Frontend — Hook & Pill

**`useEngineStatus` hook** (`frontend/src/hooks/useEngineStatus.ts`): exposes `{ status, detail }` where `status" is `"checking" | "online" | "offline"`.

* Initial fetch on mount (status starts `"checking"`).
* `setInterval` poll every **30 s**; skipped while `document.hidden`.
* Immediate re-check on `visibilitychange` → visible and on `window` `focus` (covers a laptop sleep/wake cycle far better than a fixed cadence).
* Connect errors (backend down) map to `offline` with detail "Backend unreachable" — same visual, honest tooltip.
* Timers and listeners cleaned up on unmount; in-flight responses from a previous mount are ignored.

**`DashboardLayout` pill** — same DOM shape, now driven:

| Status | Dot | Text | Notes |
| --- | --- | --- | --- |
| `checking` | gray, no glow | "Checking…" | first load only; resolves within seconds |
| `online` | green + glow (existing `.status-dot`) | "Engine online" | `title` shows API version |
| `offline` | red + red glow | "No connection" | `title` shows daemon error or "Backend unreachable" |

CSS delta is three modifiers on the existing class: `.status-dot` keeps its green pulse for online, `.status-dot.checking` is static gray, `.status-dot.offline` is red (soft glow, no animation — a dead indicator should not pulse). The pill gets `role="status"` + `aria-live="polite"` so screen readers announce transitions without stealing focus.

### 10.4. Testing

* **Backend:** httptest fake for `/_ping` — healthy response (assert `connected` + `api_version` propagation), daemon error (assert `connected: false` + message, **and that no Connect error is returned**), and the interceptor reflection test picks up the new `RoleViewer` classification automatically.
* **Frontend:** hook tests with fake timers — initial `checking` → `online`, daemon-down → `offline` with message, transport error → "Backend unreachable", poll fires at 30 s and is skipped while hidden, focus/visibility triggers an immediate re-check, unmount stops the interval. `DashboardLayout` tests render the three states and assert the aria-live region and titles.


---

## 11. Dialog System (issue #176)

### 11.1. Scope & Problem

The app has **zero modal/dialog infrastructure**: every interaction is inline (Settings forms, the STORY-057 two-step inline confirm). Destructive actions lack a proper blocking confirmation surface — passkey deletion today fires on a single click with no confirmation at all — and there is no component to host future flows (resource details, forms, wizards).

This story ships the **primitive only**:

1. `frontend/src/components/Dialog.tsx` — a reusable modal: overlay, focus trap, Esc-to-close, click-outside-to-close, `role="dialog"` + `aria-modal`, focus restore, body scroll lock, hand-rolled styling consistent with the design system.
2. `frontend/src/components/ConfirmDialog.tsx` — a specialization: title, message, confirm/cancel buttons, `danger` variant, `busy` lockout.

**Non-goals (out of scope here):** migrating the image-delete inline confirm (#177, STORY-060), gating passkey deletion and other destructive operations (#178, STORY-061), nested/stacked dialogs (a single modal at a time is the contract; nothing in the backlog needs stacking), a toast/notification system (already exists, §8), and any new dependency — the primitive is hand-rolled like every other component, styled in `frontend/src/index.css`.

### 11.2. State Model — Declarative Component, Not an Imperative Hook

Two viable shapes were considered:

- **Imperative context** (`useDialogs()` returning `confirm({...}): Promise<boolean>`, mirroring `useToast`): rejected. The identified consumers confirm an action **tied to component state** — a specific row ID plus its in-flight spinner (`useAdminResources`' `deletingId` pattern). A promise-based API has to park that promise while the mutation runs, cannot express "confirm button shows a spinner" without extra plumbing, and implies a global queue/stacking model we deliberately do not need.
- **Declarative component** (`<ConfirmDialog open={target !== null} busy={deletingId === target?.id} …>`) — **chosen**. The caller owns what is being confirmed; open/close is plain state; in-flight and disabled states compose with existing `useState` patterns; tests render the two states directly.

The contrast with toasts (§8) is intentional and documented here so it is not "fixed" later: toasts are fire-and-forget *global ephemera* (imperative fits), dialogs are *modal and owned by one screen's state* (declarative fits).

Rendering: each `Dialog` portals itself to `document.body` when open (`createPortal`) and renders `null` when closed — no always-mounted provider, no open-dialogs registry. The overlay sits at `z-index: 1000`, **below** the toast container (9999): a success toast confirming a dialog-initiated action must stay visible while the dialog closes.

### 11.3. `Dialog` Primitive — Contract

```tsx
interface DialogProps {
  open: boolean;
  onClose: () => void;              // every dismiss path (Esc, backdrop) funnels here
  title: string;                    // aria-labelledby (id via useId)
  description?: string;             // aria-describedby
  children?: React.ReactNode;       // body content
  footer?: React.ReactNode;         // action buttons row
  initialFocus?: "confirm" | "cancel"; // which footer button gets focus on open (default "cancel")
}
```

Behavior, all implemented in the component (no library):

- **On open:** save `document.activeElement` (the opener button — e.g. a table's delete icon) for restore; add a scroll-lock class to `<html>` (`overflow: hidden`); render the portal; focus the `initialFocus` element.
- **Focus trap:** a `keydown` handler wraps Tab/Shift+Tab through the card's focusable descendants (`a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])`), first ↔ last.
- **Dismiss:** Escape key and a `mousedown` whose target is the overlay itself (not the card — clicking inside never dismisses) call `onClose`.
- **On close or unmount:** restore focus to the saved opener, remove the scroll lock, detach listeners.

DOM and CSS (new block in `index.css`, using existing tokens):

```html
<div class="dialog-overlay">          <!-- fixed inset 0, rgba(0,0,0,0.5) backdrop, centered flex -->
  <div class="dialog-card" role="dialog" aria-modal="true"
       aria-labelledby="<useId>" aria-describedby="<useId, when description>">
    <h2 class="dialog-title">…</h2>
    <p class="dialog-message">…</p>   <!-- optional -->
    …children…                        <!-- footer slot rendered last -->
    <div class="dialog-footer">…</div>
  </div>
</div>
```

`.dialog-card` reuses `--card-bg`, `--border`, `--shadow`, ~420px `max-width`; footer buttons reuse the existing button styling (`Cancel` secondary, confirm variant per 11.4). A short `dialogIn` keyframe (fade + slight scale) animates the card in; `@media (prefers-reduced-motion: reduce)` disables it.

### 11.4. `ConfirmDialog` Specialization

```tsx
interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;               // cancel / dismiss
  onConfirm: () => void;             // confirm button only
  title: string;
  message: string;                   // required here: state the consequence, not the request
  confirmLabel?: string;             // default "Confirm" — always a verb ("Delete", "Revoke")
  cancelLabel?: string;              // default "Cancel"
  variant?: "default" | "danger";    // danger → confirm button uses --error tokens
  busy?: boolean;                    // in-flight mutation
}
```

- **`busy` locks the dialog:** confirm button shows the `Loader2` spinner (same pattern as the ImageTable inline confirm) and is disabled; Cancel is disabled; Esc/backdrop dismissal is suppressed (the RPC is already running — closing mid-flight would desync the spinner; the result arrives via toasts and the caller's `onClose`).
- **Initial focus:** Cancel for `variant="danger"` (the safe action gets focus — pressing Enter immediately would confirm; we never pre-arm destruction), Confirm for `variant="default"`.
- Consumers own their state: `open={target !== null}`, close handlers null the target. This is exactly the shape #177 and #178 need.

**Consumers (as shipped).** Image deletion (§9.7, #177) plus the three Settings destructive actions (#178): passkey deletion (*Delete passkey?* — warns that a lone remaining credential locks the user out), single-session revocation (*Revoke session?* — names the device, notes it can sign in again), and revoke-all-others (*Revoke other sessions?*). Each is a danger dialog driven by one `pendingDestructive` state, `busy` bound to the existing per-action in-flight flag; non-destructive mutations (container start/stop, passkey rename, settings save) stay unconfirmed to avoid prompt fatigue.

### 11.5. Testing

- **`Dialog.test.tsx`** (jsdom): renders nothing when closed; renders `role="dialog"` with `aria-modal`, `aria-labelledby`/`aria-describedby` pointing at real nodes; Esc and backdrop `mousedown` dismiss while a click inside the card does not; Tab focus wraps first ↔ last and never escapes the card; focus is restored to the opener element on close; the scroll-lock class is applied while open and removed on close/unmount.
- **`ConfirmDialog.test.tsx`**: renders title/message/labels; `danger` marks the confirm button with the danger class; confirm and cancel fire their callbacks (cancel via `onClose`, button and Esc both); `busy` disables both buttons, blocks Esc/backdrop dismissal, and shows the spinner.
- No consumer is migrated in this story — #177/#178 carry their own behavior-preserving migrations with tests updated from the inline confirm to the dialog.
