import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Ban,
  Boxes,
  Check,
  CheckCircle2,
  CirclePlay,
  Coins,
  ExternalLink,
  FileJson,
  Gauge,
  KeyRound,
  Minus,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  Pagination as UIPagination,
  QueryStatus,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  TableSkeleton,
} from "../components/ui";
import type { Account, AccountsPage, OverviewResponse, Provider } from "../shared/types";

function accountBalanceUnavailable(account: Account): boolean {
  return account.provider_id === "creativefabrica" && account.status === "invalid";
}
import {
  AccountStatusCell,
  accountOperationalState,
  sessionExpiryText,
  sessionRefreshText,
} from "../components/account-status";
import { Metric } from "../components/metric";
import { api } from "../shared/api";
import { providerCreditUnit, providerDefinition } from "../shared/providers";

type ArchiveAccountsResponse = {
  archived: number;
  failed: number;
  results: Array<{
    id: string;
    archived: boolean;
    error_code?: string;
    error_message?: string;
  }>;
};

type ArchiveBatchResponse = ArchiveAccountsResponse & {
  requestFailed: number;
};

type ProviderPricingSyncResponse = {
  account: Account;
  pricing_rules_synced: number;
  pricing_source?: string;
};

type BulkCookieImportStatus = "ready" | "invalid" | "importing" | "submitted" | "failed";

type BulkCookieImportItem = {
  id: string;
  fileName: string;
  name: string;
  cookieJSON: unknown | null;
  status: BulkCookieImportStatus;
  error: string;
};

const MAX_BULK_COOKIE_FILES = 50;
const MAX_COOKIE_FILE_BYTES = 512 * 1024;
const ARCHIVE_BATCH_SIZE = 100;
const COOKIE_IMPORT_CONCURRENCY = 4;

function accountNameFromFile(fileName: string) {
  const withoutExtension = fileName.replace(/\.json$/i, "").trim();
  return (withoutExtension || "provider-account").slice(0, 200);
}

function accountSessionExpiryText(account: Account) {
  const tokenLabel = account.provider_id === "adobe" ? "AT" : account.provider_id === "creativefabrica" ? "RPC" : "JWT";
  return sessionExpiryText(
    account.access_token_expires_at,
    tokenLabel,
  );
}

function accountCredentialText(account: Account, compact = false) {
  if (account.has_pending_cookie_json) return "完整 Cookie 待验证";
  if (account.has_complete_cookie_json) {
    if (account.provider_id === "adobe") return compact ? "Cookie 自动续期" : "完整 Cookie · 自动续期";
    if (account.provider_id === "creativefabrica") return compact ? "Cookie · RPC 自动续期" : "完整 Cookie · RPC 自动续期";
    return compact ? "完整 Cookie" : "完整 Cookie 已保存";
  }
  if (account.has_login_credentials) return compact ? "自动登录" : "自动登录已配置";
  if (account.provider_id === "adobe") return "仅 Access Token";
  if (account.provider_id === "creativefabrica") return "仅 RPC Token";
  return sessionRefreshText(account);
}

function providerConcurrencyMaximum(providerID: string) {
  return providerID === "adobe" ? 100 : providerID === "creativefabrica" ? 10 : 5;
}

function providerSupportsCookie(provider: Provider) {
  return provider.enabled && provider.adapter_registered !== false && provider.auth_type.toLowerCase().includes("cookie");
}

function validateCookieJSON(value: unknown, providerID: string) {
  if (!Array.isArray(value)) return "必须是浏览器导出的 Cookie JSON 数组";
	const maximum = providerID === "leonardo" ? 64 : 128;
	const minimum = providerID === "leonardo" ? 2 : 1;
  if (value.length < minimum || value.length > maximum) return `Cookie 数量必须在 ${minimum} 到 ${maximum} 条之间`;
  const names = value.map((cookie) => {
    if (!cookie || typeof cookie !== "object") return "";
    const name = (cookie as { name?: unknown }).name;
    return typeof name === "string" ? name.trim().toLowerCase() : "";
  });
  if (names.some((name) => !name)) return "每条 Cookie 都必须包含 name";
	if (providerID === "adobe" || providerID === "creativefabrica") {
		const invalidScope = value.some((cookie) => {
		  const item = cookie as { domain?: unknown; url?: unknown };
		  const domain = typeof item.domain === "string" ? item.domain.replace(/^\./, "").toLowerCase() : "";
		  let host = "";
		  if (typeof item.url === "string") {
			try { host = new URL(item.url).hostname.toLowerCase(); } catch { host = ""; }
		  }
		  if (providerID === "adobe") {
			return ![domain, host].some((value) => value === "adobe.com" || value.endsWith(".adobe.com") || value === "adobelogin.com" || value.endsWith(".adobelogin.com"));
		  }
		  return ![domain, host].some((value) => value === "creativefabrica.com" || value.endsWith(".creativefabrica.com"));
		});
		return invalidScope ? `${providerID === "adobe" ? "Adobe" : "Creative Fabrica"} Cookie 文件包含非平台域名` : "";
	}
  if (!names.some((name) => name.includes("session_token")) || !names.some((name) => name.includes("session_data"))) {
    return "缺少 Leonardo session_token 或 session_data";
  }
  return "";
}

function chunkAccountIDs(ids: string[]) {
  const chunks: string[][] = [];
  for (let index = 0; index < ids.length; index += ARCHIVE_BATCH_SIZE) {
    chunks.push(ids.slice(index, index + ARCHIVE_BATCH_SIZE));
  }
  return chunks;
}

function SelectionCheckbox({
  indeterminate = false,
  ...props
}: React.InputHTMLAttributes<HTMLInputElement> & { indeterminate?: boolean }) {
  const ref = React.useRef<HTMLInputElement>(null);
  React.useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);
  return <input ref={ref} type="checkbox" className="account-checkbox" {...props} />;
}

