import { create as webauthnCreate } from "@github/webauthn-json";
import { useParams } from "@tanstack/react-router";
import {
  Activity,
  Check,
  CheckCircle2,
  Clock,
  Edit2,
  Fingerprint,
  Globe,
  Key,
  Laptop,
  Loader2,
  LogOut,
  Play,
  Plus,
  RefreshCw,
  Save,
  Settings as SettingsIcon,
  Shield,
  ShieldAlert,
  Smartphone,
  Trash2,
  User,
  X,
} from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useState } from "react";

import { authClient, settingsClient } from "../client";
import { useToast } from "../context/ToastContext";
import type { AuthEvent, Passkey, Session } from "../gen/proto/dmanager/v1/auth_pb";
import type { RegistryStatus } from "../gen/proto/dmanager/v1/settings_pb";
import { ConfirmDialog } from "./ConfirmDialog";
import { PageTabs, type PageTabItem } from "./PageTabs";

interface SettingsProps {
  initialTab?: "general" | "security";
}

// The destructive action awaiting ConfirmDialog confirmation (#178): one
// modal at a time; the kind picks the title/message/verb.
type PendingDestructive =
  | { kind: "passkey"; passkey: Passkey }
  | { kind: "session"; session: Session }
  | { kind: "allSessions" };

// Consequence-focused copy per design.md §11.4.
const destructiveDialogCopy = (pending: PendingDestructive) => {
  switch (pending.kind) {
    case "passkey":
      return {
        title: "Delete passkey?",
        message: `Passkey "${pending.passkey.name}" will be removed from your account. If it is your only remaining credential, you will be locked out.`,
        confirmLabel: "Delete",
      };
    case "session":
      return {
        title: "Revoke session?",
        message: `The session on ${pending.session.deviceLabel} will be signed out. It can sign in again with your credentials.`,
        confirmLabel: "Revoke",
      };
    default:
      return {
        title: "Revoke other sessions?",
        message:
          "All sessions except this one will be signed out. You stay signed in on this device.",
        confirmLabel: "Revoke all",
      };
  }
};

