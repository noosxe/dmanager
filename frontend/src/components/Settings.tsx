import {
  Activity,
  CheckCircle2,
  Clock,
  Globe,
  Laptop,
  Loader2,
  LogOut,
  Play,
  RefreshCw,
  Save,
  Settings as SettingsIcon,
  Shield,
  ShieldAlert,
  Smartphone,
  Trash2,
  User,
} from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useState } from "react";
import { authClient, settingsClient } from "../client";
import { useToast } from "../context/ToastContext";
import type { AuthEvent, Session } from "../gen/proto/dmanager/v1/auth_pb";
import type { RegistryStatus } from "../gen/proto/dmanager/v1/settings_pb";

export function Settings() {
  const [activeTab, setActiveTab] = useState<"general" | "security">("general");

  // General Settings State
  const [gotifyUrl, setGotifyUrl] = useState("");
  const [gotifyToken, setGotifyToken] = useState("");
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

  // Security Tab State: Sessions & Audit Events
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsError, setSessionsError] = useState<string | null>(null);
  const [isRevokingOther, setIsRevokingOther] = useState(false);
  const [revokingSessionId, setRevokingSessionId] = useState<string | null>(null);

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
      fetchSessions();
      fetchAuthEvents();
    }
  }, [activeTab, fetchSessions, fetchAuthEvents]);

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

  return (
    <div style={{ padding: "24px", maxWidth: "800px", margin: "0 auto" }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "12px",
          marginBottom: "20px",
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

      <div className="settings-nav-tabs">
        <button
          type="button"
          className={`settings-nav-tab ${activeTab === "general" ? "active" : ""}`}
          onClick={() => setActiveTab("general")}
        >
          <SettingsIcon size={16} />
          <span>General</span>
        </button>
        <button
          type="button"
          className={`settings-nav-tab ${activeTab === "security" ? "active" : ""}`}
          onClick={() => setActiveTab("security")}
        >
          <Shield size={16} />
          <span>Security & Sessions</span>
        </button>
      </div>

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

          <div className="logs-viewer-card" style={{ marginTop: "24px" }}>
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
                    onClick={handleRevokeAllOtherSessions}
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
                            onClick={() => handleRevokeSession(sess.sessionId)}
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
    </div>
  );
}
