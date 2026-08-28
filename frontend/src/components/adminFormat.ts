import type { Timestamp } from "@bufbuild/protobuf/wkt";

import type { Image } from "../gen/proto/dmanager/v1/admin_pb";

// Formatting helpers shared by the Administration resource tables. The
// AdminService proto maps Docker int64 values to bigint and timestamps to
// google.protobuf.Timestamp; these helpers convert them for display.

/**
 * Formats a byte count using SI units. Defaults match Docker CLI output
 * (e.g. "142 MB", one decimal under 10); `oneDecimal` keeps one decimal at
 * every magnitude (e.g. "142.6 MB") for the summary stat cards.
 */
export function formatBytes(bytes: bigint, oneDecimal = false): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = Number(bytes);
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit++;
  }
  if (oneDecimal && value !== 0) {
    return `${value.toFixed(1)} ${units[unit]}`;
  }
  const rounded = unit === 0 || value >= 10 ? Math.round(value) : Number(value.toFixed(1));
  return `${rounded} ${units[unit]}`;
}

/** Renders a protobuf timestamp as a locale date string; "—" when absent. */
export function formatTimestamp(ts: Timestamp | undefined): string {
  if (!ts) return "—";
  const ms = Number(ts.seconds) * 1000;
  if (!Number.isFinite(ms) || ms <= 0) return "—";
  return new Date(ms).toLocaleDateString([], {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/** Renders a Unix-seconds value as a relative time (e.g. "3 days ago"). */
export function formatRelativeUnix(seconds: bigint): string {
  const ms = Number(seconds) * 1000;
  if (!Number.isFinite(ms) || ms <= 0) return "—";
  const diff = Date.now() - ms;
  if (diff < 0) return "—";

  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return plural(minutes, "minute");
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return plural(hours, "hour");
  const days = Math.floor(hours / 24);
  if (days < 30) return plural(days, "day");
  const months = Math.floor(days / 30);
  if (months < 12) return plural(months, "month");
  return plural(Math.floor(months / 12), "year");
}

function plural(count: number, unit: string): string {
  return `${count} ${unit}${count === 1 ? "" : "s"} ago`;
}

/** Truncates a Docker object ID to 12 characters, dropping the "sha256:" prefix. */
export function formatShortId(id: string): string {
  const bare = id.startsWith("sha256:") ? id.slice("sha256:".length) : id;
  return bare.slice(0, 12);
}

/** Splits "repository:tag" into its parts, defaulting like the Docker CLI. */
export function splitRepoTag(repoTag: string | undefined): { repository: string; tag: string } {
  if (!repoTag) return { repository: "<none>", tag: "<none>" };
  const lastColon = repoTag.lastIndexOf(":");
  // A trailing colon only starts a tag when followed by a non-path segment;
  // this keeps registry hosts with ports (e.g. registry:5000/img) intact.
  if (lastColon > 0 && !repoTag.slice(lastColon + 1).includes("/")) {
    return { repository: repoTag.slice(0, lastColon), tag: repoTag.slice(lastColon + 1) };
  }
  return { repository: repoTag, tag: "<none>" };
}

/** Default Docker networks recognized as system infrastructure. */
export function isDefaultNetwork(name: string): boolean {
  return name === "bridge" || name === "host" || name === "none";
}

/**
 * Summary stats for the Images tab, derived client-side from ListImages.
 * Sizes are per-image sums as reported by the daemon (shared layers counted
 * per referencing image); images whose usage count the daemon did not
 * calculate (-1) are treated as in use so freeable never overstates.
 */
export function deriveImageStats(images: Image[]): {
  totalBytes: bigint;
  freeableBytes: bigint;
  imageCount: number;
} {
  let totalBytes = 0n;
  let freeableBytes = 0n;
  for (const image of images) {
    totalBytes += image.sizeBytes;
    if (image.containersCount === 0n) {
      freeableBytes += image.sizeBytes;
    }
  }
  return { totalBytes, freeableBytes, imageCount: images.length };
}
