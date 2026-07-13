import {
  CheckCircle2,
  Loader2,
  Play,
  RefreshCw,
  Save,
  Settings as SettingsIcon,
  ShieldAlert,
} from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useState } from "react";
import { settingsClient } from "../client";
import { useToast } from "../context/ToastContext";
import type { RegistryStatus } from "../gen/proto/dmanager/v1/settings_pb";

export function Settings() {
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

  const toast = useToast();

  // Load existing settings
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
          marginBottom: "24px",
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
          System Settings
        </h1>
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
            Notification Configurations
          </h2>
          <p style={{ fontSize: "14px", opacity: 0.8, margin: "0 0 24px 0" }}>
            Configure integrations to receive real-time notifications about image updates and docker
            container re-deployments.
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
            <button type="submit" disabled={isSaving || isTesting} className="settings-btn-primary">
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
                    <span style={{ fontWeight: "700", color: "var(--text-h)", fontSize: "15px" }}>
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
    </div>
  );
}
