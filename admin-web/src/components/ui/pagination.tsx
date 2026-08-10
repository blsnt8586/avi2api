import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "./button";
import { Select } from "./forms";
import { cn } from "../../lib/utils";

export type PaginationProps = {
  page: number;
  totalPages: number;
  total: number;
  pageSize: number;
  sizes?: number[];
  busy?: boolean;
  className?: string;
  onPage: (page: number) => void;
  onPageSize: (size: number) => void;
};

export function Pagination({
  page,
  totalPages,
  total,
  pageSize,
  sizes = [10, 20, 40],
  busy = false,
  className,
  onPage,
  onPageSize,
}: PaginationProps) {
  const currentPage = Math.min(page, totalPages);

  return (
    <div className={cn("flex min-h-14 flex-wrap items-center justify-between gap-3 border-t border-[var(--border)] px-4 py-2 text-xs text-[var(--text-muted)]", className)}>
      <span>
        共 {total} 条 · 第 {currentPage} / {totalPages} 页
      </span>
      <div className="flex items-center gap-2">
        <label className="flex items-center gap-2">
          每页
          <Select
            aria-label="每页数量"
            className="min-h-8 w-20 py-1"
            value={pageSize}
            onChange={(event) => onPageSize(Number(event.target.value))}
          >
            {sizes.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </Select>
        </label>
        <Button
          variant="secondary"
          size="sm"
          disabled={busy || page <= 1}
          onClick={() => onPage(page - 1)}
        >
          <ChevronLeft size={15} />
          上一页
        </Button>
        <Button
          variant="secondary"
          size="sm"
          disabled={busy || page >= totalPages}
          onClick={() => onPage(page + 1)}
        >
          下一页
          <ChevronRight size={15} />
        </Button>
      </div>
    </div>
  );
}
