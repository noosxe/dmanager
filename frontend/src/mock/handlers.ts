// In-memory implementation of every backend service, driven by fixtures.
// Server-side rules that the UI depends on are mirrored from the Go services
// (internal/container): externally managed containers reject upgrade and
// enabling auto-update with FailedPrecondition and the same message. Role rules
// follow the procedure matrix in internal/auth/interceptor.go (admin-only: all
// mutations, GetSettings, GetRegistryStatus, ListAuditLogs).
import { Code, ConnectError, createRouterTransport, type Interceptor } from "@connectrpc/connect";

import { AdminService } from "../gen/proto/dmanager/v1/admin_pb";
import { AuthService } from "../gen/proto/dmanager/v1/auth_pb";
import { ContainerService } from "../gen/proto/dmanager/v1/container_pb";
import { LogService } from "../gen/proto/dmanager/v1/log_pb";
import { SettingsService } from "../gen/proto/dmanager/v1/settings_pb";
import {
  ADMIN_USER,
  BUILD_CACHE,
  EXTERNAL_SELECTOR,
  IMAGES,
  LOG_LINES,
  NETWORKS,
  REGISTRIES,
  SERVER_VERSION,
  SETTINGS,
  SYSTEM_LOGS,
  VIEWER_USER,
  VOLUMES,
  VOLUME_USAGE,
  auditFixtures,
  authEventFixtures,
  containerFixtures,
  passkeyFixtures,
  sessionFixtures,
  tailscaleFixture,
  type MockContainer,
  type Scenario,
  type TailscaleVariant,
} from "./fixtures";

export interface MockOptions {
  scenario: Scenario;
  tailscale: TailscaleVariant;
  interceptors?: Interceptor[];
  /** Delay before the scripted state change on the container stream. */
  streamEventDelayMs?: number;
  /** Replay a Compose-style recreate of one container on the stream. */
  recreate?: boolean;
  /** Delay before that recreate. */
  recreateDelayMs?: number;
}

const sleep = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const t = setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      clearTimeout(t);
      resolve();
    });
  });

const managedBy = (c: MockContainer): string => {
  const label = c.labels["dmanager.managed-by"]?.trim();
  if (label) return label;
  return EXTERNAL_SELECTOR in c.labels ? EXTERNAL_SELECTOR : "";
};

const externallyManagedError = (name: string, by: string) =>
  new ConnectError(
    `container ${name} is managed externally (${by}) and cannot be upgraded or auto-updated by dmanager`,
    Code.FailedPrecondition,
  );

const toProto = (c: MockContainer) => {
  const by = managedBy(c);
  return {
    id: c.id,
    name: c.name,
    image: c.image,
    imageId: c.imageId,
    state: c.state,
    autoUpdate: c.autoUpdate,
    updateAvailable: c.updateAvailable,
    latestImageDigest: c.latestImageDigest,
    lastCheckedAt: c.lastCheckedAt,
    lastUpdatedAt: c.lastUpdatedAt,
    externallyManaged: by !== "",
    managedBy: by,
  };
};

type StreamMessage = {
  action: string;
  containerId: string;
  container?: ReturnType<typeof toProto>;
};

