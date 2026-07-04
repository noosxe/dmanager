import {
  Activity,
  AlertCircle,
  ArrowUpCircle,
  Play,
  RefreshCw,
  Search,
  Server,
  Sparkles,
  Square,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { containerClient } from "../client";
import { useAuth } from "../hooks/useAuth";

// Shape matching GenService type definitions
interface Container {
  id: string;
  name: string;
  image: string;
  imageId: string;
  state: string;
  autoUpdate: boolean;
  updateAvailable: boolean;
  latestImageDigest: string;
  lastCheckedAt: string;
  lastUpdatedAt: string;
}

interface ProtoContainer {
  id?: string;
  name?: string;
  image?: string;
  imageId?: string;
  state?: string;
  autoUpdate?: boolean;
  updateAvailable?: boolean;
  latestImageDigest?: string;
  lastCheckedAt?: string;
  lastUpdatedAt?: string;
}

export function ContainerGrid() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const [containers, setContainers] = useState<Container[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<"all" | "running" | "stopped">("all");
  const [actionLoading, setActionLoading] = useState<Record<string, string>>({}); // { containerId: actionType }

  const fetchContainers = useCallback(async () => {
    try {
      const response = await containerClient.listContainers({});
      // Map Protobuf message fields safely
      const items = (response.containers || []).map((c: ProtoContainer) => ({
        id: c.id || "",
        name: c.name || "",
        image: c.image || "",
        imageId: c.imageId || "",
        state: c.state || "",
        autoUpdate: !!c.autoUpdate,
        updateAvailable: !!c.updateAvailable,
        latestImageDigest: c.latestImageDigest || "",
        lastCheckedAt: c.lastCheckedAt || "",
        lastUpdatedAt: c.lastUpdatedAt || "",
      }));
      setContainers(items);
      setError(null);
    } catch (err: unknown) {
      console.error("Failed to load containers:", err);
      setError("Unable to connect to the Docker monitor backend.");
    } finally {
      setLoading(false);
    }
  }, []);

  // Poll for container list periodically
  useEffect(() => {
    fetchContainers();
    const interval = setInterval(fetchContainers, 5000);
    return () => clearInterval(interval);
  }, [fetchContainers]);

  const handleStart = async (id: string) => {
    if (!isAdmin) return;
    setActionLoading((prev) => ({ ...prev, [id]: "starting" }));
    try {
      await containerClient.startContainer({ id });
      await fetchContainers();
    } catch (err: unknown) {
      console.error("Start failed:", err);
      const msg = err instanceof Error ? err.message : "Failed to start container";
      alert(msg);
    } finally {
      setActionLoading((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    }
  };

  const handleStop = async (id: string) => {
    if (!isAdmin) return;
    setActionLoading((prev) => ({ ...prev, [id]: "stopping" }));
    try {
      await containerClient.stopContainer({ id });
      await fetchContainers();
    } catch (err: unknown) {
      console.error("Stop failed:", err);
      const msg = err instanceof Error ? err.message : "Failed to stop container";
      alert(msg);
    } finally {
      setActionLoading((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    }
  };

  const handleUpgrade = async (id: string) => {
    if (!isAdmin) return;
    setActionLoading((prev) => ({ ...prev, [id]: "upgrading" }));
    try {
      await containerClient.upgradeContainer({ id });
      await fetchContainers();
    } catch (err: unknown) {
      console.error("Upgrade failed:", err);
      const msg = err instanceof Error ? err.message : "Failed to upgrade container";
      alert(msg);
    } finally {
      setActionLoading((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    }
  };

  const handleToggleAutoUpdate = async (id: string, currentVal: boolean) => {
    if (!isAdmin) return;
    setActionLoading((prev) => ({ ...prev, [id]: "toggling" }));
    try {
      await containerClient.setContainerAutoUpdate({ id, autoUpdate: !currentVal });
      await fetchContainers();
    } catch (err: unknown) {
      console.error("Failed to set auto-update:", err);
    } finally {
      setActionLoading((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    }
  };

  const handleCheckUpdates = async (id: string) => {
    if (!isAdmin) return;
    setActionLoading((prev) => ({ ...prev, [id]: "checking" }));
    try {
      await containerClient.checkContainerUpdates({ id });
      await fetchContainers();
    } catch (err: unknown) {
      console.error("Updates check failed:", err);
    } finally {
      setActionLoading((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    }
  };

  // Helper formatting dates
  const formatDate = (isoStr: string) => {
    if (!isoStr || isoStr.startsWith("0001-01-01")) return "Never";
    try {
      const date = new Date(isoStr);
      return (
        date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) +
        " " +
        date.toLocaleDateString([], { month: "short", day: "numeric" })
      );
    } catch {
      return "Invalid date";
    }
  };

  // Computed metrics
  const totalCount = containers.length;
  const runningCount = containers.filter((c) => c.state === "running").length;
  const stoppedCount = containers.filter((c) => c.state !== "running").length;
  const updateCount = containers.filter((c) => c.updateAvailable).length;

  // Search & filter filtering
  const filteredContainers = containers.filter((c) => {
    const matchesSearch =
      c.name.toLowerCase().includes(search.toLowerCase()) ||
      c.image.toLowerCase().includes(search.toLowerCase());

    if (filter === "running") return matchesSearch && c.state === "running";
    if (filter === "stopped") return matchesSearch && c.state !== "running";
    return matchesSearch;
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "24px", width: "100%" }}>
      {/* Dashboard Top Header Title */}
      <div className="dashboard-header">
        <div className="header-title-section">
          <h2>Container Grid</h2>
          <p>Monitor status, toggle auto-updates and redeploy host workloads.</p>
        </div>
        <button
          type="button"
          className="auth-submit-btn"
          style={{ padding: "10px 16px", fontSize: "13px" }}
          onClick={fetchContainers}
          disabled={loading}
        >
          <RefreshCw size={14} className={loading ? "spinner" : ""} />
          <span>Sync Now</span>
        </button>
      </div>

      {/* Metrics Cards Grid */}
      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-icon-wrapper total">
            <Server size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading ? "--" : totalCount}</span>
            <span className="stat-label">Total Discovered</span>
          </div>
        </div>

        <div className="stat-card">
          <div className="stat-icon-wrapper running">
            <Activity size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading ? "--" : runningCount}</span>
            <span className="stat-label">Running</span>
          </div>
        </div>

        <div className="stat-card">
          <div className="stat-icon-wrapper stopped">
            <Square size={16} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading ? "--" : stoppedCount}</span>
            <span className="stat-label">Stopped</span>
          </div>
        </div>

        <div className="stat-card">
          <div className="stat-icon-wrapper updates">
            <ArrowUpCircle size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading ? "--" : updateCount}</span>
            <span className="stat-label">Updates Ready</span>
          </div>
        </div>
      </div>

      {/* Error display banner */}
      {error && (
        <div className="auth-error-banner" style={{ margin: 0 }}>
          <AlertCircle size={18} className="auth-error-icon" />
          <span>{error}</span>
        </div>
      )}

      {/* Toolbar controls */}
      <div className="control-bar">
        <div className="search-input-wrapper">
          <Search size={16} className="search-icon" />
          <input
            type="text"
            className="search-input"
            placeholder="Search by container name or tag..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>

        <div className="filter-group">
          <button
            type="button"
            className={`filter-btn ${filter === "all" ? "active" : ""}`}
            onClick={() => setFilter("all")}
          >
            All
          </button>
          <button
            type="button"
            className={`filter-btn ${filter === "running" ? "active" : ""}`}
            onClick={() => setFilter("running")}
          >
            Running
          </button>
          <button
            type="button"
            className={`filter-btn ${filter === "stopped" ? "active" : ""}`}
            onClick={() => setFilter("stopped")}
          >
            Stopped
          </button>
        </div>
      </div>

      {/* Loading state spinner */}
      {loading && containers.length === 0 ? (
        <div
          style={{
            display: "flex",
            justifyContent: "center",
            padding: "48px",
            color: "var(--text)",
          }}
        >
          <RefreshCw size={24} className="spinner" style={{ color: "var(--accent)" }} />
        </div>
      ) : (
        <div className="containers-grid-layout">
          {filteredContainers.map((container) => {
            const isRunning = container.state === "running";
            const loadingType = actionLoading[container.id];
            const hasUpdate = container.updateAvailable;

            return (
              <div key={container.id} className="container-card">
                {/* Header row */}
                <div className="card-header-row">
                  <div className="card-title-section">
                    <h3 className="container-title" title={container.name}>
                      {container.name}
                    </h3>
                    <span className="container-image" title={container.image}>
                      {container.image}
                    </span>
                  </div>
                  <span className={`status-badge ${isRunning ? "running" : "stopped"}`}>
                    <span
                      style={{
                        width: "6px",
                        height: "6px",
                        borderRadius: "50%",
                        background: isRunning ? "#10b981" : "#6b7280",
                        display: "inline-block",
                      }}
                    />
                    <span>{container.state}</span>
                  </span>
                </div>

                {/* Details layout details */}
                <div className="card-details-list">
                  <div className="detail-item">
                    <span className="detail-label">Container ID</span>
                    <span className="detail-value">{container.id.slice(0, 12)}</span>
                  </div>

                  <div className="detail-item">
                    <span className="detail-label">Image ID</span>
                    <span className="detail-value">
                      {container.imageId ? container.imageId.slice(0, 12) : "Unknown"}
                    </span>
                  </div>

                  <div className="detail-item">
                    <span className="detail-label">Auto Updates</span>
                    <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                      <span style={{ fontSize: "11px", color: "var(--text)", fontWeight: 500 }}>
                        {container.autoUpdate ? "Active" : "Disabled"}
                      </span>
                      <button
                        type="button"
                        onClick={() => handleToggleAutoUpdate(container.id, container.autoUpdate)}
                        disabled={!isAdmin || !!loadingType}
                        style={{
                          background: container.autoUpdate
                            ? "var(--accent)"
                            : "rgba(255,255,255,0.06)",
                          border: "1px solid var(--border)",
                          borderRadius: "20px",
                          width: "36px",
                          height: "20px",
                          padding: "2px",
                          cursor: isAdmin ? "pointer" : "not-allowed",
                          display: "flex",
                          alignItems: "center",
                          justifyContent: container.autoUpdate ? "flex-end" : "flex-start",
                          transition: "all var(--transition-fast)",
                          opacity: isAdmin ? 1 : 0.6,
                        }}
                        title={isAdmin ? "Toggle automatic updates" : "Admin required"}
                      >
                        <span
                          style={{
                            width: "14px",
                            height: "14px",
                            borderRadius: "50%",
                            background: "#fff",
                            display: "inline-block",
                            boxShadow: "0 1px 3px rgba(0,0,0,0.2)",
                          }}
                        />
                      </button>
                    </div>
                  </div>

                  <div className="detail-item">
                    <span className="detail-label">Last Checked</span>
                    <span className="detail-value">{formatDate(container.lastCheckedAt)}</span>
                  </div>
                </div>

                {/* Display update available alert banner */}
                {hasUpdate && (
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: "8px",
                      background: "rgba(245, 158, 11, 0.1)",
                      border: "1px solid rgba(245, 158, 11, 0.2)",
                      color: "#f59e0b",
                      padding: "8px 12px",
                      borderRadius: "8px",
                      fontSize: "12px",
                      fontWeight: 500,
                    }}
                  >
                    <Sparkles size={14} className="spinner" style={{ animationDuration: "3s" }} />
                    <span>New version available in registry!</span>
                  </div>
                )}

                {/* Card footer buttons */}
                <div className="card-actions-row">
                  {/* Start/Stop command button */}
                  {isRunning ? (
                    <button
                      type="button"
                      className="card-action-btn stop"
                      onClick={() => handleStop(container.id)}
                      disabled={!isAdmin || !!loadingType}
                    >
                      {loadingType === "stopping" ? (
                        <RefreshCw size={14} className="spinner" />
                      ) : (
                        <Square size={12} />
                      )}
                      <span>Stop</span>
                    </button>
                  ) : (
                    <button
                      type="button"
                      className="card-action-btn start"
                      onClick={() => handleStart(container.id)}
                      disabled={!isAdmin || !!loadingType}
                    >
                      {loadingType === "starting" ? (
                        <RefreshCw size={14} className="spinner" />
                      ) : (
                        <Play size={12} />
                      )}
                      <span>Start</span>
                    </button>
                  )}

                  {/* Pull & Re-create upgrade button */}
                  {hasUpdate && (
                    <button
                      type="button"
                      className="card-action-btn upgrade"
                      onClick={() => handleUpgrade(container.id)}
                      disabled={!isAdmin || !!loadingType}
                    >
                      {loadingType === "upgrading" ? (
                        <RefreshCw size={14} className="spinner" />
                      ) : (
                        <ArrowUpCircle size={14} />
                      )}
                      <span>Upgrade</span>
                    </button>
                  )}

                  {/* Manual checking button */}
                  <button
                    type="button"
                    className="card-action-btn"
                    style={{
                      flex: "0 0 40px",
                      background: "rgba(0,0,0,0.02)",
                      border: "1px solid var(--border)",
                      color: "var(--text)",
                    }}
                    onClick={() => handleCheckUpdates(container.id)}
                    disabled={!isAdmin || !!loadingType}
                    title="Check updates immediately"
                  >
                    {loadingType === "checking" ? (
                      <RefreshCw size={14} className="spinner" />
                    ) : (
                      <RefreshCw size={14} />
                    )}
                  </button>
                </div>
              </div>
            );
          })}

          {filteredContainers.length === 0 && (
            <div className="empty-state-card">
              <Server size={32} className="empty-state-icon" />
              <h3>No Containers Found</h3>
              <p>
                {search
                  ? "No results matched your search query."
                  : "No Docker containers discovered on the host system."}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
