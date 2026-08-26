import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { authClient, settingsClient } from "../client";
import { ToastProvider } from "../context/ToastContext";
import type {
  GetServerStatusResponse,
  ListAuthEventsResponse,
  ListPasskeysResponse,
  ListSessionsResponse,
  RevokeAllOtherSessionsResponse,
  RevokeSessionResponse,
} from "../gen/proto/dmanager/v1/auth_pb";
import type {
  GetRegistryStatusResponse,
  GetSettingsResponse,
} from "../gen/proto/dmanager/v1/settings_pb";
import { Settings } from "./Settings";

// Mock @github/webauthn-json
vi.mock("@github/webauthn-json", () => ({
  create: vi.fn().mockResolvedValue({ id: "mock-cred-id", rawId: "mock-cred-id" }),
  get: vi.fn().mockResolvedValue({ id: "mock-cred-id", rawId: "mock-cred-id" }),
}));

// Mock the clients
vi.mock("../client", () => ({
  settingsClient: {
    getSettings: vi.fn(),
    updateSettings: vi.fn(),
    testGotifyNotification: vi.fn(),
    getRegistryStatus: vi.fn(),
  },
  authClient: {
    getServerStatus: vi.fn(),
    listPasskeys: vi.fn(),
    beginPasskeyRegistration: vi.fn(),
    finishPasskeyRegistration: vi.fn(),
    renamePasskey: vi.fn(),
    deletePasskey: vi.fn(),
    listSessions: vi.fn(),
    revokeSession: vi.fn(),
    revokeAllOtherSessions: vi.fn(),
    listAuthEvents: vi.fn(),
  },
}));

