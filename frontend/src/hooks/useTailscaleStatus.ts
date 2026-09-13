import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useCallback, useEffect, useRef, useState } from "react";

import { adminClient } from "../client";

export type TailscaleLifecycleState = "disabled" | "starting" | "running" | "failed";

export interface TailscaleStatusState {
  /** True once the first RPC resolved (enabled or not). */
  loaded: boolean;
  enabled: boolean;
  state: TailscaleLifecycleState;
  /** Live ipn backend state when reachable (e.g. "Running"). */
  backendState: string;
  hostname: string;
  dnsName: string;
  ips: string[];
  port: number;
  httpsEnabled: boolean;
  certDomains: string[];
  /** Undefined when the node key does not expire or it is unknown. */
  keyExpiry?: Date;
  /** Failure detail when state is "failed" or the live probe failed. */
  error: string;
}

const POLL_INTERVAL_MS = 60_000;

const initial: TailscaleStatusState = {
  loaded: false,
  enabled: false,
  state: "disabled",
  backendState: "",
  hostname: "",
  dnsName: "",
  ips: [],
  port: 80,
  httpsEnabled: false,
  certDomains: [],
  error: "",
};

/**
 * Drives the Tailscale node card on the Administration page
 * (docs/tailscale.md §9 Q8/Q11).
 *
 * Polls AdminService.GetTailscaleStatus every 60s — skipped while the tab is
 * hidden, with an immediate re-check on focus/visibility — mirroring the
 * useEngineStatus pattern. Status-not-error semantics: a disabled or
 * degraded node is a successful response; a transport failure keeps the
 * last known values (the engine pill separately covers backend outages).
 */
export function useTailscaleStatus(): TailscaleStatusState {
  const [state, setState] = useState<TailscaleStatusState>(initial);
  const inFlight = useRef(0);
  const timerRef = useRef<number | null>(null);

  const check = useCallback(async () => {
    const ticket = ++inFlight.current;
    try {
      const resp = await adminClient.getTailscaleStatus({});
      if (ticket !== inFlight.current) {
        return;
      }
      setState({
        loaded: true,
        enabled: resp.enabled,
        state: (["disabled", "starting", "running", "failed"] as const).includes(
          resp.state as TailscaleLifecycleState,
        )
          ? (resp.state as TailscaleLifecycleState)
          : "starting",
        backendState: resp.backendState,
        hostname: resp.hostname,
        dnsName: resp.dnsName,
        ips: [...resp.ips],
        port: resp.port,
        httpsEnabled: resp.httpsEnabled,
        certDomains: [...resp.certDomains],
        keyExpiry: resp.keyExpiry ? timestampDate(resp.keyExpiry) : undefined,
        error: resp.error,
      });
    } catch {
      // Transport-level failure: keep the last known status; the engine
      // status pill already surfaces backend unreachability.
    }
  }, []);

  useEffect(() => {
    check();

    const schedulePoll = () => {
      if (timerRef.current !== null) {
        return;
      }
      timerRef.current = window.setInterval(() => {
        if (!document.hidden) {
          check();
        }
      }, POLL_INTERVAL_MS);
    };

    const stopPoll = () => {
      if (timerRef.current !== null) {
        window.clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };

    const onVisibility = () => {
      if (document.hidden) {
        stopPoll();
      } else {
        check();
        schedulePoll();
      }
    };

    const onFocus = () => {
      check();
    };

    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("focus", onFocus);
    schedulePoll();

    return () => {
      stopPoll();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("focus", onFocus);
      inFlight.current++;
    };
  }, [check]);

  return state;
}
