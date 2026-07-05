# Missing Work Checklist

This document tracks frontend, backend, and background-related features and integrations that are missing from the current implementation and need to be completed in a future session.

## 1. Backend Missing Work Checklist (Go / ConnectRPC)

### 1.1. Service Implementations
- [ ] **[LogServiceHandler](file:///home/mechsoull/Projects/dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect/log.connect.go#L76)** (or [log.proto](file:///home/mechsoull/Projects/dmanager/proto/dmanager/v1/log.proto))
  - [ ] Implement the `SyncLogs` method to ingest frontend client logs, parse/format them, and log them into the structured `log/slog` output with attributes `source: frontend`, `client_level`, and `client_timestamp`.
  - [ ] Register `LogService` via `NewLogServiceHandler` in the server bootstrapper [serve.go](file:///home/mechsoull/Projects/dmanager/cmd/serve.go).
- [x] **[ContainerServiceHandler](file:///home/mechsoull/Projects/dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect/container.connect.go#L64)** (in [service.go](file:///home/mechsoull/Projects/dmanager/internal/container/service.go))
  - [x] Implement the `SetContainerAutoUpdate` method to persist a container's auto-update settings in the SQLite database and publish the sync event to container streams.
  - [x] Implement the `CheckContainerUpdates` method to trigger an immediate, out-of-band registry check for a specific container image and update its database state.

### 1.2. Background Schedulers & Daemons
- [x] **Periodic Registry Update Checker** (in a new file under [internal/container/](file:///home/mechsoull/Projects/dmanager/internal/container))
  - [x] Implement a long-running background supervisor/loop utilizing a `time.Ticker` initialized at startup.
  - [x] Check registry tag digests for all containers according to the configured `scheduler.interval_minutes`.
  - [x] Utilize optional credentials stored in the `registries` block configuration for private repository authentication.
  - [x] Compare digests and set `update_available = 1` in the database when a newer tag is found.
- [x] **Automated Container Re-Deployment Workflow** (in [upgrade.go](file:///home/mechsoull/Projects/dmanager/internal/container/upgrade.go) or a new file)
  - [x] Implement the automated check-and-deploy path for containers with auto-update enabled.
  - [x] When a newer image is detected, pull the updated image from the registry.
  - [x] Retrieve exact execution parameters of the target container (environment variables, mounts, networks, labels, ports, configs).
  - [x] Stop and remove the old container resource.
  - [x] Recreate the container with the original parameters and the new image version, and start it.

## 2. Frontend Missing Work Checklist (React / TypeScript)

### 2.1. Client-Side Logging Framework
- [ ] **Dexie.js Buffer Setup** (in new logging service files under [frontend/src/](file:///home/mechsoull/Projects/dmanager/frontend/src))
  - [ ] Install `dexie` and configure IndexedDB to store client-side warnings, errors, uncaught exceptions, and user action events locally.
- [ ] **Browser Idle-Time Syncer** (in new sync files under [frontend/src/](file:///home/mechsoull/Projects/dmanager/frontend/src))
  - [ ] Implement an idle syncer utilizing `requestIdleCallback` (with standard timer fallbacks) to process IndexedDB backlogs.
  - [ ] Call `LogService.syncLogs` client method to upload logs to the backend.
  - [ ] Mark logs as successfully synced or prune them from IndexedDB upon response.
