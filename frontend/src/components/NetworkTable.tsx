import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { Network as NetworkIcon, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";

import type { Network } from "../gen/proto/dmanager/v1/admin_pb";
import { formatShortId, formatTimestamp, isDefaultNetwork } from "./adminFormat";
import { ConfirmDialog } from "./ConfirmDialog";
import { SortButton } from "./SortButton";

interface NetworkTableProps {
  networks: Network[];

  // Delete affordances are admin-gated (§9.12, #215); viewer rows render an
  // em dash in the Actions column.
  isAdmin: boolean;

  // Row currently being deleted (spinner on its button); null while idle.
  deletingId: string | null;

  onDelete: (id: string) => void | Promise<void>;
}

export function NetworkTable({ networks, isAdmin, deletingId, onDelete }: NetworkTableProps) {
  // The network awaiting confirmation in the dialog; null while closed.
  const [pendingDelete, setPendingDelete] = useState<Network | null>(null);

  // Dispatches through the deleting hook (toasts + refresh) and closes the
  // dialog once the outcome settles — the error toast is the failure path's
  // feedback, so the dialog does not linger.
  const handleConfirm = async () => {
    if (pendingDelete === null) {
      return;
    }
    const id = pendingDelete.id;
    await onDelete(id);
    setPendingDelete(null);
  };

  const columns = useMemo<ColumnDef<Network>[]>(() => {
    const actionsColumn: ColumnDef<Network> = {
      id: "actions",
      header: "Actions",
      enableSorting: false,
      cell: ({ row }) => {
        const network = row.original;
        // Only unused (zero attachments), non-predefined networks are
        // deletable; in-use, pre-defined and unknown-count (-1) rows render
        // an em dash — the daemon would refuse all of them anyway.
        if (!isAdmin || network.containersCount !== 0n || network.predefined) {
          return <span className="table-cell-date">—</span>;
        }
        const deleting = deletingId === network.id;
        return (
          <button
            type="button"
            className="card-action-btn"
            disabled={!isAdmin || deletingId !== null}
            onClick={() => setPendingDelete(network)}
            title="Delete network"
          >
            <Trash2 size={16} className={deleting ? "spinner" : undefined} />
          </button>
        );
      },
    };
    return [...staticColumns, actionsColumn];
  }, [isAdmin, deletingId]);

  // Name asc — a network inventory is scanned by name, not by size.
  const [sorting, setSorting] = useState<SortingState>([{ id: "name", desc: false }]);

  const table = useReactTable({
    data: networks,
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

      {networks.length === 0 && (
        <div
          className="empty-state-card"
          style={{ borderTop: "none", borderTopLeftRadius: 0, borderTopRightRadius: 0 }}
        >
          <NetworkIcon size={32} className="empty-state-icon" />
          <h3>No Networks Found</h3>
          <p>No Docker networks present on the host system.</p>
        </div>
      )}

      {pendingDelete !== null && (
        <ConfirmDialog
          open
          onClose={() => setPendingDelete(null)}
          onConfirm={handleConfirm}
          title="Delete network?"
          message={`Network ${pendingDelete.name} (${pendingDelete.driver}) will be permanently removed from the host. This cannot be undone.`}
          confirmLabel="Delete"
          variant="danger"
          busy={deletingId === pendingDelete.id}
        />
      )}
    </div>
  );
}

const staticColumns: ColumnDef<Network>[] = [
  {
    id: "name",
    accessorKey: "name",
    header: ({ column }) => <SortButton column={column} label="Network" />,
    cell: ({ row }) => {
      const network = row.original;
      return (
        <div style={{ display: "flex", alignItems: "center", gap: "8px", minWidth: "120px" }}>
          <span className="container-name-text" title={network.name}>
            {network.name}
          </span>
          {isDefaultNetwork(network.name) && (
            <span
              title="Default Docker network"
              style={{
                fontSize: "10px",
                fontWeight: "600",
                letterSpacing: "0.04em",
                textTransform: "uppercase",
                color: "var(--text)",
                opacity: 0.55,
                padding: "2px 8px",
                borderRadius: "9999px",
                border: "1px solid var(--border)",
                whiteSpace: "nowrap",
              }}
            >
              system
            </span>
          )}
        </div>
      );
    },
  },
  {
    id: "id",
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => (
      <code className="table-cell-id" title={row.original.id}>
        {formatShortId(row.original.id)}
      </code>
    ),
  },
  {
    id: "driver",
    accessorKey: "driver",
    header: ({ column }) => <SortButton column={column} label="Driver" />,
    cell: ({ row }) => <span className="table-cell-date">{row.original.driver}</span>,
  },
  {
    id: "scope",
    accessorKey: "scope",
    header: ({ column }) => <SortButton column={column} label="Scope" />,
    cell: ({ row }) => <span className="table-cell-date">{row.original.scope}</span>,
  },
  {
    id: "internal",
    accessorKey: "internal",
    header: ({ column }) => <SortButton column={column} label="Internal" />,
    cell: ({ row }) => (
      <span className={`status-badge ${row.original.internal ? "running" : "stopped"}`}>
        {row.original.internal ? "Yes" : "No"}
      </span>
    ),
  },
  {
    // Attachment count from the per-network inspect enrichment (§9.12, #215).
    // A stopped container still counts — its endpoint persists until removal.
    // -1 = inspect failure, rendered unknown.
    id: "inUse",
    accessorKey: "containersCount",
    header: ({ column }) => <SortButton column={column} label="In Use" />,
    cell: ({ row }) => {
      const count = row.original.containersCount;
      if (count < 0n) {
        return <span className="table-cell-date">—</span>;
      }
      return (
        <span
          className={`status-badge ${count > 0n ? "running" : "stopped"}`}
          title={count === 1n ? "1 attached container" : `${count} attached containers`}
        >
          {count > 0n ? "Yes" : "No"}
        </span>
      );
    },
  },
  {
    id: "createdAt",
    accessorKey: "createdAt",
    header: ({ column }) => <SortButton column={column} label="Created" />,
    cell: ({ row }) => (
      <span className="table-cell-date" title={String(row.original.createdAt?.seconds ?? 0n)}>
        {formatTimestamp(row.original.createdAt)}
      </span>
    ),
  },
];
