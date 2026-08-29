import { useCallback, useEffect, useState } from "react";

import { adminClient } from "../client";
import { formatBytes } from "../components/adminFormat";
import { useToast } from "../context/ToastContext";
import type {
  BuildCacheRecord,
  GetBuildCacheStatsResponse,
  GetVolumeUsageResponse,
  Image,
  ListBuildCacheRecordsResponse,
  Network,
  Volume,
} from "../gen/proto/dmanager/v1/admin_pb";

export type AdminResourceKind = "images" | "volumes" | "networks" | "builder";

// The fetch result is a discriminated union keyed by resource kind so the
// page can narrow to a concrete resource type without casts.
export type AdminResourcesResult =
  | { kind: "images"; data: Image[] }
  | { kind: "volumes"; data: Volume[] }
  | { kind: "networks"; data: Network[] }
  | { kind: "builder"; data: GetBuildCacheStatsResponse };

/**
 * Fetches one kind of Docker host resource (images, volumes, networks) from
 * the AdminService, plus the images-tab deletion and prune mutations. Lists have no
 * polling or streaming — data is fetched on mount, on tab change, and on
 * manual refresh only; deletion reports via the toast system and
 * re-fetches via refresh().
 */
export function useAdminResources(kind: AdminResourceKind) {
  const toast = useToast();
  const [result, setResult] = useState<AdminResourcesResult | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [pruning, setPruning] = useState(false);
  const [pruningScope, setPruningScope] = useState<
    "unused" | "dangling" | "builder" | "volumes" | null
  >(null);
  const [pruningRecordId, setPruningRecordId] = useState<string | null>(null);
  // Builder records are the Builder tab's drill-down slice (design.md §9.10):
  // they load and fail independently of the stats so a records problem never
  // hides the cards.
  const [builderRecords, setBuilderRecords] = useState<BuildCacheRecord[]>([]);
  const [recordsLoading, setRecordsLoading] = useState(false);
  const [recordsError, setRecordsError] = useState(false);
  // Volume usage is measured strictly on demand (design.md §9.11, #212):
  // the daemon walks every volume's directory tree per call, so nothing
  // fetches it automatically — measureVolumeUsage() is button-triggered only,
  // and the slice starts null ("never measured").
  const [volumeUsage, setVolumeUsage] = useState<GetVolumeUsageResponse | null>(null);
  const [measuring, setMeasuring] = useState(false);
  useEffect(() => {
    let cancelled = false;

    const fetchResources = async () => {
      setIsLoading(true);
      setError(null);
      try {
        let next: AdminResourcesResult;
        if (kind === "images") {
          const resp = await adminClient.listImages({});
          next = { kind, data: resp.images };
        } else if (kind === "volumes") {
          const resp = await adminClient.listVolumes({});
          next = { kind, data: resp.volumes };
        } else if (kind === "networks") {
          const resp = await adminClient.listNetworks({});
          next = { kind, data: resp.networks };
        } else {
          const resp = await adminClient.getBuildCacheStats({});
          next = { kind, data: resp };
        }
        if (!cancelled) {
          setResult(next);
        }
      } catch (err: unknown) {
        console.error(`Failed to load ${kind}:`, err);
        if (!cancelled) {
          setError("Unable to connect to the Docker monitor backend.");
          setResult(null);
        }
      } finally {
        if (!cancelled) {
          setIsLoading(false);
        }
      }
    };

    fetchResources();

    return () => {
      cancelled = true;
    };
  }, [kind, refreshNonce]);

  // The drill-down list fetches on its own slice: a failure here renders the
  // records section's error state while the stats cards stay usable.
  useEffect(() => {
    if (kind !== "builder") {
      return;
    }
    let cancelled = false;

    const fetchRecords = async () => {
      setRecordsLoading(true);
      setRecordsError(false);
      try {
        const resp: ListBuildCacheRecordsResponse = await adminClient.listBuildCacheRecords({});
        if (!cancelled) {
          setBuilderRecords(resp.records);
        }
      } catch (err: unknown) {
        console.error("Failed to load build cache records:", err);
        if (!cancelled) {
          setRecordsError(true);
          setBuilderRecords([]);
        }
      } finally {
        if (!cancelled) {
          setRecordsLoading(false);
        }
      }
    };

    fetchRecords();

    return () => {
      cancelled = true;
    };
  }, [kind, refreshNonce]);

  const refresh = useCallback(() => {
    setRefreshNonce((n) => n + 1);
  }, []);

  // Deletes one image at a time; the daemon is the source of truth so a
  // successful delete simply triggers a re-fetch rather than patching state.
  const deleteImage = useCallback(
    async (id: string) => {
      if (deletingId !== null) {
        return;
      }
      setDeletingId(id);
      try {
        // force=true avoids spurious tag-conflict failures for multi-tag
        // images; the daemon still refuses images in use (design.md §9.7).
        await adminClient.deleteImage({ id, force: true });
      } catch (err: unknown) {
        console.error("Failed to delete image:", err);
        const msg = err instanceof Error ? err.message : String(err);
        toast.error(`Failed to delete image: ${msg}`);
        return;
      } finally {
        setDeletingId(null);
      }
      toast.success("Image deleted successfully.");
      refresh();
    },
    [deletingId, refresh, toast],
  );

  // Prunes in one daemon call in the scope the caller picks (§9.8, #196/#203):
  // `danglingOnly: false` = every unused image (tagged + untagged), `true` =
  // untagged only. One prune at a time — `pruning` gates both buttons while
  // `pruningScope` names which one spins. The toast reports the
  // daemon-returned reclaimed bytes, not a client-side estimate; the list is
  // re-fetched as the source of truth.
  const pruneImages = useCallback(
    async (danglingOnly: boolean) => {
      if (pruning) {
        return;
      }
      setPruning(true);
      setPruningScope(danglingOnly ? "dangling" : "unused");
      try {
        const resp = await adminClient.pruneImages({ danglingOnly });
        const count = resp.imagesDeleted.length;
        const noun = count === 1 ? "image" : "images";
        const scope = danglingOnly ? "dangling" : "unused";
        toast.success(
          `Reclaimed ${formatBytes(resp.spaceReclaimed, true)} from ${count} ${scope} ${noun}.`,
        );
      } catch (err: unknown) {
        console.error("Failed to prune images:", err);
        const msg = err instanceof Error ? err.message : String(err);
        toast.error(`Failed to prune images: ${msg}`);
        return;
      } finally {
        setPruning(false);
        setPruningScope(null);
      }
      refresh();
    },
    [pruning, refresh, toast],
  );

  // Prunes build cache records in one daemon call (design.md §9.9, #206) —
  // the builder-owned space image prunes cannot free. `all=false` preserves
  // buildkit-internal cache types; InUse records are daemon-protected.
  // Shares the single-flight `pruning` flag with the image prunes; the
  // toast reports the daemon-returned reclaimed bytes.
  const pruneBuildCache = useCallback(async () => {
    if (pruning) {
      return;
    }
    setPruning(true);
    setPruningScope("builder");
    try {
      const resp = await adminClient.pruneBuildCache({ all: false });
      const count = resp.cachesDeleted;
      const noun = count === 1 ? "record" : "records";
      toast.success(
        `Reclaimed ${formatBytes(resp.spaceReclaimed, true)} from ${count} cache ${noun}.`,
      );
    } catch (err: unknown) {
      console.error("Failed to prune build cache:", err);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to prune build cache: ${msg}`);
      return;
    } finally {
      setPruning(false);
      setPruningScope(null);
    }
    refresh();
  }, [pruning, refresh, toast]);

  // Deletes exactly one build cache record (design.md §9.10, #209) — the
  // daemon's id filter scopes the prune to that record. Joins the same
  // single-flight `pruning` guard as the other prunes: one destructive
  // builder op at a time. The toast reports daemon truth — count can be 0
  // when the daemon protected the record.
  const pruneBuildCacheRecord = useCallback(
    async (id: string) => {
      if (pruning) {
        return;
      }
      setPruning(true);
      setPruningRecordId(id);
      try {
        const resp = await adminClient.pruneBuildCacheRecord({ id });
        const count = resp.cachesDeleted;
        const noun = count === 1 ? "record" : "records";
        toast.success(
          `Deleted ${count} cache ${noun}, reclaimed ${formatBytes(resp.spaceReclaimed, true)}.`,
        );
      } catch (err: unknown) {
        console.error("Failed to delete cache record:", err);
        const msg = err instanceof Error ? err.message : String(err);
        toast.error(`Failed to delete cache record: ${msg}`);
        return;
      } finally {
        setPruning(false);
        setPruningRecordId(null);
      }
      refresh();
    },
    [pruning, refresh, toast],
  );

  // Measures volume disk usage on explicit user action only (design.md
  // §9.11, #212) — seconds-scale on the daemon, so it never joins any
  // auto-effect. Result replaces the previous measurement wholesale.
  const measureVolumeUsage = useCallback(async () => {
    if (measuring) {
      return;
    }
    setMeasuring(true);
    try {
      const resp = await adminClient.getVolumeUsage({});
      setVolumeUsage(resp);
    } catch (err: unknown) {
      console.error("Failed to measure volume usage:", err);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to measure volume usage: ${msg}`);
    } finally {
      setMeasuring(false);
    }
  }, [measuring, toast]);

  // Prunes all unused volumes in one daemon call (design.md §9.11, #212).
  // The daemon re-evaluates container references at prune time — a stale
  // measurement can never cause a protected volume to be deleted, and the
  // toast reports the daemon's actual names and bytes. Joins the shared
  // single-flight `pruning` guard; when sizes were previously calculated,
  // the measurement re-runs automatically to validate the reclaim.
  const pruneVolumes = useCallback(async () => {
    if (pruning) {
      return;
    }
    setPruning(true);
    setPruningScope("volumes");
    try {
      const resp = await adminClient.pruneVolumes({});
      const count = resp.volumesDeleted;
      const noun = count === 1 ? "volume" : "volumes";
      const namesSuffix =
        count > 0 && count <= 3 && resp.names.length > 0 ? ` (${resp.names.join(", ")})` : "";
      toast.success(
        `Reclaimed ${formatBytes(resp.spaceReclaimed, true)} from ${count} unused ${noun}${namesSuffix}.`,
      );
    } catch (err: unknown) {
      console.error("Failed to prune volumes:", err);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to prune volumes: ${msg}`);
      return;
    } finally {
      setPruning(false);
      setPruningScope(null);
    }
    refresh();
    if (volumeUsage !== null) {
      void measureVolumeUsage();
    }
  }, [pruning, refresh, toast, volumeUsage, measureVolumeUsage]);

  return {
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
    volumeUsage,
    measuring,
    measureVolumeUsage,
    pruneVolumes,
  };
}
