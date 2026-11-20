import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { AdminService } from "../gen/proto/dmanager/v1/admin_pb";
import { AuthService } from "../gen/proto/dmanager/v1/auth_pb";
import { ContainerService } from "../gen/proto/dmanager/v1/container_pb";
import { LogService } from "../gen/proto/dmanager/v1/log_pb";
import { SettingsService } from "../gen/proto/dmanager/v1/settings_pb";
import { createMockTransport, type MockOptions } from "./handlers";

const clients = (opts: Partial<MockOptions> = {}) => {
  const t = createMockTransport({ scenario: "admin", tailscale: "running", ...opts });
  return {
    auth: createClient(AuthService, t),
    containers: createClient(ContainerService, t),
    admin: createClient(AdminService, t),
    settings: createClient(SettingsService, t),
    logs: createClient(LogService, t),
  };
};

describe("mock transport", () => {
  it("answers the main read calls for the admin scenario", async () => {
    const c = clients();
    expect((await c.auth.getServerStatus({})).needsSetup).toBe(false);
    expect((await c.auth.getMe({})).role).toBe("admin");
    const { containers } = await c.containers.listContainers({});
    expect(containers.length).toBeGreaterThan(5);
    expect(containers.some((x) => x.managedBy === "com.example.stack")).toBe(true);
    expect((await c.admin.getTailscaleStatus({})).state).toBe("running");
    expect((await c.settings.getRegistryStatus({})).registries.length).toBeGreaterThan(0);
    expect((await c.admin.listAuditLogs({})).entries.length).toBeGreaterThan(0);
    expect((await c.logs.getSystemLogs({})).entries.length).toBeGreaterThan(0);
  });

  it("covers the login, setup and empty scenarios", async () => {
    const login = clients({ scenario: "login" });
    await expect(login.auth.getMe({})).rejects.toMatchObject({ code: Code.Unauthenticated });
    expect((await login.auth.login({ username: "u", password: "p" })).username).toBe("u");
    expect((await login.auth.getMe({})).username).toBe("u");

    expect((await clients({ scenario: "setup" }).auth.getServerStatus({})).needsSetup).toBe(true);
    expect((await clients({ scenario: "empty" }).containers.listContainers({})).containers).toEqual(
      [],
    );
  });

  it("applies mutations and mirrors the externally-managed rules", async () => {
    const { containers } = clients();
    const stopped = await containers.stopContainer({ id: "a1" });
    expect(stopped.currentState).toBe("exited");
    const list = (await containers.listContainers({})).containers;
    expect(list.find((x) => x.id === "a1")?.state).toBe("exited");

    for (const id of ["a6", "a7"]) {
      const err = await containers.upgradeContainer({ id }).catch((e) => e);
      expect(err).toBeInstanceOf(ConnectError);
      expect(err.code).toBe(Code.FailedPrecondition);
      expect(err.rawMessage).toContain("is managed externally");
      await expect(
        containers.setContainerAutoUpdate({ id, autoUpdate: true }),
      ).rejects.toMatchObject({ code: Code.FailedPrecondition });
      await containers.setContainerAutoUpdate({ id, autoUpdate: false });
    }
    await containers.upgradeContainer({ id: "a3" });
  });

  it("streams a snapshot, then mutations and the scripted change", async () => {
    const { containers } = clients({ streamEventDelayMs: 10 });
    const ac = new AbortController();
    const live: string[] = [];
    let count = 0;
    let total = 0;
    for await (const msg of containers.streamContainers({}, { signal: ac.signal })) {
      if (++count === 1) total = (await containers.listContainers({})).containers.length;
      if (count > total) live.push(msg.containerId);
      if (count === total) await containers.stopContainer({ id: "a2" });
      if (live.length >= 2) break;
    }
    ac.abort();
    expect(live.sort()).toEqual(["a1", "a2"]);
  });

  it("streams container logs", async () => {
    const { containers } = clients();
    const lines: string[] = [];
    for await (const l of containers.getContainerLogs({
      id: "a1",
      tailLines: 100,
      follow: false,
    })) {
      lines.push(l.logLine);
    }
    expect(lines.length).toBeGreaterThan(2);
  });
});
