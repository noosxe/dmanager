import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { Image as ImageIcon, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";

import type { Image } from "../gen/proto/dmanager/v1/admin_pb";
import { formatBytes, formatRelativeUnix, formatShortId, splitRepoTag } from "./adminFormat";
import { ConfirmDialog } from "./ConfirmDialog";
import { SortButton } from "./SortButton";

interface ImageTableProps {
  images: Image[];
  isAdmin: boolean;
  deletingId: string | null;
  onDelete: (id: string) => void | Promise<void>;
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
// Actions column (design.md §9.7): unused images can be deleted after a
// ConfirmDialog (danger variant); in-use and unknown-usage rows are inert.
// Human-readable repo:tag for the confirm dialog's consequence message.
const repoTagLabel = (image: Image): string => {
  const { repository, tag } = splitRepoTag(image.repoTags[0]);
  return `${repository}:${tag}`;
};

export function ImageTable({ images, isAdmin, deletingId, onDelete }: ImageTableProps) {
  // The image awaiting confirmation in the dialog; null while closed.
  const [pendingDelete, setPendingDelete] = useState<Image | null>(null);

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
        return (
          <button
            type="button"
            className="card-action-btn"
            disabled={!isAdmin || deletingId !== null}
            onClick={() => setPendingDelete(image)}
            title={isAdmin ? "Delete image" : "Admin required"}
          >
            <Trash2 size={16} className={deleting ? "spinner" : undefined} />
          </button>
        );
      },
    };
    return [...staticColumns, actionsColumn];
  }, [isAdmin, deletingId]);

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

      {pendingDelete !== null && (
        <ConfirmDialog
          open
          onClose={() => setPendingDelete(null)}
          onConfirm={handleConfirm}
          title="Delete image?"
          message={`Image ${repoTagLabel(pendingDelete)} (${formatShortId(pendingDelete.id)}) will be permanently removed from the host. This cannot be undone.`}
          confirmLabel="Delete"
          variant="danger"
          busy={deletingId === pendingDelete.id}
        />
      )}
    </div>
  );
}
