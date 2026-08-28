import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { Image as ImageIcon, Loader2, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import type { Image } from "../gen/proto/dmanager/v1/admin_pb";
import { formatBytes, formatRelativeUnix, formatShortId, splitRepoTag } from "./adminFormat";
import { SortButton } from "./SortButton";

interface ImageTableProps {
  images: Image[];
  isAdmin: boolean;
  deletingId: string | null;
  onDelete: (id: string) => void;
}
// bigint columns are exposed to the sorter as numbers; double precision is
// ample for image sizes and Unix seconds.
const staticColumns: ColumnDef<Image>[] = [
  {
    id: "repository",
    accessorFn: (row) => splitRepoTag(row.repoTags[0]).repository,
    header: ({ column }) => <SortButton column={column} label="Repository" />,
    cell: ({ row }) => {
      const { repository } = splitRepoTag(row.original.repoTags[0]);
      return (
        <span className="container-name-text" title={repository}>
          {repository}
        </span>
      );
    },
  },
  {
    id: "tag",
    accessorFn: (row) => splitRepoTag(row.repoTags[0]).tag,
    header: ({ column }) => <SortButton column={column} label="Tag" />,
    cell: ({ row }) => {
      const { tag } = splitRepoTag(row.original.repoTags[0]);
      return (
        <code className="table-cell-id" title={tag}>
          {tag}
        </code>
      );
    },
  },
  {
    id: "id",
    accessorKey: "id",
    header: "Image ID",
    cell: ({ row }) => (
      <code className="table-cell-id" title={row.original.id}>
        {formatShortId(row.original.id)}
      </code>
    ),
  },
  {
    id: "size",
    accessorFn: (row) => Number(row.sizeBytes),
    header: ({ column }) => <SortButton column={column} label="Size" />,
    cell: ({ row }) => (
      <span className="table-cell-date">{formatBytes(row.original.sizeBytes)}</span>
    ),
  },
  {
    id: "created",
    accessorFn: (row) => Number(row.createdUnix),
    header: ({ column }) => <SortButton column={column} label="Created" />,
    cell: ({ row }) => (
      <span
        className="table-cell-date"
        title={new Date(Number(row.original.createdUnix) * 1000).toLocaleString()}
      >
        {formatRelativeUnix(row.original.createdUnix)}
      </span>
    ),
  },
  {
    id: "inUse",
    accessorFn: (row) => Number(row.containersCount),
    header: ({ column }) => <SortButton column={column} label="In Use" />,
    cell: ({ row }) => {
      const count = row.original.containersCount;
      if (count < 0n) {
        return <span className="table-cell-date">—</span>;
      }
      return <span className="table-cell-date">{count.toString()}</span>;
    },
  },
];

// Image inventory table with a size-first default sort and an admin-gated
// Actions column (design.md §9.7): unused images can be deleted via a
// two-step inline confirm; in-use and unknown-usage rows are inert.
export function ImageTable({ images, isAdmin, deletingId, onDelete }: ImageTableProps) {
  // Arming state for the two-step inline confirm; resets after 5 seconds.
  const [armedId, setArmedId] = useState<string | null>(null);
  const armTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearArmTimer = () => {
    if (armTimerRef.current !== null) {
      clearTimeout(armTimerRef.current);
      armTimerRef.current = null;
    }
  };

  const armRow = (id: string) => {
    setArmedId(id);
    clearArmTimer();
    armTimerRef.current = setTimeout(() => setArmedId(null), 5000);
  };

  // Second click dispatches. The row stays armed while the deletion is in
  // flight so the confirm button can show its spinner; the 5-second timer
  // from armRow still disarms it afterwards (and a successful delete makes
  // the row disappear entirely via refresh).
  const confirmDelete = (id: string) => {
    onDelete(id);
  };

  useEffect(() => clearArmTimer, []);

  const columns = useMemo<ColumnDef<Image>[]>(() => {
    const actionsColumn: ColumnDef<Image> = {
      id: "actions",
      header: "Actions",
      enableSorting: false,
      cell: ({ row }) => {
        const image = row.original;
        // Only images with a calculated usage count of zero are deletable;
        // in-use (>0) and unknown (-1) rows render an em dash.
        if (image.containersCount !== 0n) {
          return <span className="table-cell-date">—</span>;
        }
        const deleting = deletingId === image.id;
        if (armedId === image.id) {
          return (
            <button
              type="button"
              className="card-action-btn stop"
              disabled={!isAdmin || deleting}
              onClick={() => confirmDelete(image.id)}
              title="Confirm delete"
            >
              {deleting ? <Loader2 size={16} className="animate-spin" /> : <Trash2 size={16} />}
              <span style={{ marginLeft: "6px" }}>Confirm delete</span>
            </button>
          );
        }
        return (
          <button
            type="button"
            className="card-action-btn"
            disabled={!isAdmin || deletingId !== null}
            onClick={() => armRow(image.id)}
            title={isAdmin ? "Delete image" : "Admin required"}
          >
            <Trash2 size={16} />
          </button>
        );
      },
    };
    return [...staticColumns, actionsColumn];
  }, [isAdmin, deletingId, armedId]);

  // Largest images first — disk-usage triage is the primary workflow.
  const [sorting, setSorting] = useState<SortingState>([{ id: "size", desc: true }]);

  const table = useReactTable({
    data: images,
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

      {images.length === 0 && (
        <div
          className="empty-state-card"
          style={{ borderTop: "none", borderTopLeftRadius: 0, borderTopRightRadius: 0 }}
        >
          <ImageIcon size={32} className="empty-state-icon" />
          <h3>No Images Found</h3>
          <p>No Docker images present on the host system.</p>
        </div>
      )}
    </div>
  );
}
