import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Ban,
  Check,
  CirclePlay,
  Coins,
  ExternalLink,
  Gauge,
  Minus,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
  X,
} from "lucide-react";
import {
  Badge,
  Button,
  Pagination as UIPagination,
  QueryStatus,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  TableSkeleton,
} from "../components/ui";
import type { Account, AccountsPage, OverviewResponse, Provider } from "../shared/types";
import {
  AccountStatusCell,
  accountOperationalState,
  sessionExpiryText,
  sessionRefreshText,
} from "../components/account-status";
import { Metric } from "../components/metric";
import { api } from "../shared/api";

export function Accounts({
  edit,
}: {
  edit: (account: Account) => void;
}) {
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [searchParams, setSearchParams] = useSearchParams();
  const search = searchParams.get("search") || "";
  const deferredSearch = React.useDeferredValue(search.trim());
  const statusFilter = searchParams.get("status") || "all";
  const roleFilter = searchParams.get("role") || "all";
  const providerFilter = searchParams.get("provider") || "all";
  const setAccountFilter = (key: string, value: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (!value || value === "all") next.delete(key);
      else next.set(key, value);
      return next;
    }, { replace: true });
    setPage(1);
  };
  const accounts = useQuery({
    queryKey: ["accounts-page", page, pageSize, deferredSearch, statusFilter, roleFilter, providerFilter],
    queryFn: () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
      if (deferredSearch) params.set("search", deferredSearch);
      if (statusFilter !== "all") params.set("status", statusFilter);
      if (roleFilter !== "all") params.set("role", roleFilter);
      if (providerFilter !== "all") params.set("provider", providerFilter);
      return api<AccountsPage>(`/admin/api/accounts?${params}`);
    },
  });
  const overview = useQuery({
    queryKey: ["overview"],
    queryFn: () => api<OverviewResponse>("/admin/api/overview"),
  });
  const refresh = () => {
    client.invalidateQueries({ queryKey: ["accounts-page"] });
    client.invalidateQueries({ queryKey: ["overview"] });
  };
  const data = accounts.data?.data || [];
  const providerOptions = [...new Set(data.map((account) => account.provider_id))].sort();
  const filteredData = data;
  const total = accounts.data?.total || 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  React.useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);
  const m = useMutation({
    mutationFn: (id: string) =>
      api(`/admin/api/accounts/${id}/session-refresh`, { method: "POST" }),
    onSuccess: refresh,
  });
  const status = useMutation({
    mutationFn: (a: Account) =>
      api(`/admin/api/accounts/${a.id}/status`, {
        method: "PATCH",
        body: JSON.stringify({
          status: a.status === "disabled" ? "active" : "disabled",
        }),
      }),
    onSuccess: refresh,
  });
  return (
    <section>
      <div className="stats">
        <Metric icon={<Server />} label="账号数量" value={overview.data?.accounts || 0} />
        <Metric
          icon={<Coins />}
          label="净可用积分"
          value={(overview.data?.available_tokens || 0).toLocaleString()}
        />
        <Metric
          icon={<Coins />}
          label="已预留积分"
          value={(overview.data?.reserved_tokens || 0).toLocaleString()}
        />
        <Metric
          icon={<Activity />}
          label="可用账号"
          value={overview.data?.active_accounts || 0}
        />
      </div>
      <div className="video-inventory-strip" aria-label="Seedance 2.0 视频库存">
        <span><strong>{overview.data?.video_ready_720p_15s || 0}</strong><small>720p · 15秒可用账号</small></span>
        <span><strong>{overview.data?.video_ready_1080p_8s || 0}</strong><small>1080p · 8秒可用账号</small></span>
        <span><strong>{overview.data?.video_ready_1080p_10s || 0}</strong><small>1080p · 10秒可用账号</small></span>
        <span><strong>{(overview.data?.video_protected_tokens || 0).toLocaleString()}</strong><small>视频保护积分</small></span>
      </div>
      <div className="accounts-toolbar">
        <label className="search-box">
          <Search />
          <input
            value={search}
            placeholder="搜索名称、邮箱或渠道"
            onChange={(event) => {
              setAccountFilter("search", event.target.value);
            }}
          />
        </label>
        <div className="account-filter-group">
          <label>状态
            <select value={statusFilter} onChange={(event) => setAccountFilter("status", event.target.value)}>
              <option value="all">全部</option>
              <option value="active">可用</option>
              <option value="attention">需要关注</option>
              <option value="rate_limited">429 风控</option>
              <option value="cooldown">临时故障</option>
              <option value="disabled">已禁用</option>
            </select>
          </label>
          <label>用途
            <select value={roleFilter} onChange={(event) => setAccountFilter("role", event.target.value)}>
              <option value="all">全部</option>
              <option value="general">通用</option>
              <option value="video_reserved">视频保留</option>
            </select>
          </label>
          <label>渠道
            <select value={providerFilter} onChange={(event) => setAccountFilter("provider", event.target.value)}>
              <option value="all">全部</option>
              {providerOptions.map((provider) => <option value={provider} key={provider}>{provider}</option>)}
            </select>
          </label>
        </div>
        <QueryStatus
          fetching={accounts.isFetching}
          error={accounts.error}
          updatedAt={accounts.dataUpdatedAt}
          label={`本页 ${filteredData.length} / 共 ${total}`}
        />
      </div>
      <div className="table account-table">
        <div className="account-row account-head">
          <span>账号</span>
          <span>渠道 / 邮箱</span>
          <span>套餐</span>
          <span>净余额</span>
          <span>预留</span>
          <span>执行</span>
          <span>排队</span>
          <span>状态</span>
          <span>会话</span>
          <span>操作</span>
        </div>
        {accounts.isLoading && <TableSkeleton rows={6} columns={10} />}
        {filteredData.map((a) => (
          <div className="account-row" key={a.id}>
            <span className="account-name-cell">
              <strong>{a.name}</strong>
              <small>{a.routing_role === "video_reserved" ? `视频保留 · ${a.protected_tokens.toLocaleString()}` : `通用 · ${a.id.slice(0, 8)}`}</small>
            </span>
            <span className="account-provider-cell">
              <strong>{a.provider_id}</strong>
              <small title={a.email || a.id}>{a.email || "未填写邮箱"}</small>
            </span>
            <span className="account-plan-cell">{a.plan || "未知"}</span>
            <span className="account-balance-cell">
              <strong>{a.available_tokens.toLocaleString()}</strong>
              <small>
                账面{" "}
                {(
                  a.subscription_tokens +
                  a.rollover_tokens +
                  a.paid_tokens
                ).toLocaleString()}
              </small>
            </span>
            <span className="account-number-cell">
              <strong>{a.reserved_tokens.toLocaleString()}</strong>
              <small>积分</small>
            </span>
            <span className="account-number-cell">
              <strong>{a.active_reservations}/{a.image_concurrency}</strong>
              <small>生成槽位</small>
            </span>
            <span className="account-number-cell">
              <strong>{a.queued_tasks}/{a.queue_capacity}</strong>
              <small>等待任务</small>
            </span>
            <AccountStatusCell account={a} />
            <span
              className="account-updated-cell"
              title={a.last_checked_at ? `余额更新 ${new Date(a.last_checked_at).toLocaleString("zh-CN")}` : undefined}
            >
              <strong>{sessionExpiryText(a.access_token_expires_at)}</strong>
              <small>{a.has_login_credentials ? "自动登录已配置" : sessionRefreshText(a)}</small>
            </span>
            <span className="account-actions-cell">
              <button
                className="icon"
                title="修改账号配置"
                aria-label={`修改 ${a.name} 配置`}
                onClick={() => edit(a)}
              >
                <Pencil size={17} />
              </button>
              <button
                className="icon"
                title="刷新会话"
                aria-label={`刷新 ${a.name} 会话`}
                onClick={() => m.mutate(a.id)}
              >
                <RefreshCw className={m.isPending ? "spin" : ""} size={17} />
              </button>
              <button
                className="icon"
                title={a.status === "disabled" ? "启用账号" : "禁用账号"}
                aria-label={`${a.status === "disabled" ? "启用" : "禁用"} ${a.name}`}
                onClick={() => status.mutate(a)}
              >
                {a.status === "disabled" ? (
                  <CirclePlay size={17} />
                ) : (
                  <Ban size={17} />
                )}
              </button>
            </span>
          </div>
        ))}
      </div>
      <div className="account-mobile-list">
        {accounts.isLoading && <TableSkeleton rows={4} columns={1} />}
        {filteredData.map((account) => {
          const operational = accountOperationalState(account);
          return (
            <article className="account-mobile-card" key={account.id}>
              <header>
                <span><strong>{account.name}</strong><small>{account.provider_id} · {account.email || "未填写邮箱"}</small></span>
                <Badge tone={operational.className === "active" ? "success" : operational.className === "rate_limited" ? "warning" : "danger"}>{operational.label}</Badge>
              </header>
              <div>
                <span><small>净余额</small><strong>{account.available_tokens.toLocaleString()}</strong></span>
                <span><small>执行</small><strong>{account.active_reservations}/{account.image_concurrency}</strong></span>
                <span><small>排队</small><strong>{account.queued_tasks}/{account.queue_capacity}</strong></span>
              </div>
              <footer>
                <small>{sessionExpiryText(account.access_token_expires_at)} · {account.has_login_credentials ? "自动登录" : sessionRefreshText(account)}</small>
                <Button variant="secondary" size="sm" onClick={() => edit(account)}><Pencil size={15} />详情</Button>
              </footer>
            </article>
          );
        })}
      </div>
      {!accounts.isLoading && filteredData.length === 0 && (
        <div className="empty-state">
          <strong>{search ? "没有匹配账号" : "还没有账号"}</strong>
          <span>{search ? "调整搜索内容后重试。" : "点击右上角添加第一个渠道账号。"}</span>
        </div>
      )}
      <UIPagination
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        busy={accounts.isFetching}
        onPage={setPage}
        onPageSize={(size) => {
          setPageSize(size);
          setPage(1);
        }}
      />
    </section>
  );
}

