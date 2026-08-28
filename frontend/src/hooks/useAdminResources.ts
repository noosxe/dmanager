import { useCallback, useEffect, useState } from "react";

import { adminClient } from "../client";
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
 * the AdminService, plus the images-tab deletion mutation. Lists have no
 * polling or streaming — data is fetched on mount, on tab change, and on
 * manual refresh only; deletion re-fetches via refresh().
 */
export function useAdminResources(kind: AdminResourceKind) {
  const [result, setResult] = useState<AdminResourcesResult | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    const fetchResources = async () => {
      setIsLoading(true);
      setError(null);
      setDeleteError(null);
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
      setDeleteError(null);
      try {
        // force=true avoids spurious tag-conflict failures for multi-tag
        // images; the daemon still refuses images in use (design.md §9.7).
        await adminClient.deleteImage({ id, force: true });
      } catch (err: unknown) {
        console.error("Failed to delete image:", err);
        const msg = err instanceof Error ? err.message : String(err);
        setDeleteError(`Failed to delete image: ${msg}`);
        return;
      } finally {
        setDeletingId(null);
      }
      refresh();
    },
    [deletingId, refresh],
  );

  return { result, isLoading, error, refresh, deleteImage, deletingId, deleteError };
}
