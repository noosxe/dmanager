# Communication Protocol Design Document

This document outlines the API design and communication protocol between the Vite/React frontend and the Go backend. 

---

## 1. Protocol Architecture

The application communicates exclusively via **ConnectRPC**, a lightweight, type-safe RPC framework.
* **Format:** Pure Protocol Buffers (using binary serialization format via `useBinaryFormat: true` on the client transport, with no JSON REST endpoints).
* **Transport:** Connect protocol (compatible with gRPC-Web and gRPC) over HTTP/1.1 and HTTP/2.
* **Authentication Mechanism:** Session cookie authentication. The Connect interceptor on the backend extracts and validates the session cookie before routing requests to handlers (except for designated unauthenticated login/setup methods).

---

## 2. Service Definitions

Five ConnectRPC services are defined for dmanager:
1. `AuthService`: Handles onboarding, authentication, profile inspection, and session termination.
2. `ContainerService`: Handles container discovery lists, starting/stopping container lifecycles, and auto-update setups.
3. `LogService`: Receives batch client-side log uploads for centralized ingestion.
4. `SettingsService`: Handles persisted application settings, Gotify notification testing, and private registry status checks.
5. `AdminService`: Provides read-only inventories of Docker images, volumes, and networks for the Administration page.

---

## 3. Protocol Buffer Schemas (`proto/dmanager/v1/`)

The following protobuf schemas define the API data layer.

### 3.1. `proto/dmanager/v1/auth.proto`
```protobuf
syntax = "proto3";

package dmanager.v1;

option go_package = "dmanager/internal/gen/dmanager/v1;dmanagerv1";

service AuthService {
  // Check the setup and auth status of the server (Unauthenticated).
  rpc GetServerStatus(GetServerStatusRequest) returns (GetServerStatusResponse);

  // Setup the primary administrator account (Unauthenticated, only if user count is 0).
  rpc SetupAdmin(SetupAdminRequest) returns (SetupAdminResponse);

  // Authenticate user credentials and establish a session cookie (Unauthenticated).
  rpc Login(LoginRequest) returns (LoginResponse);

  // Expire the session token and clear the browser cookie (Authenticated).
  rpc Logout(LogoutRequest) returns (LogoutResponse);

  // Retrieve current active user profile details (Authenticated).
  rpc GetMe(GetMeRequest) returns (GetMeResponse);
}

message GetServerStatusRequest {}

message GetServerStatusResponse {
  // Returns true if no users exist in the database, triggering onboarding.
  bool needs_setup = 1;
}

message SetupAdminRequest {
  string username = 1;
  string password = 2;
}

message SetupAdminResponse {
  string username = 1;
  string role = 2;
}

message LoginRequest {
  string username = 1;
  string password = 2;
  bool remember_me = 3;
}

message LoginResponse {
  string username = 1;
  string role = 2;
}

message LogoutRequest {}

message LogoutResponse {}

message GetMeRequest {}

message GetMeResponse {
  int64 user_id = 1;
  string username = 2;
  string role = 3;
}
```

### 3.2. `proto/dmanager/v1/container.proto`
```protobuf
syntax = "proto3";

package dmanager.v1;

option go_package = "dmanager/internal/gen/dmanager/v1;dmanagerv1";

service ContainerService {
  // Retrieve all discovered containers on the host system (Authenticated).
  rpc ListContainers(ListContainersRequest) returns (ListContainersResponse);

  // Command to transition a container to a started execution state (Authenticated, Admin-only).
  rpc StartContainer(StartContainerRequest) returns (StartContainerResponse);

  // Command to gracefully stop a running container (Authenticated, Admin-only).
  rpc StopContainer(StopContainerRequest) returns (StopContainerResponse);

  // Toggle the automatic image checks/pulls loop for a container (Authenticated, Admin-only).
  rpc SetContainerAutoUpdate(SetContainerAutoUpdateRequest) returns (SetContainerAutoUpdateResponse);

  // Trigger an immediate, out-of-band registry digest check for a container (Authenticated, Admin-only).
  rpc CheckContainerUpdates(CheckContainerUpdatesRequest) returns (CheckContainerUpdatesResponse);

  // Stream live console outputs (stdout/stderr) from a container (Authenticated).
  rpc GetContainerLogs(GetContainerLogsRequest) returns (stream GetContainerLogsResponse);

  // Stream real-time container states/events (Authenticated).
  rpc StreamContainers(StreamContainersRequest) returns (stream StreamContainersResponse);

  // Command to pull the latest image tag digest and recreate the container (Authenticated, Admin-only).
  rpc UpgradeContainer(UpgradeContainerRequest) returns (UpgradeContainerResponse);
}

message UpgradeContainerRequest {
  string id = 1;
}

message UpgradeContainerResponse {
  string id = 1;
  string previous_image_id = 2;
  string current_image_id = 3;
}

message Container {
  string id = 1;
  string name = 2;
  string image = 3;
  string image_id = 4;
  string state = 5;
  bool auto_update = 6;
  bool update_available = 7;
  string latest_image_digest = 8;
  string last_checked_at = 9;  // RFC3339 formatted datetime string
  string last_updated_at = 10; // RFC3339 formatted datetime string
}

message ListContainersRequest {}

message ListContainersResponse {
  repeated Container containers = 1;
}

message StartContainerRequest {
  string id = 1;
}

message StartContainerResponse {
  string id = 1;
  string previous_state = 2;
  string current_state = 3;
}

message StopContainerRequest {
  string id = 1;
}

message StopContainerResponse {
  string id = 1;
  string previous_state = 2;
  string current_state = 3;
}

message SetContainerAutoUpdateRequest {
  string id = 1;
  bool auto_update = 2;
}

message SetContainerAutoUpdateResponse {
  string id = 1;
  bool auto_update = 2;
}

message CheckContainerUpdatesRequest {
  string id = 1;
}

message CheckContainerUpdatesResponse {
  string id = 1;
  bool update_available = 2;
  string latest_image_digest = 3;
}

message GetContainerLogsRequest {
  string id = 1;
  int32 tail_lines = 2; // Number of historic log lines to return first. Defaults to 100.
  bool follow = 3;      // Keep the stream open to watch logs in real-time.
}

message GetContainerLogsResponse {
  string log_line = 1;
  string timestamp = 2; // Log line timestamp if metadata is available
  string stream_type = 3; // "stdout" or "stderr"
}

message StreamContainersRequest {}

message StreamContainersResponse {
  string action = 1; // "save" or "delete"
  Container container = 2; // only present if action is "save"
  string container_id = 3; // always present
}
```

