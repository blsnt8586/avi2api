import type { Account } from "../shared/types";
import { statusText } from "../shared/status";

export function accountOperationalState(account: Account) {
  if (account.status !== "active") {
    return {
      className: account.status,
      label: statusText(account.status),
      detail: account.last_error || undefined,
    };
  }
  const now = Date.now();
  const expiresAt = account.access_token_expires_at
    ? new Date(account.access_token_expires_at).getTime()
    : 0;
  if (expiresAt <= now) {
    return {
      className: "cooldown",
      label:
        account.session_refresh_job_status === "leased"
          ? "会话刷新中"
          : "会话等待恢复",
      detail: sessionRefreshText(account),
    };
  }
  return { className: "active", label: statusText("active"), detail: undefined };
}

export function AccountStatusCell({ account }: { account: Account }) {
  const state = accountOperationalState(account);
  return (
    <span className="account-status-cell" title={state.detail}>
      <i className={`status ${state.className}`}></i>
      {state.label}
    </span>
  );
}

export function sessionExpiryText(expiresAt?: string) {
  if (!expiresAt) return "JWT 未获取";
  const remaining = new Date(expiresAt).getTime() - Date.now();
  if (remaining <= 0) return "JWT 已过期";
  const minutes = Math.max(1, Math.ceil(remaining / 60_000));
  return minutes >= 60
    ? `JWT ${Math.floor(minutes / 60)}时${minutes % 60}分`
    : `JWT ${minutes} 分`;
}

export function sessionRefreshText(account: Account) {
  if (account.session_refresh_job_status) {
    const stage = account.session_refresh_job_stage === "browser" ? "浏览器" : "Cookie";
    const status = account.session_refresh_job_status === "leased" ? "刷新中" : "等待刷新";
    return `${stage} · ${status}`;
  }
  if (account.session_refresh_last_method) {
    const method = account.session_refresh_last_method === "browser" ? "浏览器" : "Cookie";
    return `${method} · 最近成功`;
  }
  return account.session_refresh_enabled ? "等待首次调度" : "自动刷新关闭";
}
