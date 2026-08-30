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
* **Image Deletion (Admin):** The images table must offer a delete action per image, available only to admin users and only for images not used by any container (usage count 0; images whose usage count the daemon did not calculate must not be deletable either). Deletion must require an explicit confirmation step, show per-row progress while in flight, surface daemon errors (e.g. the image became in use between listing and deleting) without losing the table, and refresh the inventory and summary stats on success. Volume usage measurement and reclaim (§9.11) and network in-use visibility, deletion and reclaim (§9.12) are specified separately below.
* **Image Prune (Admin):** The images tab must offer bulk prune actions, available only to admin users, each deleting its scope in a single daemon call: **unused** (every image not used by any container, tagged and untagged alike) and **dangling** (untagged images only, matching the daemon's untagged-cleanup scope). Each action must require an explicit confirmation step stating the scope from the current listing (how many images and how much space are reclaimable), stay disabled when nothing in its scope is reclaimable, surface daemon errors without losing the table, report the actual reclaimed space from the daemon response on success, and refresh the inventory and summary stats afterwards. Volume usage measurement and reclaim (§9.11) and network in-use visibility, deletion and reclaim (§9.12) are specified separately below.
* **Network In-Use Visibility & Deletion (Admin):** The networks table must show, per network, whether any container is attached (including stopped containers, whose network endpoints persist until removal) and must offer a delete action per network, available only to admin users and only for networks with zero attached containers that the daemon does not own (pre-defined `bridge`/`host`/`none` networks must never offer deletion). Networks whose attachment count is unknown must not be deletable either. Deletion must require an explicit confirmation step, show per-row progress while in flight, surface daemon errors (e.g. the network became in use between listing and deleting) without losing the table, and refresh the inventory on success.
* **Network Prune (Admin):** The networks tab must offer a bulk prune action, available only to admin users, deleting every unused network in a single daemon call. It must require an explicit confirmation step stating the scope from the current listing, surface daemon errors without losing the table, report the daemon's actual deletion list on success (no byte figures exist for network prunes), and refresh the inventory afterwards. In-use, pre-defined and swarm-ingress networks are daemon-protected at prune time regardless of the request.
* **Builder Cache (Admin):** The administration area must expose a dedicated **Builder** tab showing the daemon-reported builder-owned disk space as summary stat cards: total build-cache size, reclaimable size, and the record count. It must offer a prune action, available only to admin users, that deletes build-cache records in a single daemon call (preserving buildkit-internal cache types and any records in active use). The action must require an explicit confirmation step that states the record count and the reclaimable size as an **upper bound** and discloses the tradeoff that future image builds will be slower until the cache is rebuilt; it must stay disabled when nothing is reclaimable, report the actual reclaimed space from the daemon response on success, and refresh the builder stats afterwards. Builder stats must load and fail independently of the images inventory.
* **Builder Records Drill-Down (Admin):** The Builder tab must additionally list the daemon-reported build-cache records in a size-sorted (descending) table — size, type, description, last-used time, usage count, and in-use/shared indicators — so the largest records surface first. Each record must offer a delete action, available only to admin users and disabled for records in active use, that prunes exactly that record via the daemon's ID filter. The action must require an explicit confirmation naming the record and stating the size as an **upper bound** (shared blob content may free less) and the rebuild cost; it must share the single-flight guard with the aggregate prune, report the daemon's actual deleted-count and reclaimed bytes on success, and refresh both the record list and the builder stats afterwards. The records list must load and fail independently of the stats cards.
* **Volume Usage Measurement & Reclaim (Admin prune; read for all):** The Volumes tab must display a stat card with the total volume count (derived from the cheap volume list — never from a disk-usage call) and an actions row with two explicit actions: **Calculate sizes**, available to every role, which fetches per-volume sizes and reference counts from the daemon on demand only — never automatically on tab open or refresh, because the daemon computes volume sizes by recursively walking every volume's directory tree with no server-side cache — and fills a Size column in which unmeasured volumes and daemon walk-failures (size -1) render as unknown; and **Reclaim space**, available only to admin users, which deletes all unused volumes in one daemon call. Unused means no container — running or stopped — references the volume; the daemon re-evaluates references at prune time, so protected volumes are never deleted even if the client's preview is stale. The reclaim action must require an explicit confirmation stating the unused-volume count and reclaimable bytes as an **upper bound** when sizes were calculated, or an explicit not-yet-calculated notice inviting the user to Calculate sizes first, and that deletion cannot be undone; it must report the daemon's actual deleted volume names and reclaimed bytes on success; and it must re-measure sizes afterwards only if they had been calculated before the prune.
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

### 3.11. Audit Logs
* **Mutation Recording:** Every mutation action performed by any user — every admin-role RPC across all services — must be recorded to the local database with the acting username and role, a dotted action verb, the affected resource type and id, the outcome (success, failure, or denied for authorization-refused attempts), and a human-readable detail summary (e.g. prune counts and reclaimed bytes; the daemon's error message on failure). System-originated automatic updates (scheduler-triggered container re-deployments) must be recorded the same way with a system source and a "system" actor. Recording must be best-effort: an audit-write failure must never fail or delay the mutation it observes.
* **Admin-Only Review:** Audit logs must be reviewable by admin users only. Non-admins must not see the navigation entry, must be redirected away from the page route, and must have the review RPC refused server-side. Read access must never record entries; denied mutation attempts must themselves be recorded.
* **Review UI:** The sidebar must show an **Audit Logs** item between Administration and Settings for admin users, leading to a dedicated page whose main content is a table of entries (time, actor, action, resource, outcome badge, detail) ordered newest-first. The page must provide a server-side search filter (substring over actor, action, resource and detail, matching the containers-page search pattern), source and outcome filter selects, and Prev/Next pagination with an n–m-of-total indicator. Search, filters and pagination must refetch server-side; the page must not poll in the background.
* **Configurable Retention:** Audit retention must be time-based and configurable by an admin in Settings → General from exactly five windows: 7 days, 1 month (30 days), 3 months (90 days, the default), 6 months (180 days), and 1 year (365 days). Entries older than the chosen window must be trimmed automatically; the change must take effect without a server restart; values outside the preset set must be refused server-side; and the settings read must always report the effective window.

### 3.12. Email Delivery (SMTP)
* **System-Only Outbound Mail:** dmanager must be able to send email through an administrator-configured SMTP relay for system purposes only. There must be no user-facing or RPC-reachable "send email" capability; the only senders are system flows and an ops-only CLI verification command. All SMTP configuration must come from the regular deployment configuration (config file and `DMANAGER_SMTP_*` environment variables), not from the settings UI or database — mail is machine-level infrastructure that must survive a broken database and must not require a running server to inspect or change.
* **Relay-Centric Configuration:** The target of the SMTP configuration is an internal relay (e.g. a postfix container on the Docker network that forwards upstream to a provider such as Resend). The configuration must therefore include the relay host and port, an optional username/password for relays that require authentication, a TLS mode (`none`, `starttls`, or `tls`), a send timeout, and the sender identity (`from_email` required, `from_name` optional). The upstream provider is transparent to dmanager — only the relay is configured — but the `from_email` must sit on the provider-verified domain or upstream delivery fails.
* **Graceful Absence, Bounded Failure:** With the SMTP section absent or disabled, the system must behave exactly as today: email-consuming features degrade to logged no-ops, and nothing fails at startup. With SMTP enabled, a send failure must never crash `serve` and never be retried indefinitely within this scope (single synchronous attempt with the configured timeout); whether a given flow treats a mail failure as best-effort or surfaces it to the user is decided by that flow's design. Credentials must never appear in logs, and recipients, subjects and headers must be built through a vetted mail-library API (no string concatenation into SMTP headers).
* **Verification Affordance:** Because the first consumers land later, the initial story must ship an ops-only verification path — a CLI subcommand that loads the deployment config and sends a short test email to a given address, reporting success or the failure reason as the process exit status.
* **Planned Consumers (follow-up stories):** password change notifications, invitation-based user creation by email address, high-impact system alerts, and a weekly stats digest. Each will define its own templates, recipient resolution, and failure semantics on top of this infrastructure.

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

