import { useCallback, useEffect, useState } from "react";

import { adminClient } from "../client";
import type { AuditLogEntry } from "../gen/proto/dmanager/v1/admin_pb";

export const AUDIT_PAGE_SIZE = 50;

// Wire values for the documented int32 source/outcome fields (see
// admin.proto — proto enums are avoided so the generated client stays
// compatible with the app's erasableSyntaxOnly TS config).
export const AUDIT_SOURCE = { ALL: 0, USER: 1, SYSTEM: 2 } as const;
export const AUDIT_OUTCOME = { ALL: 0, SUCCESS: 1, FAILURE: 2, DENIED: 3 } as const;
export type AuditSourceFilter = (typeof AUDIT_SOURCE)[keyof typeof AUDIT_SOURCE];
export type AuditOutcomeFilter = (typeof AUDIT_OUTCOME)[keyof typeof AUDIT_OUTCOME];

/**
 * Owns the Audit Logs page state: the search input (debounced 300 ms,
 * server-side), source/outcome filters and pagination. One ListAuditLogs
 * call per committed state change; no background polling — audit history
 * is not live data (design.md §12.5).
 */
export function useAuditLogs() {
  // Raw input vs debounced committed query.
  const [queryInput, setQueryInput] = useState("");
  const [query, setQuery] = useState("");

  const [source, setSource] = useState<AuditSourceFilter>(AUDIT_SOURCE.ALL);
  const [outcome, setOutcome] = useState<AuditOutcomeFilter>(AUDIT_OUTCOME.ALL);
  const [page, setPage] = useState(0); // 0-based

  const [entries, setEntries] = useState<AuditLogEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Debounce the search box so typing issues exactly one call per pause.
  useEffect(() => {
    const t = window.setTimeout(() => {
      setQuery(queryInput);
      setPage(0);
    }, 300);
    return () => window.clearTimeout(t);
  }, [queryInput]);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await adminClient.listAuditLogs({
        query,
        source,
        outcome,
        limit: AUDIT_PAGE_SIZE,
        offset: BigInt(page * AUDIT_PAGE_SIZE), // uint64 wire type
      });
      setEntries(resp.entries);
      setTotal(Number(resp.total));
    } catch (err: unknown) {
      console.error("Failed to load audit logs:", err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [query, source, outcome, page]);

  useEffect(() => {
    void fetchLogs();
  }, [fetchLogs]);

  // Filter changes reset to the first page — the result set changed.
  const updateSource = useCallback((s: AuditSourceFilter) => {
    setSource(s);
    setPage(0);
  }, []);
  const updateOutcome = useCallback((o: AuditOutcomeFilter) => {
    setOutcome(o);
    setPage(0);
  }, []);

  const pages = Math.max(1, Math.ceil(total / AUDIT_PAGE_SIZE));

  return {
    queryInput,
    setQueryInput,
    source,
    updateSource,
    outcome,
    updateOutcome,
    page,
    setPage,
    pages,
    entries,
    total,
    loading,
    error,
    refresh: fetchLogs,
  };
}
