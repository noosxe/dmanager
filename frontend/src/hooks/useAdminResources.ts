import { useCallback, useEffect, useState } from "react";

import { adminClient } from "../client";
import { formatBytes } from "../components/adminFormat";
import { useToast } from "../context/ToastContext";
import type { Image, Network, Volume } from "../gen/proto/dmanager/v1/admin_pb";

export type AdminResourceKind = "images" | "volumes" | "networks";

// The fetch result is a discriminated union keyed by resource kind so the
// page can narrow to a concrete resource type without casts.
export type AdminResourcesResult =
  | { kind: "images"; data: Image[] }
  | { kind: "volumes"; data: Volume[] }
  | { kind: "networks"; data: Network[] };

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
        } else {
          const resp = await adminClient.listNetworks({});
          next = { kind, data: resp.networks };
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

  // Prunes all unused images in one daemon call (§9.8, #196); one prune at a
  // time. The toast reports the daemon-returned reclaimed bytes, not a
  // client-side estimate; the list is re-fetched as the source of truth.
  const pruneImages = useCallback(async () => {
    if (pruning) {
      return;
    }
    setPruning(true);
    try {
      const resp = await adminClient.pruneImages({ danglingOnly: false });
      const count = resp.imagesDeleted.length;
      const noun = count === 1 ? "image" : "images";
      toast.success(`Reclaimed ${formatBytes(resp.spaceReclaimed, true)} from ${count} ${noun}.`);
    } catch (err: unknown) {
      console.error("Failed to prune images:", err);
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to prune images: ${msg}`);
      return;
    } finally {
      setPruning(false);
    }
    refresh();
  }, [pruning, refresh, toast]);

  return { result, isLoading, error, refresh, deleteImage, deletingId, pruneImages, pruning };
}
