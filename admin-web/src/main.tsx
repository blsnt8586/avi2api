import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, useLocation, useNavigate } from "react-router-dom";
import { QueryClientProvider, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  BadgeDollarSign,
  BookOpen,
  Box,
  Boxes,
  Download,
  Eye,
  EyeOff,
  FlaskConical,
  Gauge,
  KeyRound,
  LayoutDashboard,
  Menu,
  Plus,
  RefreshCw,
  ScrollText,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  SunMoon,
  X,
} from "lucide-react";
import "./styles.css";
import "./ux-polish.css";
import { OverviewTaskTable } from "./components/overview-task-table";
import { ProviderBadge } from "./components/provider-switcher";
import { providerCreditUnit } from "./shared/providers";
import type {
  Account,
  APIKeyRecord,
  APIKeysPage,
  OverviewResponse,
  SystemCapacityResponse,
  Task,
  TasksPage,
} from "./shared/types";
import { APIRequestError, api, qc } from "./shared/api";
import { CopyCode } from "./components/copy-code";
import { Metric } from "./components/metric";
import { formatCapacityWait, statusText } from "./shared/status";
import { QueryStatus } from "./components/ui";

const Accounts = React.lazy(() => import("./pages/account-pages").then((module) => ({ default: module.Accounts })));
const AccountDialog = React.lazy(() => import("./pages/account-pages").then((module) => ({ default: module.AccountDialog })));
const Tasks = React.lazy(() => import("./pages/task-pages").then((module) => ({ default: module.Tasks })));
const SystemCapacityPanel = React.lazy(() => import("./pages/system-capacity-page").then((module) => ({ default: module.SystemCapacityPanel })));
const Keys = React.lazy(() => import("./pages/api-key-page").then((module) => ({ default: module.Keys })));
const Security = React.lazy(() => import("./pages/security-page").then((module) => ({ default: module.Security })));
const Models = React.lazy(() => import("./pages/model-page").then((module) => ({ default: module.Models })));
const SalePricing = React.lazy(() => import("./pages/sale-pricing-page").then((module) => ({ default: module.SalePricing })));
const APIDocs = React.lazy(() => import("./pages/docs-page").then((module) => ({ default: module.APIDocs })));
const Playground = React.lazy(() => import("./pages/playground-page").then((module) => ({ default: module.Playground })));
const RequestLogs = React.lazy(() => import("./pages/observability-pages").then((module) => ({ default: module.RequestLogs })));
const Audit = React.lazy(() => import("./pages/observability-pages").then((module) => ({ default: module.Audit })));
const CommandPalette = React.lazy(() => import("./components/command-palette"));

function PageLoading() {
  return (
    <div className="loading-state" role="status" aria-live="polite">
      <RefreshCw className="spin" />
      <strong>正在加载页面</strong>
    </div>
  );
}

function Brand() {
  return (
    <div className="brand">
      <span className="brand-mark">
        <Box size={20} />
      </span>
      <span className="brand-copy">
        <strong>AIV2API</strong>
        <small>MULTI-PROVIDER GATEWAY</small>
      </span>
    </div>
  );
}
function Login({ done }: { done: () => void }) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const m = useMutation({
    mutationFn: () =>
      api("/admin/login", {
        method: "POST",
        body: JSON.stringify({ username, password }),
      }),
    onSuccess: done,
  });
  return (
    <main className="login">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          m.mutate();
        }}
      >
        <Brand />
        <div className="login-heading">
          <span className="eyebrow">安全访问</span>
          <h1>登录管理后台</h1>
          <p>使用管理员凭据继续。</p>
        </div>
        <label>
          用户名
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
          />
        </label>
        <label>
          密码
          <span className="password-input-wrap">
            <input
              autoFocus
              type={showPassword ? "text" : "password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
            />
            <button
              type="button"
              className="icon"
              aria-label={showPassword ? "隐藏密码" : "显示密码"}
              onClick={() => setShowPassword((value) => !value)}
            >
              {showPassword ? <EyeOff size={17} /> : <Eye size={17} />}
            </button>
          </span>
        </label>
        {m.error && <p className="error" role="alert">登录失败，请检查管理员凭据后重试。</p>}
        <button className="login-submit" disabled={m.isPending}>
          <ShieldCheck size={18} />
          {m.isPending ? "正在验证" : "安全登录"}
        </button>
      </form>
    </main>
  );
}

