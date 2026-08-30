import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
  ScrollText,
  Search,
} from "lucide-react";

import type { AuditLogEntry } from "../gen/proto/dmanager/v1/admin_pb";
import { AUDIT_OUTCOME, AUDIT_PAGE_SIZE, AUDIT_SOURCE, useAuditLogs } from "../hooks/useAuditLogs";
import { formatTimestamp } from "./adminFormat";

function sourceLabel(entry: AuditLogEntry): string {
  if (entry.source === AUDIT_SOURCE.SYSTEM) {
    return "System";
  }
  return entry.actorRole ? `${entry.actor} (${entry.actorRole})` : entry.actor;
}

function outcomeBadge(entry: AuditLogEntry): { className: string; label: string; title: string } {
  switch (entry.outcome) {
    case AUDIT_OUTCOME.FAILURE:
      return { className: "logs-row-level error", label: "Failure", title: "The action failed" };
    case AUDIT_OUTCOME.DENIED:
      return { className: "logs-row-level warn", label: "Denied", title: "Not authorized" };
    default:
      return { className: "logs-row-level info", label: "Success", title: "The action succeeded" };
  }
}

function resourceLabel(entry: AuditLogEntry): string {
  if (!entry.resourceId) {
    return entry.resourceType || "—";
  }
  return `${entry.resourceType} · ${entry.resourceId.slice(0, 12)}`;
}

export function AuditLogsPage() {
  const {
    queryInput,
    setQueryInput,
    source,
    updateSource,
    outcome,
    updateOutcome,
    page,
    setPage,
    pages,
    entries,
    total,
    loading,
    error,
    refresh,
  } = useAuditLogs();

  const rangeStart = total === 0 ? 0 : page * AUDIT_PAGE_SIZE + 1;
  const rangeEnd = Math.min(total, (page + 1) * AUDIT_PAGE_SIZE);

  return (
    <div className="page-container">
      <div className="page-header">
        <h1>Audit Logs</h1>
        <p>Recorded mutations by users and automatic updates — newest first.</p>
      </div>

      <div className="logs-viewer-card">
        <div className="logs-control-bar">
          <div className="logs-search-wrapper">
            <Search size={16} className="logs-search-icon" />
            <input
              type="text"
              placeholder="Search actor, action, resource or details..."
              value={queryInput}
              onChange={(e) => setQueryInput(e.target.value)}
              className="logs-search-input"
              aria-label="Search audit logs"
            />
          </div>

          <div className="logs-filters-group">
            <select
              value={source}
              onChange={(e) =>
                updateSource(Number(e.target.value) as Parameters<typeof updateSource>[0])
              }
              className="logs-select-filter"
              aria-label="Filter by source"
            >
              <option value={AUDIT_SOURCE.ALL}>All Sources</option>
              <option value={AUDIT_SOURCE.USER}>Users</option>
              <option value={AUDIT_SOURCE.SYSTEM}>System</option>
            </select>

            <select
              value={outcome}
              onChange={(e) =>
                updateOutcome(Number(e.target.value) as Parameters<typeof updateOutcome>[0])
              }
              className="logs-select-filter"
              aria-label="Filter by outcome"
            >
              <option value={AUDIT_OUTCOME.ALL}>All Outcomes</option>
              <option value={AUDIT_OUTCOME.SUCCESS}>Success</option>
              <option value={AUDIT_OUTCOME.FAILURE}>Failure</option>
              <option value={AUDIT_OUTCOME.DENIED}>Denied</option>
            </select>

            <button
              type="button"
              className="prune-btn"
              onClick={() => void refresh()}
              disabled={loading}
            >
              <RefreshCw size={14} className={loading ? "spinner" : ""} />
              Refresh
            </button>
          </div>
        </div>

        {error && (
          <div className="auth-error-banner" role="alert">
            <AlertTriangle size={18} className="auth-error-icon" />
            <span>{error}</span>
          </div>
        )}

        <div className="logs-table-container">
          {entries.length === 0 ? (
            <div
              className="empty-state-card"
              style={{ borderTop: "none", borderTopLeftRadius: 0, borderTopRightRadius: 0 }}
            >
              <ScrollText size={32} className="empty-state-icon" />
              <h3>{loading ? "Loading audit trail..." : "No Audit Entries Found"}</h3>
              <p>
                {loading
                  ? "Fetching recorded mutations and system actions."
                  : "No recorded actions match the current search and filters."}
              </p>
            </div>
          ) : (
            <table className="logs-table" aria-label="Audit log entries">
              <thead>
                <tr>
                  <th style={{ width: "180px" }}>Time</th>
                  <th style={{ width: "160px" }}>Actor</th>
                  <th style={{ width: "200px" }}>Action</th>
                  <th style={{ width: "200px" }}>Resource</th>
                  <th style={{ width: "110px" }}>Outcome</th>
                  <th>Details</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => {
                  const badge = outcomeBadge(entry);
                  return (
                    <tr key={entry.id.toString()}>
                      <td>
                        <span className="logs-timestamp">{formatTimestamp(entry.createdAt)}</span>
                      </td>
                      <td>
                        <span className="container-name-text">{sourceLabel(entry)}</span>
                      </td>
                      <td>
                        <code className="logs-meta-badge">{entry.action}</code>
                      </td>
                      <td>
                        <span className="logs-message-text">{resourceLabel(entry)}</span>
                      </td>
                      <td>
                        <span className={badge.className} title={badge.title}>
                          {badge.label}
                        </span>
                      </td>
                      <td>
                        <div className="logs-message-text" title={entry.detail}>
                          {entry.detail || "—"}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        <div
          className="logs-control-bar"
          style={{ justifyContent: "space-between", alignItems: "center" }}
        >
          <span style={{ fontSize: "13px", opacity: 0.7 }}>
            {total === 0 ? "No entries" : `${rangeStart}–${rangeEnd} of ${total}`}
          </span>
          <div style={{ display: "flex", gap: "8px" }}>
            <button
              type="button"
              className="prune-btn"
              disabled={page === 0 || loading}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              aria-label="Previous page"
            >
              <ChevronLeft size={14} /> Prev
            </button>
            <span style={{ fontSize: "13px", alignSelf: "center", opacity: 0.7 }}>
              Page {page + 1} of {pages}
            </span>
            <button
              type="button"
              className="prune-btn"
              disabled={page + 1 >= pages || loading}
              onClick={() => setPage((p) => p + 1)}
              aria-label="Next page"
            >
              Next <ChevronRight size={14} />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