describe("Settings Component", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    vi.mocked(authClient.getServerStatus).mockResolvedValue({
      needsSetup: false,
      passkeyLoginEnabled: true,
      rpId: "localhost",
      origins: ["http://localhost:9283"],
      $typeName: "dmanager.v1.GetServerStatusResponse",
    } as unknown as GetServerStatusResponse);

    vi.mocked(authClient.listPasskeys).mockResolvedValue({
      passkeys: [
        {
          id: "cred-1",
          name: "Work MacBook",
          aaguid: "00000000",
          friendlyDeviceName: "iCloud Keychain",
          backupEligible: true,
          backupState: true,
          signCount: 1,
          cloneWarning: false,
          createdAt: { seconds: BigInt(1700000000), nanos: 0 },
          lastUsedAt: { seconds: BigInt(1700000000), nanos: 0 },
          $typeName: "dmanager.v1.Passkey",
        },
      ],
      $typeName: "dmanager.v1.ListPasskeysResponse",
    } as unknown as ListPasskeysResponse);

    vi.mocked(settingsClient.getSettings).mockResolvedValue({
      gotifyUrl: "https://gotify.example.com",
      gotifyToken: "token123",
      $typeName: "dmanager.v1.GetSettingsResponse",
    } as unknown as GetSettingsResponse);

    vi.mocked(settingsClient.getRegistryStatus).mockResolvedValue({
      registries: [
        {
          host: "docker.io",
          username: "testuser",
          isConfigured: true,
          isHealthy: true,
          errorMessage: "",
          $typeName: "dmanager.v1.RegistryStatus",
        },
      ],
      $typeName: "dmanager.v1.GetRegistryStatusResponse",
    } as unknown as GetRegistryStatusResponse);

    vi.mocked(authClient.listSessions).mockResolvedValue({
      sessions: [
        {
          sessionId: "sess-1",
          userAgent: "Mozilla/5.0 Chrome/120.0 Linux",
          deviceLabel: "Chrome · Linux",
          isCurrent: true,
          lastSeenAt: { seconds: BigInt(1700000000), nanos: 0 },
          createdAt: { seconds: BigInt(1700000000), nanos: 0 },
          expiresAt: { seconds: BigInt(1700086400), nanos: 0 },
          absoluteExpiresAt: { seconds: BigInt(1702592000), nanos: 0 },
          $typeName: "dmanager.v1.Session",
        },
        {
          sessionId: "sess-2",
          userAgent: "Mozilla/5.0 Safari iPhone iOS",
          deviceLabel: "Safari · iOS",
          isCurrent: false,
          lastSeenAt: { seconds: BigInt(1700000000), nanos: 0 },
          createdAt: { seconds: BigInt(1700000000), nanos: 0 },
          expiresAt: { seconds: BigInt(1700086400), nanos: 0 },
          absoluteExpiresAt: { seconds: BigInt(1702592000), nanos: 0 },
          $typeName: "dmanager.v1.Session",
        },
      ],
      $typeName: "dmanager.v1.ListSessionsResponse",
    } as unknown as ListSessionsResponse);

    vi.mocked(authClient.listAuthEvents).mockResolvedValue({
      events: [
        {
          id: BigInt(1),
          userId: BigInt(1),
          username: "admin",
          eventType: "login_success",
          detail: "ip: 127.0.0.1",
          createdAt: { seconds: BigInt(1700000000), nanos: 0 },
          $typeName: "dmanager.v1.AuthEvent",
        },
        {
          id: BigInt(2),
          userId: BigInt(1),
          username: "admin",
          eventType: "session_revoked",
          detail: "action: revoke_single",
          createdAt: { seconds: BigInt(1700001000), nanos: 0 },
          $typeName: "dmanager.v1.AuthEvent",
        },
      ],
      totalCount: BigInt(2),
      $typeName: "dmanager.v1.ListAuthEventsResponse",
    } as unknown as ListAuthEventsResponse);
  });

  it("renders general settings by default", async () => {
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByDisplayValue("https://gotify.example.com")).toBeInTheDocument();
      expect(screen.getByDisplayValue("token123")).toBeInTheDocument();
    });

    expect(screen.getByText("docker.io")).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
  });

  it("switches to Security tab and renders active sessions and auth events", async () => {
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /security & sessions/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /security & sessions/i }));

    await waitFor(() => {
      expect(screen.getByText("Chrome · Linux")).toBeInTheDocument();
      expect(screen.getByText("Current Session")).toBeInTheDocument();
      expect(screen.getByText("Safari · iOS")).toBeInTheDocument();
      expect(screen.getByText("Login Success")).toBeInTheDocument();
      expect(screen.getByText("Session Revoked")).toBeInTheDocument();
    });
  });

  it("optimistically revokes session and calls API", async () => {
    vi.mocked(authClient.revokeSession).mockResolvedValue({
      $typeName: "dmanager.v1.RevokeSessionResponse",
    } as unknown as RevokeSessionResponse);

    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /security & sessions/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /security & sessions/i }));

    await waitFor(() => {
      expect(screen.getByText("Safari · iOS")).toBeInTheDocument();
    });

    const revokeBtn = screen.getByRole("button", { name: /^Revoke$/i });
    fireEvent.click(revokeBtn);

    await waitFor(() => {
      expect(authClient.revokeSession).toHaveBeenCalledWith({ sessionId: "sess-2" });
      expect(screen.queryByText("Safari · iOS")).not.toBeInTheDocument();
    });
  });

  it("revokes all other sessions when requested", async () => {
    vi.mocked(authClient.revokeAllOtherSessions).mockResolvedValue({
      revokedCount: BigInt(1),
      $typeName: "dmanager.v1.RevokeAllOtherSessionsResponse",
    } as unknown as RevokeAllOtherSessionsResponse);

    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /security & sessions/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /security & sessions/i }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /revoke all others/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /revoke all others/i }));

    await waitFor(() => {
      expect(authClient.revokeAllOtherSessions).toHaveBeenCalled();
    });
  });

  it("renders passkeys list with friendly name and synced badge", async () => {
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /security & sessions/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /security & sessions/i }));

    await waitFor(() => {
      expect(screen.getByText("Work MacBook")).toBeInTheDocument();
      expect(screen.getByText("Model: iCloud Keychain")).toBeInTheDocument();
      expect(screen.getByText("Synced Passkey")).toBeInTheDocument();
    });
  });

  it("deletes a passkey successfully", async () => {
    vi.mocked(authClient.deletePasskey).mockResolvedValue({
      $typeName: "dmanager.v1.DeletePasskeyResponse",
    });

    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /security & sessions/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /security & sessions/i }));

    await waitFor(() => {
      expect(screen.getByText("Work MacBook")).toBeInTheDocument();
    });

    const deleteBtn = screen.getByRole("button", { name: /^Delete$/i });
    fireEvent.click(deleteBtn);

    await waitFor(() => {
      expect(authClient.deletePasskey).toHaveBeenCalledWith({ id: "cred-1" });
      expect(screen.queryByText("Work MacBook")).not.toBeInTheDocument();
    });
  });
});
