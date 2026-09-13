import { KeyRound, ShieldAlert } from "lucide-react";
import type { ReactElement } from "react";

import type { TailscaleStatusState } from "../hooks/useTailscaleStatus";

/**
 * Tailnet node status card for the Administration page (docs/tailscale.md
 * §9 Q8/Q11). Rendered only when the embedded node is enabled — a disabled
 * feature stays invisible, mirroring its inertness everywhere else.
 *
 * Status-not-error: a degraded (failed) node renders with its error detail
 * rather than hiding the card — operators need to see *why* the tailnet is
 * down without reading server logs.
 */

const KEY_EXPIRY_WARN_DAYS = 7;

function stateDotClass(state: TailscaleStatusState["state"]): string {
  switch (state) {
    case "running":
      return "status-dot";
    case "failed":
      return "status-dot offline";
    default:
      return "status-dot checking";
  }
}

function stateLabel(state: TailscaleStatusState["state"]): string {
  switch (state) {
    case "running":
      return "Online";
    case "failed":
      return "Failed";
    case "starting":
      return "Connecting";
    default:
      return state;
  }
}

function detailRow(label: string, value: string): ReactElement {
  return (
    <div style={{ display: "flex", gap: "8px", minWidth: "220px" }}>
      <span style={{ color: "var(--text-secondary, #94a3b8)", minWidth: "96px" }}>{label}</span>
      <span
        style={{
          color: "var(--text)",
          fontFamily: "ui-monospace, monospace",
          fontSize: "12px",
          wordBreak: "break-all",
        }}
      >
        {value}
      </span>
    </div>
  );
}

/** Days until key expiry; negative when already expired; null when unset. */
export function keyExpiryDays(expiry: Date | undefined): number | null {
  if (!expiry) {
    return null;
  }
  return Math.floor((expiry.getTime() - Date.now()) / 86_400_000);
}

export function TailscaleStatusCard({ status }: { status: TailscaleStatusState }) {
  if (!status.enabled) {
    return null;
  }

  const expiryDays = keyExpiryDays(status.keyExpiry);
  const expiryWarn =
    expiryDays !== null && expiryDays < KEY_EXPIRY_WARN_DAYS ? (
      <div
        className={expiryDays < 0 ? "status-dot offline" : "status-dot checking"}
        style={{ display: "flex", alignItems: "center", gap: "6px", color: "var(--text)" }}
      >
        <KeyRound size={14} />
        <span style={{ fontSize: "13px" }}>
          {expiryDays < 0
            ? "Node key expired — tailnet access needs re-registration"
            : `Node key expires in ${expiryDays} day${expiryDays === 1 ? "" : "s"} — plan re-registration`}
        </span>
      </div>
    ) : null;

  return (
    <div
      className="stat-card"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "10px",
        padding: "16px",
        alignItems: "flex-start",
      }}
      data-testid="tailscale-status-card"
    >
      <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
        <span className={stateDotClass(status.state)} />
        <strong style={{ color: "var(--text)" }}>Tailscale node {stateLabel(status.state)}</strong>
        {status.backendState && status.state === "running" ? (
          <span style={{ color: "var(--text-secondary, #94a3b8)", fontSize: "12px" }}>
            ({status.backendState})
          </span>
        ) : null}
      </div>

      {status.dnsName ? detailRow("DNS name", status.dnsName) : null}
      {status.ips.length > 0 ? detailRow("Tailnet IPs", status.ips.join(", ")) : null}
      {status.dnsName
        ? detailRow(
            "Access",
            status.httpsEnabled
              ? `https://${status.dnsName} (also http port ${status.port})`
              : `http://${status.dnsName}:${status.port}`,
          )
        : null}
      {status.error ? (
        <div
          style={{ display: "flex", alignItems: "center", gap: "6px", color: "#f87171" }}
          title={status.error}
        >
          <ShieldAlert size={14} />
          <span style={{ fontSize: "12px", maxWidth: "480px", overflow: "hidden" }}>
            {status.error}
          </span>
        </div>
      ) : null}
      {expiryWarn}
    </div>
  );
}
