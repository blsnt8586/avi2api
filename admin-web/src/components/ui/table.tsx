import * as React from "react";
import { flexRender, getCoreRowModel, useReactTable, ColumnDef } from "@tanstack/react-table";
import { cn } from "../../lib/utils";

export function TableWrap(props: React.HTMLAttributes<HTMLDivElement>) {
  return <div {...props} className={cn("w-full overflow-x-auto rounded-[var(--radius)] border border-[var(--border)] bg-[var(--surface)]", props.className)} />;
}

export function DataTable<T>({
  data,
  columns,
  rowKey,
  empty = "No data",
  className,
  onRowClick,
  rowLabel,
  loading = false,
}: {
  data: T[];
  columns: ColumnDef<T>[];
  rowKey?: (row: T) => string;
  empty?: React.ReactNode;
  className?: string;
  onRowClick?: (row: T) => void;
  rowLabel?: (row: T) => string;
  loading?: boolean;
}) {
  const table = useReactTable({ data, columns, getCoreRowModel: getCoreRowModel() });
  return (
    <TableWrap className={className}>
      <table className="w-full min-w-[760px] border-collapse text-left text-sm">
        <thead className="bg-[var(--surface-raised)] text-[10px] uppercase text-[var(--text-muted)]">
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id}>
              {group.headers.map((header) => <th key={header.id} className="border-b border-[var(--border)] px-4 py-3 font-bold">{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</th>)}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr
              key={rowKey ? rowKey(row.original) : row.id}
              className={cn("border-b border-[var(--border)] last:border-0 hover:bg-[var(--surface-hover)]", onRowClick && "cursor-pointer focus-within:bg-[var(--surface-hover)]")}
              onClick={onRowClick ? () => onRowClick(row.original) : undefined}
              tabIndex={onRowClick ? 0 : undefined}
              aria-label={rowLabel?.(row.original)}
              onKeyDown={onRowClick ? (event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  onRowClick(row.original);
                }
              } : undefined}
            >
              {row.getVisibleCells().map((cell) => <td key={cell.id} className="px-4 py-3 align-middle text-[var(--text-soft)]">{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}
            </tr>
          ))}
        </tbody>
      </table>
      {loading && data.length === 0 && (
        <div className="table-skeleton compact" role="status" aria-label="正在加载数据">
          {Array.from({ length: 5 }, (_, row) => (
            <div className="table-skeleton-row" key={row}>
              {Array.from({ length: Math.max(1, columns.length) }, (_, column) => <i key={column} />)}
            </div>
          ))}
        </div>
      )}
      {!loading && data.length === 0 && <div className="grid min-h-28 place-items-center px-4 text-sm text-[var(--text-muted)]">{empty}</div>}
    </TableWrap>
  );
}
