import { Eye } from "lucide-react";
import { TableSkeleton } from "./ui";
import type { Task } from "../shared/types";
import { statusText, tokenCost } from "../shared/status";

function hasContentWarning(task: Task) {
  return Boolean(task.result?.nsfw || task.result?.data?.some((output) => output.nsfw));
}

function resultSummary(task: Task) {
  if (task.status === "failed") return task.error_message || "任务执行失败";
  if (task.status === "succeeded") {
    const count = task.result?.data?.length || 0;
    const warning = hasContentWarning(task) ? " · 显式内容警告" : "";
    return count ? `已生成 ${count} 个结果${warning}` : `生成完成${warning}`;
  }
  if (task.status === "cancelled") return "任务已取消，未生成内容";
  if (task.status === "submission_uncertain") {
    return task.error_message || "上游提交状态未知";
  }
  if (task.status === "queued" && task.queue_position) {
    return `排队中 · 第 ${task.queue_position} 位`;
  }
  return statusText(task.status);
}

export function OverviewTaskTable({
  data,
  loading = false,
  onOpenTask,
}: {
  data: Task[];
  loading?: boolean;
  onOpenTask?: (task: Task) => void;
}) {
  return (
    <div className="task-table">
      <div className="task-line task-head">
        <span>任务</span>
        <span>模型 / 类型</span>
        <span>结算 / 预留</span>
        <span>状态</span>
        <span>结果说明</span>
        <span></span>
      </div>
      {loading && data.length === 0 && <TableSkeleton rows={4} columns={6} />}
      {data.map((task) => (
        <div
          className={`task-line${onOpenTask ? " interactive-row" : ""}`}
          key={task.id}
          role={onOpenTask ? "button" : undefined}
          tabIndex={onOpenTask ? 0 : undefined}
          onClick={onOpenTask ? () => onOpenTask(task) : undefined}
          onKeyDown={onOpenTask ? (event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              onOpenTask(task);
            }
          } : undefined}
        >
          <span>
            <strong className="task-prompt" title={task.prompt || "未命名任务"}>
              {task.prompt || "未命名任务"}
            </strong>
            <small>{new Date(task.created_at).toLocaleString("zh-CN")}</small>
          </span>
          <span>
            <b className="kind-badge">
              {task.kind === "video" ? "VIDEO" : task.kind === "audio" ? "AUDIO" : "IMAGE"}
            </b>
            <small>{task.model}</small>
          </span>
          <span>{tokenCost(task)}</span>
          <span>
            <i className={`status ${task.status}`}></i>
            {statusText(task.status)}
            {task.status === "queued" && task.queue_position && (
              <small>第 {task.queue_position} 位</small>
            )}
          </span>
          <span className={`task-summary ${task.status === "failed" ? "failed" : ""}`}>
            <strong>{resultSummary(task)}</strong>
            {task.error_details?.provider_error_code && (
              <small>{task.error_details.provider_error_code}</small>
            )}
          </span>
          <span aria-hidden="true">
            <Eye className="overview-task-eye" />
          </span>
        </div>
      ))}
    </div>
  );
}
