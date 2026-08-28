import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { HardDrive } from "lucide-react";
import { useState } from "react";

import type { Volume } from "../gen/proto/dmanager/v1/admin_pb";
import { formatTimestamp } from "./adminFormat";
import { SortButton } from "./SortButton";

interface VolumeTableProps {
  volumes: Volume[];
}

const columns: ColumnDef<Volume>[] = [
  {
    id: "name",
    accessorKey: "name",
    header: ({ column }) => <SortButton column={column} label="Volume" />,
    cell: ({ row }) => (
      <span className="container-name-text" title={row.original.name}>
        {row.original.name}
      </span>
    ),
  },
  {
    id: "driver",
    accessorKey: "driver",
    header: ({ column }) => <SortButton column={column} label="Driver" />,
    cell: ({ row }) => <span className="table-cell-date">{row.original.driver}</span>,
  },
  {
    id: "mountpoint",
    accessorKey: "mountpoint",
    header: ({ column }) => <SortButton column={column} label="Mountpoint" />,
    cell: ({ row }) => (
      <code className="table-cell-id" title={row.original.mountpoint}>
        {row.original.mountpoint}
      </code>
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
  {
    id: "labels",
    accessorFn: (row) => Object.keys(row.labels).join(","),
    header: "Labels",
    enableSorting: false,
    cell: ({ row }) => {
      const entries = Object.entries(row.original.labels);
      if (entries.length === 0) return <span className="table-cell-date">—</span>;
      return (
        <div style={{ display: "flex", gap: "6px", flexWrap: "wrap" }}>
          {entries.map(([key, value]) => (
            <code
              key={key}
              title={`${key}=${value}`}
              style={{
                fontSize: "11px",
                padding: "2px 6px",
                borderRadius: "4px",
                background: "rgba(255,255,255,0.06)",
                fontFamily: "var(--font-mono, monospace)",
                maxWidth: "220px",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                display: "inline-block",
              }}
            >
              {key}={value}
            </code>
          ))}
        </div>
      );
    },
  },
];

// Read-only volume inventory table. No actions column by design.
export function VolumeTable({ volumes }: VolumeTableProps) {
  const [sorting, setSorting] = useState<SortingState>([{ id: "name", desc: false }]);

  const table = useReactTable({
    data: volumes,
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

      {volumes.length === 0 && (
        <div
          className="empty-state-card"
          style={{ borderTop: "none", borderTopLeftRadius: 0, borderTopRightRadius: 0 }}
        >
          <HardDrive size={32} className="empty-state-icon" />
          <h3>No Volumes Found</h3>
          <p>No Docker volumes present on the host system.</p>
        </div>
      )}
    </div>
  );
}
