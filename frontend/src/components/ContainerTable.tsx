import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Play,
  RefreshCw,
  Server,
  Sparkles,
  Square,
  Terminal,
} from "lucide-react";
import { useState } from "react";

import type { Container } from "../hooks/useContainers";

interface ContainerTableProps {
  containers: Container[];
  isAdmin: boolean;
  actionLoading: Record<string, string | undefined>;
  startContainer: (id: string) => Promise<void>;
  stopContainer: (id: string) => Promise<void>;
  upgradeContainer: (id: string) => Promise<void>;
  setContainerAutoUpdate: (id: string, active: boolean) => Promise<void>;
  checkContainerUpdates: (id: string) => Promise<void>;
  onViewLogs: (id: string, name: string) => void;
  formatDate: (isoStr: string) => string;
}

export function ContainerTable({
  containers,
  isAdmin,
  actionLoading,
  startContainer,
  stopContainer,
  upgradeContainer,
  setContainerAutoUpdate,
  checkContainerUpdates,
  onViewLogs,
  formatDate,
}: ContainerTableProps) {
  const [sorting, setSorting] = useState<SortingState>([{ id: "name", desc: false }]);

  const columns: ColumnDef<Container>[] = [
    {
      accessorKey: "name",
      header: ({ column }) => (
        <button
          type="button"
          className="table-sort-btn"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        >
          Name
          {column.getIsSorted() === "asc" ? (
            <ArrowUp size={14} />
          ) : column.getIsSorted() === "desc" ? (
            <ArrowDown size={14} />
          ) : (
            <ArrowUpDown size={14} />
          )}
        </button>
      ),
      cell: ({ row }) => {
        const container = row.original;
        return (
          <div className="table-cell-name">
            <span className="container-name-text" title={container.name}>
              {container.name}
            </span>
          </div>
        );
      },
    },
    {
      accessorKey: "state",
      header: ({ column }) => (
        <button
          type="button"
          className="table-sort-btn"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        >
          Status
          {column.getIsSorted() === "asc" ? (
            <ArrowUp size={14} />
          ) : column.getIsSorted() === "desc" ? (
            <ArrowDown size={14} />
          ) : (
            <ArrowUpDown size={14} />
          )}
        </button>
      ),
      cell: ({ row }) => {
        const container = row.original;
        const isRunning = container.state === "running";
        return (
          <span className={`status-badge ${isRunning ? "running" : "stopped"}`}>
            <span
              style={{
                width: "6px",
                height: "6px",
                borderRadius: "50%",
                background: isRunning ? "#10b981" : "#6b7280",
                display: "inline-block",
              }}
            />
            <span>{container.state}</span>
          </span>
        );
      },
    },
    {
      accessorKey: "image",
      header: ({ column }) => (
        <button
          type="button"
          className="table-sort-btn"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        >
          Image & Tag
          {column.getIsSorted() === "asc" ? (
            <ArrowUp size={14} />
          ) : column.getIsSorted() === "desc" ? (
            <ArrowDown size={14} />
          ) : (
            <ArrowUpDown size={14} />
          )}
        </button>
      ),
      cell: ({ row }) => {
        const container = row.original;
        return (
          <div className="table-cell-image-wrapper">
            <span className="container-image-text" title={container.image}>
              {container.image}
            </span>
            {container.updateAvailable && (
              <span className="table-update-badge" title="Update available in registry">
                <Sparkles size={10} className="spinner" style={{ animationDuration: "3s" }} />
                <span>Update Ready</span>
              </span>
            )}
          </div>
        );
      },
    },
    {
      accessorKey: "id",
      header: "Container ID",
      cell: ({ row }) => {
        const id = row.original.id;
        return <code className="table-cell-id">{id.slice(0, 12)}</code>;
      },
    },
    {
      accessorKey: "autoUpdate",
      header: ({ column }) => (
        <button
          type="button"
          className="table-sort-btn"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        >
          Auto Updates
          {column.getIsSorted() === "asc" ? (
            <ArrowUp size={14} />
          ) : column.getIsSorted() === "desc" ? (
            <ArrowDown size={14} />
          ) : (
            <ArrowUpDown size={14} />
          )}
        </button>
      ),
      cell: ({ row }) => {
        const container = row.original;
        const loadingType = actionLoading[container.id];
        return (
          <div className="table-cell-autoupdate">
            <button
              type="button"
              onClick={() => setContainerAutoUpdate(container.id, !container.autoUpdate)}
              disabled={!isAdmin || !!loadingType}
              style={{
                background: container.autoUpdate ? "var(--accent)" : "rgba(255,255,255,0.06)",
                border: "1px solid var(--border)",
                borderRadius: "20px",
                width: "36px",
                height: "20px",
                padding: "2px",
                cursor: isAdmin ? "pointer" : "not-allowed",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: container.autoUpdate ? "flex-end" : "flex-start",
                transition: "all var(--transition-fast)",
                opacity: isAdmin ? 1 : 0.6,
              }}
              title={isAdmin ? "Toggle automatic updates" : "Admin required"}
            >
              <span
                style={{
                  width: "14px",
                  height: "14px",
                  borderRadius: "50%",
                  background: "#fff",
                  display: "inline-block",
                  boxShadow: "0 1px 3px rgba(0,0,0,0.2)",
                }}
              />
            </button>
          </div>
        );
      },
    },
    {
      accessorKey: "lastCheckedAt",
      header: ({ column }) => (
        <button
          type="button"
          className="table-sort-btn"
          onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
        >
          Last Checked
          {column.getIsSorted() === "asc" ? (
            <ArrowUp size={14} />
          ) : column.getIsSorted() === "desc" ? (
            <ArrowDown size={14} />
          ) : (
            <ArrowUpDown size={14} />
          )}
        </button>
      ),
      cell: ({ row }) => {
        const lastCheckedAt = row.original.lastCheckedAt;
        return <span className="table-cell-date">{formatDate(lastCheckedAt)}</span>;
      },
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => {
        const container = row.original;
        const isRunning = container.state === "running";
        const loadingType = actionLoading[container.id];
        const hasUpdate = container.updateAvailable;

        return (
          <div className="table-actions-row">
            {isRunning ? (
              <button
                type="button"
                className="card-action-btn stop table-action-btn-sm"
                onClick={() => stopContainer(container.id)}
                disabled={!isAdmin || !!loadingType}
                title="Stop Container"
              >
                {loadingType === "stopping" ? (
                  <RefreshCw size={12} className="spinner" />
                ) : (
                  <Square size={10} />
                )}
                <span>Stop</span>
              </button>
            ) : (
              <button
                type="button"
                className="card-action-btn start table-action-btn-sm"
                onClick={() => startContainer(container.id)}
                disabled={!isAdmin || !!loadingType}
                title="Start Container"
              >
                {loadingType === "starting" ? (
                  <RefreshCw size={12} className="spinner" />
                ) : (
                  <Play size={10} />
                )}
                <span>Start</span>
              </button>
            )}

            {hasUpdate && (
              <button
                type="button"
                className="card-action-btn upgrade table-action-btn-sm"
                onClick={() => upgradeContainer(container.id)}
                disabled={!isAdmin || !!loadingType}
                title="Upgrade Container"
              >
                {loadingType === "upgrading" ? (
                  <RefreshCw size={12} className="spinner" />
                ) : (
                  <ArrowUpDown size={12} />
                )}
                <span>Upgrade</span>
              </button>
            )}

            <button
              type="button"
              className="card-action-btn table-action-btn-sm"
              style={{
                background: "rgba(170, 59, 255, 0.08)",
                color: "var(--accent)",
                border: "1px solid rgba(170, 59, 255, 0.15)",
              }}
              onClick={() => onViewLogs(container.id, container.name)}
              title="View Logs"
            >
              <Terminal size={10} />
              <span>Logs</span>
            </button>

            <button
              type="button"
              className="card-action-btn table-action-btn-sm"
              style={{
                background: "rgba(0,0,0,0.02)",
                border: "1px solid var(--border)",
                color: "var(--text)",
              }}
              onClick={() => checkContainerUpdates(container.id)}
              disabled={!isAdmin || !!loadingType}
              title="Check Updates"
            >
              {loadingType === "checking" ? (
                <RefreshCw size={12} className="spinner" />
              ) : (
                <RefreshCw size={12} />
              )}
            </button>
          </div>
        );
      },
    },
  ];

  const table = useReactTable({
    data: containers,
    columns,
    state: {
      sorting,
    },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  return (
    <div className="container-table-wrapper">
      <table className="container-table">
        <thead>
          {table.getHeaderGroups().map((headerGroup) => (
            <tr key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <th key={header.id}>
                  {header.isPlaceholder
                    ? null
                    : flexRender(header.column.columnDef.header, header.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id} className="container-table-row">
              {row.getVisibleCells().map((cell) => (
                <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>

      {containers.length === 0 && (
        <div
          className="empty-state-card"
          style={{ borderTop: "none", borderTopLeftRadius: 0, borderTopRightRadius: 0 }}
        >
          <Server size={32} className="empty-state-icon" />
          <h3>No Containers Found</h3>
          <p>No Docker containers discovered on the host system.</p>
        </div>
      )}
    </div>
  );
}