function AdminSessionCheck({
  error,
  retry,
}: {
  error?: string;
  retry?: () => void;
}) {
  return (
    <main className="login">
      <section className="admin-session-check">
        <Brand />
        <div className="admin-session-check-body">
          <RefreshCw className={error ? undefined : "spin"} size={24} />
          <div>
            <strong>{error ? "后台连接失败" : "正在恢复管理会话"}</strong>
            <small>{error || "正在验证浏览器中已有的登录凭据。"}</small>
          </div>
        </div>
        {retry && (
          <button type="button" className="secondary" onClick={retry}>
            <RefreshCw size={16} />
            重新连接
          </button>
        )}
      </section>
    </main>
  );
}

function App() {
  const location = useLocation();
  const publicDocs =
    location.pathname === "/docs" || location.pathname.startsWith("/docs/");
  React.useEffect(() => {
    document.title = publicDocs ? "AIV2API 开发者文档" : "AIV2API 管理后台";
  }, [publicDocs]);
  return publicDocs ? <PublicDocs /> : <AdminApp />;
}
function AdminApp() {
  const [sessionVersion, setSessionVersion] = useState(0);
  const overview = useQuery({
    queryKey: ["admin-session", sessionVersion],
    queryFn: () => api<any>("/admin/api/overview"),
    retry: false,
  });
  if (overview.isPending) return <AdminSessionCheck />;
  if (overview.error instanceof APIRequestError && overview.error.status === 401)
    return <Login done={() => setSessionVersion((value) => value + 1)} />;
  if (overview.isError)
    return (
      <AdminSessionCheck
        error={overview.error.message}
        retry={() => void overview.refetch()}
      />
    );
  return <Dashboard />;
}
function PublicDocs() {
  return (
    <div className="public-docs">
      <header className="public-docs-header">
        <a
          href="/docs"
          className="public-docs-brand"
          aria-label="AIV2API 开发者文档"
        >
          <Brand />
          <span>开发者文档</span>
        </a>
        <nav aria-label="文档链接">
          <a href="#quickstart">快速开始</a>
          <a href="#errors">错误处理</a>
          <a href="/openapi.json" target="_blank" rel="noreferrer">
            OpenAPI JSON
          </a>
          <a href="/">管理后台</a>
        </nav>
      </header>
      <main>
        <div className="public-docs-title">
          <div>
            <span className="page-kicker">
              <span></span>Multi-provider AI Gateway / API v1
            </span>
            <h1>AIV2API 开发者文档</h1>
            <p>
              图像、视频、音频生成接口的统一请求、任务和错误说明。
            </p>
          </div>
          <a
            className="docs-spec-link"
            href="/openapi.json"
            target="_blank"
            rel="noreferrer"
          >
            <Download />
            下载 OpenAPI 3.1
          </a>
        </div>
        <React.Suspense fallback={<PageLoading />}>
          <APIDocs />
        </React.Suspense>
      </main>
    </div>
  );
}

type Tab =
  | "overview"
  | "accounts"
  | "tasks"
  | "capacity"
  | "keys"
  | "playground"
  | "models"
  | "pricing"
  | "docs"
  | "security"
  | "requests"
  | "audit";
const tabPaths: Record<Tab, string> = {
  overview: "/",
  accounts: "/accounts",
  tasks: "/tasks",
  capacity: "/capacity",
  models: "/models",
  pricing: "/pricing",
  keys: "/keys",
  requests: "/requests",
  audit: "/audit",
  playground: "/playground",
  docs: "/docs",
  security: "/security",
};

