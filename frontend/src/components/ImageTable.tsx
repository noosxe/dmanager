import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { Image as ImageIcon } from "lucide-react";
import { useState } from "react";

import type { Image } from "../gen/proto/dmanager/v1/admin_pb";
import { formatBytes, formatRelativeUnix, formatShortId, splitRepoTag } from "./adminFormat";
import { SortButton } from "./SortButton";

interface ImageTableProps {
  images: Image[];
}

// bigint columns are exposed to the sorter as numbers; double precision is
// ample for image sizes and Unix seconds.
const columns: ColumnDef<Image>[] = [
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

// Read-only image inventory table. No actions column by design.
export function ImageTable({ images }: ImageTableProps) {
  const [sorting, setSorting] = useState<SortingState>([{ id: "repository", desc: false }]);

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