### 3.3. `proto/dmanager/v1/log.proto`
```protobuf
syntax = "proto3";

package dmanager.v1;

option go_package = "dmanager/internal/gen/dmanager/v1;dmanagerv1";

service LogService {
  // Transmit local browser log queues for centralized ingestion (Unauthenticated/Authenticated).
  rpc SyncLogs(SyncLogsRequest) returns (SyncLogsResponse);
}

message ClientLogEntry {
  string level = 1;      // "DEBUG", "INFO", "WARN", "ERROR"
  string message = 2;    // Log statement
  string timestamp = 3;  // RFC3339 formatted client clock timestamp
  string component = 4;  // React component or service identifier (e.g. "TanStackRouter")
  string metadata = 5;   // Serialized JSON log parameters or stack trace
}

message SyncLogsRequest {
  repeated ClientLogEntry entries = 1;
}

message SyncLogsResponse {
  int32 processed_count = 1;
}
```

### 3.4. `proto/dmanager/v1/settings.proto`
```protobuf
syntax = "proto3";

package dmanager.v1;

option go_package = "dmanager/internal/gen/proto/dmanager/v1;dmanagerv1";

service SettingsService {
  rpc GetSettings(GetSettingsRequest) returns (GetSettingsResponse);
  rpc UpdateSettings(UpdateSettingsRequest) returns (UpdateSettingsResponse);
  rpc TestGotifyNotification(TestGotifyNotificationRequest) returns (TestGotifyNotificationResponse);
  rpc GetRegistryStatus(GetRegistryStatusRequest) returns (GetRegistryStatusResponse);
}
```

Persists application settings in SQLite, sends Gotify test notifications, and reports private registry credential/connectivity status. Full field definitions live in the proto source.

### 3.5. `proto/dmanager/v1/admin.proto` (planned)
```protobuf
syntax = "proto3";

package dmanager.v1;

import "google/protobuf/timestamp.proto";

option go_package = "dmanager/internal/gen/proto/dmanager/v1;dmanagerv1";

service AdminService {
  // List images present on the host (Authenticated, read-only).
  rpc ListImages(ListImagesRequest) returns (ListImagesResponse);
  // List volumes present on the host (Authenticated, read-only).
  rpc ListVolumes(ListVolumesRequest) returns (ListVolumesResponse);
  // List networks present on the host (Authenticated, read-only).
  rpc ListNetworks(ListNetworksRequest) returns (ListNetworksResponse);
}

message ListImagesRequest {}
message ListImagesResponse {
  repeated Image images = 1;
}

message Image {
  string id = 1;               // image ID (sha256:...), rendered short-form client-side
  repeated string repo_tags = 2; // e.g. ["nginx:latest"]; empty for dangling images
  int64 created_unix = 3;       // image creation time as Unix seconds
  int64 size_bytes = 4;         // on-disk size
  int64 containers_count = 5;   // number of containers referencing this image
}

message ListVolumesRequest {}
message ListVolumesResponse {
  repeated Volume volumes = 1;
}

message Volume {
  string name = 1;
  string driver = 2;            // e.g. "local"
  string mountpoint = 3;        // host path, rendered truncated client-side
  google.protobuf.Timestamp created_at = 4;
  map<string, string> labels = 5;
}

message ListNetworksRequest {}
message ListNetworksResponse {
  repeated Network networks = 1;
}

message Network {
  string id = 1;                // network ID (sha256:...), rendered short-form client-side
  string name = 2;              // e.g. "bridge"
  string driver = 3;            // e.g. "bridge", "host", "overlay"
  string scope = 4;             // "local", "swarm", or "global"
  bool internal = 5;            // isolated from external routing
  google.protobuf.Timestamp created_at = 6;
}
```

All three procedures are unary, take empty requests, and are classified as **authenticated, any role** in the Connect interceptor (same policy as `ContainerService.ListContainers`). The service is intentionally read-only: no create, mutate, or prune procedures are defined in this phase.

---

## 4. Error Mapping & Status Codes

All errors will consume standard Connect/gRPC codes:
* **Unauthenticated Access:** Rejection with `Unauthenticated` code (Status Code: `16`).
* **Insufficient Privilege:** Action on Admin endpoints by a non-admin role yields `PermissionDenied` (Status Code: `7`).
* **Admin Onboarding Block:** Attempting to run `SetupAdmin` when a user already exists returns `FailedPrecondition` (Status Code: `9`).
* **Resource Missing:** Trying to start/stop/inspect a container ID that is not discovered yields `NotFound` (Status Code: `5`).
* **Docker Client Issues:** Socket failure, connection timeout, or host system engine problems return `Internal` error (Status Code: `13`).
* **Validation Failure:** Sending empty usernames or malformed IDs returns `InvalidArgument` (Status Code: `3`).
