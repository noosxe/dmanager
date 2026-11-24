import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { authClient, settingsClient } from "../client";
import { ToastProvider, useToast } from "../context/ToastContext";
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

// Mock @tanstack/react-router
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    className,
    onClick,
  }: {
    children?: React.ReactNode;
    className?: string;
    onClick?: (e: React.MouseEvent) => void;
  }) => (
    <button
      type="button"
      className={className}
      onClick={(e) => {
        e.preventDefault();
        if (onClick) onClick(e);
      }}
    >
      {children}
    </button>
  ),
  useParams: () => ({ tab: "general" }),
}));

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

// Toasts render in DashboardLayout, outside these tests; this prints them.
function ToastList() {
  const { toasts } = useToast();
  return (
    <ul>
      {toasts.map((t) => (
        <li key={t.id}>{t.message}</li>
      ))}
    </ul>
  );
}

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
          userVerified: true,
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
      auditRetentionDays: 180,
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
        {
          id: BigInt(3),
          userId: BigInt(1),
          username: "admin",
          eventType: "passkey_clone_warning",
          detail: "name: Work MacBook",
          createdAt: { seconds: BigInt(1700002000), nanos: 0 },
          $typeName: "dmanager.v1.AuthEvent",
        },
      ],
      totalCount: BigInt(3),
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

    // Audit retention select shows the effective server value.
    expect(vi.mocked(settingsClient.getSettings).mock.calls.length).toBeGreaterThan(0);
    const retentionSelect = screen.getByLabelText("Audit log retention") as HTMLSelectElement;
    expect(retentionSelect).toHaveValue("180");
    expect(screen.getByText("Audit Log Retention")).toBeInTheDocument();
  });

  it("applies the audit retention window immediately on change", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByLabelText("Audit log retention")).toHaveValue("180");
    });

    // Closed preset select: no save click — changing it persists at once.
    await user.selectOptions(screen.getByLabelText("Audit log retention"), "7");

    await waitFor(() => {
      expect(settingsClient.updateSettings).toHaveBeenCalledWith(
        expect.objectContaining({ auditRetentionDays: 7 }),
      );
    });
    // The select keeps the new window (toasts render in DashboardLayout,
    // outside this harness — the persisted call above is the assertion).
    expect(screen.getByLabelText("Audit log retention")).toHaveValue("7");
  });

  it("renders the Audit Logs card separately from notifications", async () => {
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Audit Logs" })).toBeInTheDocument();
    });
    expect(
      screen.getByRole("heading", { name: "Notification Configurations" }),
    ).toBeInTheDocument();

    // The retention select must live in the Audit Logs card, not beside the
    // Gotify fields: its own card heading appears between the two.
    const auditHeading = screen.getByRole("heading", { name: "Audit Logs" });
    const notifHeading = screen.getByRole("heading", { name: "Notification Configurations" });
    expect(
      auditHeading.compareDocumentPosition(notifHeading) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBe(0);
    expect(
      notifHeading.compareDocumentPosition(auditHeading) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("renders exactly the five retention presets", async () => {
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByLabelText("Audit log retention")).toBeInTheDocument();
    });
    const options = within(screen.getByLabelText("Audit log retention")).getAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual([
      "7 days",
      "1 month",
      "3 months",
      "6 months",
      "1 year",
    ]);
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
      // Known event types get a label, not the raw event string.
      expect(screen.getByText("Passkey Counter Warning")).toBeInTheDocument();
      expect(screen.queryByText("passkey_clone_warning")).not.toBeInTheDocument();
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

    // The destructive action requires confirmation (#178).
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAccessibleName("Revoke session?");
    expect(dialog).toHaveAccessibleDescription(
      "The session on Safari · iOS will be signed out. It can sign in again with your credentials.",
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

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

    // Bulk revocation is destructive too — confirm through the dialog (#178).
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAccessibleName("Revoke other sessions?");
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke all" }));

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

  it("shows the server's message when the authenticator made a non-discoverable passkey", async () => {
    const message =
      "This authenticator created a passkey that can't be used for sign-in without a username. Use a passkey provider or a security key that supports discoverable credentials.";
    vi.mocked(authClient.beginPasskeyRegistration).mockResolvedValue({
      optionsJson: "{}",
    } as never);
    vi.mocked(authClient.finishPasskeyRegistration).mockRejectedValue(
      new ConnectError(message, Code.FailedPrecondition),
    );
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );
    await user.click(await screen.findByRole("button", { name: /security & sessions/i }));
    await user.click(await screen.findByRole("button", { name: /add passkey/i }));
    await user.click(await screen.findByRole("button", { name: /prompt device authenticator/i }));

    await waitFor(() => {
      expect(screen.getAllByText(message).length).toBeGreaterThan(0);
    });
  });

  it("says the authenticator is already registered on InvalidStateError", async () => {
    const { create } = await import("@github/webauthn-json");
    vi.mocked(authClient.beginPasskeyRegistration).mockResolvedValue({
      optionsJson: "{}",
    } as never);
    vi.mocked(create).mockRejectedValueOnce(
      new DOMException(
        "The user attempted to register an authenticator that contains one of the credentials already registered",
        "InvalidStateError",
      ),
    );
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );
    await user.click(await screen.findByRole("button", { name: /security & sessions/i }));
    await user.click(await screen.findByRole("button", { name: /add passkey/i }));
    await user.click(await screen.findByRole("button", { name: /prompt device authenticator/i }));

    await waitFor(() => {
      expect(
        screen.getAllByText(/This authenticator is already registered\./).length,
      ).toBeGreaterThan(0);
    });
    expect(authClient.finishPasskeyRegistration).not.toHaveBeenCalled();
  });

  it("flags a passkey with a counter warning and explains it in a tooltip", async () => {
    vi.mocked(authClient.listPasskeys).mockResolvedValue({
      passkeys: [
        {
          id: "cred-9",
          name: "Old key",
          friendlyDeviceName: "Example Security Key",
          backupEligible: false,
          cloneWarning: true,
          createdAt: { seconds: BigInt(1700000000), nanos: 0 },
          $typeName: "dmanager.v1.Passkey",
        },
      ],
      $typeName: "dmanager.v1.ListPasskeysResponse",
    } as unknown as ListPasskeysResponse);
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /security & sessions/i }));

    const badge = await screen.findByText("Counter Warning");
    expect(badge.closest("[title]")).toHaveAttribute(
      "title",
      expect.stringContaining("until the passkey is deleted"),
    );
  });

  it("flags a passkey registered without user verification", async () => {
    vi.mocked(authClient.listPasskeys).mockResolvedValue({
      passkeys: [
        {
          id: "cred-8",
          name: "PIN-less key",
          friendlyDeviceName: "Example Security Key",
          backupEligible: false,
          cloneWarning: false,
          userVerified: false,
          createdAt: { seconds: BigInt(1700000000), nanos: 0 },
          $typeName: "dmanager.v1.Passkey",
        },
      ],
      $typeName: "dmanager.v1.ListPasskeysResponse",
    } as unknown as ListPasskeysResponse);
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /security & sessions/i }));

    const badge = await screen.findByText("No User Verification");
    expect(badge.closest("[title]")).toHaveAttribute("title", expect.stringContaining("required"));
    expect(screen.queryByText("Counter Warning")).not.toBeInTheDocument();
  });

  it("treats a cancelled browser prompt as a quiet cancel when adding a passkey", async () => {
    const { create } = await import("@github/webauthn-json");
    vi.mocked(authClient.beginPasskeyRegistration).mockResolvedValue({
      optionsJson: "{}",
    } as never);
    vi.mocked(create).mockRejectedValueOnce(new DOMException("denied", "NotAllowedError"));
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );
    await user.click(await screen.findByRole("button", { name: /security & sessions/i }));
    await user.click(await screen.findByRole("button", { name: /add passkey/i }));
    await user.click(await screen.findByRole("button", { name: /prompt device authenticator/i }));

    await waitFor(() => expect(create).toHaveBeenCalled());
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /prompt device authenticator/i })).toBeEnabled(),
    );
    expect(screen.queryByText(/Passkey registration failed/)).not.toBeInTheDocument();
    expect(authClient.finishPasskeyRegistration).not.toHaveBeenCalled();
  });

  it("disables Add Passkey with an explanation when WebAuthn is unavailable", async () => {
    Object.defineProperty(window, "isSecureContext", { value: false, configurable: true });
    try {
      render(
        <ToastProvider>
          <Settings />
        </ToastProvider>,
      );
      fireEvent.click(await screen.findByRole("button", { name: /security & sessions/i }));
      expect(await screen.findByRole("button", { name: /add passkey/i })).toBeDisabled();
      expect(screen.getByRole("note")).toHaveTextContent(/secure connection/);
    } finally {
      Object.defineProperty(window, "isSecureContext", { value: true, configurable: true });
    }
  });

  it("hints at discoverable credentials on NotSupportedError", async () => {
    const { create } = await import("@github/webauthn-json");
    vi.mocked(authClient.beginPasskeyRegistration).mockResolvedValue({
      optionsJson: "{}",
    } as never);
    vi.mocked(create).mockRejectedValueOnce(
      new DOMException("No available authenticator supports resident keys", "NotSupportedError"),
    );
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );
    await user.click(await screen.findByRole("button", { name: /security & sessions/i }));
    await user.click(await screen.findByRole("button", { name: /add passkey/i }));
    await user.click(await screen.findByRole("button", { name: /prompt device authenticator/i }));

    await waitFor(() => {
      expect(screen.getAllByText(/works without a username/).length).toBeGreaterThan(0);
    });
    expect(authClient.finishPasskeyRegistration).not.toHaveBeenCalled();
  });

  it("deletes a passkey successfully", async () => {
    vi.mocked(authClient.deletePasskey).mockResolvedValue({
      $typeName: "dmanager.v1.DeletePasskeyResponse",
      revokedSessions: 0n,
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

    // Passkey deletion requires confirmation (#178) — danger focuses Cancel.
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAccessibleName("Delete passkey?");
    expect(dialog).toHaveAccessibleDescription(
      'Passkey "Work MacBook" will be removed from your account. You can no longer sign in with it. The last way to sign in cannot be removed.',
    );
    expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Cancel" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(authClient.deletePasskey).toHaveBeenCalledWith({
        id: "cred-1",
        signOutOtherSessions: true,
      });
      expect(screen.queryByText("Work MacBook")).not.toBeInTheDocument();
    });
  });

  it("deletes a passkey without signing out other sessions when the option is unticked", async () => {
    vi.mocked(authClient.deletePasskey).mockResolvedValue({
      $typeName: "dmanager.v1.DeletePasskeyResponse",
      revokedSessions: 0n,
    });

    render(
      <ToastProvider>
        <Settings />
      </ToastProvider>,
    );

    fireEvent.click(await screen.findByRole("button", { name: /security & sessions/i }));
    await screen.findByText("Work MacBook");
    fireEvent.click(screen.getByRole("button", { name: /^Delete$/i }));

    const dialog = screen.getByRole("dialog");
    const box = within(dialog).getByRole("checkbox", { name: "Also sign out all other sessions" });
    expect(box).toBeChecked();
    fireEvent.click(box);
    expect(box).not.toBeChecked();
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(authClient.deletePasskey).toHaveBeenCalledWith({
        id: "cred-1",
        signOutOtherSessions: false,
      });
    });
  });

  it("reloads the lists and explains a partial failure when the passkey was removed but sessions were not", async () => {
    vi.mocked(authClient.deletePasskey).mockRejectedValueOnce(
      new ConnectError("passkey removed, but failed to revoke other sessions: boom", Code.Internal),
    );

    render(
      <ToastProvider>
        <Settings />
        <ToastList />
      </ToastProvider>,
    );

    fireEvent.click(await screen.findByRole("button", { name: /security & sessions/i }));
    await screen.findByText("Work MacBook");

    // After the failure the server no longer has the passkey.
    const remaining = (await vi.mocked(authClient.listPasskeys)({})).passkeys.filter(
      (p) => p.id !== "cred-1",
    );
    vi.mocked(authClient.listPasskeys).mockResolvedValue({
      $typeName: "dmanager.v1.ListPasskeysResponse",
      passkeys: remaining,
    } as never);

    fireEvent.click(screen.getByRole("button", { name: /^Delete$/i }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(screen.queryByText("Work MacBook")).not.toBeInTheDocument();
    });
    expect(
      await screen.findByText(/passkey was removed, but other sessions could not be signed out/i),
    ).toBeInTheDocument();
    expect(authClient.listSessions).toHaveBeenCalledTimes(2);
  });

  it("does not delete a passkey when the confirmation is cancelled", async () => {
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

    fireEvent.click(screen.getByRole("button", { name: /^Delete$/i }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    // Dialog dismissed, nothing dispatched, passkey still listed.
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
    expect(authClient.deletePasskey).not.toHaveBeenCalled();
    expect(screen.getByText("Work MacBook")).toBeInTheDocument();
  });
});
