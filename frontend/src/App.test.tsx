import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./client", () => ({
  authClient: {
    getServerStatus: vi.fn(),
    getMe: vi.fn(),
    login: vi.fn(),
    setupAdmin: vi.fn(),
    logout: vi.fn(),
    beginPasskeyLogin: vi.fn(),
    finishPasskeyLogin: vi.fn(),
  },
  adminClient: {},
  containerClient: {},
  logClient: {},
  settingsClient: {},
  transport: {},
  setUnauthenticatedHandler: vi.fn(),
}));

// The authenticated app is not under test here; stub the dashboard.
vi.mock("./components/DashboardLayout", () => ({
  DashboardLayout: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dashboard">{children}</div>
  ),
}));
vi.mock("./components/ContainerGrid", () => ({ ContainerGrid: () => null }));

const status = (needsSetup: boolean) => ({
  needsSetup,
  passkeyLoginEnabled: false,
  version: "v",
  commit: "c",
  buildDate: "d",
});

// App and its router are module singletons; load a fresh copy per test.
async function load(path: string) {
  vi.resetModules();
  const { default: App } = await import("./App");
  const { authClient } = await import("./client");
  const { router } = await import("./routes/router");
  await router.navigate({ to: path });
  return { App, authClient };
}

describe("App auth failures (real AuthProvider)", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  const prepare = async (path: string, needsSetup: boolean) => {
    const { App, authClient } = await load(path);
    vi.mocked(authClient.getServerStatus).mockResolvedValue(status(needsSetup) as never);
    vi.mocked(authClient.getMe).mockRejectedValue(
      new ConnectError("no session", Code.Unauthenticated),
    );
    return { App, authClient };
  };

  it("keeps the login form and shows the error after a failed login", async () => {
    const { App, authClient } = await prepare("/login", false);
    vi.mocked(authClient.login).mockRejectedValue(
      new ConnectError("invalid credentials", Code.Unauthenticated),
    );
    render(<App />);

    const username = (await screen.findByLabelText(/username/i)) as HTMLInputElement;
    fireEvent.change(username, { target: { value: "alice" } });
    fireEvent.change(screen.getByLabelText(/password/i, { selector: "input" }), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText(/invalid credentials/i)).toBeInTheDocument();
    expect((screen.getByLabelText(/username/i) as HTMLInputElement).value).toBe("alice");
    expect(screen.queryByText(/loading console/i)).not.toBeInTheDocument();
  });

  it("keeps the setup form and shows the error after a failed setup", async () => {
    const { App, authClient } = await prepare("/setup", true);
    vi.mocked(authClient.setupAdmin).mockRejectedValue(new Error("setup already completed"));
    render(<App />);

    const username = (await screen.findByLabelText(/admin username/i)) as HTMLInputElement;
    fireEvent.change(username, { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/admin password/i), {
      target: { value: "a-long-passphrase-1" },
    });
    fireEvent.change(screen.getByLabelText(/confirm password/i), {
      target: { value: "a-long-passphrase-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: /create admin account/i }));

    expect(await screen.findByText(/setup already completed/i)).toBeInTheDocument();
    await waitFor(() =>
      expect((screen.getByLabelText(/admin username/i) as HTMLInputElement).value).toBe("admin"),
    );
  });

  it("leaves /login and shows the authenticated app after a successful login", async () => {
    const { App, authClient } = await prepare("/login", false);
    vi.mocked(authClient.login).mockResolvedValue({ username: "alice", role: "admin" } as never);
    render(<App />);

    fireEvent.change(await screen.findByLabelText(/username/i), { target: { value: "alice" } });
    fireEvent.change(screen.getByLabelText(/password/i, { selector: "input" }), {
      target: { value: "correct-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByTestId("dashboard")).toBeInTheDocument();
    expect(screen.queryByLabelText(/username/i)).not.toBeInTheDocument();
    expect(localStorage.getItem("dmanager_token")).toBe("session_active");
  });
});
