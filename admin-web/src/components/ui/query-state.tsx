import { AlertTriangle, RefreshCw } from "lucide-react";
import { cn } from "../../lib/utils";

export function QueryStatus({
  fetching,
  error,
  updatedAt,
  label,
  className,
}: {
  fetching: boolean;
  error?: unknown;
  updatedAt?: number;
  label?: string;
  className?: string;
}) {
  if (error) {
    return (
      <span className={cn("query-status query-status-error", className)} role="status">
        <AlertTriangle />数据更新失败
      </span>
    );
  }
  if (fetching) {
    return (
      <span className={cn("query-status", className)} role="status">
        <RefreshCw className="spin" />正在更新
      </span>
    );
  }
  return (
    <span className={cn("query-status", className)}>
      {label || (updatedAt ? `更新于 ${new Date(updatedAt).toLocaleTimeString("zh-CN")}` : "数据已同步")}
    </span>
  );
}

export function TableSkeleton({
  rows = 5,
  columns = 5,
  className,
}: {
  rows?: number;
  columns?: number;
  className?: string;
}) {
  return (
    <div className={cn("table-skeleton", className)} role="status" aria-label="正在加载数据">
      {Array.from({ length: rows }, (_, row) => (
        <div className="table-skeleton-row" key={row} style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}>
          {Array.from({ length: columns }, (_, column) => <i key={column} />)}
        </div>
      ))}
    </div>
  );
}
