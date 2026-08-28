import { Link, useParams } from "@tanstack/react-router";
import {
  Boxes,
  HardDrive,
  Image as ImageIcon,
  Layers,
  Loader2,
  Network as NetworkIcon,
  Recycle,
  RefreshCw,
  ShieldAlert,
} from "lucide-react";

import type { AdminResourceKind } from "../hooks/useAdminResources";
import { useAdminResources } from "../hooks/useAdminResources";
import { deriveImageStats, formatBytes } from "./adminFormat";
import { ImageTable } from "./ImageTable";
import { NetworkTable } from "./NetworkTable";
import { VolumeTable } from "./VolumeTable";

// Read-only Administration page: Docker host resource inventories
// (images, volumes, networks) behind three tabs. The tab comes from the
// route param (validated by the router's beforeLoad guard).
export function Administration() {
  const params = useParams({ strict: false }) as { tab?: string } | undefined;
  const routeTab = params?.tab;
  const tab: AdminResourceKind =
    routeTab === "volumes" || routeTab === "networks" ? routeTab : "images";

  const { result, isLoading, error, refresh } = useAdminResources(tab);

  // Derived Images-tab summary (design.md §9.6); null on other tabs or
  // while no successful images result exists yet (-- placeholders).
  const imageStats = result?.kind === "images" ? deriveImageStats(result.data) : null;

  return (
    <div style={{ padding: "24px", maxWidth: "1100px", margin: "0 auto" }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "12px",
          marginBottom: "20px",
          flexWrap: "wrap",
        }}
      >
        <Boxes size={28} style={{ color: "var(--accent)" }} />
        <h1
          style={{
            fontSize: "24px",
            fontWeight: "700",
            color: "var(--text-h)",
            margin: 0,
            flex: 1,
          }}
        >
          Administration
        </h1>
        <button
          type="button"
          onClick={refresh}
          disabled={isLoading}
          className="settings-btn-secondary"
          style={{ padding: "8px 16px" }}
        >
          {isLoading ? <Loader2 className="animate-spin" size={16} /> : <RefreshCw size={16} />}
          <span>Refresh</span>
        </button>
      </div>

      <div className="settings-nav-tabs">
        <Link
          to="/administration/$tab"
          params={{ tab: "images" }}
          className={`settings-nav-tab ${tab === "images" ? "active" : ""}`}
        >
          <ImageIcon size={16} />
          <span>Images</span>
        </Link>
        <Link
          to="/administration/$tab"
          params={{ tab: "volumes" }}
          className={`settings-nav-tab ${tab === "volumes" ? "active" : ""}`}
        >
          <HardDrive size={16} />
          <span>Volumes</span>
        </Link>
        <Link
          to="/administration/$tab"
          params={{ tab: "networks" }}
          className={`settings-nav-tab ${tab === "networks" ? "active" : ""}`}
        >
          <NetworkIcon size={16} />
          <span>Networks</span>
        </Link>
      </div>

      {tab === "images" && (
        <div className="stats-grid" style={{ margin: "16px 0" }}>
          <div className="stat-card">
            <div className="stat-icon-wrapper total">
              <HardDrive size={20} />
            </div>
            <div className="stat-info">
              <span className="stat-value">
                {imageStats ? formatBytes(imageStats.totalBytes) : "--"}
              </span>
              <span className="stat-label">Total Space Used</span>
            </div>
          </div>
          <div className="stat-card">
            <div className="stat-icon-wrapper updates">
              <Recycle size={20} />
            </div>
            <div className="stat-info">
              <span className="stat-value">
                {imageStats ? formatBytes(imageStats.freeableBytes) : "--"}
              </span>
              <span className="stat-label">Freeable Space</span>
            </div>
          </div>
          <div className="stat-card">
            <div className="stat-icon-wrapper stopped">
              <Layers size={20} />
            </div>
            <div className="stat-info">
              <span className="stat-value">{imageStats ? imageStats.imageCount : "--"}</span>
              <span className="stat-label">Images</span>
            </div>
          </div>
        </div>
      )}

      {error && (
        <div className="auth-error-banner" style={{ marginBottom: "16px" }}>
          <ShieldAlert size={18} className="auth-error-icon" />
          <span>{error}</span>
        </div>
      )}

      {isLoading && !result ? (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            padding: "48px",
          }}
        >
          <Loader2 className="animate-spin text-accent" size={32} />
          <span style={{ marginLeft: "12px", fontSize: "14px" }}>Loading resources...</span>
        </div>
      ) : (
        <>
          {result?.kind === "images" && <ImageTable images={result.data} />}
          {result?.kind === "volumes" && <VolumeTable volumes={result.data} />}
          {result?.kind === "networks" && <NetworkTable networks={result.data} />}
        </>
      )}
    </div>
  );
}
