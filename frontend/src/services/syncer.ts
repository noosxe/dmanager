import { Code, ConnectError } from "@connectrpc/connect";
import { logClient } from "../client";
import { logDb } from "./logger";

let isSyncing = false;
let syncTimeoutId: ReturnType<typeof setTimeout> | null = null;

const scheduleIdleWork = (callback: () => void) => {
  if (typeof window !== "undefined" && "requestIdleCallback" in window) {
    window.requestIdleCallback(() => callback(), { timeout: 2000 });
  } else {
    setTimeout(callback, 200);
  }
};

export async function syncPendingLogs() {
  if (isSyncing) return;
  isSyncing = true;

  try {
    while (true) {
      const batchSize = 50;
      const entries = await logDb.logs.limit(batchSize).toArray();

      if (entries.length === 0) {
        break;
      }

      const protoEntries = entries.map((entry) => ({
        level: entry.level,
        message: entry.message,
        timestamp: entry.timestamp,
        component: entry.component,
        metadata: entry.metadata,
      }));

      const response = await logClient.syncLogs({ entries: protoEntries });

      if (response && response.processedCount > 0) {
        const idsToDelete = entries
          .map((e) => e.id)
          .filter((id): id is number => id !== undefined);

        if (idsToDelete.length > 0) {
          await logDb.logs.bulkDelete(idsToDelete);
        }

        // If we synced fewer than batchSize, we are done
        if (entries.length < batchSize) {
          break;
        }
      } else {
        break;
      }
    }
  } catch (err: unknown) {
    if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
      // Unauthenticated session, drop local queue to prevent infinite retry storm
      try {
        await logDb.logs.clear();
      } catch {
        // Ignore DB clear errors
      }
    }
  } finally {
    isSyncing = false;
  }
}

export function startSyncer(intervalMs = 10000) {
  if (syncTimeoutId) return;

  const run = () => {
    scheduleIdleWork(async () => {
      await syncPendingLogs();
      syncTimeoutId = setTimeout(run, intervalMs);
    });
  };

  run();
}

export function stopSyncer() {
  if (syncTimeoutId) {
    clearTimeout(syncTimeoutId);
    syncTimeoutId = null;
  }
}
