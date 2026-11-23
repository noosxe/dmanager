import { RouterProvider } from "@tanstack/react-router";
// @vitest-environment jsdom
import { render, waitFor } from "@testing-library/react";
import { act, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../client", () => {
  const noopRpc = () => new Promise(() => {});
  const anyClient = new Proxy({}, { get: () => noopRpc });
  return {
    authClient: anyClient,
    adminClient: anyClient,
    containerClient: anyClient,
    logClient: anyClient,
    settingsClient: anyClient,
    setUnauthenticatedHandler: () => {},
  };
});

// Page bodies need auth/toast providers and RPC data; titles don't depend on
// them, so stub every page component.
vi.mock("../components/DashboardLayout", () => ({
  DashboardLayout: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("../components/Administration", () => ({ Administration: () => <div /> }));
vi.mock("../components/AuditLogsPage", () => ({ AuditLogsPage: () => <div /> }));
vi.mock("../components/ContainerGrid", () => ({ ContainerGrid: () => <div /> }));
vi.mock("../components/Login", () => ({ Login: () => <div /> }));
vi.mock("../components/Settings", () => ({ Settings: () => <div /> }));
vi.mock("../components/Setup", () => ({ Setup: () => <div /> }));
vi.mock("../components/SystemLogs", () => ({ SystemLogs: () => <div /> }));

import type { RouterContext } from "./router";

type AppRouter = (typeof import("./router"))["router"];

const authed: RouterContext["auth"] = {
  isAuthenticated: true,
  needsSetup: false,
  user: { username: "admin", role: "admin" },
  logout: async () => {},
};

async function open(path: string, auth = authed) {
  vi.resetModules();
  const { router } = (await import("./router")) as { router: AppRouter };
  window.history.replaceState(null, "", path);
  router.update({ context: { auth } });
  render(<RouterProvider router={router} />);
  await act(async () => {
    await router.load();
  });
}

describe("browser tab titles", () => {
  beforeEach(() => {
    document.title = "";
  });

  it.each([
    ["/", "dmanager"],
    ["/logs", "Logs · dmanager"],
    ["/audit-logs", "Audit Logs · dmanager"],
    ["/administration/images", "Images · Administration · dmanager"],
    ["/administration/volumes", "Volumes · Administration · dmanager"],
    ["/settings/general", "General · Settings · dmanager"],
    ["/settings/security", "Security & Sessions · Settings · dmanager"],
    ["/nope", "Page not found · dmanager"],
  ])("%s -> %s", async (path, title) => {
    await open(path);
    await waitFor(() => expect(document.title).toBe(title));
  });

  it("/login -> Sign in", async () => {
    await open("/login", { ...authed, isAuthenticated: false, user: null });
    await waitFor(() => expect(document.title).toBe("Sign in · dmanager"));
  });

  it("/setup -> Setup", async () => {
    await open("/setup", { ...authed, isAuthenticated: false, needsSetup: true, user: null });
    await waitFor(() => expect(document.title).toBe("Setup · dmanager"));
  });
});