export function Accounts({
  edit,
}: {
  edit: (account: Account) => void;
}) {
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [selectedAccounts, setSelectedAccounts] = useState<Map<string, Account>>(() => new Map());
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [archiveMode, setArchiveMode] = useState<"selected" | "invalid">("selected");
  const [archiveFeedback, setArchiveFeedback] = useState("");
  const [cleanupScanning, setCleanupScanning] = useState(false);
  const [cleanupError, setCleanupError] = useState("");
  const [bulkImportOpen, setBulkImportOpen] = useState(false);
  const [bulkImportBusy, setBulkImportBusy] = useState(false);
	const [bulkImportSummary, setBulkImportSummary] = useState("");
	const [bulkImportItems, setBulkImportItems] = useState<BulkCookieImportItem[]>([]);
	const [bulkImportProvider, setBulkImportProvider] = useState("leonardo");
  const [bulkImportConfig, setBulkImportConfig] = useState({
    imageConcurrency: 5,
    queueCapacity: 40,
    workerGroup: "default",
  });
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
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => api<Provider[]>("/admin/api/providers"),
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
  const providerOptions = providers.data || [];
  const cookieProviderOptions = providerOptions.filter(providerSupportsCookie);
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
  const permissionCheck = useMutation({
    mutationFn: (id: string) =>
      api(`/admin/api/accounts/${id}/generation-permission`, { method: "POST" }),
    onSuccess: refresh,
    onError: (error) => setCleanupError(`生成权限检测失败：${error.message}`),
  });
  const pricingSync = useMutation({
    mutationFn: (id: string) =>
      api<ProviderPricingSyncResponse>(`/admin/api/accounts/${id}/pricing-sync`, { method: "POST" }),
    onSuccess: (result) => {
      const label = result.pricing_source === "creativefabrica-preset" ? "Creative Fabrica Coins" : "Adobe BKS";
      setArchiveFeedback(`已同步 ${result.pricing_rules_synced} 条 ${label} 价格规则。`);
      refresh();
    },
    onError: (error) => setCleanupError(`平台价格同步失败：${error.message}`),
  });
  const importCookieJSON = useMutation({
    mutationFn: ({ id, cookieJSON }: { id: string; cookieJSON: unknown }) =>
      api(`/admin/api/accounts/${id}/cookie-json`, {
        method: "PUT",
        body: JSON.stringify({ cookie_json: cookieJSON }),
    }),
    onSuccess: refresh,
    onError: (error) => window.alert(error.message),
  });
  const onCookieFile = async (account: Account, file?: File) => {
    if (!file) return;
    try {
      const cookieJSON: unknown = JSON.parse(await file.text());
      importCookieJSON.mutate({ id: account.id, cookieJSON });
    } catch {
      // The API performs authoritative validation; this only rejects malformed files early.
      window.alert("Cookie JSON 文件不是有效的 JSON 数组。");
    }
  };
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
  const archive = useMutation({
    mutationFn: async (ids: string[]): Promise<ArchiveBatchResponse> => {
      const batches = chunkAccountIDs(ids);
      const combined: ArchiveBatchResponse = { archived: 0, failed: 0, requestFailed: 0, results: [] };
      for (const batch of batches) {
        try {
          const result = await api<ArchiveAccountsResponse>("/admin/api/accounts/archive", {
            method: "POST",
            body: JSON.stringify({ ids: batch }),
          });
          combined.archived += result.archived;
          combined.failed += result.failed;
          combined.results.push(...result.results);
        } catch {
          combined.failed += batch.length;
          combined.requestFailed += batch.length;
        }
      }
      return combined;
    },
    onSuccess: (result) => {
      const archivedIDs = new Set(result.results.filter((item) => item.archived).map((item) => item.id));
      setSelectedAccounts((current) => {
        const next = new Map(current);
        archivedIDs.forEach((id) => next.delete(id));
        return next;
      });
      setArchiveOpen(false);
      setArchiveFeedback(
        result.requestFailed
          ? `已归档 ${result.archived} 个账号；${result.requestFailed} 个账号请求失败，仍保留在当前选择中，可稍后重试。`
          : result.failed
          ? `已归档 ${result.archived} 个账号；${result.failed} 个账号仍有任务或预留，已保留在账号池。`
          : `已从运营池归档 ${result.archived} 个账号，历史任务与账本记录保持不变。`,
      );
      refresh();
    },
  });
  const scanInvalidAccounts = async () => {
    setCleanupScanning(true);
    setCleanupError("");
    setArchiveFeedback("");
    try {
      const invalidAccounts = new Map<string, Account>();
      let currentPage = 1;
      let expectedTotal = 0;
      do {
        const providerQuery = providerFilter === "all" ? "" : `&provider=${encodeURIComponent(providerFilter)}`;
        const result = await api<AccountsPage>(
          `/admin/api/accounts?page=${currentPage}&page_size=${ARCHIVE_BATCH_SIZE}&status=invalid${providerQuery}`,
        );
        expectedTotal = result.total;
        result.data.forEach((account) => invalidAccounts.set(account.id, account));
        if (result.data.length === 0) break;
        currentPage += 1;
      } while (invalidAccounts.size < expectedTotal);
      if (invalidAccounts.size === 0) {
        setArchiveFeedback("当前没有凭证失效账号，无需清理。");
        return;
      }
      setSelectedAccounts(invalidAccounts);
      setArchiveMode("invalid");
      setArchiveOpen(true);
    } catch (error) {
      setCleanupError(error instanceof Error ? error.message : "扫描失效账号失败");
    } finally {
      setCleanupScanning(false);
    }
  };
  const openBulkImport = () => {
	  const availableIDs = cookieProviderOptions.map((provider) => provider.id);
	  const nextProvider = availableIDs.includes(providerFilter)
	    ? providerFilter
	    : availableIDs.includes(bulkImportProvider)
	      ? bulkImportProvider
	      : availableIDs[0] || "leonardo";
	  setBulkImportProvider(nextProvider);
	  setBulkImportConfig((current) => ({ ...current, imageConcurrency: Math.min(providerConcurrencyMaximum(nextProvider), current.imageConcurrency || 5) }));
    setBulkImportItems([]);
    setBulkImportSummary("");
    setBulkImportOpen(true);
  };
  const addCookieFiles = async (files: File[]) => {
    const remaining = Math.max(0, MAX_BULK_COOKIE_FILES - bulkImportItems.length);
    const acceptedFiles = files.slice(0, remaining);
    if (files.length > remaining) {
      setBulkImportSummary(`单次最多导入 ${MAX_BULK_COOKIE_FILES} 个文件，超出的文件未加入。`);
    } else {
      setBulkImportSummary("");
    }
    const parsed = await Promise.all(acceptedFiles.map(async (file, index): Promise<BulkCookieImportItem> => {
      const id = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${index}-${Math.random()}`;
      if (file.size > MAX_COOKIE_FILE_BYTES) {
        return { id, fileName: file.name, name: accountNameFromFile(file.name), cookieJSON: null, status: "invalid", error: "文件超过 512 KB" };
      }
      try {
        const cookieJSON: unknown = JSON.parse(await file.text());
		const error = validateCookieJSON(cookieJSON, bulkImportProvider);
        return {
          id,
          fileName: file.name,
          name: accountNameFromFile(file.name),
          cookieJSON: error ? null : cookieJSON,
          status: error ? "invalid" : "ready",
          error,
        };
      } catch {
        return { id, fileName: file.name, name: accountNameFromFile(file.name), cookieJSON: null, status: "invalid", error: "文件不是有效 JSON" };
      }
    }));
    setBulkImportItems((current) => [...current, ...parsed]);
  };
  const importCookieAccounts = async () => {
    const pendingItems = bulkImportItems.filter(
      (item) => (item.status === "ready" || item.status === "failed") && item.cookieJSON && item.name.trim(),
    );
    if (pendingItems.length === 0) return;
    setBulkImportBusy(true);
    setBulkImportSummary("");
    let succeeded = 0;
    let failed = 0;
    let nextIndex = 0;
    const worker = async () => {
      while (nextIndex < pendingItems.length) {
        const item = pendingItems[nextIndex++];
        setBulkImportItems((current) => current.map((entry) => (
          entry.id === item.id ? { ...entry, status: "importing", error: "" } : entry
        )));
        try {
          await api("/admin/api/accounts", {
            method: "POST",
            body: JSON.stringify({
			  provider_id: bulkImportProvider,
              name: item.name.trim(),
              email: "",
              password: "",
              cookie_json: item.cookieJSON,
              proxy_url: "",
              image_concurrency: bulkImportConfig.imageConcurrency,
              queue_capacity: bulkImportConfig.queueCapacity,
              routing_role: "general",
              protected_tokens: 0,
              video_reserved_slots: 0,
			  ...(bulkImportProvider === "leonardo" ? { browser_worker_group: bulkImportConfig.workerGroup.trim() } : {}),
            }),
          });
          succeeded += 1;
          setBulkImportItems((current) => current.map((entry) => (
            entry.id === item.id ? { ...entry, status: "submitted", error: "" } : entry
          )));
        } catch (error) {
          failed += 1;
          setBulkImportItems((current) => current.map((entry) => (
            entry.id === item.id
              ? { ...entry, status: "failed", error: error instanceof Error ? error.message : "导入失败" }
              : entry
          )));
        }
      }
    };
    await Promise.all(Array.from(
      { length: Math.min(COOKIE_IMPORT_CONCURRENCY, pendingItems.length) },
      () => worker(),
    ));
    setBulkImportBusy(false);
    setBulkImportSummary(
      failed
		? `已导入 ${succeeded} 个账号，${failed} 个失败。`
		: `已导入 ${succeeded} 个账号。`,
    );
    refresh();
  };
  const pageSelected = filteredData.filter((account) => selectedAccounts.has(account.id));
  const allPageSelected = filteredData.length > 0 && pageSelected.length === filteredData.length;
  const somePageSelected = pageSelected.length > 0 && !allPageSelected;
  const toggleAccount = (account: Account, checked: boolean) => {
    setSelectedAccounts((current) => {
      const next = new Map(current);
      if (checked) next.set(account.id, account);
      else next.delete(account.id);
      return next;
    });
  };
  const togglePage = (checked: boolean) => {
    setSelectedAccounts((current) => {
      const next = new Map(current);
      filteredData.forEach((account) => {
        if (checked) next.set(account.id, account);
        else next.delete(account.id);
      });
      return next;
    });
  };
  const clearFilters = () => {
    setSearchParams(new URLSearchParams(), { replace: true });
    setPage(1);
  };
  const hasFilters = Boolean(search || statusFilter !== "all" || roleFilter !== "all" || providerFilter !== "all");
  const importableCount = bulkImportItems.filter(
    (item) => (item.status === "ready" || item.status === "failed") && item.cookieJSON && item.name.trim(),
  ).length;
  const bulkImportConfigValid =
    bulkImportConfig.imageConcurrency >= 1 &&
	bulkImportConfig.imageConcurrency <= providerConcurrencyMaximum(bulkImportProvider) &&
    bulkImportConfig.queueCapacity >= 1 &&
    bulkImportConfig.queueCapacity <= 1000 &&
	(bulkImportProvider !== "leonardo" || /^[A-Za-z0-9._-]{1,100}$/.test(bulkImportConfig.workerGroup.trim()));
  const providerSummaries = overview.data?.provider_summaries || [];
  const selectedProviderSummary = providerFilter === "all"
    ? undefined
    : providerSummaries.find((provider) => provider.provider_id === providerFilter);
  const totalExecutionSlots = providerSummaries.reduce((sum, provider) => sum + provider.execution_slots, 0);
	const creditUnit = selectedProviderSummary ? providerCreditUnit(selectedProviderSummary.provider_id, [selectedProviderSummary]) : "积分";
  return (
    <section>
      <div className="stats">
        <Metric icon={<Server />} label="账号数量" value={selectedProviderSummary?.accounts ?? overview.data?.accounts ?? 0} />
        {selectedProviderSummary ? (
          <>
            <Metric icon={<Coins />} label={`净可用 ${creditUnit}`} value={selectedProviderSummary.available_credits.toLocaleString()} />
            <Metric icon={<Coins />} label={`已预留 ${creditUnit}`} value={selectedProviderSummary.reserved_credits.toLocaleString()} />
            <Metric icon={<Activity />} label="可用账号" value={`${selectedProviderSummary.active_accounts}/${selectedProviderSummary.accounts}`} />
          </>
        ) : (
          <>
            <Metric icon={<Activity />} label="可用账号" value={`${overview.data?.active_accounts || 0}/${overview.data?.accounts || 0}`} />
            <Metric icon={<Boxes />} label="平台数量" value={providerSummaries.length} />
            <Metric icon={<Gauge />} label="可用执行槽位" value={totalExecutionSlots} />
          </>
        )}
      </div>
      {(providerFilter === "all" || providerFilter === "leonardo") && (() => {
        const leonardo = providerSummaries.find((provider) => provider.provider_id === "leonardo");
        return (
          <div className="video-inventory-strip" aria-label="Leonardo Seedance 2.0 视频库存">
            <span><strong>Leonardo</strong><small>Seedance 2.0 库存</small></span>
            <span><strong>{leonardo?.video_ready_720p_15s || 0}</strong><small>720p · 15秒可用账号</small></span>
            <span><strong>{leonardo?.video_ready_1080p_8s || 0}</strong><small>1080p · 8秒可用账号</small></span>
            <span><strong>{leonardo?.video_ready_1080p_10s || 0}</strong><small>1080p · 10秒可用账号</small></span>
            <span><strong>{(leonardo?.video_protected_credits || 0).toLocaleString()}</strong><small>视频保护积分</small></span>
          </div>
        );
      })()}
      <div className="account-operations-bar">
        <span>账号操作</span>
        <div>
          {(providerFilter === "all" || providerFilter === "leonardo" || providerFilter === "adobe" || providerFilter === "creativefabrica") && (
			<button type="button" className="secondary" onClick={openBulkImport}>
			  <Upload />批量导入 Cookie
            </button>
          )}
          <button type="button" className="account-clean-invalid" disabled={cleanupScanning} onClick={() => void scanInvalidAccounts()}>
            {cleanupScanning ? <RefreshCw className="spin" /> : <Trash2 />}
            {cleanupScanning ? "正在扫描" : "清理失效账号"}
          </button>
        </div>
      </div>
      {cleanupError && <p className="error account-operation-error">操作失败：{cleanupError}</p>}
      <div className="accounts-toolbar account-filter-workspace">
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
              <option value="invalid">凭证失效</option>
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
              {providerOptions.map((provider) => <option value={provider.id} key={provider.id}>{provider.display_name}</option>)}
            </select>
          </label>
          {hasFilters && (
            <button type="button" className="secondary account-clear-filters" onClick={clearFilters}>
              <X size={15} />清除
            </button>
          )}
        </div>
        <QueryStatus
          fetching={accounts.isFetching}
          error={accounts.error}
          updatedAt={accounts.dataUpdatedAt}
          label={`本页 ${filteredData.length} / 共 ${total}`}
        />
      </div>
      {selectedAccounts.size > 0 && (
        <div className="account-bulk-bar" role="status">
          <span className="account-bulk-count"><CheckCircle2 />已选 <strong>{selectedAccounts.size}</strong> 个账号</span>
          <span className="account-bulk-context">选择会跨页保留</span>
          <div>
            <button type="button" className="text-action" onClick={() => setSelectedAccounts(new Map())}>清空选择</button>
            <button type="button" className="danger-button" onClick={() => { setArchiveMode("selected"); setArchiveFeedback(""); setArchiveOpen(true); }}>
              <Trash2 />归档所选
            </button>
          </div>
        </div>
      )}
      {archiveFeedback && (
        <div className="account-action-feedback">
          <CheckCircle2 />
          <span>{archiveFeedback}</span>
          <button className="icon" aria-label="关闭提示" title="关闭" onClick={() => setArchiveFeedback("")}><X /></button>
        </div>
      )}
      <div className="table account-table">
        <div className="account-row account-head">
          <span className="account-select-cell">
            <SelectionCheckbox
              checked={allPageSelected}
              indeterminate={somePageSelected}
              disabled={filteredData.length === 0}
              aria-label="选择本页账号"
              title="选择本页账号"
              onChange={(event) => togglePage(event.target.checked)}
            />
          </span>
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
        {accounts.isLoading && <TableSkeleton rows={6} columns={11} />}
        {filteredData.map((a) => (
          <div className={`account-row${selectedAccounts.has(a.id) ? " selected" : ""}`} key={a.id}>
            <span className="account-select-cell">
              <SelectionCheckbox
                checked={selectedAccounts.has(a.id)}
                aria-label={`选择 ${a.name}`}
                onChange={(event) => toggleAccount(a, event.target.checked)}
              />
            </span>
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
              <strong>{accountBalanceUnavailable(a) ? "—" : a.available_tokens.toLocaleString()}</strong>
              <small>
                {accountBalanceUnavailable(a) ? "余额未核实" : `账面 ${(
                  a.subscription_tokens +
                  a.rollover_tokens +
                  a.paid_tokens
                ).toLocaleString()}`}
              </small>
            </span>
            <span className="account-number-cell">
              <strong>{a.reserved_tokens.toLocaleString()}</strong>
			  <small>{providerCreditUnit(a.provider_id, providerOptions)}</small>
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
              <strong>{accountSessionExpiryText(a)}</strong>
              <small>{accountCredentialText(a)}</small>
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
              {a.provider_id === "leonardo" && (
                <button
                  className="icon"
                  title="检测生成权限"
                  aria-label={`检测 ${a.name} 的生成权限`}
                  disabled={permissionCheck.isPending}
                  onClick={() => {
                    setCleanupError("");
                    permissionCheck.mutate(a.id);
                  }}
                >
                  <ShieldCheck className={permissionCheck.isPending && permissionCheck.variables === a.id ? "spin" : ""} size={17} />
                </button>
              )}
              {(a.provider_id === "adobe" || a.provider_id === "creativefabrica") && (
                <button
                  className="icon"
                  title={`同步 ${a.provider_id === "creativefabrica" ? "Creative Fabrica" : "Adobe"} 模型价格`}
                  aria-label={`同步 ${a.name} 的平台模型价格`}
                  disabled={pricingSync.isPending}
                  onClick={() => {
                    setCleanupError("");
                    setArchiveFeedback("");
                    pricingSync.mutate(a.id);
                  }}
                >
                  <Coins className={pricingSync.isPending && pricingSync.variables === a.id ? "spin" : ""} size={17} />
                </button>
              )}
              <label className="icon account-cookie-import" title="导入完整 Cookie JSON" aria-label={`导入 ${a.name} 的完整 Cookie JSON`}>
                <ShieldCheck size={17} />
                <input
                  type="file"
                  accept="application/json,.json"
              onChange={(event) => {
                void onCookieFile(a, event.currentTarget.files?.[0]);
                event.currentTarget.value = "";
              }}
                />
              </label>
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
            <article className={`account-mobile-card${selectedAccounts.has(account.id) ? " selected" : ""}`} key={account.id}>
              <header>
                <SelectionCheckbox checked={selectedAccounts.has(account.id)} aria-label={`选择 ${account.name}`} onChange={(event) => toggleAccount(account, event.target.checked)} />
                <span><strong>{account.name}</strong><small>{account.provider_id} · {account.email || "未填写邮箱"}</small></span>
                <Badge tone={operational.className === "active" ? "success" : operational.className === "rate_limited" ? "warning" : "danger"}>{operational.label}</Badge>
              </header>
              <div>
                <span><small>{accountBalanceUnavailable(account) ? "余额未核实" : "净余额"}</small><strong>{accountBalanceUnavailable(account) ? "—" : account.available_tokens.toLocaleString()}</strong></span>
                <span><small>执行</small><strong>{account.active_reservations}/{account.image_concurrency}</strong></span>
                <span><small>排队</small><strong>{account.queued_tasks}/{account.queue_capacity}</strong></span>
              </div>
              <footer>
              <small>{accountSessionExpiryText(account)} · {accountCredentialText(account, true)}</small>
                <span className="account-mobile-actions">
                  {account.provider_id === "leonardo" && (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={permissionCheck.isPending}
                      onClick={() => permissionCheck.mutate(account.id)}
                    >
                      <ShieldCheck size={15} />检测权限
                    </Button>
                  )}
                  {(account.provider_id === "adobe" || account.provider_id === "creativefabrica") && (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={pricingSync.isPending}
                      onClick={() => {
                        setCleanupError("");
                        setArchiveFeedback("");
                        pricingSync.mutate(account.id);
                      }}
                    >
                      <Coins size={15} />同步价格
                    </Button>
                  )}
                  <Button variant="secondary" size="sm" onClick={() => edit(account)}><Pencil size={15} />详情</Button>
                </span>
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
      <Dialog open={archiveOpen} onOpenChange={setArchiveOpen}>
        <DialogContent className="account-archive-dialog" showClose={false}>
          <span className="account-archive-icon"><Trash2 /></span>
          <DialogTitle>{archiveMode === "invalid" ? `清理 ${selectedAccounts.size} 个凭证失效账号？` : `归档 ${selectedAccounts.size} 个账号？`}</DialogTitle>
          <DialogDescription>
            {archiveMode === "invalid"
              ? "已自动找出全部凭证失效账号。确认后会从运营池归档并停止刷新；有任务或积分预留的账号会跳过。"
              : "账号会从运营池移除并停止会话刷新，已有任务和财务记录仍会保留。正在执行、排队或持有积分预留的账号会自动跳过。"}
          </DialogDescription>
          <div className="account-archive-list">
            {[...selectedAccounts.values()].map((account) => (
              <span key={account.id}><strong>{account.name}</strong><small>{account.email || account.provider_id}</small></span>
            ))}
          </div>
          {archive.error && <p className="error">归档失败：{archive.error.message}</p>}
          <footer>
            <button type="button" className="secondary" onClick={() => setArchiveOpen(false)}>取消</button>
            <button type="button" className="danger-button" disabled={archive.isPending} onClick={() => archive.mutate([...selectedAccounts.keys()])}>
              <Trash2 />{archive.isPending ? "正在归档" : "确认归档"}
            </button>
          </footer>
        </DialogContent>
      </Dialog>
      <Dialog open={bulkImportOpen} onOpenChange={(open) => { if (!bulkImportBusy) setBulkImportOpen(open); }}>
        <DialogContent className="account-bulk-import-dialog" showClose={!bulkImportBusy}>
          <div className="account-bulk-import-heading">
            <span><FileJson /></span>
			<div>
			  <DialogTitle>批量导入完整 Cookie</DialogTitle>
			  <DialogDescription>每个 JSON 文件创建一个所选平台账号，账号名称可在提交前修改。</DialogDescription>
			</div>
		  </div>
		  <div className="segmented compact" role="group" aria-label="批量导入平台">
			{cookieProviderOptions.map((provider) => {
			  const providerID = provider.id;
			  return (
			  <button
				type="button"
				key={providerID}
				className={bulkImportProvider === providerID ? "active" : ""}
				disabled={bulkImportBusy}
				 onClick={() => {
				   setBulkImportProvider(providerID);
				  setBulkImportItems([]);
				  setBulkImportSummary("");
				   setBulkImportConfig((current) => ({ ...current, imageConcurrency: Math.min(providerConcurrencyMaximum(providerID), current.imageConcurrency || 5) }));
				 }}
			  >
				 {provider.display_name}
			  </button>
			  );
			})}
		  </div>
          <label
            className="account-bulk-cookie-drop"
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault();
              if (!bulkImportBusy) void addCookieFiles(Array.from(event.dataTransfer.files));
            }}
          >
            <input
              type="file"
              accept="application/json,.json"
              multiple
              disabled={bulkImportBusy || bulkImportItems.length >= MAX_BULK_COOKIE_FILES}
              onChange={(event) => {
                void addCookieFiles(Array.from(event.currentTarget.files || []));
                event.currentTarget.value = "";
              }}
            />
            <Upload />
            <span><strong>选择或拖入多个 Cookie JSON</strong><small>单次最多 {MAX_BULK_COOKIE_FILES} 个文件，每个文件最大 512 KB</small></span>
            <span>{bulkImportItems.length}/{MAX_BULK_COOKIE_FILES}</span>
          </label>
		  <div className="account-bulk-import-config">
			<span><strong>{providerDefinition(bulkImportProvider, cookieProviderOptions.find((provider) => provider.id === bulkImportProvider)).shortName}</strong><small>平台</small></span>
            <label>并发槽位
              <input
                type="number"
                min={1}
				 max={providerConcurrencyMaximum(bulkImportProvider)}
                value={bulkImportConfig.imageConcurrency}
                disabled={bulkImportBusy}
                onChange={(event) => setBulkImportConfig((current) => ({ ...current, imageConcurrency: Number(event.target.value) || 1 }))}
              />
            </label>
            <label>等待队列
              <input
                type="number"
                min={1}
                max={1000}
                value={bulkImportConfig.queueCapacity}
                disabled={bulkImportBusy}
                onChange={(event) => setBulkImportConfig((current) => ({ ...current, queueCapacity: Number(event.target.value) || 1 }))}
              />
            </label>
			{bulkImportProvider === "leonardo" && <label>Worker 组
              <input
                maxLength={100}
                value={bulkImportConfig.workerGroup}
                disabled={bulkImportBusy}
                onChange={(event) => setBulkImportConfig((current) => ({ ...current, workerGroup: event.target.value }))}
              />
			</label>}
          </div>
          {bulkImportItems.length > 0 && (
            <div className="account-bulk-import-list">
              <div className="account-bulk-import-list-head"><span>文件</span><span>账号名称</span><span>状态</span><span></span></div>
              {bulkImportItems.map((item) => (
                <div className="account-bulk-import-item" key={item.id}>
                  <span title={item.fileName}><FileJson /><small>{item.fileName}</small></span>
                  <input
                    value={item.name}
                    maxLength={200}
                    aria-label={`${item.fileName} 的账号名称`}
                    disabled={bulkImportBusy || item.status === "submitted"}
                    onChange={(event) => setBulkImportItems((current) => current.map((entry) => (
                      entry.id === item.id ? { ...entry, name: event.target.value, error: entry.status === "failed" ? "" : entry.error } : entry
                    )))}
                  />
                  <span className={`account-bulk-import-status ${item.status}`}>
                    {item.status === "importing" && <RefreshCw className="spin" />}
                    {item.status === "submitted" && <CheckCircle2 />}
                    {item.status === "invalid" || item.status === "failed" ? <X /> : null}
                    <small title={item.error || undefined}>
					  {item.status === "ready" ? "待导入" : item.status === "invalid" ? item.error : item.status === "importing" ? "提交中" : item.status === "submitted" ? (bulkImportProvider === "adobe" ? "已导入" : "待验证") : item.error}
                    </small>
                  </span>
                  <button
                    type="button"
                    className="icon"
                    title="移除"
                    aria-label={`移除 ${item.fileName}`}
                    disabled={bulkImportBusy}
                    onClick={() => setBulkImportItems((current) => current.filter((entry) => entry.id !== item.id))}
                  ><Trash2 /></button>
                </div>
              ))}
            </div>
          )}
          {bulkImportSummary && <p className="account-bulk-import-summary">{bulkImportSummary}</p>}
			 {!bulkImportConfigValid && <p className="error">并发范围为 1–{providerConcurrencyMaximum(bulkImportProvider)}，队列范围为 1–1000{bulkImportProvider === "leonardo" ? "，Worker 组只允许字母、数字、点、下划线和连字符" : ""}。</p>}
          <footer>
            <button type="button" className="secondary" disabled={bulkImportBusy} onClick={() => setBulkImportOpen(false)}>关闭</button>
            <button type="button" disabled={bulkImportBusy || importableCount === 0 || !bulkImportConfigValid} onClick={() => void importCookieAccounts()}>
              {bulkImportBusy ? <RefreshCw className="spin" /> : <Upload />}
              {bulkImportBusy ? "正在导入" : `导入 ${importableCount} 个账号`}
            </button>
          </footer>
        </DialogContent>
      </Dialog>
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
  type AccountAuthMethod = "complete_cookie" | "complete_cookie_password" | "access_token" | "email_password";
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => api<Provider[]>("/admin/api/providers"),
  });
  const [v, setV] = useState(() => ({
    provider_id: account?.provider_id || "leonardo",
    name: account?.name || "",
    email: account?.email || "",
    password: "",
    otp: "",
    access_token: "",
    cookie_json: null as unknown,
    cookie_file_name: "",
    proxy_url: account?.proxy_url || "",
    image_concurrency: account?.image_concurrency || 5,
    queue_capacity: account?.queue_capacity || 40,
    routing_role: account?.routing_role || "general",
    protected_tokens: account?.protected_tokens || 0,
    video_reserved_slots: account?.video_reserved_slots || 0,
    browser_worker_group: account?.browser_worker_group || "default",
    auth_method: (account?.provider_id === "adobe"
      ? "access_token"
      : account?.has_login_credentials
        ? "complete_cookie_password"
        : "complete_cookie") as AccountAuthMethod,
  }));
  const selected = providers.data?.find(
    (provider) => provider.id === v.provider_id,
  );
  const concurrencyMaximum = providerConcurrencyMaximum(account?.provider_id || v.provider_id);
  const isCookieMethod = v.auth_method === "complete_cookie" || v.auth_method === "complete_cookie_password";
  const isPasswordMethod = v.provider_id === "creativefabrica" && v.auth_method === "email_password";
  const requiresPassword = !isEditing && (
    (v.provider_id === "leonardo" && v.auth_method === "complete_cookie_password") ||
    (v.provider_id === "creativefabrica" && (v.auth_method === "complete_cookie_password" || v.auth_method === "email_password"))
  );
  const authLabel = (authType: string) => {
    if (authType === "browser_session") return "完整 Cookie";
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
                provider_id: v.provider_id,
                name: v.name.trim(),
                email: v.email.trim(),
                ...((v.provider_id === "leonardo" || v.provider_id === "creativefabrica") && v.password ? { password: v.password } : {}),
                ...(v.provider_id === "creativefabrica" && v.otp.trim() ? { otp: v.otp.trim() } : {}),
                ...(v.provider_id === "adobe" && v.auth_method === "access_token"
                  ? { access_token: v.access_token.trim() }
                  : isCookieMethod
                    ? { cookie_json: v.cookie_json }
                    : {}),
                proxy_url: v.proxy_url.trim(),
                image_concurrency: v.image_concurrency,
                queue_capacity: v.queue_capacity,
                routing_role: v.routing_role,
                protected_tokens: v.protected_tokens,
                video_reserved_slots: v.video_reserved_slots,
                browser_worker_group: v.browser_worker_group.trim(),
              },
        ),
      }),
    onSuccess: done,
  });
  const canSubmit = Boolean(
    (isEditing || selected) &&
      v.name.trim() &&
       (isEditing || (v.provider_id === "adobe" && v.auth_method === "access_token" ? v.access_token.trim() : isCookieMethod ? v.cookie_json : true)) &&
       (!requiresPassword || (v.email.trim() && v.password)) &&
       (!v.password || v.email.trim()) &&
      (v.provider_id !== "leonardo" || /^[A-Za-z0-9._-]{1,100}$/.test(v.browser_worker_group.trim())) &&
      v.image_concurrency >= 1 &&
      v.image_concurrency <= concurrencyMaximum &&
      v.queue_capacity >= 1 &&
      v.queue_capacity <= 1000 &&
      v.protected_tokens >= 0 &&
      v.video_reserved_slots >= 0 &&
      v.video_reserved_slots <= v.image_concurrency,
  );
  const setConcurrency = (next: number) => {
    const concurrency = Math.min(concurrencyMaximum, Math.max(1, next));
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

        <section className="account-dialog-section account-platform-section">
          <div className="account-section-heading">
            <Server />
            <div>
              <strong>选择平台</strong>
              <small>
                {isEditing
                  ? "账号所属平台创建后保持不变。"
                  : "每个平台使用独立凭据和积分，不会混用账号池。"}
              </small>
            </div>
          </div>
          <fieldset className="provider-choice-fieldset">
            <legend className="sr-only">平台</legend>
            <div className="provider-choice-grid">
                {(providers.data || []).map((provider) => {
                  const isSelected = provider.id === v.provider_id;
                  const isAvailable = provider.enabled && provider.adapter_registered !== false;
                  return (
                    <label
                      key={provider.id}
                      className={`provider-choice${isSelected ? " selected" : ""}${isAvailable ? "" : " disabled"}`}
                    >
                      <input
                        type="radio"
                        name="provider_id"
                        value={provider.id}
                        checked={isSelected}
                        disabled={isEditing || !isAvailable}
                        onChange={() =>
                          setV({
                            ...v,
                            provider_id: provider.id,
                            auth_method: provider.id === "adobe" ? "access_token" : "complete_cookie",
                            access_token: "",
                            otp: "",
                            password: "",
                            cookie_json: null,
                            cookie_file_name: "",
                            image_concurrency: providerConcurrencyMaximum(provider.id),
                          })
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
                        {isAvailable
                          ? isSelected
                            ? "已选"
                            : ""
                          : "未开放"}
                      </span>
                    </label>
                  );
                })}
            </div>
          </fieldset>
          {providers.isError && <p className="error">渠道配置加载失败，请重试。</p>}
          {!providers.isError && !selected && (
            <p className="error">正在加载渠道配置…</p>
          )}
        </section>

        {!isEditing && selected?.id === "leonardo" && (
          <section className="account-dialog-section account-auth-section">
            <div className="account-section-heading account-auth-heading">
              <KeyRound />
              <div>
                <strong>选择认证方式</strong>
                <small>完整 Cookie 是主凭据；账号密码只在 Cookie 恢复失败时作为辅助。</small>
              </div>
              <button
                type="button"
                className="secondary account-login-button"
                onClick={() => window.open("https://app.leonardo.ai/auth/login", "_blank", "noopener,noreferrer")}
              >
                <ExternalLink />打开登录页
              </button>
            </div>
            <div className="account-auth-methods" role="radiogroup" aria-label="Leonardo 认证方式">
              <button
                type="button"
                role="radio"
                aria-checked={v.auth_method === "complete_cookie"}
                className={`account-auth-method${v.auth_method === "complete_cookie" ? " selected" : ""}`}
                onClick={() => setV({ ...v, auth_method: "complete_cookie", password: "" })}
              >
                <span className="account-auth-icon"><FileJson /></span>
                <span><strong>完整 Cookie</strong><small>导入浏览器 Cookie JSON，直接恢复平台会话</small></span>
                <span className="account-auth-tag">推荐</span>
              </button>
              <button
                type="button"
                role="radio"
                aria-checked={v.auth_method === "complete_cookie_password"}
                className={`account-auth-method${v.auth_method === "complete_cookie_password" ? " selected" : ""}`}
                onClick={() => setV({ ...v, auth_method: "complete_cookie_password" })}
              >
                <span className="account-auth-icon"><ShieldCheck /></span>
                <span><strong>Cookie + 密码恢复</strong><small>保留邮箱密码，必要时由浏览器重新登录</small></span>
              </button>
            </div>
          </section>
        )}

        {!isEditing && selected?.id === "adobe" && (
          <section className="account-dialog-section account-auth-section">
            <div className="account-section-heading account-auth-heading">
              <KeyRound />
              <div>
                <strong>选择认证方式</strong>
                <small>Access Token 可立即使用；完整 Cookie 会在 AT 到期前自动续期。</small>
              </div>
              <button
                type="button"
                className="secondary account-login-button"
                onClick={() => window.open("https://firefly.adobe.com/", "_blank", "noopener,noreferrer")}
              >
                <ExternalLink />打开 Firefly
              </button>
            </div>
            <div className="account-auth-methods" role="radiogroup" aria-label="Adobe 认证方式">
              <button
                type="button"
                role="radio"
                aria-checked={v.auth_method === "access_token"}
                className={`account-auth-method${v.auth_method === "access_token" ? " selected" : ""}`}
                onClick={() => setV({ ...v, auth_method: "access_token", cookie_json: null, cookie_file_name: "" })}
              >
                <span className="account-auth-icon"><KeyRound /></span>
                <span><strong>Access Token</strong><small>直接校验 Profile、积分和模型价格，有效期通常约 24 小时</small></span>
                <span className="account-auth-tag">立即接入</span>
              </button>
              <button
                type="button"
                role="radio"
                aria-checked={v.auth_method === "complete_cookie"}
                className={`account-auth-method${v.auth_method === "complete_cookie" ? " selected" : ""}`}
                onClick={() => setV({ ...v, auth_method: "complete_cookie", access_token: "" })}
              >
                <span className="account-auth-icon"><FileJson /></span>
                <span><strong>完整 Cookie</strong><small>加密保存 Cookie JSON，由 Go worker 自动换取新 AT</small></span>
                <span className="account-auth-tag">长期推荐</span>
              </button>
            </div>
          </section>
        )}

        {!isEditing && selected?.id === "creativefabrica" && (
          <section className="account-dialog-section account-auth-section">
            <div className="account-section-heading account-auth-heading">
              <KeyRound />
              <div>
                <strong>选择认证方式</strong>
                <small>完整 Cookie 优先；也可以使用邮箱密码登录，遇到邮箱验证时填写一次性验证码。</small>
              </div>
              <button
                type="button"
                className="secondary account-login-button"
                onClick={() => window.open("https://studio.creativefabrica.com/", "_blank", "noopener,noreferrer")}
              >
                <ExternalLink />打开 Studio
              </button>
            </div>
            <div className="account-auth-methods" role="radiogroup" aria-label="Creative Fabrica 认证方式">
              <button
                type="button"
                role="radio"
                aria-checked={v.auth_method === "complete_cookie"}
                className={`account-auth-method${v.auth_method === "complete_cookie" ? " selected" : ""}`}
                onClick={() => setV({ ...v, auth_method: "complete_cookie", password: "", otp: "" })}
              >
                <span className="account-auth-icon"><FileJson /></span>
                <span><strong>完整 Cookie</strong><small>导入 Studio Cookie JSON，并保存 ST、AT/RPC 等派生凭据</small></span>
                <span className="account-auth-tag">推荐</span>
              </button>
              <button
                type="button"
                role="radio"
                aria-checked={v.auth_method === "complete_cookie_password"}
                className={`account-auth-method${v.auth_method === "complete_cookie_password" ? " selected" : ""}`}
                onClick={() => setV({ ...v, auth_method: "complete_cookie_password", otp: "" })}
              >
                <span className="account-auth-icon"><ShieldCheck /></span>
                <span><strong>Cookie + 密码恢复</strong><small>保留邮箱密码，Cookie 失效后可回退到自动登录</small></span>
              </button>
              <button
                type="button"
                role="radio"
                aria-checked={v.auth_method === "email_password"}
                className={`account-auth-method${v.auth_method === "email_password" ? " selected" : ""}`}
                onClick={() => setV({ ...v, auth_method: "email_password", cookie_json: null, cookie_file_name: "", access_token: "" })}
              >
                <span className="account-auth-icon"><KeyRound /></span>
                <span><strong>邮箱密码登录</strong><small>首次登录通过邮箱验证码建立 Studio 会话</small></span>
              </button>
            </div>
          </section>
        )}

        {(isEditing || selected?.id === "leonardo" || selected?.id === "adobe" || selected?.id === "creativefabrica") && (
          <section className="account-dialog-section">
            <div className="account-section-heading">
              <ShieldCheck />
              <div>
                <strong>{isEditing ? "账号信息" : "填写账号与凭据"}</strong>
                <small>
                   {isEditing
                     ? account?.provider_id === "adobe"
                       ? "Adobe 凭据通过列表中的 Cookie 导入操作更新；这里仅调整账号和路由配置。"
                       : `${account?.has_login_credentials ? "自动登录已配置；留空密码保持不变。" : "补录密码后可在 Cookie 失效时自动重新登录。"}`
                   : selected?.id === "adobe" && v.auth_method === "access_token"
                     ? "AT 只会加密保存；创建时同步读取 Adobe Profile、余额与 BKS 价格。"
                   : selected?.id === "adobe"
                     ? "上传 Adobe 域名的完整 Cookie JSON，创建时直接换取并验证 AT。"
                   : selected?.id === "creativefabrica" && isPasswordMethod
                     ? "邮箱和密码只在服务端加密保存；如果首次登录触发邮箱验证，填写验证码后重新提交。"
                   : selected?.id === "creativefabrica"
                     ? "上传 Creative Fabrica 域名的完整 Cookie JSON；服务端会换取并保存 ST、AT/RPC 等派生凭据。"
                   : v.auth_method === "complete_cookie_password"
                   ? "Cookie JSON 与辅助登录密码会分别加密保存，不会在列表中回显。"
                   : "上传完整 Cookie JSON；账号名称用于运营识别。"}
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
                   placeholder={selected?.id === "adobe" ? "例如：adobe-main" : selected?.id === "creativefabrica" ? "例如：creativefabrica-main" : "例如：leonardo-main"}
                  value={v.name}
                  onChange={(e) => setV({ ...v, name: e.target.value })}
                />
              </label>
              <label>
                邮箱
                <input
                  type="email"
                   required={!isEditing && requiresPassword}
                  autoComplete="off"
                  placeholder="用于识别账号，可选"
                  value={v.email}
                  onChange={(e) => setV({ ...v, email: e.target.value })}
                />
              </label>
              {(isEditing ? (account?.provider_id === "leonardo" || account?.provider_id === "creativefabrica") : requiresPassword) && (
                <label>
                  {isEditing ? "新登录密码" : "辅助登录密码"}
                  <input
                    type="password"
                     required={!isEditing && requiresPassword}
                    autoComplete="new-password"
                     placeholder={isEditing ? "留空表示不修改" : v.provider_id === "creativefabrica" ? "用于邮箱密码登录或 Cookie 失效后的恢复" : "仅用于 Cookie 失效后的浏览器恢复"}
                    value={v.password}
                    onChange={(e) => setV({ ...v, password: e.target.value })}
                  />
                </label>
              )}
              {!isEditing && selected?.id === "creativefabrica" && v.auth_method === "email_password" && (
                <label>
                  邮箱验证码（可选）
                  <input
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    maxLength={12}
                    placeholder="首次提交未触发验证时留空"
                    value={v.otp}
                    onChange={(event) => setV({ ...v, otp: event.target.value })}
                  />
                  <small className="field-help">如果提交后返回“需要邮箱验证码”，从账号邮箱读取验证码，再填入后重新提交。</small>
                </label>
              )}
            </div>
            {!isEditing && selected?.id === "adobe" && v.auth_method === "access_token" && (
              <label className="account-token-field">
                Access Token
                <textarea
                  required
                  rows={5}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="粘贴 Adobe access_token（JWT）"
                  value={v.access_token}
                  onChange={(event) => setV({ ...v, access_token: event.target.value })}
                />
                <small className="field-help">令牌不会回显；创建完成后只保存 AES-GCM 密文。</small>
              </label>
            )}
            {!isEditing && isCookieMethod && (
              <label className={`account-cookie-drop${v.cookie_json ? " ready" : ""}`}>
                <input
                  required
                  type="file"
                  accept="application/json,.json"
                  onChange={async (event) => {
                    const file = event.currentTarget.files?.[0];
                    if (!file) return;
                    try {
                      const cookieJSON: unknown = JSON.parse(await file.text());
                      setV({ ...v, cookie_json: cookieJSON, cookie_file_name: file.name });
                    } catch {
                      setV({ ...v, cookie_json: null, cookie_file_name: "" });
                    }
                  }}
                />
                <span className="account-cookie-drop-icon">{v.cookie_json ? <CheckCircle2 /> : <Upload />}</span>
                <span>
                  <strong>{v.cookie_file_name || "选择完整 Cookie JSON"}</strong>
                  <small>
                    {v.cookie_json
                      ? selected?.id === "adobe"
                        ? "文件已读取，提交后会直接换取并验证 AT"
                        : selected?.id === "creativefabrica"
                          ? "文件已读取，提交后会换取并验证 ST、AT/RPC"
                          : "文件已读取，提交后会在浏览器中验证"
                      : `Chrome/Patchright 导出的 ${selected?.id === "adobe" ? "Adobe" : selected?.id === "creativefabrica" ? "Creative Fabrica" : "Leonardo"} Cookie 数组，支持 .json`}
                  </small>
                </span>
                <span className="account-cookie-drop-action">{v.cookie_json ? "重新选择" : "选择文件"}</span>
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
                  max={concurrencyMaximum}
                  value={v.image_concurrency}
                  onChange={(e) => setConcurrency(Number(e.target.value) || 1)}
                  aria-label="账号最大并发"
                />
                <button
                  type="button"
                  onClick={() => setConcurrency(v.image_concurrency + 1)}
                  disabled={v.image_concurrency >= concurrencyMaximum}
                  aria-label="增加账号并发"
                  title="增加并发"
                >
                  <Plus />
                </button>
              </div>
              <small className="field-help">{selected?.id === "adobe" || account?.provider_id === "adobe" ? "Adobe 图片和视频共用槽位；支持 1–100，用于验证真实上游并发能力。" : "图片、视频、音频共用该账号的生成槽位；范围 1–5，与 BASIC 实测上限一致。"}</small>
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
                placeholder={selected?.id === "adobe" || account?.provider_id === "adobe" ? "http:// 或 https://；留空使用服务器出口" : "http://、https:// 或 socks5://；留空使用服务器出口"}
                value={v.proxy_url}
                onChange={(e) => setV({ ...v, proxy_url: e.target.value })}
              />
            </label>
            {(selected?.id === "leonardo" || account?.provider_id === "leonardo") && <label>
              会话 Worker 组
              <input
                required
                maxLength={100}
                placeholder="default"
                value={v.browser_worker_group}
                onChange={(e) => setV({ ...v, browser_worker_group: e.target.value })}
              />
              <small className="field-help">账号浏览器配置固定归属该组；多服务器部署时按代理出口或节点分组。</small>
            </label>}
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
              : selected?.id === "adobe"
                ? "创建时验证 Profile、积分余额和实时价格，不会提交生成任务"
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
