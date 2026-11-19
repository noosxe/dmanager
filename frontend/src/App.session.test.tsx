import { create, toBinary } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  GetMeResponseSchema,
  GetServerStatusResponseSchema,
} from "./gen/proto/dmanager/v1/auth_pb";

// The authenticated app is not under test; its container view just makes one
// real RPC through the real transport.
vi.mock("./components/DashboardLayout", () => ({
  DashboardLayout: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dashboard">{children}</div>
  ),
}));
vi.mock("./components/ContainerGrid", () => ({
  ContainerGrid: () => {
    useEffect(() => {
      void import("./client").then(({ containerClient }) =>
        containerClient.listContainers({}).catch(() => {}),
      );
    }, []);
    return null;
  },
}));

// App and its router are module singletons; load a fresh copy per test.
async function load(path: string) {
  vi.resetModules();
  const { default: App } = await import("./App");
  const { router } = await import("./routes/router");
  await router.navigate({ to: path });
  return App;
}

const unauthenticated = () =>
  new Response(JSON.stringify({ code: "unauthenticated", message: "session expired" }), {
    status: 401,
    headers: { "content-type": "application/json" },
  });
const proto = (body: Uint8Array) =>
  new Response(body as BodyInit, { headers: { "content-type": "application/proto" } });

describe("session expiry through the real transport", () => {
  let loginCalls = 0;
  let sessionValid: boolean;

  beforeEach(() => {
    loginCalls = 0;
    sessionValid = true;
    vi.spyOn(console, "error").mockImplementation(() => {});
    localStorage.setItem("dmanager_token", "session_active");
    localStorage.setItem("dmanager_user", JSON.stringify({ username: "alice", role: "admin" }));
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
        const url = String(input instanceof Request ? input.url : input);
        if (url.endsWith("/GetServerStatus")) {
          return proto(
            toBinary(
              GetServerStatusResponseSchema,
              create(GetServerStatusResponseSchema, { needsSetup: false }),
            ),
          );
        }
        if (url.endsWith("/GetMe")) {
          return sessionValid
            ? proto(
                toBinary(
                  GetMeResponseSchema,
                  create(GetMeResponseSchema, { username: "alice", role: "admin" }),
                ),
              )
            : unauthenticated();
        }
        if (url.endsWith("/Login")) loginCalls += 1;
        // Every other RPC (container calls, login) reports Unauthenticated.
        return unauthenticated();
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("returns to the login screen when a container RPC reports Unauthenticated", async () => {
    const App = await load("/");
    render(<App />);

    // The dashboard is shown first (stored session), then the RPC fails.
    expect(await screen.findByTestId("dashboard")).toBeInTheDocument();
    await waitFor(() => expect(localStorage.getItem("dmanager_token")).toBeNull());
    expect(localStorage.getItem("dmanager_user")).toBeNull();
    await waitFor(() => expect(screen.getByLabelText(/username/i)).toBeInTheDocument());
    expect(screen.queryByTestId("dashboard")).not.toBeInTheDocument();
  });

  it("shows a wrong-password error on the login screen without a retry loop", async () => {
    localStorage.clear();
    sessionValid = false;
    const App = await load("/login");
    render(<App />);

    fireEvent.change(await screen.findByLabelText(/username/i), { target: { value: "alice" } });
    fireEvent.change(screen.getByLabelText(/password/i, { selector: "input" }), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText(/session expired/i)).toBeInTheDocument();
    expect((screen.getByLabelText(/username/i) as HTMLInputElement).value).toBe("alice");
    await new Promise((r) => setTimeout(r, 100));
    expect(loginCalls).toBe(1);
  });
});
