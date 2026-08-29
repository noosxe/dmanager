import { Database, Layers, Recycle, Trash2 } from "lucide-react";

import type { GetBuildCacheStatsResponse } from "../gen/proto/dmanager/v1/admin_pb";
import { formatBytes } from "./adminFormat";

interface BuilderPanelProps {
  /** Daemon-aggregated build cache stats; null while loading or on failure. */
  stats: GetBuildCacheStatsResponse | null;
  isAdmin: boolean;
  /** True while any prune or deletion is in flight — disables the button. */
  pruning: boolean;
  /** True while the builder prune specifically is in flight — spins the button. */
  builderPruning: boolean;
  /** Arms the prune confirmation dialog (owned by the parent page). */
  onPrune: () => void;
}

// Builder tab (design.md §9.9, #206): builder-owned disk space — the
// BuildKit build cache image prunes cannot free — as three stat cards plus
// the prune control. No table: the aggregate is the actionable unit; per-
// record drill-down is future work.
export function Builder({ stats, isAdmin, pruning, builderPruning, onPrune }: BuilderPanelProps) {
  return (
    <>
      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-icon-wrapper total">
            <Database size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{stats ? formatBytes(stats.totalBytes, true) : "--"}</span>
            <span className="stat-label">Build Cache</span>
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-icon-wrapper updates">
            <Recycle size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">
              {stats ? formatBytes(stats.reclaimableBytes, true) : "--"}
            </span>
            <span className="stat-label">Reclaimable</span>
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-icon-wrapper stopped">
            <Layers size={20} />
          </div>
          <div className="stat-info">
            <span className="stat-value">{stats ? stats.recordCount : "--"}</span>
            <span className="stat-label">Records</span>
          </div>
        </div>
      </div>

      <div className="images-prune-row">
        <button
          type="button"
          className="prune-btn"
          disabled={pruning || !isAdmin || !stats || stats.reclaimableBytes === 0n}
          title={
            !isAdmin
              ? "Admin role required"
              : stats && stats.reclaimableBytes === 0n
                ? "No build cache to prune"
                : undefined
          }
          onClick={onPrune}
        >
          <Trash2 size={14} className={builderPruning ? "spinner" : ""} />
          {builderPruning ? "Pruning…" : "Prune Build Cache"}
        </button>
      </div>
    </>
  );
}
