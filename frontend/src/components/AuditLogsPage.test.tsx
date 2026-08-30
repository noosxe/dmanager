import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AuditLogEntry } from "../gen/proto/dmanager/v1/admin_pb";
import { AUDIT_OUTCOME, AUDIT_SOURCE } from "../hooks/useAuditLogs";
import { AuditLogsPage } from "./AuditLogsPage";

// Mock the admin client.
vi.mock("../client", () => ({
  adminClient: {
    listAuditLogs: vi.fn(),
  },
}));

// Admin client shape is asserted via the mocked module below.
const adminClient = vi.mocked((await import("../client")).adminClient, { partial: true });
const listAuditLogsMock = vi.mocked(adminClient.listAuditLogs);

// useAuth is not used by the page itself; the nav item and route guard live
// in DashboardLayout/router — this file tests the page surface.
const mockEntry = (overrides: Partial<AuditLogEntry>): AuditLogEntry =>
  ({
    id: 1n,
    createdAt: { seconds: 1717171717n },
    actor: "admin",
    actorRole: "admin",
    source: AUDIT_SOURCE.USER,
    action: "image.delete",
    resourceType: "image",
    resourceId: "sha256:abcdef1234567890",
    outcome: AUDIT_OUTCOME.SUCCESS,
    detail: "image deleted",
    ...overrides,
  }) as AuditLogEntry;

function entryList(): AuditLogEntry[] {
  return [
    mockEntry({
      id: 3n,
      action: "network.prune",
      outcome: AUDIT_OUTCOME.SUCCESS,
      detail: "pruned 1 network(s) (stale)",
    }),
    mockEntry({
      id: 2n,
      actor: "system",
      actorRole: "",
      source: AUDIT_SOURCE.SYSTEM,
      action: "container.upgrade",
      resourceType: "container",
      resourceId: "abc123def456",
      detail: "upgraded sha256:aaa… → sha256:bbb…",
    }),
    mockEntry({
      id: 1n,
      actor: "viewer",
      actorRole: "viewer",
      outcome: AUDIT_OUTCOME.DENIED,
      detail: "admin role required",
    }),
    mockEntry({ id: 0n, outcome: AUDIT_OUTCOME.FAILURE, detail: "engine unreachable" }),
  ];
}

describe("AuditLogsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listAuditLogsMock.mockResolvedValue({ entries: entryList(), total: 4n } as never);
  });

  it("renders entries with actor, action, outcome badges and detail", async () => {
    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(screen.getByText("network.prune")).toBeInTheDocument();
    });

    const table = screen.getByRole("table", { name: "Audit log entries" });

    // User actor with role (two admin-sourced rows share the label).
    expect(screen.getAllByText("admin (admin)").length).toBe(2);
    // System actor renders without role (scoped to the table — the source
    // filter option also says "System").
    expect(within(table).getByText("System")).toBeInTheDocument();
    // Denied viewer keeps its role label.
    expect(screen.getByText("viewer (viewer)")).toBeInTheDocument();

    // Outcome badges (scoped to the table — filter options share labels).
    expect(within(table).getAllByText("Success").length).toBe(2);
    expect(within(table).getByText("Denied")).toBeInTheDocument();
    expect(within(table).getByText("Failure")).toBeInTheDocument();

    // Digest-transition detail with tooltip.
    expect(screen.getByText("upgraded sha256:aaa… → sha256:bbb…")).toBeInTheDocument();
  });

  it("requests the first page from the server", async () => {
    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenCalled();
    });
    expect(listAuditLogsMock).toHaveBeenCalledWith(
      expect.objectContaining({ query: "", limit: 50, offset: 0n }),
    );
    expect(screen.getByText("1–4 of 4")).toBeInTheDocument();
  });

  it("debounces the search box into a single server-side query", async () => {
    const user = userEvent.setup();
    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenCalledTimes(1);
    });

    await user.type(screen.getByLabelText("Search audit logs"), "prune");

    // One committed call after the 300 ms pause, not one per keystroke.
    await waitFor(
      () => {
        expect(listAuditLogsMock).toHaveBeenCalledTimes(2);
      },
      { timeout: 1500 },
    );
    expect(listAuditLogsMock).toHaveBeenLastCalledWith(expect.objectContaining({ query: "prune" }));
  });

  it("filters by source and outcome, resetting to page 1", async () => {
    const user = userEvent.setup();
    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenCalledTimes(1);
    });

    await user.selectOptions(
      screen.getByLabelText("Filter by source"),
      String(AUDIT_SOURCE.SYSTEM),
    );
    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ source: AUDIT_SOURCE.SYSTEM, offset: 0n }),
      );
    });

    await user.selectOptions(
      screen.getByLabelText("Filter by outcome"),
      String(AUDIT_OUTCOME.FAILURE),
    );
    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ outcome: AUDIT_OUTCOME.FAILURE }),
      );
    });
  });

  it("paginates forward and back", async () => {
    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenCalledTimes(1);
    });

    const next = screen.getByRole("button", { name: "Next page" });
    expect(next).toBeDisabled(); // 4 entries, 1 page

    // Simulate a multi-page result to exercise the controls.
    listAuditLogsMock.mockResolvedValue({
      entries: entryList(),
      total: 120n,
    } as never);
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    await waitFor(() => {
      expect(screen.getByText("Page 1 of 3")).toBeInTheDocument();
    });
    expect(next).toBeEnabled();

    fireEvent.click(next);
    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 50n }));
    });
    expect(screen.getByText("51–100 of 120")).toBeInTheDocument();

    const prev = screen.getByRole("button", { name: "Previous page" });
    fireEvent.click(prev);
    await waitFor(() => {
      expect(listAuditLogsMock).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 0n }));
    });
  });

  it("shows the empty state and surfaces errors with a working retry", async () => {
    listAuditLogsMock.mockResolvedValue({ entries: [], total: 0n } as never);
    render(<AuditLogsPage />);

    await waitFor(() => {
      expect(screen.getByText("No Audit Entries Found")).toBeInTheDocument();
    });
    expect(screen.getByText("No entries")).toBeInTheDocument();

    // Error state + retry via Refresh.
    listAuditLogsMock.mockRejectedValue(new Error("storage unavailable"));
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent("storage unavailable");
    });

    listAuditLogsMock.mockResolvedValue({ entries: entryList(), total: 4n } as never);
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => {
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    });
    expect(screen.getByText("network.prune")).toBeInTheDocument();
  });
});
