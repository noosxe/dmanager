import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, describe, expect, it, vi } from "vitest";

import { authClient, containerClient, getBaseUrl, setUnauthenticatedHandler } from "./client";

describe("getBaseUrl", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("uses relative URLs in production builds regardless of the page port", () => {
    vi.stubEnv("DEV", false);
    vi.stubGlobal("location", { port: "3000" });
    expect(getBaseUrl()).toBe("");
    vi.stubGlobal("location", { port: "5173" });
    expect(getBaseUrl()).toBe("");
  });

  it("points at the backend on 9283 in dev mode", () => {
    vi.stubEnv("DEV", true);
    expect(getBaseUrl()).toBe("http://localhost:9283");
  });
});

describe("session expiry interceptor", () => {
  const unauthenticated = () =>
    new Response(JSON.stringify({ code: "unauthenticated", message: "session expired" }), {
      status: 401,
      headers: { "content-type": "application/json" },
    });

  afterEach(() => {
    setUnauthenticatedHandler(null);
    vi.unstubAllGlobals();
  });

  it("notifies the handler when a non-auth RPC returns Unauthenticated", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(async () => unauthenticated()),
    );
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

    await expect(containerClient.listContainers({})).rejects.toMatchObject({
      code: Code.Unauthenticated,
    });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("notifies the handler for authenticated AuthService procedures", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(async () => unauthenticated()),
    );
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

    await expect(authClient.listSessions({})).rejects.toBeInstanceOf(ConnectError);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("ignores Unauthenticated from procedures that fail without a session", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(async () => unauthenticated()),
    );
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

    await expect(authClient.login({ username: "a", password: "b" })).rejects.toBeInstanceOf(
      ConnectError,
    );
    await expect(authClient.getMe({})).rejects.toBeInstanceOf(ConnectError);
    await expect(authClient.beginPasskeyLogin({})).rejects.toBeInstanceOf(ConnectError);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores other error codes", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(
        async () =>
          new Response(JSON.stringify({ code: "internal", message: "boom" }), {
            status: 500,
            headers: { "content-type": "application/json" },
          }),
      ),
    );
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

    await expect(containerClient.listContainers({})).rejects.toBeInstanceOf(ConnectError);
    expect(handler).not.toHaveBeenCalled();
  });
});
