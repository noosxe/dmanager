import { useParams } from "@tanstack/react-router";
import {
  HardDrive,
  Hammer,
  Image as ImageIcon,
  Layers,
  Loader2,
  Network as NetworkIcon,
  PackageOpen,
  Recycle,
  RefreshCw,
  ShieldAlert,
  TagX,
  Trash2,
} from "lucide-react";
import { useState } from "react";

import type { BuildCacheRecord } from "../gen/proto/dmanager/v1/admin_pb";
import type { AdminResourceKind } from "../hooks/useAdminResources";
import { useAdminResources } from "../hooks/useAdminResources";
import { useAuth } from "../hooks/useAuth";
import { deriveImageStats, formatBytes, formatShortId } from "./adminFormat";
import { Builder } from "./Builder";
import { BuilderRecords } from "./BuilderRecords";
import { ConfirmDialog } from "./ConfirmDialog";
import { ImageTable } from "./ImageTable";
import { NetworkTable } from "./NetworkTable";
import { PageTabs, type PageTabItem } from "./PageTabs";
import { VolumeTable } from "./VolumeTable";

// Administration page: Docker host resource inventories (images,
// volumes, networks) behind three tabs, plus admin-gated image deletion.
// The tab comes from the route param (validated by the router's beforeLoad
// guard).
export function Administration() {
  const params = useParams({ strict: false }) as { tab?: string } | undefined;
  const routeTab = params?.tab;
  const tab: AdminResourceKind =
    routeTab === "volumes" || routeTab === "networks" || routeTab === "builder"
      ? routeTab
      : "images";

  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const {
    result,
    isLoading,
    error,
    refresh,
    deleteImage,
    deletingId,
    pruneImages,
    pruneBuildCache,
    pruneBuildCacheRecord,
    pruning,
    pruningScope,
    pruningRecordId,
    builderRecords,
    recordsLoading,
    recordsError,
  } = useAdminResources(tab);

  // Derived Images-tab summary (design.md §9.6); null on other tabs or

  // The prune confirmation is armed by the actions-row buttons (#196/#203); the
  // dialog owns the busy lockout while the RPC is in flight. One scope-driven
  // dialog serves both buttons.
  const [pendingPrune, setPendingPrune] = useState<"unused" | "dangling" | "builder" | null>(null);
  // The per-record delete dialog is a separate arm (design.md §9.10): it
  // names the specific record instead of an aggregate scope.
  const [pendingRecord, setPendingRecord] = useState<BuildCacheRecord | null>(null);
  // while no successful images result exists yet (-- placeholders).
  const imageStats = result?.kind === "images" ? deriveImageStats(result.data) : null;
  const buildStats = result?.kind === "builder" ? result.data : null;

  // Tab bar items (§9.4): active state follows the resolved route tab.
  const adminTabs: PageTabItem[] = [
    {
      to: "/administration/$tab",
      params: { tab: "images" },
      icon: ImageIcon,
      label: "Images",
      active: tab === "images",
    },
    {
      to: "/administration/$tab",
      params: { tab: "builder" },
      icon: Hammer,
      label: "Builder",
      active: tab === "builder",
    },
    {
      to: "/administration/$tab",
      params: { tab: "volumes" },
      icon: HardDrive,
      label: "Volumes",
      active: tab === "volumes",
    },
    {
      to: "/administration/$tab",
      params: { tab: "networks" },
      icon: NetworkIcon,
      label: "Networks",
      active: tab === "networks",
    },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "24px", width: "100%" }}>
      <div className="dashboard-header">
        <div className="header-title-section">
          <h2>Administration</h2>
          <p>
            Browse host images, builder cache, volumes, and networks — manage what the daemon
            stores.
          </p>
        </div>
        <button
          type="button"
          className="auth-submit-btn"
          style={{ padding: "10px 16px", fontSize: "13px" }}
          onClick={refresh}
          disabled={isLoading}
        >
          <RefreshCw size={14} className={isLoading ? "spinner" : ""} />
          <span>Sync Now</span>
        </button>
      </div>

      <PageTabs tabs={adminTabs} />

      {tab === "images" && (
        <div className="stats-grid">
          <div className="stat-card">
            <div className="stat-icon-wrapper total">
              <HardDrive size={20} />
            </div>
            <div className="stat-info">
              <span className="stat-value">
                {imageStats ? formatBytes(imageStats.totalBytes, true) : "--"}
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
                {imageStats ? formatBytes(imageStats.freeableBytes, true) : "--"}
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
          <div className="stat-card">
            <div className="stat-icon-wrapper unused">
              <PackageOpen size={20} />
            </div>
            <div className="stat-info">
              <span className="stat-value">{imageStats ? imageStats.unusedCount : "--"}</span>
              <span className="stat-label">Unused</span>
            </div>
          </div>
          <div className="stat-card">
            <div className="stat-icon-wrapper dangling">
              <TagX size={20} />
            </div>
            <div className="stat-info">
              <span className="stat-value">{imageStats ? imageStats.danglingCount : "--"}</span>
              <span className="stat-label">Dangling</span>
            </div>
          </div>
        </div>
      )}

      {tab === "images" && (
        <div className="images-prune-row">
          <button
            type="button"
            className="prune-btn"
            disabled={pruning || !isAdmin || !imageStats || imageStats.freeableBytes === 0n}
            title={
              !isAdmin
                ? "Admin role required"
                : imageStats && imageStats.freeableBytes === 0n
                  ? "No unused images to prune"
                  : undefined
            }
            onClick={() => setPendingPrune("unused")}
          >
            <Trash2 size={14} className={pruningScope === "unused" ? "spinner" : ""} />
            {pruningScope === "unused" ? "Pruning…" : "Prune Unused"}
          </button>
          <button
            type="button"
            className="prune-btn"
            disabled={pruning || !isAdmin || !imageStats || imageStats.danglingFreeableBytes === 0n}
            title={
              !isAdmin
                ? "Admin role required"
                : imageStats && imageStats.danglingFreeableBytes === 0n
                  ? "No dangling images to prune"
                  : undefined
            }
            onClick={() => setPendingPrune("dangling")}
          >
            <Trash2 size={14} className={pruningScope === "dangling" ? "spinner" : ""} />
            {pruningScope === "dangling" ? "Pruning…" : "Prune Dangling"}
          </button>
        </div>
      )}
      {error && (
        <div className="auth-error-banner">
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
          {result?.kind === "images" && (
            <ImageTable
              images={result.data}
              isAdmin={isAdmin}
              deletingId={deletingId}
              onDelete={deleteImage}
            />
          )}
          {result?.kind === "volumes" && <VolumeTable volumes={result.data} />}
          {result?.kind === "networks" && <NetworkTable networks={result.data} />}
          {tab === "builder" && (
            <>
              <Builder
                stats={result?.kind === "builder" ? result.data : null}
                isAdmin={isAdmin}
                pruning={pruning}
                builderPruning={pruningScope === "builder"}
                onPrune={() => setPendingPrune("builder")}
              />
              <BuilderRecords
                records={builderRecords}
                loading={recordsLoading}
                error={recordsError}
                isAdmin={isAdmin}
                busy={pruning}
                pruningRecordId={pruningRecordId}
                onPrune={setPendingRecord}
              />
            </>
          )}
        </>
      )}
      <ConfirmDialog
        open={pendingPrune !== null}
        onClose={() => setPendingPrune(null)}
        onConfirm={() => {
          const scope = pendingPrune;
          setPendingPrune(null);
          if (scope === "builder") {
            void pruneBuildCache();
          } else {
            void pruneImages(scope === "dangling");
          }
        }}
        title={
          pendingPrune === "dangling"
            ? "Prune dangling images?"
            : pendingPrune === "builder"
              ? "Prune build cache?"
              : "Prune unused images?"
        }
        message={
          pendingPrune === "dangling"
            ? `Deletes all ${imageStats?.danglingCount ?? 0} dangling images, reclaiming up to ${imageStats ? formatBytes(imageStats.danglingFreeableBytes, true) : "0 B"}. Tagged images are never touched.`
            : pendingPrune === "builder"
              ? `Deletes ${buildStats?.recordCount ?? 0} build cache records, reclaiming up to ${buildStats ? formatBytes(buildStats.reclaimableBytes, true) : "0 B"}. Future image builds will be slower until the cache is rebuilt.`
              : `Deletes all ${imageStats?.unusedCount ?? 0} unused images, reclaiming up to ${imageStats ? formatBytes(imageStats.freeableBytes, true) : "0 B"}. Images in use are never touched.`
        }
        confirmLabel="Prune"
        variant="danger"
        busy={pruning}
      />
      <ConfirmDialog
        open={pendingRecord !== null}
        onClose={() => setPendingRecord(null)}
        onConfirm={() => {
          const record = pendingRecord;
          setPendingRecord(null);
          if (record) {
            void pruneBuildCacheRecord(record.id);
          }
        }}
        title="Delete cache record?"
        message={
          pendingRecord
            ? `Deletes build cache record ${formatShortId(pendingRecord.id)} (${formatBytes(pendingRecord.sizeBytes, true)}). Shared blob content may free less. Rebuilding this step will be slower until the cache is rebuilt.`
            : ""
        }
        confirmLabel="Delete"
        variant="danger"
        busy={pruningRecordId !== null}
      />
    </div>
  );
}
