import type { Account } from "../shared/types";
import { statusText } from "../shared/status";

export function accountOperationalState(account: Account) {
  if (account.generation_permission_status === "blocked") {
    return {
      className: "invalid",
      label: "生成权限失效",
      detail: account.generation_permission_error || "上游拒绝该账号的生成请求",
    };
  }
  if (account.status !== "active") {
    return {
      className: account.status,
      label: statusText(account.status),
      detail: account.last_error || undefined,
    };
  }
  if (account.provider_id === "leonardo" && account.generation_permission_status !== "verified") {
    return {
      className: "queued",
      label: "生成权限待检测",
      detail: account.generation_permission_error || "会话有效，等待生成权限探针完成",
    };
  }
  const now = Date.now();
  const expiresAt = account.access_token_expires_at
    ? new Date(account.access_token_expires_at).getTime()
    : 0;
  if (expiresAt <= now) {
    if (account.provider_id === "adobe" && !account.session_refresh_enabled) {
      return {
        className: "invalid",
        label: "AT 已过期",
        detail: "导入新的 Adobe Access Token，或导入完整 Cookie 开启自动续期",
      };
    }
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
  const permission = account.generation_permission_status || "unknown";
  const permissionLabel = permission === "verified" ? "生成权限已验证" : permission === "blocked" ? "生成权限失效" : permission === "rate_limited" ? "生成权限检测遇到 429" : permission === "error" ? "生成权限待重试" : "生成权限待检测";
  const permissionClass = permission === "verified" ? "verified" : permission === "blocked" ? "blocked" : "pending";
  return (
    <span className="account-status-cell" title={state.detail || account.generation_permission_error || undefined}>
      <span><i className={`status ${state.className}`}></i>{state.label}</span>
      <small className={`account-permission-health ${permissionClass}`}>{permissionLabel}</small>
    </span>
  );
}

export function sessionExpiryText(expiresAt?: string, tokenLabel = "JWT") {
  if (!expiresAt) return `${tokenLabel} 未获取`;
  const remaining = new Date(expiresAt).getTime() - Date.now();
  if (remaining <= 0) return `${tokenLabel} 已过期`;
  const minutes = Math.max(1, Math.ceil(remaining / 60_000));
  return minutes >= 60
    ? `${tokenLabel} ${Math.floor(minutes / 60)}时${minutes % 60}分`
    : `${tokenLabel} ${minutes} 分`;
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
