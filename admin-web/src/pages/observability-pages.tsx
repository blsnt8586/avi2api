import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Gauge, ScrollText, Search } from "lucide-react";
import {
  Badge,
  DataTable,
  Pagination as UIPagination,
  QueryStatus,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  TableSkeleton,
} from "../components/ui";
import type { APIRequestLog, APIRequestLogsPage, AuditLog, AuditLogsPage } from "../shared/types";
import { CopyCode } from "../components/copy-code";
import { api } from "../shared/api";
import { appendLocalDateRange } from "../shared/status";
import { useDebouncedValue } from "../shared/cache";

function requestParameterSummary(parameters: Record<string, unknown>) {
  return Object.entries(parameters || {})
    .filter(([, value]) => value !== "" && value !== null && value !== undefined)
    .map(([key, value]) => `${key}=${typeof value === "object" ? JSON.stringify(value) : String(value)}`)
    .join(" · ");
}

export function RequestLogs() {
  const [searchParams] = useSearchParams();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [search, setSearch] = useState(() => searchParams.get("search") || "");
  const [status, setStatus] = useState(0);
  const [method, setMethod] = useState("all");
  const [kind, setKind] = useState("all");
  const [pathFilter, setPathFilter] = useState("");
  const [modelFilter, setModelFilter] = useState("");
  const [apiKeyFilter, setAPIKeyFilter] = useState("");
  const [ipFilter, setIPFilter] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [selected, setSelected] = useState<APIRequestLog | null>(null);
  const deferredSearch = useDebouncedValue(search.trim());
  const deferredPath = useDebouncedValue(pathFilter.trim());
  const deferredModel = useDebouncedValue(modelFilter.trim());
  const deferredAPIKey = useDebouncedValue(apiKeyFilter.trim());
  const deferredIP = useDebouncedValue(ipFilter.trim());
  const logs = useQuery({
    queryKey: ["request-logs", page, pageSize, deferredSearch, status, method, kind, deferredPath, deferredModel, deferredAPIKey, deferredIP, dateFrom, dateTo],
    queryFn: () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
      if (deferredSearch) params.set("search", deferredSearch);
      if (status) params.set("status", String(status));
      if (method !== "all") params.set("method", method);
      if (kind !== "all") params.set("kind", kind);
      if (deferredPath) params.set("path", deferredPath);
      if (deferredModel) params.set("model", deferredModel);
      if (deferredAPIKey) params.set("api_key", deferredAPIKey);
      if (deferredIP) params.set("ip", deferredIP);
      appendLocalDateRange(params, dateFrom, dateTo);
      return api<APIRequestLogsPage>(`/admin/api/request-logs?${params}`);
    },
    placeholderData: (previous) => previous,
  });
  const data = logs.data?.data || [];
  const filteredData = data;
  const total = logs.data?.total || 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const requestedSearch = searchParams.get("search") || "";
  React.useEffect(() => {
    if (requestedSearch) {
      setSearch(requestedSearch);
      setPage(1);
    }
  }, [requestedSearch]);
  React.useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);
  return (
    <section className="audit-workspace request-log-workspace">
      <div className="section-heading request-log-heading">
        <div>
          <span className="eyebrow">API Traffic</span>
          <h2>全部 API 请求</h2>
        </div>
        <QueryStatus fetching={logs.isFetching} error={logs.error} updatedAt={logs.dataUpdatedAt} label={`共 ${total} 条`} />
      </div>
      <div className="accounts-toolbar request-log-toolbar">
        <label className="search-box">
          <Search size={17} />
          <input
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder="搜索 Request ID、模型、账号或错误码"
          />
        </label>
        <label className="request-status-filter">
          HTTP 状态
          <select
            value={status}
            onChange={(event) => {
              setStatus(Number(event.target.value));
              setPage(1);
            }}
          >
            <option value={0}>全部</option>
            {[200, 202, 400, 401, 409, 422, 429, 500, 502, 503, 504].map((value) => (
              <option value={value} key={value}>{value}</option>
            ))}
          </select>
        </label>
        <label className="request-status-filter">
          方法
          <select value={method} onChange={(event) => { setMethod(event.target.value); setPage(1); }}>
            <option value="all">全部</option>
            {["GET", "POST", "PUT", "PATCH", "DELETE"].map((value) => <option value={value} key={value}>{value}</option>)}
          </select>
        </label>
        <label className="request-status-filter">
          类型
          <select value={kind} onChange={(event) => { setKind(event.target.value); setPage(1); }}>
            <option value="all">全部</option>
            <option value="image">图像</option>
            <option value="video">视频</option>
            <option value="audio">音频</option>
            <option value="chat">Chat</option>
          </select>
        </label>
      </div>
      <div className="request-advanced-filter-row">
        <label>接口路径<input value={pathFilter} onChange={(event) => { setPathFilter(event.target.value); setPage(1); }} placeholder="/v1/images" /></label>
        <label>模型<input value={modelFilter} onChange={(event) => { setModelFilter(event.target.value); setPage(1); }} placeholder="模型 ID" /></label>
        <label>API Key<input value={apiKeyFilter} onChange={(event) => { setAPIKeyFilter(event.target.value); setPage(1); }} placeholder="Key 前缀" /></label>
        <label>客户端 IP<input value={ipFilter} onChange={(event) => { setIPFilter(event.target.value); setPage(1); }} placeholder="IP 地址" /></label>
        <label>开始日期<input type="date" value={dateFrom} onChange={(event) => { setDateFrom(event.target.value); setPage(1); }} /></label>
        <label>结束日期<input type="date" value={dateTo} onChange={(event) => { setDateTo(event.target.value); setPage(1); }} /></label>
      </div>
      {logs.error && <p className="error audit-list-error">{logs.error.message}</p>}
      <div className="table request-log-table">
        <div className="row task head">
          <span>时间 / Request ID</span>
          <span>接口 / API Key</span>
          <span>模型参数</span>
          <span>路由账号 / 积分</span>
          <span>结果 / 耗时</span>
        </div>
        {logs.isLoading && <TableSkeleton rows={6} columns={5} />}
        {filteredData.map((entry) => {
          const parameterSummary = requestParameterSummary(entry.parameters);
          return (
            <div className="row task interactive-row" key={entry.id} role="button" tabIndex={0} onClick={() => setSelected(entry)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); setSelected(entry); } }}>
              <span>
                <strong>{new Date(entry.created_at).toLocaleString("zh-CN")}</strong>
                <small title={entry.request_id}>{entry.request_id}</small>
              </span>
              <span>
                <strong>{entry.method} {entry.path}</strong>
                <small>{entry.api_key_prefix || "未认证"} · {entry.client_ip || "未知 IP"}</small>
              </span>
              <span>
                <strong>{entry.model || entry.kind || "无模型参数"}</strong>
                <small title={parameterSummary}>{parameterSummary || `prompt ${entry.prompt_chars} 字符`}</small>
              </span>
              <span>
                <strong>{entry.account_name || "未分配账号"}</strong>
                <small>{entry.estimated_tokens === undefined ? "未进入定价" : `预估 ${entry.estimated_tokens.toLocaleString()} 积分`}</small>
              </span>
              <span>
                <strong className={entry.status >= 400 ? "request-failed" : "request-succeeded"}>
                  HTTP {entry.status}{entry.error_code ? ` · ${entry.error_code}` : ""}
                </strong>
                <small>{entry.duration_ms.toLocaleString()} ms{entry.task_id ? ` · 任务 ${entry.task_id.slice(0, 8)}` : ""}</small>
              </span>
            </div>
          );
        })}
      </div>
      {!logs.isLoading && filteredData.length === 0 && (
        <div className="empty-state compact-empty">
          <Gauge />
          <strong>没有匹配的请求</strong>
          <span>调整搜索词或 HTTP 状态后重试。</span>
        </div>
      )}
      <UIPagination
        className="request-log-pagination"
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        sizes={[20, 50, 100]}
        busy={logs.isFetching}
        onPage={setPage}
        onPageSize={(size) => { setPageSize(size); setPage(1); }}
      />
      <Sheet open={Boolean(selected)} onOpenChange={(open) => { if (!open) setSelected(null); }}>
        <SheetContent className="request-detail-sheet">
          <SheetTitle>请求详情</SheetTitle>
          <SheetDescription>完整请求路由、定价和响应摘要。</SheetDescription>
          {selected && (
            <div className="request-detail-content">
              <div className="request-detail-grid">
                <span><small>Request ID</small><strong>{selected.request_id}</strong></span>
                <span><small>时间</small><strong>{new Date(selected.created_at).toLocaleString("zh-CN")}</strong></span>
                <span><small>接口</small><strong>{selected.method} {selected.path}</strong></span>
                <span><small>HTTP</small><Badge tone={selected.status >= 400 ? "danger" : "success"}>{selected.status}</Badge></span>
                <span><small>API Key</small><strong>{selected.api_key_prefix || "未认证"}</strong></span>
                <span><small>客户端 IP</small><strong>{selected.client_ip || "未知"}</strong></span>
                <span><small>模型</small><strong>{selected.model || selected.kind || "无"}</strong></span>
                <span><small>路由账号</small><strong>{selected.account_name || "未分配"}</strong></span>
                <span><small>预估积分</small><strong>{selected.estimated_tokens?.toLocaleString() || "未进入定价"}</strong></span>
                <span><small>耗时</small><strong>{selected.duration_ms.toLocaleString()} ms</strong></span>
              </div>
              <section>
                <div className="request-detail-heading"><strong>请求参数</strong><CopyCode value={JSON.stringify(selected.parameters, null, 2)} /></div>
                <pre>{JSON.stringify(selected.parameters, null, 2)}</pre>
              </section>
              {selected.error_code && <p className="error" role="alert">{selected.error_code}</p>}
            </div>
          )}
        </SheetContent>
      </Sheet>
    </section>
  );
}