const refreshScopes: Record<Tab, string[]> = {
  overview: ["overview", "tasks", "system-capacity", "keys"],
  accounts: ["accounts-page", "overview", "providers"],
  tasks: ["tasks-page", "task-detail"],
  capacity: ["system-capacity"],
  models: ["platform-models", "model-costs", "cost-rules"],
  pricing: ["sale-pricing", "cost-rules", "providers"],
  keys: ["api-keys-page", "keys"],
  requests: ["request-logs"],
  audit: ["audit-page"],
  playground: ["playground-video", "playground-audio"],
  docs: [],
  security: ["admin-settings", "system-capacity"],
};

function tabFromPath(pathname: string): Tab {
  return (
    (Object.entries(tabPaths).find(([, path]) => path === pathname)?.[0] as
      | Tab
      | undefined) || "overview"
  );
}

function Dashboard() {
  const client = useQueryClient();
  const location = useLocation();
  const navigate = useNavigate();
  const tab = tabFromPath(location.pathname);
  const [navOpen, setNavOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const [commandQuery, setCommandQuery] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const pageTitleRef = React.useRef<HTMLHeadingElement>(null);
  const [theme, setTheme] = useState<"dark" | "light">(() =>
    localStorage.getItem("aiv2api-theme") === "light" ? "light" : "dark",
  );
  React.useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("aiv2api-theme", theme);
  }, [theme]);
  React.useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandOpen(true);
      }
      if (event.key === "Escape") {
        setCommandOpen(false);
        setNavOpen(false);
      }
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, []);
  React.useEffect(() => {
    if (!Object.values(tabPaths).includes(location.pathname)) {
      navigate("/", { replace: true });
    }
  }, [location.pathname, navigate]);
  const overview = useQuery({
    queryKey: ["overview"],
    queryFn: () => api<OverviewResponse>("/admin/api/overview"),
    refetchInterval: 15_000,
  });
  const tasks = useQuery({
    queryKey: ["tasks", "recent"],
    queryFn: () =>
      api<TasksPage>("/admin/api/tasks?page=1&page_size=6"),
    refetchInterval: 10_000,
    enabled: tab === "overview",
  });
  const capacity = useQuery({
    queryKey: ["system-capacity"],
    queryFn: () => api<SystemCapacityResponse>("/admin/api/system-capacity"),
    refetchInterval: tab === "overview" ? 5_000 : 15_000,
  });
  const keys = useQuery({
    queryKey: ["keys"],
    queryFn: () => api<APIKeyRecord[] | APIKeysPage>("/admin/api/api-keys"),
    enabled: tab === "overview",
  });
  const [showAccount, setShowAccount] = useState(false);
  const [editingAccount, setEditingAccount] = useState<Account | null>(null);
  const [newKey, setNewKey] = useState<string>("");
  const labels: Record<Tab, [string, string]> = {
    overview: ["运营概览", "全局运行与风险"],
    accounts: ["账号池", "会话状态、余额和并发控制"],
    tasks: ["任务管理", "队列与执行"],
    capacity: ["系统容量", "全局并发、等待队列与过载保护"],
    keys: ["API 密钥", "管理客户端凭据、并发和用量"],
    playground: ["测试中心", "直接验证图像、视频和音频生成接口"],
    models: ["模型与计价", "平台目录与参数"],
    pricing: ["成本与售价", "平台成本、对外报价与毛利核算"],
    docs: ["接口文档", "公开 API 合约与请求示例"],
    security: ["系统设置", "安全与保留策略"],
    requests: ["请求日志", "追踪 API 参数、路由账号、成本预估与错误码"],
    audit: ["审计日志", "查看管理员配置变更记录"],
  };
  const [title, description] = labels[tab];
  React.useEffect(() => {
    document.title = `${title} · AIV2API 管理后台`;
    const frame = window.requestAnimationFrame(() => pageTitleRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [title]);
  const keyRows = Array.isArray(keys.data) ? keys.data : keys.data?.data || [];
  const openTab = (next: Tab, params?: Record<string, string>) => {
    const query = params ? `?${new URLSearchParams(params)}` : "";
    navigate(`${tabPaths[next]}${query}`);
    setNavOpen(false);
    setCommandOpen(false);
    setCommandQuery("");
    window.scrollTo({
      top: 0,
      behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches
        ? "auto"
        : "smooth",
    });
  };
  const refreshCurrent = async () => {
    if (refreshing) return;
    setRefreshing(true);
    await Promise.all(
      refreshScopes[tab].map((scope) =>
        client.invalidateQueries({
          predicate: (query) => query.queryKey[0] === scope,
        }),
      ),
    );
    setRefreshing(false);
  };
  const navigation: Array<{
    label: string;
    items: Array<{
      id: Tab;
      label: string;
      icon: React.ComponentType<{ size?: number }>;
      count?: number;
    }>;
  }> = [
    {
      label: "运营",
      items: [
        { id: "overview", label: "运营概览", icon: LayoutDashboard },
        {
          id: "accounts",
          label: "账号池",
          icon: Server,
          count: overview.data?.accounts,
        },
        {
          id: "tasks",
          label: "任务管理",
          icon: Activity,
          count: tasks.data?.total,
        },
      ],
    },
    {
      label: "资源与策略",
      items: [
        { id: "capacity", label: "系统容量", icon: Gauge },
        { id: "models", label: "模型与计价", icon: Boxes },
        { id: "pricing", label: "成本与售价", icon: BadgeDollarSign },
        {
          id: "keys",
          label: "API Key",
          icon: KeyRound,
          count: keyRows.length,
        },
      ],
    },
    {
      label: "可观测性",
      items: [
        { id: "requests", label: "请求日志", icon: ScrollText },
        { id: "audit", label: "审计日志", icon: ShieldCheck },
      ],
    },
    {
      label: "开发工具",
      items: [
        { id: "playground", label: "测试中心", icon: FlaskConical },
        { id: "docs", label: "接口文档", icon: BookOpen },
        { id: "security", label: "系统设置", icon: Settings2 },
      ],
    },
  ];
  const capacityState = capacity.data?.capacity;
  const systemSignal = capacity.isError || overview.isError
    ? { className: "failed", title: "状态检查失败", detail: "点击刷新重新检查" }
    : capacityState?.maintenance_mode
      ? { className: "queued", title: "维护排空中", detail: "现有任务继续执行" }
      : capacityState?.execution_paused
        ? { className: "queued", title: "任务执行已暂停", detail: "新任务保留在队列" }
        : capacityState?.overload_active
          ? { className: "queued", title: "过载保护中", detail: `${capacityState.queued} 个任务等待` }
          : { className: "active", title: "系统运行正常", detail: "容量与数据库可用" };
  const commandNeedle = commandQuery.trim().toLowerCase();
  const commandSearchTargets: Array<{ id: Tab; label: string }> = [
    { id: "accounts", label: "在账号池中搜索" },
    { id: "tasks", label: "在任务中搜索" },
    { id: "requests", label: "在请求日志中搜索" },
    { id: "models", label: "在模型目录中搜索" },
  ];
  const commandItems = [
    ...(commandNeedle ? commandSearchTargets.map((target) => ({
      key: `search-${target.id}`,
      label: target.label,
      detail: commandQuery.trim(),
      icon: navigation.flatMap((group) => group.items).find((item) => item.id === target.id)?.icon || Search,
      group: "search" as const,
      onSelect: () => openTab(target.id, { search: commandQuery.trim() }),
    })) : []),
    ...navigation
      .flatMap((group) => group.items)
      .filter((item) => item.label.toLowerCase().includes(commandNeedle))
      .map((item) => ({
        key: item.id,
        label: item.label,
        detail: labels[item.id][1],
        icon: item.icon,
        group: "navigation" as const,
        onSelect: () => openTab(item.id),
      })),
  ];
  return (
    <div className="app">
      <aside className={navOpen ? "open" : ""} aria-label="管理后台导航">
        <Brand />
        <nav aria-label="主要模块">
          {navigation.map((group) => (
            <React.Fragment key={group.label}>
              <span className="nav-label">{group.label}</span>
              {group.items.map((item) => {
                const Icon = item.icon;
                return (
                  <button
                    key={item.id}
                    data-nav-tab={item.id}
                    className={tab === item.id ? "active" : ""}
                    aria-current={tab === item.id ? "page" : undefined}
                    onClick={() => openTab(item.id)}
                  >
                    <Icon size={18} />
                    <span>{item.label}</span>
                    {item.count !== undefined && (
                      <span className="nav-count">{item.count}</span>
                    )}
                  </button>
                );
              })}
            </React.Fragment>
          ))}
        </nav>
        <div className="sidebar-status">
          <i className={`status ${systemSignal.className}`}></i>
          <span>
            <strong>{systemSignal.title}</strong>
            <small>{systemSignal.detail}</small>
          </span>
        </div>
      </aside>
      <button
        className={"sidebar-scrim" + (navOpen ? " open" : "")}
        aria-label="关闭导航"
        onClick={() => setNavOpen(false)}
      />
      <main className="content">
        <header className="admin-topbar">
          <button
            className="admin-menu-button icon secondary"
            aria-label="打开导航"
            onClick={() => setNavOpen(true)}
          >
            <Menu />
          </button>
          <div className="admin-topbar-title">
            <strong>AIV2API</strong>
            <span>/ {title}</span>
          </div>
          <div className="admin-topbar-actions">
            <span className="environment-badge">生产环境</span>
            <button
              className="icon secondary"
              aria-label="全局搜索"
              title="全局搜索 (Ctrl+K)"
              onClick={() => setCommandOpen(true)}
            >
              <Search />
            </button>
            <button
              className="icon secondary"
              aria-label="切换主题"
              title="切换主题"
              onClick={() => setTheme((value) => (value === "dark" ? "light" : "dark"))}
            >
              <SunMoon />
            </button>
            <button
              className="icon secondary"
              aria-label="刷新当前数据"
              title="刷新当前数据"
              disabled={refreshing || refreshScopes[tab].length === 0}
              onClick={() => void refreshCurrent()}
            >
              <RefreshCw className={refreshing ? "spin" : undefined} />
            </button>
          </div>
        </header>
        <div className="content-inner">
          <header className="page-header">
            <div>
              <h1 ref={pageTitleRef} tabIndex={-1}>{title}</h1>
              <p>{description}</p>
            </div>
            {tab === "accounts" && (
              <button
                onClick={() => {
                  setEditingAccount(null);
                  setShowAccount(true);
                }}
              >
                <Plus />
                添加账号
              </button>
            )}
          </header>
          <div className="page-stage" key={tab}>
          <React.Suspense fallback={<PageLoading />}>
          {tab === "overview" && (
            <Operations
              overview={overview.data}
              overviewFetching={overview.isFetching}
              tasks={tasks.data?.data || []}
              tasksLoading={tasks.isLoading}
              keys={keyRows}
              capacity={capacity.data}
              updatedAt={overview.dataUpdatedAt}
              openTasks={(providerID) => openTab("tasks", providerID ? { provider: providerID } : undefined)}
              openTask={(id) => openTab("tasks", { search: id })}
              openAccounts={(providerID, attention) => openTab("accounts", {
                ...(providerID ? { provider: providerID } : {}),
                ...(attention ? { status: "attention" } : {}),
              })}
              openKeys={() => openTab("keys")}
            />
          )}{" "}
          {tab === "accounts" && (
            <Accounts
              edit={setEditingAccount}
            />
          )}{" "}
          {tab === "tasks" && <Tasks />}{" "}
          {tab === "capacity" && <SystemCapacityPanel />}{" "}
          {tab === "keys" && <Keys setKey={setNewKey} />}{" "}
          {tab === "playground" && <Playground />}{" "}
          {tab === "models" && <Models />}{" "}
          {tab === "pricing" && <SalePricing />}{" "}
          {tab === "docs" && <APIDocs />} {tab === "security" && <Security />}{" "}
          {tab === "requests" && <RequestLogs />} {" "}
          {tab === "audit" && <Audit />}
          </React.Suspense>
          </div>
        </div>
        {commandOpen && (
          <React.Suspense fallback={null}>
            <CommandPalette
              open={commandOpen}
              query={commandQuery}
              items={commandItems}
              onOpenChange={setCommandOpen}
              onQueryChange={setCommandQuery}
            />
          </React.Suspense>
        )}
        {(showAccount || editingAccount) && (
          <React.Suspense fallback={null}>
            <AccountDialog
              key={editingAccount?.id || "create"}
              account={editingAccount || undefined}
              close={() => {
                setShowAccount(false);
                setEditingAccount(null);
              }}
              done={() => {
                setShowAccount(false);
                setEditingAccount(null);
                client.invalidateQueries({ queryKey: ["accounts-page"] });
                client.invalidateQueries({ queryKey: ["overview"] });
              }}
            />
          </React.Suspense>
        )}
        {newKey && (
          <div className="notice key-notice">
            <KeyRound />
            <div>
              <strong>新 API 密钥</strong>
              <code>{newKey}</code>
              <span>完整值只显示一次，请立即复制保存。</span>
              <CopyCode value={newKey} />
            </div>
            <button
              className="icon"
              onClick={() => setNewKey("")}
              aria-label="关闭"
            >
              <X />
            </button>
          </div>
        )}
      </main>
    </div>
  );
}

function Operations({
  overview,
  overviewFetching,
  tasks,
  keys,
  capacity,
  openTasks,
  openTask,
  openAccounts,
  openKeys,
  updatedAt,
  tasksLoading,
}: {
  overview?: OverviewResponse;
  overviewFetching: boolean;
  tasks: Task[];
  keys: APIKeyRecord[];
  capacity?: SystemCapacityResponse;
  openTasks: (providerID?: string) => void;
  openTask: (id: string) => void;
  openAccounts: (providerID?: string, attention?: boolean) => void;
  openKeys: () => void;
  updatedAt: number;
  tasksLoading: boolean;
}) {
  const activeAccounts = overview?.active_accounts || 0;
  const accountCount = overview?.accounts || 0;
  const unavailableAccounts = accountCount - activeAccounts;
  const activeTasks = tasks.filter((t) =>
    ["queued", "reserving", "uploading", "submitted", "polling"].includes(
      t.status,
    ),
  );
  const failedLastHour = overview?.failed_last_hour || 0;
  const providerSummaries = overview?.provider_summaries || [];
  const uncertainTasks = providerSummaries.reduce((sum, provider) => sum + provider.submission_uncertain, 0);
  const capacityState = capacity?.capacity;
  const executing = capacityState?.executing || activeTasks.length;
  const executingLimit = capacityState?.effective_execution_limit || 100;
  const queued = capacityState?.queued || tasks.filter((t) => t.status === "queued").length;
  const queuedLimit = capacityState?.max_queued || 1000;
  const oldestQueued = capacityState?.oldest_queued_seconds || 0;
  const statusCounts = overview?.task_counts || {};
  const queueSegments = [
    { status: "succeeded", count: statusCounts.succeeded || 0 },
    {
      status: "polling",
      count: ["reserving", "uploading", "submitted", "polling"].reduce(
        (sum, status) => sum + (statusCounts[status] || 0),
        0,
      ),
    },
    { status: "queued", count: statusCounts.queued || 0 },
    { status: "failed", count: statusCounts.failed || 0 },
    { status: "submission_uncertain", count: statusCounts.submission_uncertain || 0 },
  ];
  const max = Math.max(1, ...queueSegments.map((segment) => segment.count));
  return (
    <section className="operations">
      <div className="ops-context-line">
        <span><i className="status active"></i>实时运行视图</span>
        <QueryStatus fetching={overviewFetching} updatedAt={updatedAt} />
      </div>
      <div className="ops-metrics">
        <Metric
          icon={<ShieldCheck />}
          label="健康账号"
          value={`${activeAccounts} / ${accountCount}`}
          detail={unavailableAccounts > 0 ? `${unavailableAccounts} 个需要关注` : "当前可调度"}
          tone={unavailableAccounts > 0 ? "warning" : "normal"}
        />
        <Metric
          icon={<Activity />}
          label="执行槽位"
          value={`${executing} / ${executingLimit}`}
          detail={`${Math.max(0, executingLimit - executing)} 全局余量`}
        />
        <Metric
          icon={<ScrollText />}
          label="等待队列"
          value={`${queued} / ${queuedLimit}`}
          detail={oldestQueued ? `最老等待 ${formatCapacityWait(oldestQueued)}` : "当前无等待"}
        />
        <Metric
          icon={<Boxes />}
          label="接入平台"
          value={providerSummaries.length}
          detail={`${providerSummaries.filter((provider) => provider.enabled).length} 个正在接单`}
        />
      </div>
      <section className="ops-provider-section">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Provider Operations</span>
            <h2>平台运行矩阵</h2>
          </div>
          <span className="ops-provider-note">余额与预留按平台独立核算</span>
        </div>
        <div className="ops-provider-grid">
          {providerSummaries.map((provider) => {
			const creditUnit = providerCreditUnit(provider.provider_id, [provider]);
            const providerHealthy = provider.enabled && provider.active_accounts > 0 && provider.submission_uncertain === 0;
            return (
              <article className={`ops-provider-card provider-${provider.provider_id}`} key={provider.provider_id}>
                <header>
                  <div>
                    <ProviderBadge providerID={provider.provider_id} />
                    <span className={`provider-health ${providerHealthy ? "active" : "warning"}`}>
                      <i className={`status ${providerHealthy ? "active" : "queued"}`} />
                      {provider.enabled ? (providerHealthy ? "可调度" : "需要关注") : "已停用"}
                    </span>
                  </div>
                  <strong>{provider.available_credits.toLocaleString()} <small>{creditUnit}</small></strong>
                </header>
                <div className="ops-provider-ledger">
                  <span><small>账面余额</small><strong>{provider.total_credits.toLocaleString()}</strong></span>
                  <span><small>已预留</small><strong>{provider.reserved_credits.toLocaleString()}</strong></span>
                  <span><small>健康账号</small><strong>{provider.active_accounts}/{provider.accounts}</strong></span>
                </div>
                <div className="ops-provider-flow">
                  <span><small>执行</small><strong>{provider.executing_tasks}/{provider.execution_slots}</strong></span>
                  <span><small>排队</small><strong>{provider.queued_tasks}/{provider.queue_slots}</strong></span>
                  <span><small>近 1 小时失败</small><strong className={provider.failed_last_hour ? "danger" : ""}>{provider.failed_last_hour}</strong></span>
                  <span><small>待确认提交</small><strong className={provider.submission_uncertain ? "danger" : ""}>{provider.submission_uncertain}</strong></span>
                </div>
                {provider.provider_id === "leonardo" && (
                  <div className="ops-provider-inventory">
                    <span><strong>{provider.video_ready_720p_15s || 0}</strong><small>720p · 15秒</small></span>
                    <span><strong>{provider.video_ready_1080p_8s || 0}</strong><small>1080p · 8秒</small></span>
                    <span><strong>{(provider.video_protected_credits || 0).toLocaleString()}</strong><small>视频保护积分</small></span>
                  </div>
                )}
                <footer>
                  <button className="secondary" onClick={() => openAccounts(provider.provider_id)}><Server />账号池</button>
                  <button className="secondary" onClick={() => openTasks(provider.provider_id)}><Activity />任务</button>
                </footer>
              </article>
            );
          })}
        </div>
      </section>
      <div className="ops-grid">
        <section className="ops-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">队列健康</span>
              <h2>任务队列</h2>
            </div>
            <button className="text-action" onClick={() => openTasks()}>
              查看全部
            </button>
          </div>
          <div className="queue-bars">
            {queueSegments.map((segment) => (
              <div className="queue-bar" key={segment.status}>
                <span>{statusText(segment.status)}</span>
                <div>
                  <i
                    className={segment.status}
                    style={{
                      width: `${segment.count ? Math.max(4, (segment.count / max) * 100) : 0}%`,
                    }}
                  ></i>
                </div>
                <strong>{segment.count}</strong>
              </div>
            ))}
          </div>
        </section>
        <section className="ops-panel attention-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">风险聚合</span>
              <h2>需要关注</h2>
            </div>
            <button className="text-action" onClick={() => openTasks()}>
              全部告警
            </button>
          </div>
          <div className="attention-list">
            <button className="attention-item" onClick={() => openAccounts(undefined, true)}>
              <span className="attention-icon amber"><Activity size={16} /></span>
              <span>
                <strong>{unavailableAccounts ? `${unavailableAccounts} 个账号需要恢复` : "账号池运行正常"}</strong>
                <small>会话、余额和上游风控状态</small>
              </span>
              <b>{unavailableAccounts}</b>
            </button>
            <button className="attention-item" onClick={() => openTasks()}>
              <span className="attention-icon red"><AlertTriangle size={16} /></span>
              <span>
                <strong>{failedLastHour ? `过去 1 小时 ${failedLastHour} 个任务失败` : "过去 1 小时无失败任务"}</strong>
                <small>查看失败原因和上游响应</small>
              </span>
              <b>{failedLastHour}</b>
            </button>
            <button className="attention-item" onClick={() => openTasks()}>
              <span className="attention-icon red"><ShieldCheck size={16} /></span>
              <span>
                <strong>{uncertainTasks ? `${uncertainTasks} 个任务等待提交确认` : "没有待确认的上游提交"}</strong>
                <small>提交不确定任务会继续保留积分</small>
              </span>
              <b>{uncertainTasks}</b>
            </button>
            <button className="attention-item" onClick={openKeys}>
              <span className="attention-icon blue"><KeyRound size={16} /></span>
              <span>
                <strong>{keys.filter((k) => k.enabled).length} 个 API Key 正在使用</strong>
                <small>凭据并发和速率限制</small>
              </span>
              <b>{keys.filter((k) => k.enabled).length}</b>
            </button>
          </div>
        </section>
        <section className="ops-panel recent-runs">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">执行记录</span>
              <h2>最近任务</h2>
            </div>
            <span
              className={`health-summary ${failedLastHour ? "warning" : ""}`}
            >
              {failedLastHour ? `近 1 小时 ${failedLastHour} 个失败` : "运行稳定"}
            </span>
          </div>
		  <OverviewTaskTable data={tasks.slice(0, 6)} loading={tasksLoading} providers={providerSummaries} onOpenTask={(task) => openTask(task.id)} />
        </section>
      </div>
    </section>
  );
}
createRoot(document.getElementById("root")!).render(
  <BrowserRouter>
    <QueryClientProvider client={qc}>
      <App />
    </QueryClientProvider>
  </BrowserRouter>,
);
