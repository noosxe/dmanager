# Missing Tests Checklist

This document tracks unit and integration tests that are missing and need to be implemented in a future session.

## 1. Frontend Test Checklist (React / Vitest)

### 1.1. Core Components
- [ ] **[ContainerGrid.tsx](file:///home/mechsoull/Projects/dmanager/frontend/src/components/ContainerGrid.tsx)**
  - [ ] Render container card grid containing container state, names, images, and port mappings.
  - [ ] Verify state-based rendering (e.g. running, stopped, upgrading/updating).
  - [ ] Test text search filtering (filtering by container name or image tag).
  - [ ] Test action buttons (click start/stop triggers corresponding ConnectRPC requests).
  - [ ] Test upgrade banner visibility and click trigger behavior.
- [ ] **[LogsDrawer.tsx](file:///home/mechsoull/Projects/dmanager/frontend/src/components/LogsDrawer.tsx)**
  - [ ] Assert drawer renders correctly when triggered (slide-over / visibility toggle).
  - [ ] Verify `xterm.js` terminal instance mounts properly.
  - [ ] Mock the ConnectRPC logs stream and verify that incoming log lines write to the terminal.
  - [ ] Test closing drawer terminates/cancels the active stream.
- [ ] **[DashboardLayout.tsx](file:///home/mechsoull/Projects/dmanager/frontend/src/components/DashboardLayout.tsx)**
  - [ ] Assert sidebar links, profile metadata, and logo render correctly.
  - [ ] Test toggle operations (mobile sidebar menus, sidebar collapse).
  - [ ] Assert logout button click triggers sign-out routine and calls API.
- [ ] **[Setup.tsx](file:///home/mechsoull/Projects/dmanager/frontend/src/components/Setup.tsx)**
  - [ ] Verify validation constraints (missing username, short password).
  - [ ] Test successful admin registration form submit, API call mapping, and onboarding flow.

### 1.2. React Hooks & Router
- [ ] **[useAuth.tsx](file:///home/mechsoull/Projects/dmanager/frontend/src/hooks/useAuth.tsx)**
  - [ ] Assert user state loading and validation flow.
  - [ ] Test saving session token to local storage and deleting it on logout.
- [ ] **[useContainers.ts](file:///home/mechsoull/Projects/dmanager/frontend/src/hooks/useContainers.ts)**
  - [ ] Assert stream subscription initialization.
  - [ ] Verify correct patching of hook's state when new events arrive from the SSE/Connect stream.
  - [ ] Test reconnection behavior/interval when stream fails.
- [ ] **[router.tsx](file:///home/mechsoull/Projects/dmanager/frontend/src/routes/router.tsx)**
  - [ ] Assert access control rules (routing unauthenticated users to `/login` and redirecting authenticated users away from `/login`).

---

## 2. Backend Test Checklist (Go / `testing` + `mockery`)

### 2.1. ConnectRPC Middleware & Interceptors
- [ ] **[interceptor.go](file:///home/mechsoull/Projects/dmanager/internal/auth/interceptor.go)**
  - [ ] **[WrapUnary](file:///home/mechsoull/Projects/dmanager/internal/auth/interceptor.go#L55)**:
    - [ ] Request without cookie to a protected endpoint returns `Unauthenticated` error.
    - [ ] Request to bypassed endpoint (`Login`, `SetupAdmin`, `GetServerStatus`) is allowed without cookie.
    - [ ] Request with valid session propagates user context and session ID down the chain.
    - [ ] Request with expired session is rejected, and session database deletion is triggered.
  - [ ] **[WrapStreamingHandler](file:///home/mechsoull/Projects/dmanager/internal/auth/interceptor.go#L81)**:
    - [ ] Test streaming session validation (authenticated stream vs. unauthenticated stream).
  - [ ] **[WrapStreamingClient](file:///home/mechsoull/Projects/dmanager/internal/auth/interceptor.go#L77)**:
    - [ ] Cover client stream interception wrapper (currently 0% coverage).

### 2.2. Database Operations & Helper Functions
- [ ] **[db.go](file:///home/mechsoull/Projects/dmanager/internal/db/db.go)**
  - [ ] Test **[WithTx](file:///home/mechsoull/Projects/dmanager/internal/db/db.go#L27)** transaction wrapper (successful execution commits transaction; error inside callback rollbacks transaction).
- [ ] **[open.go](file:///home/mechsoull/Projects/dmanager/internal/db/open.go)**
  - [ ] Test **[Open](file:///home/mechsoull/Projects/dmanager/internal/db/open.go#L11)** helper connection behavior and driver parameters.
- [ ] **[sessions.sql.go](file:///home/mechsoull/Projects/dmanager/internal/db/sessions.sql.go)**
  - [ ] Assert **[PurgeExpiredSessions](file:///home/mechsoull/Projects/dmanager/internal/db/sessions.sql.go#L66)** removes rows where `expires_at` has passed.
- [ ] **[containers.sql.go](file:///home/mechsoull/Projects/dmanager/internal/db/containers.sql.go)**
  - [ ] Assert **[GetUpgradableContainers](file:///home/mechsoull/Projects/dmanager/internal/db/containers.sql.go#L71)** returns rows where `update_available = 1` and `auto_update = 1`.
  - [ ] Assert **[MarkContainerUpgraded](file:///home/mechsoull/Projects/dmanager/internal/db/containers.sql.go#L158)** flags the container correctly.

### 2.3. Docker Client Setup
- [ ] **[client.go](file:///home/mechsoull/Projects/dmanager/internal/docker/client.go)**
  - [ ] Unit test **[NewClient](file:///home/mechsoull/Projects/dmanager/internal/docker/client.go#L8)** with mock host parameters to verify client initialization.

---

## 3. Unimplemented Service (Log Centralized Ingestion)

- [x] **[LogService](file:///home/mechsoull/Projects/dmanager/proto/dmanager/v1/log.proto#L7)**
  - [x] Create Go backend implementation of the ConnectRPC [LogServiceHandler](file:///home/mechsoull/Projects/dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect/log.connect.go#L76).
  - [x] Implement the [SyncLogs](file:///home/mechsoull/Projects/dmanager/proto/dmanager/v1/log.proto#L9) RPC to ingest frontend client logs, format them, and write them into the Go backend structured logger `log/slog` containing the attributes `source: frontend`, `client_level`, and `client_timestamp`.
  - [x] Write backend unit tests for `SyncLogs` ensuring payloads are processed, logged, and return counts.
  - [x] Register `LogService` in [serve.go](file:///home/mechsoull/Projects/dmanager/cmd/serve.go).