export function Audit() {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [search, setSearch] = useState("");
  const [action, setAction] = useState("all");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [selected, setSelected] = useState<AuditLog | null>(null);
  const deferredSearch = useDebouncedValue(search.trim());
  const audit = useQuery({
    queryKey: ["audit-page", page, pageSize, deferredSearch, action, dateFrom, dateTo],
    queryFn: () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
      if (deferredSearch) params.set("search", deferredSearch);
      if (action !== "all") params.set("action", action);
      appendLocalDateRange(params, dateFrom, dateTo);
      return api<AuditLogsPage>(`/admin/api/audit-logs?${params}`);
    },
    placeholderData: (previous) => previous,
  });
  const data = audit.data?.data || [];
  const actionOptions = audit.data?.actions || [];
  const filteredData = data;
  const columns = React.useMemo<ColumnDef<AuditLog>[]>(() => [
    { accessorKey: "action", header: "操作", cell: ({ row }) => <strong>{row.original.action}</strong> },
    { accessorKey: "actor", header: "操作者" },
    { accessorKey: "target", header: "目标", cell: ({ row }) => row.original.target || "全局" },
    { accessorKey: "created_at", header: "时间", cell: ({ row }) => new Date(row.original.created_at).toLocaleString("zh-CN") },
  ], []);
  const total = audit.data?.total || 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  React.useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);
  return (
    <section className="audit-workspace">
      <div className="section-heading audit-list-heading">
        <div>
          <span className="eyebrow">Audit Trail</span>
          <h2>操作记录</h2>
        </div>
        <QueryStatus fetching={audit.isFetching} error={audit.error} updatedAt={audit.dataUpdatedAt} label={`第 ${Math.min(page, totalPages)} / ${totalPages} 页`} />
      </div>
      <div className="accounts-toolbar audit-toolbar">
        <label className="search-box">
          <Search size={17} />
          <input value={search} onChange={(event) => { setSearch(event.target.value); setPage(1); }} placeholder="搜索操作者、动作或目标" />
        </label>
        <label className="request-status-filter">操作类型
          <select value={action} onChange={(event) => { setAction(event.target.value); setPage(1); }}>
            <option value="all">全部</option>
            {actionOptions.map((value) => <option value={value} key={value}>{value}</option>)}
          </select>
        </label>
        <label className="request-status-filter">开始日期<input type="date" value={dateFrom} onChange={(event) => { setDateFrom(event.target.value); setPage(1); }} /></label>
        <label className="request-status-filter">结束日期<input type="date" value={dateTo} onChange={(event) => { setDateTo(event.target.value); setPage(1); }} /></label>
        <span className="result-count">共 {total} 条</span>
      </div>
      {audit.error && <p className="error audit-list-error">{audit.error.message}</p>}
      <DataTable
        data={filteredData}
        columns={columns}
        rowKey={(entry) => String(entry.id)}
        rowLabel={(entry) => `查看审计记录 ${entry.action}`}
        empty="暂无审计记录"
        loading={audit.isLoading}
        onRowClick={setSelected}
        className="audit-data-table"
      />
      {!audit.isLoading && filteredData.length === 0 && (
        <div className="empty-state compact-empty">
          <ScrollText />
          <strong>暂无审计记录</strong>
          <span>管理操作发生后会显示在这里。</span>
        </div>
      )}
      <UIPagination page={page} totalPages={totalPages} total={total} pageSize={pageSize} sizes={[20, 50, 100]} busy={audit.isFetching} onPage={setPage} onPageSize={(size) => { setPageSize(size); setPage(1); }} />
      <Sheet open={Boolean(selected)} onOpenChange={(open) => { if (!open) setSelected(null); }}>
        <SheetContent className="audit-detail-sheet">
          <SheetTitle>审计记录</SheetTitle>
          <SheetDescription>管理员操作与不可编辑的元数据快照。</SheetDescription>
          {selected && (
            <div className="request-detail-content">
              <div className="request-detail-grid">
                <span><small>操作者</small><strong>{selected.actor}</strong></span>
                <span><small>动作</small><strong>{selected.action}</strong></span>
                <span><small>目标</small><strong>{selected.target || "全局"}</strong></span>
                <span><small>时间</small><strong>{new Date(selected.created_at).toLocaleString("zh-CN")}</strong></span>
              </div>
              <section>
                <div className="request-detail-heading"><strong>变更元数据</strong><CopyCode value={JSON.stringify(selected.metadata || {}, null, 2)} /></div>
                <pre>{JSON.stringify(selected.metadata || {}, null, 2)}</pre>
              </section>
            </div>
          )}
        </SheetContent>
      </Sheet>
    </section>
  );
}