export function createMockTransport(opts: MockOptions) {
  let needsSetup = opts.scenario === "setup";
  let user: { userId: bigint; username: string; role: string } | null =
    opts.scenario === "admin" || opts.scenario === "empty"
      ? { ...ADMIN_USER }
      : opts.scenario === "viewer"
        ? { ...VIEWER_USER }
        : null;
  const containers = opts.scenario === "empty" ? [] : containerFixtures();
  let settings = { ...SETTINGS };
  let images = [...IMAGES];
  let volumes = [...VOLUMES];
  let networks = [...NETWORKS];
  let buildCache = [...BUILD_CACHE];
  let sessions = sessionFixtures();
  let passkeys = passkeyFixtures();
  // Container stream subscribers; mutations push to them like the real broker.
  const subscribers = new Set<(msg: StreamMessage) => void>();

  const requireUser = () => {
    if (!user) throw new ConnectError("not logged in", Code.Unauthenticated);
    return user;
  };
  const requireAdmin = () => {
    if (requireUser().role !== "admin") {
      throw new ConnectError("admin role required", Code.PermissionDenied);
    }
  };
  const find = (id: string) => {
    const c = containers.find((x) => x.id === id);
    if (!c) throw new ConnectError("container not found", Code.NotFound);
    return c;
  };
  const publish = (c: MockContainer) => {
    for (const s of subscribers) s({ action: "save", containerId: c.id, container: toProto(c) });
  };
  const transition = (id: string, next: string) => {
    requireAdmin();
    const c = find(id);
    const previousState = c.state;
    c.state = next;
    publish(c);
    return { id, previousState, currentState: next };
  };
  const send = (msg: StreamMessage) => {
    for (const s of subscribers) s(msg);
  };
  // What Compose does when it recreates a container behind dmanager's back: the
  // replacement is created under a temporary name, the old one is removed, and
  // the replacement then takes over the name.
  let recreated = false;
  const recreateExternally = () => {
    const index = containers.findIndex((x) => x.id === "a2");
    if (recreated || index < 0) return;
    recreated = true;
    const old = containers[index];
    const next = { ...old, id: "b2", name: `${old.id}_${old.name}`, state: "created" };
    containers.push(next);
    publish(next);
    old.state = "removing";
    publish(old);
    containers.splice(index, 1);
    send({ action: "delete", containerId: old.id });
    next.name = old.name;
    publish(next);
    next.state = "running";
    publish(next);
  };
  const noPasskeys = () => {
    throw new ConnectError("passkeys are not available in the mock preview", Code.Unimplemented);
  };

  return createRouterTransport(
    ({ service }) => {
      service(AuthService, {
        getServerStatus: () => ({
          needsSetup,
          passkeyLoginEnabled: true,
          rpId: "example.com",
          origins: ["https://example.com"],
          ...SERVER_VERSION,
        }),
        setupAdmin: (req) => {
          if (!needsSetup)
            throw new ConnectError("setup already completed", Code.FailedPrecondition);
          needsSetup = false;
          user = { ...ADMIN_USER, username: req.username || ADMIN_USER.username };
          return { username: user.username, role: user.role };
        },
        login: (req) => {
          if (!req.username || !req.password) {
            throw new ConnectError("invalid username or password", Code.Unauthenticated);
          }
          user = { ...ADMIN_USER, username: req.username };
          return { username: user.username, role: user.role };
        },
        logout: () => {
          user = null;
          return {};
        },
        getMe: () => requireUser(),
        listAuthEvents: () => {
          requireUser();
          const events = authEventFixtures();
          return { events, totalCount: BigInt(events.length) };
        },
        listSessions: () => {
          requireUser();
          return { sessions };
        },
        revokeSession: (req) => {
          requireUser();
          sessions = sessions.filter((s) => s.sessionId !== req.sessionId || s.isCurrent);
          return {};
        },
        revokeAllOtherSessions: () => {
          requireUser();
          const revokedCount = BigInt(sessions.filter((s) => !s.isCurrent).length);
          sessions = sessions.filter((s) => s.isCurrent);
          return { revokedCount };
        },
        beginPasskeyRegistration: noPasskeys,
        finishPasskeyRegistration: noPasskeys,
        beginPasskeyLogin: noPasskeys,
        finishPasskeyLogin: noPasskeys,
        listPasskeys: () => {
          requireUser();
          return { passkeys };
        },
        renamePasskey: (req) => {
          requireUser();
          const pk = passkeys.find((p) => p.id === req.id);
          if (!pk) throw new ConnectError("passkey not found", Code.NotFound);
          pk.name = req.name;
          return { passkey: pk };
        },
        deletePasskey: (req) => {
          requireUser();
          passkeys = passkeys.filter((p) => p.id !== req.id);
          let revokedSessions = 0n;
          if (req.signOutOtherSessions) {
            revokedSessions = BigInt(sessions.filter((s) => !s.isCurrent).length);
            sessions = sessions.filter((s) => s.isCurrent);
          }
          return { revokedSessions };
        },
      });

      service(ContainerService, {
        listContainers: () => {
          requireUser();
          return { containers: containers.map(toProto) };
        },
        startContainer: (req) => transition(req.id, "running"),
        stopContainer: (req) => transition(req.id, "exited"),
        setContainerAutoUpdate: (req) => {
          requireAdmin();
          const c = find(req.id);
          if (req.autoUpdate) {
            const by = managedBy(c);
            if (by) throw externallyManagedError(c.name, by);
          }
          c.autoUpdate = req.autoUpdate;
          publish(c);
          return { id: c.id, autoUpdate: c.autoUpdate };
        },
        checkContainerUpdates: (req) => {
          requireAdmin();
          const c = find(req.id);
          c.lastCheckedAt = new Date().toISOString();
          publish(c);
          return {
            id: c.id,
            updateAvailable: c.updateAvailable,
            latestImageDigest: c.latestImageDigest,
          };
        },
        upgradeContainer: (req) => {
          requireAdmin();
          const c = find(req.id);
          const by = managedBy(c);
          if (by) throw externallyManagedError(c.name, by);
          const previousImageId = c.imageId;
          c.imageId = c.latestImageDigest || c.imageId;
          c.updateAvailable = false;
          c.state = "running";
          c.lastUpdatedAt = new Date().toISOString();
          publish(c);
          return { id: c.id, previousImageId, currentImageId: c.imageId };
        },
        async *getContainerLogs(req, ctx) {
          requireUser();
          find(req.id);
          const now = Date.now();
          const lines = LOG_LINES.slice(-(req.tailLines > 0 ? req.tailLines : 100));
          for (const [i, l] of lines.entries()) {
            yield {
              logLine: l.line,
              timestamp: new Date(now - (lines.length - i) * 1000).toISOString(),
              streamType: l.stream,
            };
          }
          let n = 0;
          while (req.follow && !ctx.signal.aborted) {
            await sleep(3000, ctx.signal);
            if (ctx.signal.aborted) break;
            yield {
              logLine: `heartbeat ${++n}`,
              timestamp: new Date().toISOString(),
              streamType: "stdout",
            };
          }
        },
        async *streamContainers(_req, ctx) {
          requireUser();
          const queue: StreamMessage[] = [];
          let wake: (() => void) | null = null;
          const push = (m: StreamMessage) => {
            queue.push(m);
            wake?.();
          };
          // Subscribe before the snapshot so no mutation made meanwhile is lost.
          subscribers.add(push);
          // A running container becomes update-available shortly after load so
          // the streaming UI visibly updates.
          const scripted = setTimeout(() => {
            const target = containers.find((c) => c.id === "a1");
            if (target) {
              target.updateAvailable = true;
              target.latestImageDigest = "sha256:" + "9".repeat(64);
              publish(target);
            }
          }, opts.streamEventDelayMs ?? 4000);
          const recreate = opts.recreate
            ? setTimeout(recreateExternally, opts.recreateDelayMs ?? 3000)
            : undefined;
          ctx.signal.addEventListener("abort", () => wake?.());
          try {
            // Initial snapshot, then live events (mutations plus one scripted change).
            for (const c of containers) {
              yield { action: "save", containerId: c.id, container: toProto(c) };
            }
            while (!ctx.signal.aborted) {
              const next = queue.shift();
              if (next) yield next;
              else await new Promise<void>((r) => (wake = r));
            }
          } finally {
            clearTimeout(scripted);
            clearTimeout(recreate);
            subscribers.delete(push);
          }
        },
      });

      service(SettingsService, {
        getSettings: () => {
          requireAdmin();
          return settings;
        },
        updateSettings: (req) => {
          requireAdmin();
          settings = {
            gotifyUrl: req.gotifyUrl,
            gotifyToken: req.gotifyToken,
            auditRetentionDays: req.auditRetentionDays,
          };
          return {};
        },
        testGotifyNotification: (req) => {
          requireAdmin();
          return req.gotifyUrl
            ? { success: true, errorMessage: "" }
            : { success: false, errorMessage: "gotify url is empty" };
        },
        getRegistryStatus: () => {
          requireAdmin();
          return { registries: REGISTRIES };
        },
      });

      service(LogService, {
        syncLogs: (req) => {
          requireUser();
          return { processedCount: req.entries.length };
        },
        getSystemLogs: (req) => {
          requireUser();
          const q = req.searchQuery.toLowerCase();
          const entries = SYSTEM_LOGS.filter(
            (e) =>
              (!req.levelFilter || e.level === req.levelFilter) &&
              (!q || e.message.toLowerCase().includes(q)),
          );
          return { entries };
        },
      });

      service(AdminService, {
        listImages: () => {
          requireUser();
          return { images };
        },
        listVolumes: () => {
          requireUser();
          return { volumes };
        },
        getVolumeUsage: () => {
          requireUser();
          const usage = VOLUME_USAGE.filter((u) => volumes.some((v) => v.name === u.name));
          const unused = usage.filter((u) => u.refCount === 0n);
          return {
            volumes: usage,
            totalSizeBytes: usage.reduce((a, u) => a + u.sizeBytes, 0n),
            reclaimableBytes: unused.reduce((a, u) => a + u.sizeBytes, 0n),
            unusedCount: unused.length,
          };
        },
        listNetworks: () => {
          requireUser();
          return { networks };
        },
        deleteImage: (req) => {
          requireAdmin();
          images = images.filter((i) => i.id !== req.id);
          return {};
        },
        pruneImages: () => {
          requireAdmin();
          const gone = images.filter((i) => i.containersCount === 0n);
          images = images.filter((i) => i.containersCount !== 0n);
          return {
            imagesDeleted: gone.map((i) => ({ deleted: i.id, untagged: "" })),
            spaceReclaimed: gone.reduce((a, i) => a + i.sizeBytes, 0n),
          };
        },
        pruneVolumes: () => {
          requireAdmin();
          const gone = VOLUME_USAGE.filter(
            (u) => u.refCount === 0n && volumes.some((v) => v.name === u.name),
          );
          volumes = volumes.filter((v) => !gone.some((g) => g.name === v.name));
          return {
            volumesDeleted: gone.length,
            names: gone.map((g) => g.name),
            spaceReclaimed: gone.reduce((a, g) => a + g.sizeBytes, 0n),
          };
        },
        deleteNetwork: (req) => {
          requireAdmin();
          networks = networks.filter((n) => n.id !== req.id);
          return {};
        },
        pruneNetworks: () => {
          requireAdmin();
          const gone = networks.filter((n) => !n.predefined && n.containersCount === 0n);
          networks = networks.filter((n) => !gone.includes(n));
          return { networksDeleted: BigInt(gone.length), names: gone.map((n) => n.name) };
        },
        getBuildCacheStats: () => {
          requireUser();
          return {
            totalBytes: buildCache.reduce((a, r) => a + r.sizeBytes, 0n),
            reclaimableBytes: buildCache
              .filter((r) => !r.inUse)
              .reduce((a, r) => a + r.sizeBytes, 0n),
            recordCount: buildCache.length,
            activeCount: buildCache.filter((r) => r.inUse).length,
          };
        },
        pruneBuildCache: () => {
          requireAdmin();
          const gone = buildCache.filter((r) => !r.inUse);
          buildCache = buildCache.filter((r) => r.inUse);
          return {
            cachesDeleted: gone.length,
            spaceReclaimed: gone.reduce((a, r) => a + r.sizeBytes, 0n),
          };
        },
        listBuildCacheRecords: () => {
          requireUser();
          return { records: buildCache };
        },
        pruneBuildCacheRecord: (req) => {
          requireAdmin();
          const rec = buildCache.find((r) => r.id === req.id);
          buildCache = buildCache.filter((r) => r.id !== req.id);
          return { cachesDeleted: rec ? 1 : 0, spaceReclaimed: rec?.sizeBytes ?? 0n };
        },
        checkEngine: () => {
          requireUser();
          return { connected: true, apiVersion: "1.51", error: "" };
        },
        getTailscaleStatus: () => {
          requireUser();
          return tailscaleFixture(opts.tailscale);
        },
        listAuditLogs: (req) => {
          requireAdmin();
          const q = req.query.toLowerCase();
          const all = auditFixtures().filter(
            (e) =>
              (!req.source || e.source === req.source) &&
              (!req.outcome || e.outcome === req.outcome) &&
              (!q || `${e.actor} ${e.action} ${e.detail}`.toLowerCase().includes(q)),
          );
          const offset = Number(req.offset);
          const limit = req.limit || 50;
          return { entries: all.slice(offset, offset + limit), total: BigInt(all.length) };
        },
      });
    },
    { transport: { interceptors: opts.interceptors } },
  );
}
