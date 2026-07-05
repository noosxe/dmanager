import { beforeEach, describe, expect, it, vi } from "vitest";
import { logClient } from "../client";
import { logDb } from "./logger";
import { syncPendingLogs } from "./syncer";

// Mock client and logger exports
vi.mock("../client", () => ({
  logClient: {
    syncLogs: vi.fn(),
  },
}));

vi.mock("./logger", () => {
  const mockTable = {
    limit: vi.fn().mockReturnThis(),
    toArray: vi.fn(),
    bulkDelete: vi.fn().mockResolvedValue(undefined),
  };
  return {
    logDb: {
      logs: mockTable,
    },
  };
});

describe("Browser Idle-Time Syncer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(logDb.logs.toArray).mockReset();
    vi.mocked(logClient.syncLogs).mockReset();
  });

  it("should not run if there are no logs to sync", async () => {
    vi.mocked(logDb.logs.toArray).mockResolvedValueOnce([]);

    await syncPendingLogs();

    expect(logClient.syncLogs).not.toHaveBeenCalled();
    expect(logDb.logs.bulkDelete).not.toHaveBeenCalled();
  });

  it("should batch logs and send them to the backend, then prune them", async () => {
    const mockLogs = [
      { id: 1, level: "INFO", message: "msg 1", timestamp: "2026-01-01T00:00:00Z", component: "Comp", metadata: "{}" },
      { id: 2, level: "ERROR", message: "msg 2", timestamp: "2026-01-01T00:01:00Z", component: "Comp", metadata: "{}" },
    ];
    vi.mocked(logDb.logs.toArray).mockResolvedValueOnce(mockLogs);
    vi.mocked(logDb.logs.toArray).mockResolvedValueOnce([]);

    vi.mocked(logClient.syncLogs).mockResolvedValueOnce({ processedCount: 2 } as any);

    await syncPendingLogs();

    expect(logClient.syncLogs).toHaveBeenCalledTimes(1);
    expect(logClient.syncLogs).toHaveBeenCalledWith({
      entries: [
        { level: "INFO", message: "msg 1", timestamp: "2026-01-01T00:00:00Z", component: "Comp", metadata: "{}" },
        { level: "ERROR", message: "msg 2", timestamp: "2026-01-01T00:01:00Z", component: "Comp", metadata: "{}" },
      ],
    });

    expect(logDb.logs.bulkDelete).toHaveBeenCalledTimes(1);
    expect(logDb.logs.bulkDelete).toHaveBeenCalledWith([1, 2]);
  });

  it("should not delete logs if backend sync returns processedCount = 0", async () => {
    const mockLogs = [
      { id: 1, level: "INFO", message: "msg 1", timestamp: "2026-01-01T00:00:00Z", component: "Comp", metadata: "{}" },
    ];
    vi.mocked(logDb.logs.toArray).mockResolvedValueOnce(mockLogs);

    vi.mocked(logClient.syncLogs).mockResolvedValueOnce({ processedCount: 0 } as any);

    await syncPendingLogs();

    expect(logClient.syncLogs).toHaveBeenCalledTimes(1);
    expect(logDb.logs.bulkDelete).not.toHaveBeenCalled();
  });
});
