import { Code, ConnectError } from "@connectrpc/connect";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useContainers } from "./useContainers";

const { containerClient } = vi.hoisted(() => ({
  containerClient: { listContainers: vi.fn(), streamContainers: vi.fn() },
}));

vi.mock("../client", () => ({ containerClient }));
vi.mock("../context/ToastContext", () => ({
  useToast: () => ({ info: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));

const listResponse = (...names: string[]) => ({
  containers: names.map((name) => ({ id: `id-${name}`, name })),
});

// A stream that ends cleanly right away.
const endedStream = () => ({
  async *[Symbol.asyncIterator]() {},
});

const failingStream = (err: Error) => ({
  // eslint-disable-next-line require-yield
  async *[Symbol.asyncIterator]() {
    throw err;
  },
});

const tick = (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });

describe("useContainers stream", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it("refetches the list on every (re)subscribe and waits after a clean end", async () => {
    containerClient.listContainers
      .mockResolvedValueOnce(listResponse("a"))
      .mockResolvedValue(listResponse("a", "b"));
    containerClient.streamContainers.mockImplementation(endedStream);

    const { result, unmount } = renderHook(() => useContainers());
    await tick(0);

    expect(containerClient.streamContainers).toHaveBeenCalledTimes(1);
    expect(containerClient.listContainers).toHaveBeenCalledTimes(1);
    expect(result.current.containers.map((c) => c.name)).toEqual(["a"]);

    // No immediate re-subscribe after a clean end.
    await tick(900);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(1);

    await tick(200);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(2);
    expect(containerClient.listContainers).toHaveBeenCalledTimes(2);
    expect(result.current.containers.map((c) => c.name)).toEqual(["a", "b"]);

    unmount();
  });

  it("does not retry tightly on Unauthenticated", async () => {
    containerClient.listContainers.mockResolvedValue(listResponse());
    containerClient.streamContainers.mockImplementation(() =>
      failingStream(new ConnectError("expired", Code.Unauthenticated)),
    );

    const { unmount } = renderHook(() => useContainers());
    await tick(0);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(1);

    await tick(20000);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(1);

    unmount();
  });

  it("backs off between failures and stops after unmount", async () => {
    containerClient.listContainers.mockResolvedValue(listResponse());
    containerClient.streamContainers.mockImplementation(() =>
      failingStream(new ConnectError("down", Code.Unavailable)),
    );

    const { unmount } = renderHook(() => useContainers());
    await tick(0);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(1);
    await tick(2000);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(2);
    await tick(3900);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(2);
    await tick(200);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(3);

    unmount();
    await tick(60000);
    expect(containerClient.streamContainers).toHaveBeenCalledTimes(3);
  });
});
