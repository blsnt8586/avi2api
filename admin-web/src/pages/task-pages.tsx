import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Activity, AlertTriangle, CheckCircle2, Coins, Eye, Gauge, RefreshCw, Search, X } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  Pagination as UIPagination,
  QueryStatus,
  TableSkeleton,
  Tabs,
  TabsList,
  TabsTrigger,
} from "../components/ui";
import { CopyCode } from "../components/copy-code";
import { Metric } from "../components/metric";
import { ProviderBadge, ProviderSwitcher } from "../components/provider-switcher";
import type { Provider, Task, TasksPage } from "../shared/types";
import { api } from "../shared/api";
import {
  appendLocalDateRange,
  formatTokens,
  reservationText,
  statusText,
  taskCreditUnit,
  tokenCost,
} from "../shared/status";
import { useDebouncedValue, useMediaQuery } from "../shared/cache";
import { providerCreditUnit, providerDisplayName, providerModelGroups } from "../shared/providers";

function TaskRow({
  task,
  onSelect,
  style,
  rowRef,
  virtualIndex,
  providers,
}: {
  task: Task;
  onSelect?: (task: Task) => void;
  style?: React.CSSProperties;
  rowRef?: (node: HTMLDivElement | null) => void;
  virtualIndex?: number;
  providers: Provider[];
}) {
  const t = task;
  return (
        <div className="task-line" key={t.id} style={style} ref={rowRef} data-index={virtualIndex}>
          <span>
            <strong
              className="task-prompt"
              title={t.prompt || "未命名任务"}
            >
              {t.prompt || "未命名任务"}
            </strong>
            <small>{new Date(t.created_at).toLocaleString("zh-CN")}</small>
          </span>
          <span>
            <ProviderBadge providerID={t.provider_id} providers={providers} />
            <b className="kind-badge">
              {t.kind === "video"
                ? "VIDEO"
                : t.kind === "audio"
                  ? "AUDIO"
                  : "IMAGE"}
            </b>
            <small>{t.model}</small>
          </span>
          <span>{taskCreditUnit(t, providers)} {tokenCost(t)}</span>
          <span>
            <i className={`status ${t.status}`}></i>
            {statusText(t.status)}
            {t.status === "queued" && t.queue_position && (
              <small>第 {t.queue_position} 位</small>
            )}
          </span>
          <span
            className={`task-summary ${t.status === "failed" ? "failed" : ""}`}
          >
            <strong>{taskResultSummary(t)}</strong>
            {t.error_details?.provider_error_code && (
              <small>{t.error_details.provider_error_code}</small>
            )}
          </span>
          <span>
            {onSelect && (
              <button
                className="icon"
                title="查看任务详情"
                aria-label={`查看任务 ${t.id} 详情`}
                onClick={() => onSelect(t)}
              >
                <Eye />
              </button>
            )}
          </span>
        </div>
  );
}

function TaskTable({
  data,
  onSelect,
  loading = false,
  providers,
}: {
  data: Task[];
  onSelect?: (task: Task) => void;
  loading?: boolean;
  providers: Provider[];
}) {
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const compact = useMediaQuery("(max-width: 640px)");
  const virtualized = data.length >= 100 && !compact;
  const rowVirtualizer = useVirtualizer({
    count: virtualized ? data.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 76,
    overscan: 8,
  });
  return (
    <div className={`task-table${virtualized ? " task-table-virtualized" : ""}`}>
      <div className="task-line task-head">
        <span>任务</span>
        <span>平台 / 模型</span>
        <span>结算 / 预留</span>
        <span>状态</span>
        <span>结果说明</span>
        <span></span>
      </div>
      {loading && data.length === 0 && <TableSkeleton rows={6} columns={6} />}
      {virtualized ? (
        <div ref={scrollRef} className="task-virtual-scroll">
          <div className="task-virtual-canvas" style={{ height: rowVirtualizer.getTotalSize() }}>
            {rowVirtualizer.getVirtualItems().map((item) => (
              <TaskRow
                key={data[item.index].id}
                task={data[item.index]}
                onSelect={onSelect}
                rowRef={rowVirtualizer.measureElement}
                virtualIndex={item.index}
                providers={providers}
                style={{ transform: `translateY(${item.start}px)` }}
              />
            ))}
          </div>
        </div>
      ) : (
        data.map((task) => <TaskRow key={task.id} task={task} onSelect={onSelect} providers={providers} />)
      )}
    </div>
  );
}

