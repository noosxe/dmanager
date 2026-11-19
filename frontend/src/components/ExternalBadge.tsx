import { Lock } from "lucide-react";

import type { Container } from "../hooks/useContainers";

/** Tooltip for controls that are disabled because another tool owns the container. */
export function externalReason(container: Container): string {
  return `Managed externally (${container.managedBy}); dmanager does not upgrade this container`;
}

/** Badge naming the tool that manages the container; renders nothing otherwise. */
export function ExternalBadge({ container }: { container: Container }) {
  if (!container.externallyManaged) return null;
  return (
    <span className="external-badge" title={externalReason(container)}>
      <Lock size={10} />
      <span>Managed by {container.managedBy}</span>
    </span>
  );
}
