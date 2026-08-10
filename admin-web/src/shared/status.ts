import type { Task } from "./types";

export function statusText(status: string) {
  return (
    (
      {
        active: "可用",
        disabled: "已禁用",
        invalid: "登录凭据失效",
        rate_limited: "429 临时风控",
        cooldown: "临时故障，等待重试",
        queued: "排队中",
        processing: "处理中",
        reserving: "分配账号",
        uploading: "上传中",
        submitted: "上游处理中",
        polling: "上游处理中",
        succeeded: "成功",
        failed: "失败",
        cancelled: "已取消",
        submission_uncertain: "提交状态未知",
      } as Record<string, string>
    )[status] || status
  );
}

export function tokenCost(task: Task) {
	if (task.settled_tokens != null) return task.settled_tokens.toLocaleString();
	if (task.reservation_state === "held" && task.estimated_tokens != null)
		return `${task.estimated_tokens.toLocaleString()} 预留`;
	return "—";
}

export function reservationText(state?: string, reason?: string) {
  if (!state) return "旧任务，无预留记录";
  const label =
    (
      { held: "已预留", released: "已释放", consumed: "已结算" } as Record<
        string,
        string
      >
    )[state] || state;
  return reason ? `${label} · ${reason}` : label;
}

export function formatTokens(value: number) {
  return Number.isInteger(value) ? value.toLocaleString() : value.toFixed(1);
}

export function formatOptionalTokens(value?: number) {
  return value == null ? "—" : formatTokens(value);
}

export function appendLocalDateRange(params: URLSearchParams, from: string, to: string) {
  if (from) params.set("from", new Date(`${from}T00:00:00`).toISOString());
  if (to) {
    const end = new Date(`${to}T00:00:00`);
    end.setDate(end.getDate() + 1);
    params.set("to", end.toISOString());
  }
}

export function formatCapacityWait(seconds: number) {
  if (seconds < 1) return "0 秒";
  if (seconds < 60) return `${Math.round(seconds)} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分`;
  return `${Math.floor(seconds / 3600)} 时 ${Math.floor((seconds % 3600) / 60)} 分`;
}