export function AccountDialog({
  account,
  close,
  done,
}: {
  account?: Account;
  close: () => void;
  done: () => void;
}) {
  const isEditing = Boolean(account);
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => api<Provider[]>("/admin/api/providers"),
  });
  const [v, setV] = useState(() => ({
    provider_id: account?.provider_id || "leonardo",
    name: account?.name || "",
    email: account?.email || "",
    password: "",
    cookie: "",
    proxy_url: account?.proxy_url || "",
    image_concurrency: account?.image_concurrency || 5,
    queue_capacity: account?.queue_capacity || 40,
    routing_role: account?.routing_role || "general",
    protected_tokens: account?.protected_tokens || 0,
    video_reserved_slots: account?.video_reserved_slots || 0,
    browser_worker_group: account?.browser_worker_group || "default",
  }));
  const selected = providers.data?.find(
    (provider) => provider.id === v.provider_id,
  );
  const authLabel = (authType: string) => {
    if (authType === "browser_session") return "浏览器会话";
    if (authType.includes("cookie")) return "Cookie 会话";
    if (authType === "api_key") return "API Key";
    if (authType === "oauth") return "OAuth";
    return "独立凭据";
  };
  const creditLabel = (creditUnit: string) =>
    creditUnit === "credits" || creditUnit === "tokens"
      ? "平台积分"
      : creditUnit || "独立积分";
  const m = useMutation({
    mutationFn: () =>
      api(isEditing ? `/admin/api/accounts/${account!.id}` : "/admin/api/accounts", {
        method: isEditing ? "PATCH" : "POST",
        body: JSON.stringify(
          isEditing
            ? {
                name: v.name.trim(),
                email: v.email.trim(),
                ...(v.password ? { password: v.password } : {}),
                proxy_url: v.proxy_url.trim(),
                image_concurrency: v.image_concurrency,
                queue_capacity: v.queue_capacity,
                routing_role: v.routing_role,
                protected_tokens: v.protected_tokens,
                video_reserved_slots: v.video_reserved_slots,
                browser_worker_group: v.browser_worker_group.trim(),
              }
            : {
                ...v,
                name: v.name.trim(),
                email: v.email.trim(),
                cookie: v.cookie.trim(),
                proxy_url: v.proxy_url.trim(),
                browser_worker_group: v.browser_worker_group.trim(),
              },
        ),
      }),
    onSuccess: done,
  });
  const canSubmit = Boolean(
    (isEditing || selected) &&
      v.name.trim() &&
      (isEditing || v.cookie.trim()) &&
      (!v.password || v.email.trim()) &&
      /^[A-Za-z0-9._-]{1,100}$/.test(v.browser_worker_group.trim()) &&
      v.image_concurrency >= 1 &&
      v.image_concurrency <= 5 &&
      v.queue_capacity >= 1 &&
      v.queue_capacity <= 1000 &&
      v.protected_tokens >= 0 &&
      v.video_reserved_slots >= 0 &&
      v.video_reserved_slots <= v.image_concurrency,
  );
  const setConcurrency = (next: number) => {
    const concurrency = Math.min(5, Math.max(1, next));
    setV({
      ...v,
      image_concurrency: concurrency,
      video_reserved_slots: Math.min(v.video_reserved_slots, concurrency),
    });
  };
  const setQueueCapacity = (next: number) =>
    setV({
      ...v,
      queue_capacity: Math.min(1000, Math.max(1, next)),
    });
  const setRoutingRole = (role: "general" | "video_reserved") =>
    setV({
      ...v,
      routing_role: role,
      protected_tokens: role === "video_reserved" ? Math.max(v.protected_tokens, 6804) : 0,
      video_reserved_slots: role === "video_reserved" ? Math.max(v.video_reserved_slots, 1) : 0,
      queue_capacity: role === "video_reserved" ? Math.min(v.queue_capacity, 20) : Math.max(v.queue_capacity, 40),
    });
  return (
    <Sheet open onOpenChange={(open) => { if (!open) close(); }}>
      <SheetContent className="account-sheet">
        <SheetTitle className="sr-only">{isEditing ? "修改账号配置" : "添加渠道账号"}</SheetTitle>
        <SheetDescription className="sr-only">渠道登录、路由、并发和会话 Worker 配置。</SheetDescription>
      <form
        className="dialog account-dialog account-sheet-form"
        onSubmit={(e) => {
          e.preventDefault();
          m.mutate();
        }}
      >
        <header className="dialog-heading account-dialog-heading">
          <div>
            <span className="eyebrow">Provider Account</span>
            <h2>{isEditing ? "修改账号配置" : "添加渠道账号"}</h2>
            <p>
              {isEditing
                ? "调整账号信息、生成并发、等待队列和会话 Worker 归属。"
                : "登录凭据、余额、积分规则和并发容量均按渠道隔离。"}
            </p>
          </div>
          <button
            type="button"
            className="icon"
            onClick={close}
            aria-label={`关闭${isEditing ? "修改" : "添加"}账号窗口`}
            title="关闭"
          >
            <X />
          </button>
        </header>

        <section className="account-dialog-section">
          <div className="account-section-heading">
            <Server />
            <div>
              <strong>渠道与登录</strong>
              <small>
                {isEditing
                  ? "渠道和登录凭据保持不变，本次只修改运行配置。"
                  : "选择上游平台，并在新页面完成登录准备。"}
              </small>
            </div>
          </div>
          <div className="account-provider-layout">
            <fieldset className="provider-choice-fieldset">
              <legend>渠道</legend>
              <div className="provider-choice-grid">
                {(providers.data || []).map((provider) => {
                  const isSelected = provider.id === v.provider_id;
                  return (
                    <label
                      key={provider.id}
                      className={`provider-choice${isSelected ? " selected" : ""}${provider.enabled ? "" : " disabled"}`}
                    >
                      <input
                        type="radio"
                        name="provider_id"
                        value={provider.id}
                        checked={isSelected}
                        disabled={isEditing || !provider.enabled}
                        onChange={() =>
                          setV({ ...v, provider_id: provider.id })
                        }
                      />
                      <span className="provider-choice-mark" aria-hidden="true">
                        <Check />
                      </span>
                      <span className="provider-choice-copy">
                        <strong>{provider.display_name}</strong>
                        <small>
                          {authLabel(provider.auth_type)} · {creditLabel(provider.credit_unit)}
                        </small>
                      </span>
                      <span className="provider-choice-state">
                        {provider.enabled
                          ? isSelected
                            ? "已选"
                            : ""
                          : "暂不可用"}
                      </span>
                    </label>
                  );
                })}
              </div>
            </fieldset>
            {selected?.id === "leonardo" && (
              <aside className="provider-access-panel">
                <div className="provider-access-copy">
                  <span>当前渠道</span>
                  <strong>{selected.display_name}</strong>
                  <small>
                    {authLabel(selected.auth_type)} · 登录后复制 Cookie Header
                  </small>
                </div>
                {!isEditing && (
                  <button
                    type="button"
                    className="secondary account-login-button"
                    onClick={() =>
                      window.open(
                        "https://app.leonardo.ai/auth/login",
                        "_blank",
                        "noopener,noreferrer",
                      )
                    }
                  >
                    <ExternalLink />
                    打开登录页
                  </button>
                )}
              </aside>
            )}
          </div>
          {providers.isError && <p className="error">渠道配置加载失败，请重试。</p>}
          {!providers.isError && !selected && (
            <p className="error">正在加载渠道配置…</p>
          )}
        </section>

        {(isEditing || selected?.id === "leonardo") && (
          <section className="account-dialog-section">
            <div className="account-section-heading">
              <ShieldCheck />
              <div>
                <strong>{isEditing ? "账号信息" : "账号与凭据"}</strong>
                <small>
                  {isEditing
                    ? `${account?.has_login_credentials ? "自动登录已配置；留空密码保持不变。" : "补录密码后可在 Cookie 失效时自动重新登录。"}`
                    : "Cookie 与可选登录密码会分别加密保存，不会在列表中回显。"}
                </small>
              </div>
            </div>
            <div className="account-identity-grid">
              <label>
                账号名称
                <input
                  required
                  autoFocus
                  maxLength={200}
                  placeholder="例如：leonardo-main"
                  value={v.name}
                  onChange={(e) => setV({ ...v, name: e.target.value })}
                />
              </label>
              <label>
                邮箱
                <input
                  type="email"
                  autoComplete="off"
                  placeholder="用于识别账号，可选"
                  value={v.email}
                  onChange={(e) => setV({ ...v, email: e.target.value })}
                />
              </label>
              <label>
                {isEditing ? "新登录密码" : "登录密码（推荐）"}
                <input
                  type="password"
                  autoComplete="new-password"
                  placeholder={isEditing ? "留空表示不修改" : "用于会话失效后自动登录"}
                  value={v.password}
                  onChange={(e) => setV({ ...v, password: e.target.value })}
                />
              </label>
            </div>
            {!isEditing && (
              <label>
                Cookie Header
                <textarea
                  required
                  rows={4}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="cookie_name=value; another_cookie=value"
                  value={v.cookie}
                  onChange={(e) => setV({ ...v, cookie: e.target.value })}
                />
                <small className="field-help">从已登录 Leonardo 请求中复制完整 Cookie Header。</small>
              </label>
            )}
          </section>
        )}

        <section className="account-dialog-section">
          <div className="account-section-heading">
            <Gauge />
            <div>
              <strong>路由与容量</strong>
              <small>控制该账号同时占用的生成任务数量和独立网络出口。</small>
            </div>
          </div>
          <div className="account-routing-grid">
            <fieldset className="routing-role-fieldset">
              <legend>账号用途</legend>
              <div className="segmented compact">
                <button type="button" className={v.routing_role === "general" ? "active" : ""} onClick={() => setRoutingRole("general")}>通用</button>
                <button type="button" className={v.routing_role === "video_reserved" ? "active" : ""} onClick={() => setRoutingRole("video_reserved")}>视频保留</button>
              </div>
              <small className="field-help">视频保留账号优先承接视频；图片只使用保护线以上的积分。</small>
            </fieldset>
            <label>
              视频保护积分
              <input
                type="number"
                min={0}
                step={1}
                value={v.protected_tokens}
                onChange={(e) => setV({ ...v, protected_tokens: Math.max(0, Number(e.target.value) || 0) })}
              />
              <small className="field-help">推荐 6,804，可覆盖 Seedance 2.0 1080p 10秒。</small>
            </label>
            <div className="account-concurrency-field">
              <span className="field-label">视频保留槽位</span>
              <div className="number-stepper">
                <button type="button" onClick={() => setV({ ...v, video_reserved_slots: Math.max(0, v.video_reserved_slots - 1) })} disabled={v.video_reserved_slots <= 0} aria-label="减少视频保留槽位"><Minus /></button>
                <input type="number" min={0} max={v.image_concurrency} value={v.video_reserved_slots} onChange={(e) => setV({ ...v, video_reserved_slots: Math.min(v.image_concurrency, Math.max(0, Number(e.target.value) || 0)) })} aria-label="视频保留槽位" />
                <button type="button" onClick={() => setV({ ...v, video_reserved_slots: Math.min(v.image_concurrency, v.video_reserved_slots + 1) })} disabled={v.video_reserved_slots >= v.image_concurrency} aria-label="增加视频保留槽位"><Plus /></button>
              </div>
              <small className="field-help">没有视频排队时可借给普通任务；视频到达后优先收回。</small>
            </div>
            <div className="account-concurrency-field">
              <span className="field-label">账号最大并发</span>
              <div className="number-stepper">
                <button
                  type="button"
                  onClick={() => setConcurrency(v.image_concurrency - 1)}
                  disabled={v.image_concurrency <= 1}
                  aria-label="减少账号并发"
                  title="减少并发"
                >
                  <Minus />
                </button>
                <input
                  type="number"
                  min={1}
                  max={5}
                  value={v.image_concurrency}
                  onChange={(e) => setConcurrency(Number(e.target.value) || 1)}
                  aria-label="账号最大并发"
                />
                <button
                  type="button"
                  onClick={() => setConcurrency(v.image_concurrency + 1)}
                  disabled={v.image_concurrency >= 5}
                  aria-label="增加账号并发"
                  title="增加并发"
                >
                  <Plus />
                </button>
              </div>
              <small className="field-help">图片、视频、音频共用该账号的生成槽位；默认 5，与 BASIC 实测上限一致。</small>
            </div>
            <div className="account-concurrency-field">
              <span className="field-label">等待队列长度</span>
              <div className="number-stepper">
                <button
                  type="button"
                  onClick={() => setQueueCapacity(v.queue_capacity - 1)}
                  disabled={v.queue_capacity <= 1}
                  aria-label="减少等待队列长度"
                  title="减少容量"
                >
                  <Minus />
                </button>
                <input
                  type="number"
                  min={1}
                  max={1000}
                  value={v.queue_capacity}
                  onChange={(e) =>
                    setQueueCapacity(Number(e.target.value) || 1)
                  }
                  aria-label="等待队列长度"
                />
                <button
                  type="button"
                  onClick={() => setQueueCapacity(v.queue_capacity + 1)}
                  disabled={v.queue_capacity >= 1000}
                  aria-label="增加等待队列长度"
                  title="增加容量"
                >
                  <Plus />
                </button>
              </div>
              <small className="field-help">仅计算本地等待任务；图片建议40，实际准入仍受积分预留限制。</small>
            </div>
            <label>
              代理地址
              <input
                placeholder="http://、https:// 或 socks5://；留空使用服务器出口"
                value={v.proxy_url}
                onChange={(e) => setV({ ...v, proxy_url: e.target.value })}
              />
            </label>
            <label>
              会话 Worker 组
              <input
                required
                maxLength={100}
                placeholder="default"
                value={v.browser_worker_group}
                onChange={(e) => setV({ ...v, browser_worker_group: e.target.value })}
              />
              <small className="field-help">账号浏览器配置固定归属该组；多服务器部署时按代理出口或节点分组。</small>
            </label>
          </div>
          <div className="account-routing-summary">
            <span><strong>{v.image_concurrency}</strong><small>并发槽位</small></span>
            <span><strong>{v.queue_capacity}</strong><small>等待队列</small></span>
            <span><strong>{v.image_concurrency + v.queue_capacity}</strong><small>总承载</small></span>
            <span><strong>{v.routing_role === "video_reserved" ? "视频优先" : "Best-Fit"}</strong><small>路由策略</small></span>
            <span><strong>{v.protected_tokens.toLocaleString()}</strong><small>保护积分</small></span>
          </div>
        </section>

        {m.error && <p className="error">{m.error.message}</p>}
        <footer>
          <span className="account-validation-note">
            <ShieldCheck />
            {isEditing
              ? "降低容量时不能小于当前执行或排队任务数"
              : "创建时验证登录和余额"}
          </span>
          <div>
            <button type="button" className="secondary" onClick={close}>取消</button>
            <button disabled={!canSubmit || m.isPending}>
              {isEditing ? <Check /> : <Plus />}
              {m.isPending
                ? isEditing
                  ? "保存中…"
                  : "正在验证…"
                : isEditing
                  ? "保存修改"
                  : "添加并验证"}
            </button>
          </div>
        </footer>
      </form>
      </SheetContent>
    </Sheet>
  );
}
