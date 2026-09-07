/**
 * Regression test for https://github.com/noosxe/dmanager/issues/235
 *
 * Login (password and passkey) must auto-navigate to the dashboard.
 *
 * TanStack Router does not re-evaluate `beforeLoad` guards on context-only
 * updates, and remounting `RouterProvider` (the isLoading dance in App.tsx)
 * skips `router.load()` for an already-resolved location. App.tsx therefore
 * calls `router.invalidate()` when the auth state flips — these tests fail
 * (location stays on /login) if that effect is removed.
 */
// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/react";
type AppRouter = (typeof import("./router"))["router"];
import { act, type ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const loginMock = vi.fn();
const beginPasskeyMock = vi.fn();
const finishPasskeyMock = vi.fn();
const getMeMock = vi.fn();
const getServerStatusMock = vi.fn();
const webauthnGetMock = vi.fn();

vi.mock("../client", () => {
  // Any RPC method on the non-auth clients resolves to an empty object so
  // dashboard effects fired after the redirect are harmless in jsdom.
  const noopRpc = () => Promise.resolve({});
  const anyClient = new Proxy({}, { get: () => noopRpc });
  return {
    authClient: {
      login: (...args: unknown[]) => loginMock(...args),
      beginPasskeyLogin: (...args: unknown[]) => beginPasskeyMock(...args),
      finishPasskeyLogin: (...args: unknown[]) => finishPasskeyMock(...args),
      getMe: (...args: unknown[]) => getMeMock(...args),
      getServerStatus: (...args: unknown[]) => getServerStatusMock(...args),
    },
    adminClient: anyClient,
    containerClient: anyClient,
    logClient: anyClient,
    settingsClient: anyClient,
  };
});

vi.mock("@github/webauthn-json", () => ({
  get: (...args: unknown[]) => webauthnGetMock(...args),
}));

// Node's experimental localStorage global shadows jsdom's and is unusable
// under vitest — stub an in-memory implementation for useAuth.
const memStore = (() => {
  let m = new Map<string, string>();
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, String(v)),
    removeItem: (k: string) => void m.delete(k),
    clear: () => void m.clear(),
    key: (i: number) => Array.from(m.keys())[i] ?? null,
    get length() {
      return m.size;
    },
  };
})();
vi.stubGlobal("localStorage", memStore);

const okStatus = {
  needsSetup: false,
  passkeyLoginEnabled: true,
  version: "test",
  commit: "test",
  buildDate: "test",
};
const okUser = { username: "admin", role: "admin" };

async function atLogin(router: AppRouter) {
  window.history.replaceState(null, "", "/login");
  await act(async () => {
    await router.navigate({ to: "/login" });
  });
}

// Fresh module graph per test: the router in ./router is a module singleton,
// so reusing it across tests leaks resolved-location/match state and pollutes
// subsequent tests. vi.resetModules() gives each test a cold-start app.
async function freshApp() {
  vi.resetModules();
  const { router } = await import("./router");
  const { default: App } = await import("../App");
  return { router, App };
}

function renderLogin(App: () => ReactElement, router: AppRouter) {
  const screen = render(<App />);
  return waitFor(() => {
    expect(screen.getByText(/container management console/i)).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  }).then(() => screen);
}

describe("login auto-navigates to dashboard (#235)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    memStore.clear();
    getServerStatusMock.mockResolvedValue(okStatus);
    getMeMock.mockRejectedValue(new Error("unauthenticated"));
  });

  it("password login navigates to / without a refresh", async () => {
    const { router, App } = await freshApp();
    await atLogin(router);
    loginMock.mockResolvedValue(okUser);
    const screen = await renderLogin(App, router);

    fireEvent.change(screen.container.querySelector("#username")!, {
      target: { value: "admin" },
    });
    fireEvent.change(screen.container.querySelector("#password")!, {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign In" }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
    expect(loginMock).toHaveBeenCalledTimes(1);
  });

  it("passkey login navigates to / without a refresh", async () => {
    const { router, App } = await freshApp();
    await atLogin(router);
    beginPasskeyMock.mockResolvedValue({ optionsJson: JSON.stringify({ challenge: "abc" }) });
    webauthnGetMock.mockResolvedValue({ id: "cred1", rawId: "cred1", response: {} });
    finishPasskeyMock.mockResolvedValue(okUser);
    const screen = await renderLogin(App, router);

    fireEvent.click(screen.getByRole("button", { name: "Sign in with Passkey" }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
    expect(finishPasskeyMock).toHaveBeenCalledTimes(1);
  });

  // Regression: after a successful passkey login the localStorage token is
  // already set, so on the next page load isAuthenticated never flips and the
  // invalidate must fire when isLoading settles instead. Without the
  // isLoading gate the app boots and stays on /login forever (#235).
  it("cold boot with a stored session navigates to / without a refresh", async () => {
    memStore.setItem("dmanager_token", "session_active");
    memStore.setItem("dmanager_user", JSON.stringify(okUser));
    getMeMock.mockResolvedValue(okUser);

    const { router, App } = await freshApp();
    window.history.replaceState(null, "", "/login");
    render(<App />);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });
});
