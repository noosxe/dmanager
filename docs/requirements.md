# High-Level Requirements Document

## 1. Introduction
This document outlines the high-level requirements for the Docker Container Manager web application. The goal of this application is to provide a user-friendly interface for monitoring and managing Docker containers, along with automated update management.

---

## 2. Scope & Target Audience
The system is intended for administrators and developers who need to manage Docker containers on a host system. It provides remote management and automated maintenance without requiring direct terminal or command-line interface (CLI) access.

---

## 3. Functional Requirements

### 3.1. Container Discovery
* **Discovery of Host Containers:** The system must automatically discover all Docker containers existing on the host machine.
* **State Detection:** The system must identify and display the current execution state of each discovered container (e.g., running, stopped, paused, restarting).
* **Metadata Collection:** The system must retrieve essential container information, including container name, image version, and status, to display to the user.

### 3.2. Web User Interface
* **Access Control:** The system must serve a web interface accessible via standard modern web browsers.
* **Container Dashboard:** The UI must display a clean dashboard listing all discovered containers along with their current status and metadata.
* **Execution Control:** The UI must provide interactive controls allowing the user to trigger the following actions on any selected container:
  * **Start:** Power on a stopped container.
  * **Stop:** Gracefully shut down a running container.

### 3.3. Automated Image Update Checks
* **Scheduled Checks:** The system must periodically check if newer versions of the images used by the discovered containers are available in their respective registries.
* **Scheduling Mechanism:** The check frequency must follow a configurable schedule (e.g., a user-defined interval or cron-like schedule).
* **Notification of Updates:** The system must identify and flag containers that have newer images available.

### 3.4. Automated Container Updates
* **Per-Container Enablement:** Users must be able to enable or disable automatic updates individually for each container.
* **Automatic Re-deployment Workflow:** For any container with automatic updates enabled, when a newer image version is detected, the system must automatically execute the following sequence:
  1. **Image Retrieval:** Pull the updated container image from the registry.
  2. **Graceful Stop:** Stop the currently running container.
  3. **Container Re-creation:** Start a new container instance using the updated image while preserving the original container configuration (e.g., environment variables, network settings, port mappings, and volume mounts).
### 3.5. Release Workflow & Distribution
* **Tag-based Release Trigger:** The release workflow must run when a version tag matching `vX.Y.Z` (or similar semantic version formats) is pushed.
* **Multi-Platform Container Builds:** The release workflow must build Docker images for both `linux/amd64` and `linux/arm64` platforms.
* **Registry Distribution:** Built Docker images must be pushed to the GitHub Container Registry (GHCR).
* **Official GitHub Release:** The workflow must create an official GitHub Release referencing the container images and including a list of changes made in the release.

### 3.6. Notifications & Gotify Integration
* **Gotify Integration:** The system must support sending notifications to a Gotify server on specific events:
  * **Image Updates Found:** When a newer container image version/digest is detected in the registry.
  * **Update Check Failures:** When the periodic checker fails to inspect the registry for updates (e.g. registry down, credentials error).
  * **Auto-update Outcomes:** Both success (re-deployment completed successfully) and failure (re-deployment errored out) cases of automated container updates.
* **Web UI Configuration:** The user must be able to configure the Gotify server parameters (server URL and application token) through a settings page in the Web UI.
* **Test Notification:** The user must be able to send a test notification from the settings page to verify correct integration.

### 3.7. Private Registry Status Monitoring
* **Private Registry Config Info:** The system must show all configured private registries (e.g. Host, Username) in the Web UI.
* **Authentication and Connectivity Health Check:** The system must verify if credentials are configured correctly and perform active checks to ensure it is able to query updates from each private registry without errors.
* **Registry Status Section:** The settings page must display a registry status section displaying the results of these verification checks, including error details if any registry check fails.

