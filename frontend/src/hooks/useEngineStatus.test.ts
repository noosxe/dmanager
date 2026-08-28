import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useEngineStatus } from "./useEngineStatus";

const { adminClient } = vi.hoisted(() => ({
  adminClient: { checkEngine: vi.fn() },
}));

vi.mock("../client", () => ({
  adminClient,
}));

const onlineResponse = (version = "1.51") => ({
  $typeName: "dmanager.v1.CheckEngineResponse",
  connected: true,
  apiVersion: version,
  error: "",
});

const offlineResponse = (reason: string) => ({
  $typeName: "dmanager.v1.CheckEngineResponse",
  connected: false,
  apiVersion: "",
  error: reason,
});

describe("useEngineStatus", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("starts as checking and resolves to online with the API version detail", async () => {
    adminClient.checkEngine.mockResolvedValue(onlineResponse());

    const { result } = renderHook(() => useEngineStatus());
    expect(result.current.status).toBe("checking");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current).toEqual({
      status: "online",
      detail: "Docker Engine API v1.51",
    });
  });

  it("reports daemon unreachability as offline with the daemon reason", async () => {
    adminClient.checkEngine.mockResolvedValue(
      offlineResponse("Cannot connect to the Docker daemon"),
    );

    const { result } = renderHook(() => useEngineStatus());

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current).toEqual({
      status: "offline",
      detail: "Cannot connect to the Docker daemon",
    });
  });

  it("maps transport failures to offline with Backend unreachable", async () => {
    adminClient.checkEngine.mockRejectedValue(new Error("[unavailable] network error"));

    const { result } = renderHook(() => useEngineStatus());

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current).toEqual({ status: "offline", detail: "Backend unreachable" });
  });

  it("falls back to a generic detail when the daemon reports no reason", async () => {
    adminClient.checkEngine.mockResolvedValue(offlineResponse(""));

    const { result } = renderHook(() => useEngineStatus());

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current.detail).toBe("Docker Engine unreachable");
  });

  it("polls every 30 seconds while the tab is visible", async () => {
    adminClient.checkEngine.mockResolvedValue(onlineResponse());

    renderHook(() => useEngineStatus());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(4);
  });

  it("skips polls while the tab is hidden", async () => {
    Object.defineProperty(document, "hidden", { configurable: true, value: true });
    adminClient.checkEngine.mockResolvedValue(onlineResponse());

    renderHook(() => useEngineStatus());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
    });
    // No poll fired during two full intervals while hidden.
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(1);

    Object.defineProperty(document, "hidden", { configurable: true, value: false });
  });

  it("re-checks immediately on window focus", async () => {
    adminClient.checkEngine.mockResolvedValue(onlineResponse());

    renderHook(() => useEngineStatus());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(1);

    await act(async () => {
      window.dispatchEvent(new Event("focus"));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(2);
  });

  it("stops polling after unmount", async () => {
    adminClient.checkEngine.mockResolvedValue(onlineResponse());

    const { unmount } = renderHook(() => useEngineStatus());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(1);

    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
      window.dispatchEvent(new Event("focus"));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(adminClient.checkEngine).toHaveBeenCalledTimes(1);
  });

  it("ignores a stale response that was overtaken by a newer check", async () => {
    let resolveFirst!: (v: unknown) => void;
    adminClient.checkEngine.mockImplementationOnce(
      () => new Promise((resolve) => (resolveFirst = resolve)),
    );
    adminClient.checkEngine.mockResolvedValueOnce(onlineResponse());

    const { result } = renderHook(() => useEngineStatus());

    await act(async () => {
      window.dispatchEvent(new Event("focus"));
      await vi.advanceTimersByTimeAsync(0);
    });

    // The focus check resolved; the first, slow check must not overwrite it.
    expect(result.current.status).toBe("online");

    await act(async () => {
      resolveFirst(offlineResponse("stale daemon down"));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.status).toBe("online");
  });
});