export function Settings({ initialTab }: SettingsProps = {}) {
  const params = useParams({ strict: false }) as { tab?: string } | undefined;
  const routeTab = params?.tab;

  const [localTab, setLocalTab] = useState<"general" | "security">(
    routeTab === "security" || initialTab === "security" ? "security" : "general",
  );

  useEffect(() => {
    if (routeTab === "security" || routeTab === "general") {
      setLocalTab(routeTab);
    }
  }, [routeTab]);

  const activeTab = localTab;

  // General Settings State
  const [gotifyUrl, setGotifyUrl] = useState("");
  const [gotifyToken, setGotifyToken] = useState("");
  // Audit retention presets (issue #222) — int32 wire values, days.
  const AUDIT_RETENTION_PRESETS = [
    { value: 7, label: "7 days" },
    { value: 30, label: "1 month" },
    { value: 90, label: "3 months" },
    { value: 180, label: "6 months" },
    { value: 365, label: "1 year" },
  ] as const;
  const [auditRetentionDays, setAuditRetentionDays] = useState<number>(90);
  const [loading, setLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isTesting, setIsTesting] = useState(false);

  // Status feedback
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{
    success: boolean;
    message: string;
  } | null>(null);

  // Registry status state
  const [registryStatuses, setRegistryStatuses] = useState<RegistryStatus[]>([]);
  const [registriesLoading, setRegistriesLoading] = useState(true);
  const [registriesError, setRegistriesError] = useState<string | null>(null);

  // Security Tab State: Passkeys, Sessions & Audit Events
  const [passkeys, setPasskeys] = useState<Passkey[]>([]);
  const [passkeysLoading, setPasskeysLoading] = useState(false);
  const [passkeysError, setPasskeysError] = useState<string | null>(null);
  const [isAddingPasskey, setIsAddingPasskey] = useState(false);
  const [showAddPasskey, setShowAddPasskey] = useState(false);
  const [newPasskeyName, setNewPasskeyName] = useState("");
  const [editingPasskeyId, setEditingPasskeyId] = useState<string | null>(null);
  const [editingPasskeyName, setEditingPasskeyName] = useState("");
  const [deletingPasskeyId, setDeletingPasskeyId] = useState<string | null>(null);
  const [serverInfo, setServerInfo] = useState<{
    passkeyLoginEnabled: boolean;
    rpId: string;
    origins: string[];
  } | null>(null);

  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsError, setSessionsError] = useState<string | null>(null);
  const [isRevokingOther, setIsRevokingOther] = useState(false);
  const [revokingSessionId, setRevokingSessionId] = useState<string | null>(null);

  // Destructive confirmations (design.md §11.4): one modal at a time — the
  // pending target decides title/message/verb; #178 gates passkey deletion,
  // session revocation, and revoke-all-others behind the danger dialog.
  const [pendingDestructive, setPendingDestructive] = useState<PendingDestructive | null>(null);
  const destructiveBusy =
    (pendingDestructive?.kind === "passkey" &&
      deletingPasskeyId === pendingDestructive.passkey.id) ||
    (pendingDestructive?.kind === "session" &&
      revokingSessionId === pendingDestructive.session.sessionId) ||
    (pendingDestructive?.kind === "allSessions" && isRevokingOther);

  const [authEvents, setAuthEvents] = useState<AuthEvent[]>([]);
  const [authEventsLoading, setAuthEventsLoading] = useState(false);
  const [authEventsError, setAuthEventsError] = useState<string | null>(null);

  const toast = useToast();

  // Load existing general settings
  useEffect(() => {
    async function loadSettings() {
      try {
        const resp = await settingsClient.getSettings({});
        setGotifyUrl(resp.gotifyUrl);
        setGotifyToken(resp.gotifyToken);
        setAuditRetentionDays(resp.auditRetentionDays);
      } catch (err: unknown) {
        console.error("Failed to load settings:", err);
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    }
    loadSettings();
  }, []);

  const fetchRegistryStatus = useCallback(async () => {
    setRegistriesLoading(true);
    setRegistriesError(null);
    try {
      const resp = await settingsClient.getRegistryStatus({});
      setRegistryStatuses(resp.registries);
    } catch (err: unknown) {
      console.error("Failed to load registry statuses:", err);
      setRegistriesError(err instanceof Error ? err.message : String(err));
    } finally {
      setRegistriesLoading(false);
    }
  }, []);

  // Load registry status
  useEffect(() => {
    fetchRegistryStatus();
  }, [fetchRegistryStatus]);

  // Load Passkeys
  const fetchPasskeys = useCallback(async () => {
    setPasskeysLoading(true);
    setPasskeysError(null);
    try {
      const resp = await authClient.listPasskeys({});
      setPasskeys(resp.passkeys);
    } catch (err: unknown) {
      console.error("Failed to load passkeys:", err);
      setPasskeysError(err instanceof Error ? err.message : String(err));
    } finally {
      setPasskeysLoading(false);
    }
  }, []);

  // Load Server Status (RP info)
  const fetchServerStatus = useCallback(async () => {
    try {
      const resp = await authClient.getServerStatus({});
      setServerInfo({
        passkeyLoginEnabled: resp.passkeyLoginEnabled,
        rpId: resp.rpId,
        origins: resp.origins,
      });
    } catch (err: unknown) {
      console.error("Failed to load server status:", err);
    }
  }, []);

  // Load Sessions
  const fetchSessions = useCallback(async () => {
    setSessionsLoading(true);
    setSessionsError(null);
    try {
      const resp = await authClient.listSessions({});
      setSessions(resp.sessions);
    } catch (err: unknown) {
      console.error("Failed to load sessions:", err);
      setSessionsError(err instanceof Error ? err.message : String(err));
    } finally {
      setSessionsLoading(false);
    }
  }, []);

  // Load Auth Events
  const fetchAuthEvents = useCallback(async () => {
    setAuthEventsLoading(true);
    setAuthEventsError(null);
    try {
      const resp = await authClient.listAuthEvents({ limit: 50, offset: 0 });
      setAuthEvents(resp.events);
    } catch (err: unknown) {
      console.error("Failed to load auth events:", err);
      setAuthEventsError(err instanceof Error ? err.message : String(err));
    } finally {
      setAuthEventsLoading(false);
    }
  }, []);

  // Load Security data when tab switches to security
  useEffect(() => {
    if (activeTab === "security") {
      fetchPasskeys();
      fetchServerStatus();
      fetchSessions();
      fetchAuthEvents();
    }
  }, [activeTab, fetchPasskeys, fetchServerStatus, fetchSessions, fetchAuthEvents]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);
    setTestResult(null);

    setIsSaving(true);
    try {
      await settingsClient.updateSettings({
        gotifyUrl: gotifyUrl.trim(),
        gotifyToken: gotifyToken.trim(),
        auditRetentionDays,
      });
      setSuccess("Settings saved successfully.");
      toast.success("Settings saved successfully.");
    } catch (err: unknown) {
      console.error("Failed to save settings:", err);
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      toast.error(`Failed to save settings: ${msg}`);
    } finally {
      setIsSaving(false);
    }
  };

  // Retention is a closed preset select — apply on change instead of asking
  // for a separate save click. The wire update is the full settings object,
  // so the loaded Gotify values ride along unchanged. On failure the select
  // reverts to the previously effective window.
  const handleRetentionChange = async (days: number) => {
    const previous = auditRetentionDays;
    setAuditRetentionDays(days);
    setIsSaving(true);
    setError(null);
    setSuccess(null);
    try {
      await settingsClient.updateSettings({
        gotifyUrl: gotifyUrl.trim(),
        gotifyToken: gotifyToken.trim(),
        auditRetentionDays: days,
      });
      toast.success("Audit log retention updated.");
    } catch (err: unknown) {
      setAuditRetentionDays(previous);
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      toast.error(`Failed to update audit log retention: ${msg}`);
    } finally {
      setIsSaving(false);
    }
  };

  const handleTestConnection = async () => {
    setError(null);
    setSuccess(null);
    setTestResult(null);
    setIsTesting(true);
    toast.info("Testing connection to Gotify server...");

    try {
      const resp = await settingsClient.testGotifyNotification({
        gotifyUrl: gotifyUrl.trim(),
        gotifyToken: gotifyToken.trim(),
      });

      if (resp.success) {
        const msg = "Connection test succeeded! A test notification was sent.";
        setTestResult({
          success: true,
          message: msg,
        });
        toast.success(msg);
      } else {
        const msg = resp.errorMessage || "Connection test failed.";
        setTestResult({
          success: false,
          message: msg,
        });
        toast.error(msg);
      }
    } catch (err: unknown) {
      console.error("Test connection failed:", err);
      const msg = err instanceof Error ? err.message : String(err);
      setTestResult({
        success: false,
        message: msg,
      });
      toast.error(`Connection test failed: ${msg}`);
    } finally {
      setIsTesting(false);
    }
  };

  const handleRevokeSession = async (sessionId: string) => {
    const previousSessions = [...sessions];
    // Optimistic UI update
    setSessions((prev) => prev.filter((s) => s.sessionId !== sessionId));
    setRevokingSessionId(sessionId);

    try {
      await authClient.revokeSession({ sessionId });
      toast.success("Session revoked successfully.");
      fetchAuthEvents();
    } catch (err: unknown) {
      console.error("Failed to revoke session:", err);
      // Rollback
      setSessions(previousSessions);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to revoke session: ${msg}`);
    } finally {
      setRevokingSessionId(null);
    }
  };

  const handleRevokeAllOtherSessions = async () => {
    setIsRevokingOther(true);
    try {
      const resp = await authClient.revokeAllOtherSessions({});
      toast.success(`Revoked ${resp.revokedCount} other session(s).`);
      await fetchSessions();
      fetchAuthEvents();
    } catch (err: unknown) {
      console.error("Failed to revoke other sessions:", err);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to revoke other sessions: ${msg}`);
    } finally {
      setIsRevokingOther(false);
    }
  };

  // Dispatches the confirmed destructive action. The handlers catch their
  // own errors (toasts are the failure feedback), so the dialog closes once
  // the outcome settles — same contract as the image-delete dialog.
  const confirmDestructive = async () => {
    if (pendingDestructive === null) {
      return;
    }
    if (pendingDestructive.kind === "passkey") {
      await handleDeletePasskey(pendingDestructive.passkey.id);
    } else if (pendingDestructive.kind === "session") {
      await handleRevokeSession(pendingDestructive.session.sessionId);
    } else {
      await handleRevokeAllOtherSessions();
    }
    setPendingDestructive(null);
  };

  const handleAddPasskey = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsAddingPasskey(true);
    setPasskeysError(null);
    try {
      const name = newPasskeyName.trim();
      const beginResp = await authClient.beginPasskeyRegistration({ name });
      const options = JSON.parse(beginResp.optionsJson);
      const credential = await webauthnCreate({ publicKey: options });
      const finishResp = await authClient.finishPasskeyRegistration({
        name,
        responseJson: JSON.stringify(credential),
      });
      if (finishResp.passkey) {
        setPasskeys((prev) => [finishResp.passkey as Passkey, ...prev]);
      }
      setShowAddPasskey(false);
      setNewPasskeyName("");
      toast.success("Passkey registered successfully.");
      fetchAuthEvents();
    } catch (err: unknown) {
      console.error("Passkey registration failed:", err);
      const msg = err instanceof Error ? err.message : String(err);
      setPasskeysError(msg);
      toast.error(`Passkey registration failed: ${msg}`);
    } finally {
      setIsAddingPasskey(false);
    }
  };

  const handleRenamePasskey = async (id: string) => {
    if (!editingPasskeyName.trim()) return;
    try {
      const resp = await authClient.renamePasskey({
        id,
        name: editingPasskeyName.trim(),
      });
      if (resp.passkey) {
        setPasskeys((prev) => prev.map((p) => (p.id === id ? (resp.passkey as Passkey) : p)));
      }
      setEditingPasskeyId(null);
      setEditingPasskeyName("");
      toast.success("Passkey renamed successfully.");
    } catch (err: unknown) {
      console.error("Failed to rename passkey:", err);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to rename passkey: ${msg}`);
    }
  };

  const handleDeletePasskey = async (id: string) => {
    setDeletingPasskeyId(id);
    try {
      await authClient.deletePasskey({ id });
      setPasskeys((prev) => prev.filter((p) => p.id !== id));
      toast.success("Passkey removed successfully.");
      fetchAuthEvents();
    } catch (err: unknown) {
      console.error("Failed to delete passkey:", err);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to delete passkey: ${msg}`);
    } finally {
      setDeletingPasskeyId(null);
    }
  };

  const renderPasskeyIcon = (friendlyName: string) => {
    const f = friendlyName.toLowerCase();
    if (f.includes("yubikey") || f.includes("security key")) {
      return <Key size={18} style={{ color: "var(--accent)" }} />;
    }
    return <Fingerprint size={18} style={{ color: "var(--accent)" }} />;
  };

  const renderDeviceIcon = (label: string) => {
    const l = label.toLowerCase();
    if (l.includes("ios") || l.includes("android") || l.includes("mobile")) {
      return <Smartphone size={18} style={{ color: "var(--accent)" }} />;
    }
    if (l.includes("windows") || l.includes("mac") || l.includes("linux")) {
      return <Laptop size={18} style={{ color: "var(--accent)" }} />;
    }
    return <Globe size={18} style={{ color: "var(--accent)" }} />;
  };

  const renderEventBadge = (eventType: string) => {
    switch (eventType) {
      case "login_success":
        return <span className="auth-event-badge auth-event-badge-success">Login Success</span>;
      case "login_failed":
        return <span className="auth-event-badge auth-event-badge-error">Login Failed</span>;
      case "rate_limited":
        return <span className="auth-event-badge auth-event-badge-warning">Rate Limited</span>;
      case "logout":
        return <span className="auth-event-badge auth-event-badge-info">Logout</span>;
      case "session_revoked":
        return <span className="auth-event-badge auth-event-badge-warning">Session Revoked</span>;
      case "setup_admin":
        return <span className="auth-event-badge auth-event-badge-info">Setup Admin</span>;
      default:
        return <span className="auth-event-badge auth-event-badge-info">{eventType}</span>;
    }
  };

  if (loading) {
    return (
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          padding: "48px",
        }}
      >
        <Loader2 className="animate-spin text-accent" size={32} />
        <span style={{ marginLeft: "12px", fontSize: "14px" }}>Loading settings...</span>
      </div>
    );
  }

  // Tab bar items (§7.5): optimistic local state via onClick; the route
  // remains the source of truth.
  const settingsTabs: PageTabItem[] = [
    {
      to: "/settings/$tab",
      params: { tab: "general" },
      icon: SettingsIcon,
      label: "General",
      active: activeTab === "general",
      onClick: () => setLocalTab("general"),
    },
    {
      to: "/settings/$tab",
      params: { tab: "security" },
      icon: Shield,
      label: "Security & Sessions",
      active: activeTab === "security",
      onClick: () => setLocalTab("security"),
    },
  ];

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "24px",
        padding: "24px",
        maxWidth: "800px",
        margin: "0 auto",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "12px",
        }}
      >
        <SettingsIcon size={28} style={{ color: "var(--accent)" }} />
        <h1
          style={{
            fontSize: "24px",
            fontWeight: "700",
            color: "var(--text-h)",
            margin: 0,
          }}
        >
          Settings
        </h1>
      </div>

      <PageTabs tabs={settingsTabs} />

      {activeTab === "general" && (
        <>
          <div className="logs-viewer-card">
            <div>
              <h2
                style={{
                  fontSize: "18px",
                  fontWeight: "600",
                  color: "var(--text-h)",
                  margin: "0 0 8px 0",
                }}
              >
                Notification Configurations
              </h2>
              <p style={{ fontSize: "14px", opacity: 0.8, margin: "0 0 24px 0" }}>
                Configure integrations to receive real-time notifications about image updates and
                docker container re-deployments.
              </p>
            </div>

            {error && (
              <div className="auth-error-banner" style={{ marginBottom: "16px" }}>
                <ShieldAlert size={18} className="auth-error-icon" />
                <span>{error}</span>
              </div>
            )}

            {success && (
              <div
                className="auth-error-banner"
                style={{
                  background: "rgba(16, 185, 129, 0.08)",
                  border: "1px solid rgba(16, 185, 129, 0.2)",
                  color: "#10b981",
                  marginBottom: "16px",
                }}
              >
                <CheckCircle2 size={18} style={{ marginRight: "8px" }} />
                <span>{success}</span>
              </div>
            )}

            <form
              onSubmit={handleSave}
              style={{ display: "flex", flexDirection: "column", gap: "20px" }}
            >
              <div className="form-group">
                <label
                  htmlFor="gotify-url"
                  style={{
                    fontWeight: "600",
                    fontSize: "14px",
                    display: "block",
                    marginBottom: "8px",
                    color: "var(--text-h)",
                  }}
                >
                  Gotify Server URL
                </label>
                <div className="input-wrapper">
                  <input
                    id="gotify-url"
                    type="text"
                    value={gotifyUrl}
                    onChange={(e) => setGotifyUrl(e.target.value)}
                    placeholder="e.g. https://gotify.example.com"
                    className="settings-input"
                    disabled={isSaving || isTesting}
                  />
                </div>
                <p style={{ fontSize: "12px", opacity: 0.6, margin: "4px 0 0 0" }}>
                  The base URL of your running Gotify server instance.
                </p>
              </div>

              <div className="form-group">
                <label
                  htmlFor="gotify-token"
                  style={{
                    fontWeight: "600",
                    fontSize: "14px",
                    display: "block",
                    marginBottom: "8px",
                    color: "var(--text-h)",
                  }}
                >
                  Application Token
                </label>
                <div className="input-wrapper">
                  <input
                    id="gotify-token"
                    type="password"
                    value={gotifyToken}
                    onChange={(e) => setGotifyToken(e.target.value)}
                    placeholder="Enter Gotify application token"
                    className="settings-input"
                    disabled={isSaving || isTesting}
                  />
                </div>
                <p style={{ fontSize: "12px", opacity: 0.6, margin: "4px 0 0 0" }}>
                  The secret application token generated inside your Gotify server panel.
                </p>
              </div>

              {testResult && (
                <div
                  className="auth-error-banner"
                  style={{
                    background: testResult.success ? "rgba(16, 185, 129, 0.08)" : "var(--error-bg)",
                    border: testResult.success
                      ? "1px solid rgba(16, 185, 129, 0.2)"
                      : "1px solid var(--error-border)",
                    color: testResult.success ? "#10b981" : "var(--error)",
                    marginBottom: "16px",
                  }}
                >
                  {testResult.success ? (
                    <CheckCircle2 size={18} style={{ marginRight: "8px" }} />
                  ) : (
                    <ShieldAlert size={18} className="auth-error-icon" />
                  )}
                  <span>{testResult.message}</span>
                </div>
              )}

              <div
                style={{
                  display: "flex",
                  gap: "12px",
                  marginTop: "12px",
                  flexWrap: "wrap",
                }}
              >
                <button
                  type="submit"
                  disabled={isSaving || isTesting}
                  className="settings-btn-primary"
                >
                  {isSaving ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
                  <span>Save Settings</span>
                </button>

                <button
                  type="button"
                  onClick={handleTestConnection}
                  disabled={isSaving || isTesting}
                  className="settings-btn-secondary"
                >
                  {isTesting ? <Loader2 className="animate-spin" size={16} /> : <Play size={16} />}
                  <span>Test Connection</span>
                </button>
              </div>
            </form>
          </div>

          <div className="logs-viewer-card">
            <div>
              <h2
                style={{
                  fontSize: "18px",
                  fontWeight: "600",
                  color: "var(--text-h)",
                  margin: "0 0 8px 0",
                }}
              >
                Audit Logs
              </h2>
              <p style={{ fontSize: "14px", opacity: 0.8, margin: "0 0 24px 0" }}>
                How long the mutation and system-action history is kept in the local database.
                Entries older than the window are deleted when the next entry is recorded.
              </p>
            </div>

            <div className="form-group" style={{ marginBottom: 0 }}>
              <label
                htmlFor="audit-retention"
                style={{
                  fontWeight: "600",
                  fontSize: "14px",
                  display: "block",
                  marginBottom: "8px",
                  color: "var(--text-h)",
                }}
              >
                Audit Log Retention
              </label>
              <div className="input-wrapper">
                <select
                  id="audit-retention"
                  aria-label="Audit log retention"
                  className="logs-select-filter"
                  style={{ width: "100%" }}
                  value={auditRetentionDays}
                  onChange={(e) => handleRetentionChange(Number(e.target.value))}
                  disabled={loading || isSaving || isTesting}
                >
                  {AUDIT_RETENTION_PRESETS.map((preset) => (
                    <option key={preset.value} value={preset.value}>
                      {preset.label}
                    </option>
                  ))}
                </select>
              </div>
              <p style={{ fontSize: "12px", opacity: 0.6, margin: "4px 0 0 0" }}>
                Applied immediately. Presets: 7 days, 1 month, 3 months, 6 months, 1 year.
              </p>
            </div>
          </div>

          <div className="logs-viewer-card">
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: "8px",
                flexWrap: "wrap",
                gap: "12px",
              }}
            >
              <div>
                <h2
                  style={{
                    fontSize: "18px",
                    fontWeight: "600",
                    color: "var(--text-h)",
                    margin: 0,
                  }}
                >
                  Private Registries Status
                </h2>
                <p style={{ fontSize: "14px", opacity: 0.8, margin: "4px 0 0 0" }}>
                  Status of configured private Docker registries from configuration.
                </p>
              </div>
              <button
                type="button"
                onClick={fetchRegistryStatus}
                disabled={registriesLoading}
                className="settings-btn-secondary"
                style={{ padding: "8px 16px" }}
              >
                {registriesLoading ? (
                  <Loader2 className="animate-spin" size={16} />
                ) : (
                  <RefreshCw size={16} />
                )}
                <span>Refresh Status</span>
              </button>
            </div>

            {registriesError && (
              <div className="auth-error-banner" style={{ marginBottom: "16px" }}>
                <ShieldAlert size={18} className="auth-error-icon" />
                <span>{registriesError}</span>
              </div>
            )}

            {registriesLoading && registryStatuses.length === 0 ? (
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  padding: "32px",
                }}
              >
                <Loader2 className="animate-spin text-accent" size={24} />
                <span style={{ marginLeft: "12px", fontSize: "14px" }}>
                  Checking registry status...
                </span>
              </div>
            ) : registryStatuses.length === 0 ? (
              <div
                style={{
                  padding: "32px",
                  textAlign: "center",
                  opacity: 0.6,
                  fontSize: "14px",
                  border: "1px dashed var(--border)",
                  borderRadius: "10px",
                  backgroundColor: "rgba(255, 255, 255, 0.02)",
                }}
              >
                No private registries configured. Define them in your server config.yaml.
              </div>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
                {registryStatuses.map((reg) => (
                  <div
                    key={reg.host}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      padding: "16px",
                      border: "1px solid var(--card-border)",
                      borderRadius: "12px",
                      background: "rgba(255, 255, 255, 0.03)",
                      backdropFilter: "blur(10px)",
                      flexWrap: "wrap",
                      gap: "12px",
                    }}
                  >
                    <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                      <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                        <span
                          style={{ fontWeight: "700", color: "var(--text-h)", fontSize: "15px" }}
                        >
                          {reg.host}
                        </span>
                        {reg.isHealthy ? (
                          <span
                            style={{
                              fontSize: "11px",
                              fontWeight: "600",
                              color: "#10b981",
                              background: "rgba(16, 185, 129, 0.08)",
                              border: "1px solid rgba(16, 185, 129, 0.2)",
                              padding: "2px 8px",
                              borderRadius: "9999px",
                            }}
                          >
                            Healthy
                          </span>
                        ) : (
                          <span
                            style={{
                              fontSize: "11px",
                              fontWeight: "600",
                              color: "var(--error)",
                              background: "var(--error-bg)",
                              border: "1px solid var(--error-border)",
                              padding: "2px 8px",
                              borderRadius: "9999px",
                            }}
                          >
                            Unhealthy
                          </span>
                        )}
                      </div>
                      <div style={{ fontSize: "13px", opacity: 0.8 }}>
                        Username:{" "}
                        <code
                          style={{
                            fontFamily: "var(--font-mono)",
                            background: "rgba(255,255,255,0.05)",
                            padding: "2px 6px",
                            borderRadius: "4px",
                          }}
                        >
                          {reg.username || "(anonymous)"}
                        </code>
                      </div>
                    </div>

                    {!reg.isConfigured ? (
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: "6px",
                          color: "var(--error)",
                          fontSize: "13px",
                        }}
                      >
                        <ShieldAlert size={16} />
                        <span>Incomplete configuration credentials</span>
                      </div>
                    ) : reg.isHealthy ? (
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: "6px",
                          color: "#10b981",
                          fontSize: "13px",
                        }}
                      >
                        <CheckCircle2 size={16} />
                        <span>Connected & Authorized</span>
                      </div>
                    ) : (
                      <div
                        style={{
                          maxWidth: "100%",
                          width: "100%",
                          marginTop: "4px",
                          padding: "8px 12px",
                          background: "var(--error-bg)",
                          border: "1px solid var(--error-border)",
                          color: "var(--error)",
                          borderRadius: "8px",
                          fontSize: "12px",
                          fontFamily: "var(--font-mono)",
                          wordBreak: "break-all",
                        }}
                      >
                        Error: {reg.errorMessage}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}

      {activeTab === "security" && (
        <div style={{ display: "flex", flexDirection: "column", gap: "24px" }}>
          {/* Passkeys / WebAuthn Section */}
          <div className="logs-viewer-card">
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: "16px",
                flexWrap: "wrap",
                gap: "12px",
              }}
            >
              <div>
                <h2
                  style={{
                    fontSize: "18px",
                    fontWeight: "600",
                    color: "var(--text-h)",
                    margin: 0,
                  }}
                >
                  Passkeys & Security Keys
                </h2>
                <p style={{ fontSize: "14px", opacity: 0.8, margin: "4px 0 0 0" }}>
                  Sign in without passwords using platform authenticators (Touch ID, Windows Hello,
                  Face ID) or hardware security keys (YubiKey).
                </p>
                {serverInfo?.rpId && (
                  <div
                    style={{
                      marginTop: "6px",
                      fontSize: "12px",
                      opacity: 0.7,
                      fontFamily: "var(--font-mono, monospace)",
                    }}
                  >
                    Relying Party: <strong>{serverInfo.rpId}</strong> · Origins:{" "}
                    <strong>{serverInfo.origins.join(", ") || "none"}</strong>
                  </div>
                )}
              </div>

              <div style={{ display: "flex", gap: "8px" }}>
                <button
                  type="button"
                  onClick={fetchPasskeys}
                  disabled={passkeysLoading}
                  className="settings-btn-secondary"
                  style={{ padding: "8px 12px" }}
                  aria-label="Refresh passkeys"
                >
                  {passkeysLoading ? (
                    <Loader2 className="animate-spin" size={16} />
                  ) : (
                    <RefreshCw size={16} />
                  )}
                </button>

                <button
                  type="button"
                  onClick={() => setShowAddPasskey(true)}
                  disabled={isAddingPasskey}
                  className="settings-btn-primary"
                  style={{ padding: "8px 14px" }}
                >
                  <Plus size={16} />
                  <span>Add Passkey</span>
                </button>
              </div>
            </div>

            {passkeysError && (
              <div className="auth-error-banner" style={{ marginBottom: "16px" }}>
                <ShieldAlert size={18} className="auth-error-icon" />
                <span>{passkeysError}</span>
              </div>
            )}

            {showAddPasskey && (
              <form
                onSubmit={handleAddPasskey}
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: "12px",
                  padding: "16px",
                  border: "1px solid var(--accent)",
                  borderRadius: "10px",
                  background: "rgba(59, 130, 246, 0.05)",
                  marginBottom: "16px",
                }}
              >
                <div
                  style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}
                >
                  <span style={{ fontWeight: "600", fontSize: "14px", color: "var(--text-h)" }}>
                    Register a New Passkey
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      setShowAddPasskey(false);
                      setNewPasskeyName("");
                    }}
                    style={{ background: "none", border: "none", cursor: "pointer", opacity: 0.6 }}
                  >
                    <X size={16} />
                  </button>
                </div>
                <div className="form-group" style={{ margin: 0 }}>
                  <label htmlFor="passkey-name" style={{ fontSize: "12px" }}>
                    Passkey Name / Label (optional)
                  </label>
                  <input
                    id="passkey-name"
                    type="text"
                    value={newPasskeyName}
                    onChange={(e) => setNewPasskeyName(e.target.value)}
                    placeholder="e.g. Work MacBook, YubiKey 5C, iPhone"
                    maxLength={50}
                    disabled={isAddingPasskey}
                  />
                </div>
                <div style={{ display: "flex", justifyContent: "flex-end", gap: "8px" }}>
                  <button
                    type="button"
                    onClick={() => setShowAddPasskey(false)}
                    disabled={isAddingPasskey}
                    className="settings-btn-secondary"
                    style={{ padding: "6px 12px", fontSize: "13px" }}
                  >
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={isAddingPasskey}
                    className="settings-btn-primary"
                    style={{ padding: "6px 14px", fontSize: "13px" }}
                  >
                    {isAddingPasskey ? (
                      <>
                        <Loader2 className="animate-spin" size={14} />
                        <span>Prompting Authenticator...</span>
                      </>
                    ) : (
                      <>
                        <Fingerprint size={14} />
                        <span>Prompt Device Authenticator</span>
                      </>
                    )}
                  </button>
                </div>
              </form>
            )}

            {passkeysLoading && passkeys.length === 0 ? (
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  padding: "32px",
                }}
              >
                <Loader2 className="animate-spin text-accent" size={24} />
                <span style={{ marginLeft: "12px", fontSize: "14px" }}>Loading passkeys...</span>
              </div>
            ) : passkeys.length === 0 ? (
              <div
                style={{
                  padding: "32px",
                  textAlign: "center",
                  opacity: 0.7,
                  fontSize: "14px",
                  border: "1px dashed var(--border)",
                  borderRadius: "10px",
                  lineHeight: "1.6",
                }}
              >
                <Fingerprint size={28} style={{ opacity: 0.5, margin: "0 auto 8px" }} />
                <div>No passkeys registered yet.</div>
                <div style={{ fontSize: "12px", opacity: 0.8, marginTop: "4px" }}>
                  Add a passkey to sign in seamlessly without passwords using Touch ID, Face ID,
                  Windows Hello, or a YubiKey.
                </div>
              </div>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
                {passkeys.map((pk) => {
                  const createdDate = pk.createdAt
                    ? new Date(Number(pk.createdAt.seconds) * 1000).toLocaleDateString()
                    : "Unknown";
                  const lastUsedDate = pk.lastUsedAt
                    ? new Date(Number(pk.lastUsedAt.seconds) * 1000).toLocaleString()
                    : "Never";

                  return (
                    <div key={pk.id} className="passkey-row-card">
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: "12px",
                          flex: 1,
                          minWidth: "220px",
                        }}
                      >
                        {renderPasskeyIcon(pk.friendlyDeviceName)}
                        <div style={{ flex: 1 }}>
                          <div
                            style={{
                              display: "flex",
                              alignItems: "center",
                              gap: "8px",
                              flexWrap: "wrap",
                            }}
                          >
                            {editingPasskeyId === pk.id ? (
                              <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                                <input
                                  type="text"
                                  value={editingPasskeyName}
                                  onChange={(e) => setEditingPasskeyName(e.target.value)}
                                  maxLength={50}
                                  style={{ padding: "4px 8px", fontSize: "13px" }}
                                />
                                <button
                                  type="button"
                                  onClick={() => handleRenamePasskey(pk.id)}
                                  className="settings-btn-primary"
                                  style={{ padding: "4px 8px" }}
                                  aria-label="Save name"
                                >
                                  <Check size={14} />
                                </button>
                                <button
                                  type="button"
                                  onClick={() => setEditingPasskeyId(null)}
                                  className="settings-btn-secondary"
                                  style={{ padding: "4px 8px" }}
                                  aria-label="Cancel rename"
                                >
                                  <X size={14} />
                                </button>
                              </div>
                            ) : (
                              <>
                                <span
                                  style={{
                                    fontWeight: "600",
                                    color: "var(--text-h)",
                                    fontSize: "14px",
                                  }}
                                >
                                  {pk.name}
                                </span>
                                <button
                                  type="button"
                                  onClick={() => {
                                    setEditingPasskeyId(pk.id);
                                    setEditingPasskeyName(pk.name);
                                  }}
                                  style={{
                                    background: "none",
                                    border: "none",
                                    cursor: "pointer",
                                    opacity: 0.6,
                                    padding: "2px",
                                  }}
                                  aria-label="Rename passkey"
                                >
                                  <Edit2 size={13} />
                                </button>
                              </>
                            )}

                            {pk.backupEligible ? (
                              <span className="passkey-synced-badge">
                                <CheckCircle2 size={12} />
                                <span>Synced Passkey</span>
                              </span>
                            ) : (
                              <span className="auth-event-badge auth-event-badge-info">
                                <span>Hardware Key</span>
                              </span>
                            )}

                            {pk.cloneWarning && (
                              <span className="auth-event-badge auth-event-badge-error">
                                <span>Clone Warning</span>
                              </span>
                            )}
                          </div>

                          <div
                            style={{
                              fontSize: "12px",
                              opacity: 0.7,
                              display: "flex",
                              alignItems: "center",
                              gap: "12px",
                              marginTop: "4px",
                              flexWrap: "wrap",
                            }}
                          >
                            <span>Model: {pk.friendlyDeviceName}</span>
                            <span>Added: {createdDate}</span>
                            <span>Last used: {lastUsedDate}</span>
                          </div>
                        </div>
                      </div>

                      <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                        <button
                          type="button"
                          onClick={() => setPendingDestructive({ kind: "passkey", passkey: pk })}
                          disabled={deletingPasskeyId === pk.id}
                          className="settings-btn-secondary"
                          style={{
                            padding: "6px 12px",
                            fontSize: "12px",
                            color: "var(--error)",
                            borderColor: "var(--error-border)",
                          }}
                        >
                          {deletingPasskeyId === pk.id ? (
                            <Loader2 className="animate-spin" size={14} />
                          ) : (
                            <Trash2 size={14} />
                          )}
                          <span>Delete</span>
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Active Sessions Section */}
          <div className="logs-viewer-card">
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: "16px",
                flexWrap: "wrap",
                gap: "12px",
              }}
            >
              <div>
                <h2
                  style={{
                    fontSize: "18px",
                    fontWeight: "600",
                    color: "var(--text-h)",
                    margin: 0,
                  }}
                >
                  Active Sessions
                </h2>
                <p style={{ fontSize: "14px", opacity: 0.8, margin: "4px 0 0 0" }}>
                  Active login sessions authorized to access this dmanager instance.
                </p>
              </div>

              <div style={{ display: "flex", gap: "8px" }}>
                <button
                  type="button"
                  onClick={fetchSessions}
                  disabled={sessionsLoading}
                  className="settings-btn-secondary"
                  style={{ padding: "8px 12px" }}
                  aria-label="Refresh sessions"
                >
                  {sessionsLoading ? (
                    <Loader2 className="animate-spin" size={16} />
                  ) : (
                    <RefreshCw size={16} />
                  )}
                </button>

                {sessions.length > 1 && (
                  <button
                    type="button"
                    onClick={() => setPendingDestructive({ kind: "allSessions" })}
                    disabled={isRevokingOther || sessionsLoading}
                    className="settings-btn-secondary"
                    style={{
                      padding: "8px 14px",
                      color: "var(--error)",
                      borderColor: "var(--error-border)",
                    }}
                  >
                    {isRevokingOther ? (
                      <Loader2 className="animate-spin" size={16} />
                    ) : (
                      <LogOut size={16} />
                    )}
                    <span>Revoke All Others</span>
                  </button>
                )}
              </div>
            </div>

            {sessionsError && (
              <div className="auth-error-banner" style={{ marginBottom: "16px" }}>
                <ShieldAlert size={18} className="auth-error-icon" />
                <span>{sessionsError}</span>
              </div>
            )}

            {sessionsLoading && sessions.length === 0 ? (
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  padding: "32px",
                }}
              >
                <Loader2 className="animate-spin text-accent" size={24} />
                <span style={{ marginLeft: "12px", fontSize: "14px" }}>Loading sessions...</span>
              </div>
            ) : sessions.length === 0 ? (
              <div
                style={{
                  padding: "32px",
                  textAlign: "center",
                  opacity: 0.6,
                  fontSize: "14px",
                  border: "1px dashed var(--border)",
                  borderRadius: "10px",
                }}
              >
                No active sessions found.
              </div>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
                {sessions.map((sess) => {
                  const lastSeenDate = sess.lastSeenAt
                    ? new Date(Number(sess.lastSeenAt.seconds) * 1000).toLocaleString()
                    : "Unknown";

                  return (
                    <div key={sess.sessionId} className="session-row-card">
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: "12px",
                          minWidth: "200px",
                        }}
                      >
                        {renderDeviceIcon(sess.deviceLabel)}
                        <div>
                          <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                            <span
                              style={{
                                fontWeight: "600",
                                color: "var(--text-h)",
                                fontSize: "14px",
                              }}
                            >
                              {sess.deviceLabel}
                            </span>
                            {sess.isCurrent && (
                              <span className="session-current-badge">
                                <CheckCircle2 size={12} />
                                <span>Current Session</span>
                              </span>
                            )}
                          </div>
                          <div
                            style={{
                              fontSize: "12px",
                              opacity: 0.7,
                              display: "flex",
                              alignItems: "center",
                              gap: "4px",
                              marginTop: "2px",
                            }}
                          >
                            <Clock size={12} />
                            <span>Last seen: {lastSeenDate}</span>
                          </div>
                        </div>
                      </div>

                      <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                        {!sess.isCurrent && (
                          <button
                            type="button"
                            onClick={() =>
                              setPendingDestructive({ kind: "session", session: sess })
                            }
                            disabled={revokingSessionId === sess.sessionId}
                            className="settings-btn-secondary"
                            style={{
                              padding: "6px 12px",
                              fontSize: "12px",
                              color: "var(--error)",
                              borderColor: "var(--error-border)",
                            }}
                          >
                            {revokingSessionId === sess.sessionId ? (
                              <Loader2 className="animate-spin" size={14} />
                            ) : (
                              <Trash2 size={14} />
                            )}
                            <span>Revoke</span>
                          </button>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Audit Events Feed Section */}
          <div className="logs-viewer-card">
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                marginBottom: "16px",
                flexWrap: "wrap",
                gap: "12px",
              }}
            >
              <div>
                <h2
                  style={{
                    fontSize: "18px",
                    fontWeight: "600",
                    color: "var(--text-h)",
                    margin: 0,
                  }}
                >
                  Recent Authentication Events
                </h2>
                <p style={{ fontSize: "14px", opacity: 0.8, margin: "4px 0 0 0" }}>
                  Audit log of recent logins, logouts, session revocations, and security events.
                </p>
              </div>

              <button
                type="button"
                onClick={fetchAuthEvents}
                disabled={authEventsLoading}
                className="settings-btn-secondary"
                style={{ padding: "8px 12px" }}
                aria-label="Refresh audit events"
              >
                {authEventsLoading ? (
                  <Loader2 className="animate-spin" size={16} />
                ) : (
                  <RefreshCw size={16} />
                )}
              </button>
            </div>

            {authEventsError && (
              <div className="auth-error-banner" style={{ marginBottom: "16px" }}>
                <ShieldAlert size={18} className="auth-error-icon" />
                <span>{authEventsError}</span>
              </div>
            )}

            {authEventsLoading && authEvents.length === 0 ? (
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  padding: "32px",
                }}
              >
                <Loader2 className="animate-spin text-accent" size={24} />
                <span style={{ marginLeft: "12px", fontSize: "14px" }}>Loading audit log...</span>
              </div>
            ) : authEvents.length === 0 ? (
              <div
                style={{
                  padding: "32px",
                  textAlign: "center",
                  opacity: 0.6,
                  fontSize: "14px",
                  border: "1px dashed var(--border)",
                  borderRadius: "10px",
                }}
              >
                No authentication events recorded yet.
              </div>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
                {authEvents.map((evt) => {
                  const eventDate = evt.createdAt
                    ? new Date(Number(evt.createdAt.seconds) * 1000).toLocaleString()
                    : "Unknown";

                  return (
                    <div
                      key={String(evt.id)}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        padding: "12px 16px",
                        border: "1px solid var(--card-border)",
                        borderRadius: "10px",
                        background: "rgba(255, 255, 255, 0.02)",
                        flexWrap: "wrap",
                        gap: "12px",
                      }}
                    >
                      <div style={{ display: "flex", alignItems: "center", gap: "12px" }}>
                        <Activity size={16} style={{ opacity: 0.6 }} />
                        <div style={{ display: "flex", flexDirection: "column", gap: "2px" }}>
                          <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                            {renderEventBadge(evt.eventType)}
                            <span
                              style={{
                                fontSize: "13px",
                                fontWeight: "600",
                                color: "var(--text-h)",
                                display: "flex",
                                alignItems: "center",
                                gap: "4px",
                              }}
                            >
                              <User size={13} style={{ opacity: 0.7 }} />
                              {evt.username || "anonymous"}
                            </span>
                          </div>
                          {evt.detail && (
                            <span
                              style={{
                                fontSize: "12px",
                                opacity: 0.7,
                                fontFamily: "var(--font-mono, monospace)",
                              }}
                            >
                              {evt.detail}
                            </span>
                          )}
                        </div>
                      </div>

                      <div
                        style={{
                          fontSize: "12px",
                          opacity: 0.6,
                          display: "flex",
                          alignItems: "center",
                          gap: "4px",
                        }}
                      >
                        <Clock size={12} />
                        <span>{eventDate}</span>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      )}

      {pendingDestructive !== null && (
        <ConfirmDialog
          open
          onClose={() => setPendingDestructive(null)}
          onConfirm={confirmDestructive}
          variant="danger"
          busy={destructiveBusy}
          {...destructiveDialogCopy(pendingDestructive)}
        />
      )}
    </div>
  );
}
