import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Layers, Trash2 } from "lucide-react";

import type { BuildCacheRecord } from "../gen/proto/dmanager/v1/admin_pb";
import { formatBytes, formatRelativeUnix } from "./adminFormat";

interface BuilderRecordsProps {
  /** Build cache records, size-descending as delivered by the daemon. */
  records: BuildCacheRecord[];
  loading: boolean;
  /** True when the records fetch failed — independent of the stats slice. */
  error: boolean;
  isAdmin: boolean;
  /** True while any prune or deletion is in flight — locks every row. */
  busy: boolean;
  /** The record currently being deleted — spins its row's button. */
  pruningRecordId: string | null;
  /** Arms the per-record delete confirmation dialog (owned by the page). */
  onPrune: (record: BuildCacheRecord) => void;
}

function relativeLastUsed(ts: Timestamp | undefined): string {
  if (!ts) return "never";
  return formatRelativeUnix(ts.seconds);
}

// Builder records drill-down (design.md §9.10, #209): the size-sorted
// top-offenders view. The server owns the ordering contract, so the table
// renders without sort controls; dust sinks to the bottom naturally.
export function BuilderRecords({
  records,
  loading,
  error,
  isAdmin,
  busy,
  pruningRecordId,
  onPrune,
}: BuilderRecordsProps) {
  if (loading) {
    return (
      <div className="empty-state-card">
        <Layers size={32} className="empty-state-icon" />
        <h3>Loading Records…</h3>
        <p>Fetching the daemon's build cache records.</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="empty-state-card">
        <Layers size={32} className="empty-state-icon" />
        <h3>Unable to Load Records</h3>
        <p>The build cache records could not be read. The stats above remain accurate.</p>
      </div>
    );
  }

  return (
    <div className="container-table-wrapper">
      <table className="container-table">
        <thead>
          <tr>
            <th>Size</th>
            <th>Type</th>
            <th>Description</th>
            <th>Last Used</th>
            <th>Used</th>
            <th aria-label="Actions" />
          </tr>
        </thead>
        <tbody>
          {records.map((record) => {
            const deleting = pruningRecordId === record.id;
            return (
              <tr key={record.id} className="container-table-row">
                <td>
                  <span className="table-cell-date">{formatBytes(record.sizeBytes)}</span>
                </td>
                <td>
                  <code className="table-cell-id" title={record.id}>
                    {record.type || "unknown"}
                  </code>
                </td>
                <td>
                  <span className="table-cell-name" title={record.description || record.id}>
                    {record.description || "—"}
                  </span>
                  {(record.inUse || record.shared) && (
                    <span className="builder-record-chips">
                      {record.inUse && <span className="record-chip inuse">In use</span>}
                      {record.shared && <span className="record-chip shared">Shared</span>}
                    </span>
                  )}
                </td>
                <td>
                  <span className="table-cell-date">{relativeLastUsed(record.lastUsedAt)}</span>
                </td>
                <td>
                  <span className="table-cell-date">{record.usageCount.toString()}</span>
                </td>
                <td>
                  <button
                    type="button"
                    className="card-action-btn"
                    disabled={!isAdmin || busy || record.inUse}
                    title={
                      !isAdmin
                        ? "Admin role required"
                        : record.inUse
                          ? "Record is in use"
                          : "Delete cache record"
                    }
                    onClick={() => onPrune(record)}
                  >
                    <Trash2 size={16} className={deleting ? "spinner" : undefined} />
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>

      {records.length === 0 && (
        <div className="empty-state-card">
          <Layers size={32} className="empty-state-icon" />
          <h3>No Build Cache Records</h3>
          <p>The builder's cache is empty.</p>
        </div>
      )}
    </div>
  );
}
