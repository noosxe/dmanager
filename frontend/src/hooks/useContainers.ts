import { useCallback, useEffect, useRef, useState } from "react";

import { containerClient } from "../client";
import { useToast } from "../context/ToastContext";

export interface Container {
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

export function useContainers() {
  const [containers, setContainers] = useState<Container[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<Record<string, string>>({}); // { containerId: actionType }

  const toast = useToast();
  const isMounted = useRef(true);
  const pendingDeletes = useRef<Record<string, any>>({});
  const containersRef = useRef<Container[]>([]);

  useEffect(() => {
    containersRef.current = containers;
  }, [containers]);

  const getContainerName = useCallback((id: string) => {
    const c = containersRef.current.find((item) => item.id === id);
    return c ? c.name : id;
  }, []);

  // Map Protobuf message fields safely to camelCase local state format
  const mapProtoContainer = useCallback((c: ProtoContainer): Container => {
    return {
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
    };
  }, []);

  // Fetch initial list of containers
  const fetchContainers = useCallback(async () => {
    try {
      const response = await containerClient.listContainers({});
      if (isMounted.current) {
        const items = (response.containers || []).map(mapProtoContainer);
        setContainers(items);
        setError(null);
      }
    } catch (err: unknown) {
      console.error("Failed to load initial containers:", err);
      if (isMounted.current) {
        setError("Unable to connect to the Docker monitor backend.");
      }
    } finally {
      if (isMounted.current) {
        setIsLoading(false);
      }
    }
  }, [mapProtoContainer]);

  // Hook actions
  const startContainer = useCallback(
    async (id: string) => {
      const name = getContainerName(id);
      toast.info(`Starting container "${name}"...`);
      setActionLoading((prev) => ({ ...prev, [id]: "starting" }));
      try {
        await containerClient.startContainer({ id });
        toast.success(`Container "${name}" started successfully.`);
      } catch (err: unknown) {
        console.error("Start container failed:", err);
        const msg = err instanceof Error ? err.message : "Failed to start container";
        toast.error(`Failed to start container "${name}": ${msg}`);
      } finally {
        setActionLoading((prev) => {
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }
    },
    [getContainerName, toast],
  );

  const stopContainer = useCallback(
    async (id: string) => {
      const name = getContainerName(id);
      toast.info(`Stopping container "${name}"...`);
      setActionLoading((prev) => ({ ...prev, [id]: "stopping" }));
      try {
        await containerClient.stopContainer({ id });
        toast.success(`Container "${name}" stopped successfully.`);
      } catch (err: unknown) {
        console.error("Stop container failed:", err);
        const msg = err instanceof Error ? err.message : "Failed to stop container";
        toast.error(`Failed to stop container "${name}": ${msg}`);
      } finally {
        setActionLoading((prev) => {
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }
    },
    [getContainerName, toast],
  );

  const upgradeContainer = useCallback(
    async (id: string) => {
      const name = getContainerName(id);
      toast.info(`Upgrading container "${name}" (pulling image & re-creating)...`, 15000);
      setActionLoading((prev) => ({ ...prev, [id]: "upgrading" }));
      try {
        await containerClient.upgradeContainer({ id });
        toast.success(`Container "${name}" upgraded successfully.`);
      } catch (err: unknown) {
        console.error("Upgrade container failed:", err);
        const msg = err instanceof Error ? err.message : "Failed to upgrade container";
        toast.error(`Failed to upgrade container "${name}": ${msg}`);
      } finally {
        setActionLoading((prev) => {
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }
    },
    [getContainerName, toast],
  );

  const setContainerAutoUpdate = useCallback(
    async (id: string, autoUpdate: boolean) => {
      const name = getContainerName(id);
      setActionLoading((prev) => ({ ...prev, [id]: "toggling" }));
      try {
        await containerClient.setContainerAutoUpdate({ id, autoUpdate });
        toast.success(
          `Automatic updates ${autoUpdate ? "enabled" : "disabled"} for container "${name}".`,
        );
      } catch (err: unknown) {
        console.error("Set auto-update failed:", err);
        const msg = err instanceof Error ? err.message : "Failed to update auto-update setting";
        toast.error(`Failed to update auto-update for container "${name}": ${msg}`);
      } finally {
        setActionLoading((prev) => {
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }
    },
    [getContainerName, toast],
  );

  const checkContainerUpdates = useCallback(
    async (id: string) => {
      const name = getContainerName(id);
      toast.info(`Checking updates for container "${name}"...`);
      setActionLoading((prev) => ({ ...prev, [id]: "checking" }));
      try {
        const resp = await containerClient.checkContainerUpdates({ id });
        if (resp.updateAvailable) {
          toast.success(`Check complete for "${name}": A new update is available!`);
        } else {
          toast.info(`Check complete for "${name}": Image is already up to date.`);
        }
      } catch (err: unknown) {
        console.error("Check updates failed:", err);
        const msg = err instanceof Error ? err.message : "Failed to check updates";
        toast.error(`Failed to check updates for container "${name}": ${msg}`);
      } finally {
        setActionLoading((prev) => {
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }
    },
    [getContainerName, toast],
  );

  // Set up real-time stream subscription with automatic reconnection
  useEffect(() => {
    isMounted.current = true;
    let abortController = new AbortController();

    // Fetch initial list first
    fetchContainers();

    const startStreamSubscription = async () => {
      while (isMounted.current) {
        try {
          // Initialize stream
          const stream = await containerClient.streamContainers(
            {},
            { signal: abortController.signal },
          );

          for await (const response of stream) {
            if (!isMounted.current) break;

            const action = response.action;
            const containerId = response.containerId;
            const protoContainer = response.container;

            setContainers((prev) => {
              if (action === "delete") {
                const containerToDelete = prev.find((c) => c.id === containerId);
                if (containerToDelete) {
                  const name = containerToDelete.name;
                  if (pendingDeletes.current[name]) {
                    window.clearTimeout(pendingDeletes.current[name]);
                  }
                  pendingDeletes.current[name] = window.setTimeout(() => {
                    setContainers((p) => p.filter((c) => c.id !== containerId));
                    delete pendingDeletes.current[name];
                  }, 300);
                  return prev;
                }
                return prev.filter((c) => c.id !== containerId);
              }
              if (action === "save" && protoContainer) {
                const mapped = mapProtoContainer(protoContainer);
                if (pendingDeletes.current[mapped.name]) {
                  window.clearTimeout(pendingDeletes.current[mapped.name]);
                  delete pendingDeletes.current[mapped.name];
                }
                // Try matching by ID first
                let idx = prev.findIndex((c) => c.id === mapped.id);
                // Fallback to matching by name to handle container ID changes on recreation
                if (idx < 0) {
                  idx = prev.findIndex((c) => c.name === mapped.name);
                }
                if (idx >= 0) {
                  const updated = [...prev];
                  updated[idx] = mapped;
                  return updated;
                }
                return [...prev, mapped];
              }
              return prev;
            });
          }
        } catch (err: unknown) {
          // If aborted, exit cleanly
          if (abortController.signal.aborted) {
            break;
          }
          console.error("Containers stream connection failed, reconnecting in 2s...", err);
          // Wait 2s before reconnecting
          await new Promise((resolve) => setTimeout(resolve, 2000));
          // Reset AbortController for the next attempt
          if (isMounted.current) {
            abortController = new AbortController();
          }
        }
      }
    };

    startStreamSubscription();

    return () => {
      isMounted.current = false;
      abortController.abort();
      // Clear all pending delete timeouts on unmount
      Object.values(pendingDeletes.current).forEach((timeoutId) => {
        if (timeoutId) {
          window.clearTimeout(timeoutId);
        }
      });
    };
  }, [fetchContainers, mapProtoContainer]);

  return {
    containers,
    isLoading,
    error,
    actionLoading,
    refetch: fetchContainers,
    startContainer,
    stopContainer,
    upgradeContainer,
    setContainerAutoUpdate,
    checkContainerUpdates,
  };
}