const activeTaskStatuses = [
  "queued",
  "reserving",
  "uploading",
  "submitted",
  "polling",
];

const taskStatusFilters = [
  ["all", "全部"],
  ["queued", "排队中"],
  ["active", "处理中"],
  ["succeeded", "成功"],
  ["failed", "失败"],
  ["cancelled", "已取消"],
  ["submission_uncertain", "提交不确定"],
] as const;

type UncertainTaskCleanupResponse = {
  resolved: number;
  skipped: number;
  results: Array<{
    id: string;
    resolved: boolean;
    error_code?: string;
    error_message?: string;
  }>;
};

type UncertainTaskCleanupBatch = UncertainTaskCleanupResponse & {
  requestFailed: number;
};

const TASK_CLEANUP_BATCH_SIZE = 100;

function taskIDChunks(ids: string[]) {
  const chunks: string[][] = [];
  for (let index = 0; index < ids.length; index += TASK_CLEANUP_BATCH_SIZE) {
    chunks.push(ids.slice(index, index + TASK_CLEANUP_BATCH_SIZE));
  }
  return chunks;
}

export function Tasks() {
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleanupScanning, setCleanupScanning] = useState(false);
  const [cleanupTasks, setCleanupTasks] = useState<Task[]>([]);
  const [cleanupFeedback, setCleanupFeedback] = useState("");
  const [cleanupError, setCleanupError] = useState("");
  const [searchParams, setSearchParams] = useSearchParams();
  const statusFilter = searchParams.get("status") || "all";
  const providerFilter = searchParams.get("provider") || "all";
  const kindFilter = searchParams.get("kind") || "all";
  const modelFilter = searchParams.get("model") || "all";
  const dateFrom = searchParams.get("from") || "";
  const dateTo = searchParams.get("to") || "";
  const search = searchParams.get("search") || "";
  const deferredSearch = useDebouncedValue(search.trim().toLowerCase());
  const setFilter = (key: string, value: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (!value || value === "all") next.delete(key);
      else next.set(key, value);
      return next;
    }, { replace: true });
    setPage(1);
    setSelectedID(null);
  };
  const tasks = useQuery({
    queryKey: ["tasks-page", page, pageSize, providerFilter, statusFilter, kindFilter, modelFilter, dateFrom, dateTo, deferredSearch],
    queryFn: () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
      if (statusFilter !== "all") params.set("status", statusFilter);
      if (providerFilter !== "all") params.set("provider", providerFilter);
      if (kindFilter !== "all") params.set("kind", kindFilter);
      if (modelFilter !== "all") params.set("model", modelFilter);
      if (deferredSearch) params.set("search", deferredSearch);
      appendLocalDateRange(params, dateFrom, dateTo);
      return api<TasksPage>(`/admin/api/tasks?${params}`);
    },
    placeholderData: (previous) => previous,
    refetchInterval: (query) => {
      const current = query.state.data as TasksPage | undefined;
      return current?.data?.some((task) => activeTaskStatuses.includes(task.status))
        ? 3_000
        : false;
    },
  });
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => api<Provider[]>("/admin/api/providers"),
  });
  const data = tasks.data?.data || [];
	const keyModelGroups = providerModelGroups(providers.data || []);
	const modelOptions = [...new Set(keyModelGroups
    .filter((group) => providerFilter === "all" || group.provider_id === providerFilter)
    .flatMap((group) => group.models))].sort();
  const filteredData = data;
  const total = tasks.data?.total || 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const selected = data.find((task) => task.id === selectedID) || null;
  const cleanup = useMutation({
    mutationFn: async (ids: string[]): Promise<UncertainTaskCleanupBatch> => {
      const combined: UncertainTaskCleanupBatch = {
        resolved: 0,
        skipped: 0,
        requestFailed: 0,
        results: [],
      };
      for (const batch of taskIDChunks(ids)) {
        try {
          const result = await api<UncertainTaskCleanupResponse>(
            "/admin/api/tasks/submission-uncertain/fail",
            { method: "POST", body: JSON.stringify({ ids: batch }) },
          );
          combined.resolved += result.resolved;
          combined.skipped += result.skipped;
          combined.results.push(...result.results);
        } catch {
          combined.skipped += batch.length;
          combined.requestFailed += batch.length;
        }
      }
      return combined;
    },
    onSuccess: (result) => {
      setCleanupOpen(false);
      setCleanupTasks([]);
      setSelectedID(null);
      setCleanupFeedback(
        result.requestFailed
          ? `已将 ${result.resolved} 个异常任务标记为失败；${result.requestFailed} 个请求失败，可重新扫描后重试。`
          : result.skipped
            ? `已将 ${result.resolved} 个异常任务标记为失败；${result.skipped} 个任务状态已变化，已跳过。`
            : `已将 ${result.resolved} 个异常任务标记为失败，并释放仍持有的平台余额预留。`,
      );
      void client.invalidateQueries({ queryKey: ["tasks-page"] });
      void client.invalidateQueries({ queryKey: ["overview"] });
      void client.invalidateQueries({ queryKey: ["accounts-page"] });
      void client.invalidateQueries({ queryKey: ["system-capacity"] });
    },
  });
  const scanUncertainTasks = async () => {
    setCleanupScanning(true);
    setCleanupError("");
    setCleanupFeedback("");
    try {
      const found = new Map<string, Task>();
      let currentPage = 1;
      let expectedTotal = 0;
      do {
        const result = await api<TasksPage>(
          `/admin/api/tasks?page=${currentPage}&page_size=${TASK_CLEANUP_BATCH_SIZE}&status=submission_uncertain${providerFilter === "all" ? "" : `&provider=${encodeURIComponent(providerFilter)}`}`,
        );
        expectedTotal = result.total;
        result.data.forEach((task) => found.set(task.id, task));
        if (result.data.length === 0) break;
        currentPage += 1;
      } while (found.size < expectedTotal);
      const tasksToClean = [...found.values()];
      if (tasksToClean.length === 0) {
        setCleanupFeedback("当前没有提交状态不确定的异常任务。");
        return;
      }
      setCleanupTasks(tasksToClean);
      setCleanupOpen(true);
    } catch (error) {
      setCleanupError(error instanceof Error ? error.message : "扫描异常任务失败");
    } finally {
      setCleanupScanning(false);
    }
  };
  React.useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);
  return (
    <section>
      <div className="stats">
        <Metric icon={<Activity />} label={providerFilter === "all" ? "任务总数" : `${providerDisplayName(providerFilter, providers.data)}任务`} value={total} />
        <Metric
          icon={<Gauge />}
          label="本页进行中"
          value={
            filteredData.filter((t) => activeTaskStatuses.includes(t.status)).length
          }
        />
        <Metric
          icon={<AlertTriangle />}
          label="本页失败"
          value={filteredData.filter((t) => t.status === "failed").length}
        />
      </div>
      <div className="ops-panel task-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">Execution Log</span>
            <h2>生成任务</h2>
          </div>
          <div className="task-panel-actions">
            <button
              type="button"
              className="secondary task-cleanup-button"
              disabled={cleanupScanning}
              onClick={() => void scanUncertainTasks()}
            >
              {cleanupScanning ? <RefreshCw className="spin" /> : <AlertTriangle />}
              {cleanupScanning ? "正在扫描" : "清理异常任务"}
            </button>
            <QueryStatus
              fetching={tasks.isFetching}
              error={tasks.error}
              updatedAt={tasks.dataUpdatedAt}
              label={`第 ${Math.min(page, totalPages)} / ${totalPages} 页`}
            />
          </div>
        </div>
        {cleanupFeedback && (
          <div className="task-cleanup-feedback">
            <CheckCircle2 />
            <span>{cleanupFeedback}</span>
            <button type="button" className="icon" aria-label="关闭提示" onClick={() => setCleanupFeedback("")}><X /></button>
          </div>
        )}
        {cleanupError && <p className="error task-cleanup-error">扫描失败：{cleanupError}</p>}
        <div className="task-filter-workspace">
          <ProviderSwitcher providers={providers.data || []} value={providerFilter} includeAll includeDisabled compact onChange={(value) => setFilter("provider", value)} />
          <Tabs value={statusFilter} onValueChange={(value) => setFilter("status", value)}>
            <TabsList className="task-status-tabs">
              {taskStatusFilters.map(([value, label]) => (
                <TabsTrigger value={value} key={value}>{label}</TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <div className="task-filter-row">
            <label className="search-box task-search-box">
              <Search size={17} />
              <input value={search} onChange={(event) => setFilter("search", event.target.value)} placeholder="搜索任务 ID、提示词或模型" />
            </label>
            <label>类型
              <select value={kindFilter} onChange={(event) => setFilter("kind", event.target.value)}>
                <option value="all">全部</option>
                <option value="image">图像</option>
                <option value="video">视频</option>
                <option value="audio">音频</option>
              </select>
            </label>
            <label>模型
              <select value={modelFilter} onChange={(event) => setFilter("model", event.target.value)}>
                <option value="all">全部</option>
                {modelOptions.map((model) => <option value={model} key={model}>{model}</option>)}
              </select>
            </label>
            <label>开始日期
              <input type="date" value={dateFrom} onChange={(event) => setFilter("from", event.target.value)} />
            </label>
            <label>结束日期
              <input type="date" value={dateTo} onChange={(event) => setFilter("to", event.target.value)} />
            </label>
            <span className="task-filter-count">共 {total} 个结果</span>
          </div>
        </div>
        {tasks.error && <p className="error task-list-error">{tasks.error.message}</p>}
        <TaskTable data={filteredData} loading={tasks.isLoading} providers={providers.data || []} onSelect={(task) => setSelectedID(task.id)} />
        {!tasks.isLoading && filteredData.length === 0 && (
          <div className="empty-state compact-empty">
            <Activity />
            <strong>{data.length ? "本页没有匹配任务" : "暂无生成任务"}</strong>
            <span>{data.length ? "调整筛选条件后重试。" : "新任务提交后会显示在这里。"}</span>
          </div>
        )}
        <UIPagination
          page={page}
          totalPages={totalPages}
          total={total}
          pageSize={pageSize}
          sizes={[20, 50, 100]}
          busy={tasks.isFetching}
          onPage={(nextPage) => {
            setPage(nextPage);
            setSelectedID(null);
          }}
          onPageSize={(size) => {
            setPageSize(size);
            setPage(1);
            setSelectedID(null);
          }}
        />
      </div>
      {selected && (
        <TaskDetailDialog task={selected} providers={providers.data || []} close={() => setSelectedID(null)} />
      )}
      <Dialog open={cleanupOpen} onOpenChange={(open) => { if (!cleanup.isPending) setCleanupOpen(open); }}>
        <DialogContent className="task-cleanup-dialog" showClose={false}>
          <span className="task-cleanup-dialog-icon"><AlertTriangle /></span>
          <DialogTitle>将 {cleanupTasks.length} 个异常任务标记为失败？</DialogTitle>
          <DialogDescription>
            仅处理“提交不确定”任务。确认后任务会立即变为失败并返回 submission_not_created，仍处于 held 的平台余额预留会同步释放。
          </DialogDescription>
          <div className="task-cleanup-list">
            {cleanupTasks.map((task) => (
              <span key={task.id}>
                <strong><ProviderBadge providerID={task.provider_id} providers={providers.data || []} /> {task.kind === "video" ? "视频" : task.kind === "audio" ? "音频" : "图像"} · {task.model}</strong>
                <small title={task.id}>{task.id}</small>
                <small>{new Date(task.created_at).toLocaleString("zh-CN")}</small>
                <small>{reservationText(task.reservation_state, task.reservation_release_reason)}</small>
              </span>
            ))}
          </div>
          {cleanup.error && <p className="error">处理失败：{cleanup.error.message}</p>}
          <footer>
            <button type="button" className="secondary" disabled={cleanup.isPending} onClick={() => setCleanupOpen(false)}>取消</button>
            <button type="button" className="danger-button" disabled={cleanup.isPending || cleanupTasks.length === 0} onClick={() => cleanup.mutate(cleanupTasks.map((task) => task.id))}>
              {cleanup.isPending ? <RefreshCw className="spin" /> : <AlertTriangle />}
              {cleanup.isPending ? "正在处理" : "确认标记失败"}
            </button>
          </footer>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function taskResultSummary(task: Task) {
  if (task.status === "failed") return task.error_message || "任务执行失败";
  if (task.status === "succeeded") {
    const count = task.result?.data?.length || 0;
    const warning = resultHasContentWarning(task.result)
      ? " · 显式内容警告"
      : "";
    return count ? `已生成 ${count} 个结果${warning}` : `生成完成${warning}`;
  }
  if (task.status === "cancelled") return "任务已取消，未生成内容";
  if (task.status === "submission_uncertain")
    return task.error_message || "上游提交状态未知";
  if (task.status === "queued" && task.queue_position)
    return `排队中 · 第 ${task.queue_position} 位`;
  if (task.status === "submitted" || task.status === "polling") {
    return statusText(task.status);
  }
  return statusText(task.status);
}

type ModeratedResult = {
  nsfw?: boolean;
  moderation_classifications?: string[];
  data?: Array<{
    nsfw?: boolean;
    moderation_classifications?: string[];
  }>;
};

function resultHasContentWarning(result?: ModeratedResult) {
  return Boolean(result?.nsfw || result?.data?.some((output) => output.nsfw));
}

function resultModerationLabels(result?: ModeratedResult) {
  return [
    ...new Set([
      ...(result?.moderation_classifications || []),
      ...(result?.data?.flatMap(
        (output) => output.moderation_classifications || [],
      ) || []),
    ]),
  ];
}

function providerErrorText(code?: string) {
  return code === "PROVIDER_MODERATION_ERROR"
    ? "供应商内容审核未通过"
    : code || "未返回供应商错误码";
}

function taskDuration(task: Task) {
  if (!task.started_at) return "—";
  const end = new Date(task.completed_at || task.updated_at).getTime();
  const start = new Date(task.started_at).getTime();
  return `${Math.max(0, Math.round((end - start) / 1000))} 秒`;
}

function TaskDetailItem({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <div>
      <span>{label}</span>
      <strong className={mono ? "mono" : ""}>{value || "—"}</strong>
    </div>
  );
}

function TaskDetailDialog({ task: initialTask, providers, close }: { task: Task; providers: Provider[]; close: () => void }) {
  const client = useQueryClient();
  const taskDetail = useQuery({
    queryKey: ["task-detail", initialTask.id],
    queryFn: () => api<Task>(`/admin/api/tasks/${initialTask.id}`),
    staleTime: 10_000,
    refetchInterval: (query) => {
      const status = (query.state.data as Task | undefined)?.status || initialTask.status;
      return ["queued", "reserving", "uploading", "submitted", "polling"].includes(
        status,
      )
        ? 5_000
        : false;
    },
  });
  const task = taskDetail.data || initialTask;
  const details = task.error_details;
  const release = useMutation({
    mutationFn: () =>
      api(`/admin/api/tasks/${task.id}/reservation/release`, {
        method: "POST",
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["tasks-page"] });
      void client.invalidateQueries({ queryKey: ["task-detail", task.id] });
    },
  });
  const confirmNotCreated = useMutation({
    mutationFn: () =>
      api(`/admin/api/tasks/${task.id}/submission/not-created`, {
        method: "POST",
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["tasks-page"] });
      void client.invalidateQueries({ queryKey: ["task-detail", task.id] });
    },
  });
  const outputs = task.result?.data || [];
  const hasContentWarning = resultHasContentWarning(task.result);
  const moderationLabels = resultModerationLabels(task.result);
  const upstreamRequest = task.upstream_request;
  const upstreamRequestJSON = upstreamRequest
    ? JSON.stringify(upstreamRequest, null, 2)
    : "";
  const kindName =
    task.kind === "video" ? "视频" : task.kind === "audio" ? "音频" : "图像";
  const taskEvents = task.events || [];
  const transitionEvents = taskEvents.filter(
    (event, index) => index === 0 || taskEvents[index - 1].status !== event.status,
  );
  const mergedEventCount = taskEvents.length - transitionEvents.length;
  const timeline = [
    { id: 0, status: "created", message: "任务已创建", created_at: task.created_at },
    ...transitionEvents,
  ];
  return (
    <Dialog open onOpenChange={(open) => { if (!open) close(); }}>
      <DialogContent className="task-detail-dialog" showClose={false}>
        <DialogDescription className="sr-only">任务参数、执行状态、计费与上游结果。</DialogDescription>
        <header className="dialog-heading">
          <div>
            <span className="eyebrow">Task Detail</span>
            <DialogTitle asChild><h2 id="task-detail-title">任务执行详情</h2></DialogTitle>
          </div>
          <button className="icon" aria-label="关闭" onClick={close} autoFocus>
            <X />
          </button>
        </header>
        <div className={`task-detail-state ${task.status}`}>
          <span>
            <i className={`status ${task.status}`}></i>
            {statusText(task.status)}
          </span>
          <strong>{taskResultSummary(task)}</strong>
        </div>
        <div className="task-detail-grid">
          <TaskDetailItem label="任务 ID" value={task.id} mono />
          <TaskDetailItem label="业务平台" value={providerDisplayName(task.provider_id, providers)} />
          <TaskDetailItem
            label="上游 Generation ID"
            value={task.generation_id}
            mono
          />
          <TaskDetailItem
            label="类型 / 模型"
            value={`${kindName} · ${task.model}`}
          />
          <TaskDetailItem
            label="上游进度"
            value={
              task.status === "succeeded"
                ? "100%"
                : task.status === "submitted" || task.status === "polling"
                  ? "上游未提供百分比"
                  : task.status === "submission_uncertain"
                    ? "未收到上游确认"
                    : "—"
            }
          />
          <TaskDetailItem
            label="排队位置"
            value={task.queue_position ? `第 ${task.queue_position} 位` : "—"}
          />
          <TaskDetailItem
			label={`预计 ${providerCreditUnit(task.provider_id, providers)}`}
            value={task.estimated_tokens?.toLocaleString() || "—"}
          />
          <TaskDetailItem
			label={`本地结算 ${providerCreditUnit(task.provider_id, providers)}`}
            value={tokenCost(task)}
          />
          <TaskDetailItem
            label="上游报告成本（不入账）"
            value={
              task.upstream_reported_cost != null
                ? formatTokens(task.upstream_reported_cost)
                : "上游未报告"
            }
          />
          <TaskDetailItem
            label="预留状态"
            value={reservationText(
              task.reservation_state,
              task.reservation_release_reason,
            )}
          />
          <TaskDetailItem
            label="路由账号"
            value={task.account_id || "—"}
            mono
          />
          <TaskDetailItem label="执行耗时" value={taskDuration(task)} />
          <TaskDetailItem
            label="创建时间"
            value={new Date(task.created_at).toLocaleString("zh-CN")}
          />
          <TaskDetailItem
            label="完成时间"
            value={
              task.completed_at
                ? new Date(task.completed_at).toLocaleString("zh-CN")
                : "—"
            }
          />
        </div>
        <section className="task-detail-section">
          <span>状态时间线</span>
          <div className="task-event-timeline">
            {timeline.map((event) => (
              <div key={`${event.id}-${event.status}`}>
                <i className={`status ${event.status}`}></i>
                <span>
                  <strong>{event.status === "created" ? "已创建" : statusText(event.status)}</strong>
                  <small>{event.message || "状态已更新"}</small>
                </span>
                <time>{new Date(event.created_at).toLocaleString("zh-CN")}</time>
              </div>
            ))}
          </div>
          {mergedEventCount > 0 && (
            <small className="task-event-merged">已合并 {mergedEventCount} 条连续重复状态事件</small>
          )}
        </section>
        <section className="task-detail-section">
          <span>提示词</span>
          <p>{task.prompt || "未填写提示词"}</p>
        </section>
        <section className="task-detail-section task-upstream-section">
          <div className="task-detail-section-heading">
            <span>上游请求参数</span>
            {upstreamRequestJSON && (
              <CopyCode
                value={upstreamRequestJSON}
                title="复制上游请求参数"
              />
            )}
          </div>
          {taskDetail.isLoading ? (
            <p className="task-detail-empty">正在读取...</p>
          ) : taskDetail.error ? (
            <p className="error">{taskDetail.error.message}</p>
          ) : upstreamRequestJSON ? (
            <pre className="task-upstream-request">
              <code>{upstreamRequestJSON}</code>
            </pre>
          ) : (
            <p className="task-detail-empty">此任务未记录上游请求参数</p>
          )}
        </section>
        {task.status === "failed" && (
          <section className="task-failure-detail">
            <div>
              <AlertTriangle />
              <span>
                <strong>{task.error_message || "任务执行失败"}</strong>
                <small>{task.error_code || "unknown_error"}</small>
              </span>
            </div>
            <dl>
              <div>
                <dt>供应商原因</dt>
                <dd>{providerErrorText(details?.provider_error_code)}</dd>
              </div>
              <div>
                <dt>错误来源</dt>
                <dd>{details?.source || providerDisplayName(task.provider_id, providers)}</dd>
              </div>
              <div>
                <dt>上游状态</dt>
                <dd>{details?.upstream_status || "FAILED"}</dd>
              </div>
              <div>
                <dt>{providerDisplayName(task.provider_id, providers)} 审核</dt>
                <dd>
                  {details
                    ? `${details.nsfw ? "标记为 NSFW" : "未标记 NSFW"} · ${details.prompt_moderations?.length || 0} 条审核记录`
                    : "旧任务未保存审核详情"}
                </dd>
              </div>
            </dl>
            {details?.notes?.map((note, index) => (
              <div
                className="task-provider-note"
                key={`${note.noteType}-${index}`}
              >
                <span>{note.noteType || `Note ${index + 1}`}</span>
                <code>
                  {JSON.stringify(note.failureReason || note.notePayload || {})}
                </code>
              </div>
            ))}
            {details?.detail_error && (
              <p className="error">详情查询失败：{details.detail_error}</p>
            )}
          </section>
        )}
        {task.status === "succeeded" && hasContentWarning && (
          <section className="task-content-warning">
            <AlertTriangle />
            <span>
              <strong>上游标记为显式内容</strong>
              <small>
                {moderationLabels.length
                  ? moderationLabels.join(" · ")
                  : "上游内容安全标记"}
              </small>
            </span>
          </section>
        )}
        {task.status === "succeeded" && (
          <section className="task-detail-section">
            <span>生成结果</span>
            {outputs.length ? (
              <div className="task-output-list">
                {outputs.map((output, index) => (
                  <div key={output.id || index}>
                    <span>
                      <strong>结果 {index + 1}</strong>
                      <small>
                        {[
                          output.width && output.height
                            ? `${output.width}×${output.height}`
                            : "",
                          output.duration ? `${output.duration} 秒` : "",
                          output.nsfw ? "显式内容" : "",
                        ]
                          .filter(Boolean)
                          .join(" · ") ||
                          output.media_type ||
                          "已完成"}
                      </small>
                    </span>
                    {output.url && (
                      <a href={output.url} target="_blank" rel="noreferrer">
                        打开结果
                      </a>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p>任务成功，但响应中没有可展示的输出条目。</p>
            )}
          </section>
        )}
        <footer>
          {task.status === "submission_uncertain" ? (
            <div className="task-reconciliation-actions">
              {task.reservation_state === "held" && (
                <button
                  disabled={release.isPending || confirmNotCreated.isPending}
                  onClick={() => {
                    if (window.confirm("仅释放平台余额预留，任务仍保留为提交状态未知。确认继续？")) {
                      release.mutate();
                    }
                  }}
                >
                  <Coins />
                  {release.isPending ? "释放中" : "仅释放预留"}
                </button>
              )}
              <button
                className="danger-button"
                disabled={release.isPending || confirmNotCreated.isPending}
                onClick={() => {
                  if (window.confirm(`请先确认 ${providerDisplayName(task.provider_id, providers)} 上游没有对应任务。此操作会把任务标记为失败并释放平台余额预留，确认继续？`)) {
                    confirmNotCreated.mutate();
                  }
                }}
              >
                <AlertTriangle />
                {confirmNotCreated.isPending ? "处理中" : "确认未提交并标记失败"}
              </button>
            </div>
          ) : (
            <span />
          )}
          <button onClick={close}>关闭</button>
        </footer>
      </DialogContent>
    </Dialog>
  );
}