### 3.8. Docker Resource Administration
* **Administration Page:** The system must provide an Administration page in the web interface, placed between System Logs and Settings in the navigation, with three tabs: Images, Volumes, and Networks.
* **Read-Only Resource Inventory:** Each tab must present a table of the corresponding Docker resources (images, volumes, networks) fetched live from the Docker Engine via its socket, including relevant metadata per resource type (e.g. repository/tag, size, and usage count for images; driver and mountpoint for volumes; driver, scope, and internal flag for networks).
* **Image Deletion (Admin):** The images table must offer a delete action per image, available only to admin users and only for images not used by any container (usage count 0; images whose usage count the daemon did not calculate must not be deletable either). Deletion must require an explicit confirmation step, show per-row progress while in flight, surface daemon errors (e.g. the image became in use between listing and deleting) without losing the table, and refresh the inventory and summary stats on success. Volumes and networks remain read-only in this phase.
* **Image Prune (Admin):** The images tab must offer bulk prune actions, available only to admin users, each deleting its scope in a single daemon call: **unused** (every image not used by any container, tagged and untagged alike) and **dangling** (untagged images only, matching the daemon's untagged-cleanup scope). Each action must require an explicit confirmation step stating the scope from the current listing (how many images and how much space are reclaimable), stay disabled when nothing in its scope is reclaimable, surface daemon errors without losing the table, report the actual reclaimed space from the daemon response on success, and refresh the inventory and summary stats afterwards. Volumes and networks remain read-only in this phase.
* **Sorting:** All resource tables must support sorting by column, consistent with the existing container table behavior. The images table must default to size descending.
* **Images Summary Stats:** The images tab must display summary stat cards above the table showing total space used by images, freeable space (the sum of sizes of images not used by any container), the total image count, the count of unused images (tagged and untagged alike — every image with a calculated zero usage count), and the count of dangling images (every untagged image not used by any container — the same set the dangling-scope prune deletes, per #203). Usage counts not calculated by the daemon must be treated as in use so freeable space and both counts never overstate what could be reclaimed.

---

### 3.9. Engine Status Indicator
* **Real Connectivity, Not Static:** The sidebar engine status indicator must reflect actual Docker Engine reachability instead of hardcoded markup: it must show an online state when the Engine answers a health ping and a clear disconnected state (e.g. "No connection") when the Engine — or the backend server itself — cannot be reached, within one polling interval.
* **Automatic Recovery:** State transitions in both directions must happen automatically via periodic lightweight polling (a ping-level check, not resource listing) without requiring a page reload, and polling must pause while the browser tab is hidden.
* **Non-Intrusive:** Status changes must not produce toast notifications; the pill itself (including an accessible live region) is the feedback surface.

### 3.10. Dialog System & Destructive Confirmations
* **Reusable Modal Primitive:** A hand-rolled dialog component (no new dependencies) providing an overlay, a focus trap, Escape-key and click-outside dismissal, `aria-modal` semantics with labelled title/description, body scroll lock, and focus restoration to the opener on close.
* **Confirmation Specialization:** A `ConfirmDialog` on top of the primitive — title, consequence-focused message, explicit verb confirm button, cancel button — with a danger variant for destructive actions and a busy state that locks dismissal while the confirmed mutation is in flight.
* **Safe by Default:** Danger confirmations must not pre-arm the destructive action: initial focus lands on the safe (cancel) action, and in-flight deletions cannot be aborted or double-triggered from the dialog.
* **Layering:** Toasts remain visible above open dialogs; dialogs are owned by the screen that opened them (declarative state, no global queue).

---

## 4. Non-Functional Requirements

### 4.1. Usability
* **Intuitive Design:** The web interface must be clean, simple, and self-explanatory, requiring minimal training to operate.
* **Real-time Status Updates:** Container status changes initiated via the UI should be reflected promptly without requiring manual page refreshes.

### 4.2. Reliability
* **Configuration Preservation:** The automated update process must not result in data loss or configuration loss. Container parameters must be meticulously mapped from the old instance to the new one.
* **Host Stability:** The management application must not interfere with host-level services or the normal operations of unrelated container workloads.

### 4.3. Observability & Logging
* **Structured Logging:** The backend must use structured logging (`log/slog`) for all application components rather than unstructured text logs.
* **Module-Scoped Logging:** Log messages must automatically include context regarding the originating module or component, using a specific `"module"` attribute (e.g. `module=docker`, `module=auth`) to facilitate log aggregation and searching.

