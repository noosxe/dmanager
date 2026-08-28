import {
  Activity,
  AlertTriangle,
  ChevronDown,
  ChevronUp,
  Database,
  Info,
  RefreshCw,
  Search,
  ShieldAlert,
  Terminal,
  XCircle,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { logClient } from "../client";
import type { LogEntry } from "../gen/proto/dmanager/v1/log_pb";

export function SystemLogs() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  // Filters
  const [searchQuery, setSearchQuery] = useState<string>("");
  const [levelFilter, setLevelFilter] = useState<string>("");
  const [limit, setLimit] = useState<number>(100);
  const [autoRefreshInterval, setAutoRefreshInterval] = useState<number>(10000); // default 10s

  // Expanded logs for metadata viewing
  const [expandedLogIds, setExpandedLogIds] = useState<Record<number, boolean>>({});

  const toggleExpandLog = (index: number) => {
    setExpandedLogIds((prev) => ({
      ...prev,
      [index]: !prev[index],
    }));
  };

  // Refetch function
  const fetchLogs = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await logClient.getSystemLogs({
        limit,
        levelFilter,
        searchQuery,
      });
      setLogs(resp.entries || []);
    } catch (err: unknown) {
      console.error("Failed to load system logs:", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [limit, levelFilter, searchQuery]);

  // Fetch logs on filter/limit updates
  useEffect(() => {
    fetchLogs();
  }, [fetchLogs]);

  // Auto refresh interval implementation
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  useEffect(() => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }

    if (autoRefreshInterval > 0) {
      intervalRef.current = setInterval(() => {
        // Silent refresh in the background
        logClient
          .getSystemLogs({
            limit,
            levelFilter,
            searchQuery,
          })
          .then((resp) => {
            setLogs(resp.entries || []);
          })
          .catch((err) => {
            console.error("Background logs refresh failed:", err);
          });
      }, autoRefreshInterval);
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [autoRefreshInterval, limit, levelFilter, searchQuery]);

  // Metrics computation
  const totalCount = logs.length;
  const errorCount = logs.filter(
    (l) =>
      l.level === "ERROR" || l.level === "DPANIC" || l.level === "PANIC" || l.level === "FATAL",
  ).length;
  const warnCount = logs.filter((l) => l.level === "WARN" || l.level === "WARNING").length;
  const infoCount = logs.filter((l) => l.level === "INFO" || l.level === "DEBUG").length;

  const formatTimestamp = (tsStr: string) => {
    try {
      const d = new Date(tsStr);
      if (Number.isNaN(d.getTime())) return tsStr;
      return d.toLocaleString();
    } catch {
      return tsStr;
    }
  };

  const getLevelIcon = (level: string) => {
    const lvl = level.toUpperCase();
    if (lvl.includes("ERR") || lvl.includes("FAIL") || lvl.includes("PANIC")) {
      return <XCircle size={14} style={{ color: "#ef4444" }} />;
    }
    if (lvl.includes("WARN")) {
      return <AlertTriangle size={14} style={{ color: "#f59e0b" }} />;
    }
    if (lvl.includes("DEBUG")) {
      return <Terminal size={14} style={{ color: "#6b7280" }} />;
    }
    return <Info size={14} style={{ color: "#3b82f6" }} />;
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "24px", width: "100%" }}>
      {/* Dashboard Top Header Title */}
      <div className="dashboard-header">
        <div className="header-title-section">
          <h2>System Logs</h2>
          <p>Observe, filter, and inspect host daemon events and client synchronizations.</p>
        </div>
        <button
          type="button"
          className="auth-submit-btn"
          style={{ padding: "10px 16px", fontSize: "13px" }}
          onClick={fetchLogs}
          disabled={loading}
        >
          <RefreshCw size={14} className={loading ? "spinner" : ""} />
          <span>Refresh</span>
        </button>
      </div>

      {/* Metrics Cards Grid */}
      <div className="stats-grid">
        <div className="stat-card">
          <div
            className="stat-icon-wrapper total"
            style={{ background: "rgba(170, 59, 255, 0.1)", color: "var(--accent)" }}
          >
            <Database size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading && logs.length === 0 ? "--" : totalCount}</span>
            <span className="stat-label">Buffered Logs</span>
          </div>
        </div>

        <div className="stat-card">
          <div
            className="stat-icon-wrapper error"
            style={{ background: "rgba(239, 68, 68, 0.1)", color: "#ef4444" }}
          >
            <ShieldAlert size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading && logs.length === 0 ? "--" : errorCount}</span>
            <span className="stat-label">Errors</span>
          </div>
        </div>

        <div className="stat-card">
          <div
            className="stat-icon-wrapper warn"
            style={{ background: "rgba(245, 158, 11, 0.1)", color: "#f59e0b" }}
          >
            <AlertTriangle size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading && logs.length === 0 ? "--" : warnCount}</span>
            <span className="stat-label">Warnings</span>
          </div>
        </div>

        <div className="stat-card">
          <div
            className="stat-icon-wrapper info"
            style={{ background: "rgba(59, 130, 246, 0.1)", color: "#3b82f6" }}
          >
            <Activity size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{loading && logs.length === 0 ? "--" : infoCount}</span>
            <span className="stat-label">Info & Debug</span>
          </div>
        </div>
      </div>

      {/* Main Workspace Card */}
      <div className="logs-viewer-card">
        {/* Controls Bar */}
        <div className="logs-control-bar">
          <div className="logs-search-wrapper">
            <Search size={16} className="logs-search-icon" />
            <input
              type="text"
              placeholder="Search log messages, components or metadata..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="logs-search-input"
            />
          </div>

          <div className="logs-filters-group">
            {/* Level filter dropdown */}
            <select
              value={levelFilter}
              onChange={(e) => setLevelFilter(e.target.value)}
              className="logs-select-filter"
              aria-label="Filter by Log Level"
            >
              <option value="">All Levels</option>
              <option value="DEBUG">DEBUG</option>
              <option value="INFO">INFO</option>
              <option value="WARN">WARN</option>
              <option value="ERROR">ERROR</option>
            </select>

            {/* Limit select */}
            <select
              value={limit}
              onChange={(e) => setLimit(Number(e.target.value))}
              className="logs-select-filter"
              aria-label="Limit log entries count"
            >
              <option value="50">50 entries</option>
              <option value="100">100 entries</option>
              <option value="200">200 entries</option>
              <option value="500">500 entries</option>
            </select>

            {/* Auto-refresh interval dropdown */}
            <select
              value={autoRefreshInterval}
              onChange={(e) => setAutoRefreshInterval(Number(e.target.value))}
              className="logs-select-filter"
              aria-label="Set auto refresh interval"
            >
              <option value="0">Auto Refresh: Off</option>
              <option value="3000">Auto Refresh: 3s</option>
              <option value="5000">Auto Refresh: 5s</option>
              <option value="10000">Auto Refresh: 10s</option>
              <option value="30000">Auto Refresh: 30s</option>
            </select>
          </div>
        </div>

        {/* Error state */}
        {error && (
          <div
            style={{
              padding: "12px 16px",
              background: "rgba(239, 68, 68, 0.08)",
              border: "1px solid rgba(239, 68, 68, 0.2)",
              borderRadius: "10px",
              color: "#ef4444",
              fontSize: "14px",
            }}
          >
            {error}
          </div>
        )}

        {/* Logs List Table */}
        <div className="logs-table-container">
          {logs.length === 0 ? (
            <div
              style={{
                padding: "48px 24px",
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                gap: "12px",
                color: "var(--text)",
                opacity: 0.7,
              }}
            >
              <Terminal size={32} />
              <span style={{ fontSize: "14px", fontWeight: 500 }}>
                {loading ? "Loading logs from daemon..." : "No matching log entries found."}
              </span>
            </div>
          ) : (
            <table className="logs-table">
              <thead>
                <tr>
                  <th style={{ width: "80px" }}>Level</th>
                  <th style={{ width: "180px" }}>Timestamp</th>
                  <th style={{ width: "150px" }}>Component</th>
                  <th>Message</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((log, index) => {
                  const isExpanded = !!expandedLogIds[index];
                  const levelLower = log.level.toLowerCase();
                  return (
                    // NOTE: using index combined with timestamp as key is safe since logs are read-only and order does not change dynamically
                    <tr key={`${log.timestamp}-${index}`}>
                      <td>
                        <span className={`logs-row-level ${levelLower}`}>{log.level}</span>
                      </td>
                      <td>
                        <span className="logs-timestamp">{formatTimestamp(log.timestamp)}</span>
                      </td>
                      <td>
                        {log.component ? (
                          <span className="logs-meta-badge">{log.component}</span>
                        ) : (
                          <span style={{ opacity: 0.3 }}>-</span>
                        )}
                      </td>
                      <td>
                        <div className="logs-message-text">{log.message}</div>
                        {log.metadata && (
                          <div>
                            <button
                              type="button"
                              onClick={() => toggleExpandLog(index)}
                              className="logs-expand-btn"
                            >
                              {getLevelIcon(log.level)}
                              <span>{isExpanded ? "Hide Details" : "Show Details"}</span>
                              {isExpanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
                            </button>
                            {isExpanded && (
                              <pre className="logs-meta-block">
                                {(() => {
                                  try {
                                    return JSON.stringify(JSON.parse(log.metadata), null, 2);
                                  } catch {
                                    return log.metadata;
                                  }
                                })()}
                              </pre>
                            )}
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
}
