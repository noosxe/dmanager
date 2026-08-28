import { useCallback, useEffect, useRef, useState } from "react";

import { adminClient } from "../client";

export type EngineStatus = "checking" | "online" | "offline";

export interface EngineStatusState {
  status: EngineStatus;
  /** Human-readable detail: API version when online, failure reason when offline. */
  detail: string;
}

const POLL_INTERVAL_MS = 30_000;

/**
 * Drives the sidebar engine status pill (design.md §10.3).
 *
 * Polls AdminService.CheckEngine every 30s — skipped while the tab is hidden
 * — with an immediate re-check on window focus and on becoming visible, which
 * covers sleep/wake cycles without a fixed cadence doing the work.
 *
 * The RPC uses status-not-error semantics for daemon outages, so a Connect
 * (transport) failure reaching this hook means the *backend* is unreachable.
 */
export function useEngineStatus(): EngineStatusState {
  const [state, setState] = useState<EngineStatusState>({ status: "checking", detail: "" });
  const inFlight = useRef(0);
  const timerRef = useRef<number | null>(null);

  const check = useCallback(async () => {
    const ticket = ++inFlight.current;
    try {
      const resp = await adminClient.checkEngine({});
      // Ignore responses that were overtaken by a later check or unmount.
      if (ticket !== inFlight.current) {
        return;
      }
      if (resp.connected) {
        setState({
          status: "online",
          detail: resp.apiVersion ? `Docker Engine API v${resp.apiVersion}` : "Docker Engine",
        });
      } else {
        setState({ status: "offline", detail: resp.error || "Docker Engine unreachable" });
      }
    } catch {
      if (ticket !== inFlight.current) {
        return;
      }
      // Transport-level failure: the backend itself is unreachable.
      setState({ status: "offline", detail: "Backend unreachable" });
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
