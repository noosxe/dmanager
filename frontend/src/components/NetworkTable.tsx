import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { Network as NetworkIcon } from "lucide-react";
import { useState } from "react";

import type { Network } from "../gen/proto/dmanager/v1/admin_pb";
import { formatShortId, formatTimestamp, isDefaultNetwork } from "./adminFormat";
import { SortButton } from "./SortButton";

interface NetworkTableProps {
  networks: Network[];
}

const columns: ColumnDef<Network>[] = [
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

// Read-only network inventory table. No actions column by design.
export function NetworkTable({ networks }: NetworkTableProps) {
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
    </div>
  );
}
