import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AudioLines,
  Boxes,
  Check,
  ChevronLeft,
  ChevronRight,
  CirclePlay,
  Coins,
  Image as ImageIcon,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  X,
} from "lucide-react";
import {
  Badge,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  Tabs,
  TabsList,
  TabsTrigger,
} from "../components/ui";
import type {
  CostRule,
  MediaKind,
  ModelCostRecord,
  PlatformModelRow,
  PlatformModelsResponse,
  Provider,
} from "../shared/types";
import { Metric } from "../components/metric";
import { ProviderBadge, ProviderSwitcher } from "../components/provider-switcher";
import { api } from "../shared/api";
import {
  audioOptions,
  audioRuleLabel,
  costDimensions,
  costEstimates,
  modelCost,
  videoOptions,
  videoWorkflowLabel,
} from "../shared/catalog";
import { formatOptionalTokens, formatTokens } from "../shared/status";
import { EstimateTable } from "./pricing-components";
import { modelDisplayID, providerCreditUnit, providerDefinition, providerDisplayName, providerSupports } from "../shared/providers";

function isConfiguredCatalogSource(source?: PlatformModelsResponse["catalog_source"]) {
  return source === "configured_models" || source === "configured_models_pending_sync";
}

function catalogSourceLabel(source?: PlatformModelsResponse["catalog_source"], schemaVersion?: string) {
  if (source === "configured_models_pending_sync") return "平台配置（待同步）";
  if (source === "configured_models") return "平台配置";
  return `上游 Schema ${schemaVersion || "--"}`;
}

function FeaturedModels({ rows, providerID, providers }: { rows: PlatformModelRow[]; providerID: string; providers: Provider[] }) {
  return (
    <div className="featured-models">
      {rows.map((r) => (
        <div className="featured-model" key={r.platform.id}>
          <div className="featured-icon">
            <CirclePlay size={17} />
          </div>
          <div>
            <strong>{r.public_id}</strong>
            <small>
              <ProviderBadge providerID={providerID} providers={providers} /> {r.platform.provider || providerDisplayName(providerID, providers)} · {modelCost(r.platform)}
            </small>
          </div>
          <i className="status active"></i>
        </div>
      ))}
    </div>
  );
}

function CostReference({
  rows,
  costs,
  rules,
  kind,
  providerID,
  unit,
  loading,
  error,
}: {
  rows: PlatformModelRow[];
  costs: ModelCostRecord[];
  rules: CostRule[];
  kind: MediaKind;
  providerID: string;
  unit: string;
  loading: boolean;
  error?: Error | null;
}) {
  const estimates = costEstimates(rules, kind);
  const [selected, setSelected] = useState("");
  const active = estimates.find((e) => e.model === selected) || estimates[0];
  React.useEffect(() => {
    if (active && !selected) setSelected(active.model);
  }, [active, selected]);
  return (
    <section className="cost-reference">
      <div className="section-heading">
        <div>
          <span className="eyebrow">Platform Cost</span>
          <h2>平台成本规则</h2>
        </div>
        <small>{unit} 实时成本 · 未匹配规则的请求将被阻止</small>
      </div>
      <p className="cost-note">
        成本规则决定任务创建时预留多少上游 {unit}，按模型、尺寸或分辨率、时长和工作流匹配；任务成功后按命中的规则结算。
      </p>
      {loading ? (
        <div className="loading-state compact-loading">
          <RefreshCw className="spin" />
          正在读取平台成本规则…
        </div>
      ) : error ? (
        <p className="error">{error.message}</p>
      ) : active ? (
        <>
          <div className="cost-model-picker">
            {estimates.map((e) => (
              <button
                key={e.model}
                className={active.model === e.model ? "active" : ""}
                onClick={() => setSelected(e.model)}
              >
                {e.model}
              </button>
            ))}
          </div>
          <EstimateTable estimate={active} unit={unit} />
        </>
      ) : (
        <div className="empty-state compact-empty">
          <Coins />
          <strong>当前没有启用的成本规则</strong>
          <span>生成请求会返回 cost_unavailable。</span>
        </div>
      )}
      <div className="section-heading actual-cost-heading">
        <div>
          <span className="eyebrow">Observed Usage</span>
          <h2>历史成本记录</h2>
        </div>
        <small>本地账本结算与上游 Generate 报告值分开显示</small>
      </div>
      <p className="cost-note">
        本地结算用于 {unit} 扣账；上游报告值来自提交响应的 apiCreditCost，仅作观测，不参与财务结算。
      </p>
      <div className="cost-table">
        <div className="cost-line cost-head">
          <span>模型</span>
          <span>请求维度</span>
          <span>本地样本</span>
          <span>本地结算</span>
          <span>上游报告值</span>
        </div>
        {rows.map((r) => {
          const observed = costs.filter(
            (c) => c.provider_id === providerID && c.kind === kind && c.model === r.public_id,
          );
          return (
            <div className="cost-line" key={r.platform.id}>
              <span>
                <strong>{r.public_id}</strong>
                <small>目录基础：{modelCost(r.platform)}</small>
              </span>
              <span>
                {observed.length ? (
                  observed
                    .slice(0, 3)
                    .map((c) => (
                      <small key={`${c.size}-${c.quality}-${c.duration}`}>
                        {costDimensions(c)}
                      </small>
                    ))
                ) : (
                  <small>暂无生成记录</small>
                )}
              </span>
              <span>
                {observed.length ? (
                  observed
                    .slice(0, 3)
                    .map((c) => (
                      <small key={`${c.model}-${c.samples}-${c.last_used_at}`}>
                        {c.samples} 次
                      </small>
                    ))
                ) : (
                  <small>—</small>
                )}
              </span>
              <span>
                {observed.length ? (
                  observed.slice(0, 3).map((c) => (
                    <small key={`${c.minimum}-${c.maximum}-${c.average}`}>
                      {formatTokens(c.average)} {unit}（{c.minimum}–{c.maximum}）
                    </small>
                  ))
                ) : (
                  <small>—</small>
                )}
              </span>
              <span>
                {observed.length ? (
                  observed.slice(0, 3).map((c) => (
                    <small key={`${c.model}-${c.upstream_reported_samples}-${c.last_used_at}`}>
                      {c.upstream_reported_samples > 0 && c.upstream_reported_average != null
                        ? `${formatTokens(c.upstream_reported_average)} ${unit}（${formatOptionalTokens(c.upstream_reported_minimum)}–${formatOptionalTokens(c.upstream_reported_maximum)}）· ${c.upstream_reported_samples} 次`
                        : "上游未报告"}
                    </small>
                  ))
                ) : (
                  <small>—</small>
                )}
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
}

export function Models() {
  const client = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [media, setMedia] = useState<MediaKind>("image");
  const providerID = searchParams.get("provider") || "leonardo";
  const search = searchParams.get("search") || "";
  const [scope, setScope] = useState<"all" | "exposed">("exposed");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [showRules, setShowRules] = useState(false);
  const [selectedModel, setSelectedModel] = useState<PlatformModelRow | null>(null);
  const path =
    media === "image"
      ? "/admin/api/platform-models"
      : media === "video"
        ? "/admin/api/platform-video-models"
        : "/admin/api/platform-audio-models";
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => api<Provider[]>("/admin/api/providers"),
  });
  const models = useQuery({
    queryKey: ["platform-models", providerID, media],
    queryFn: () => api<PlatformModelsResponse>(`${path}?provider=${encodeURIComponent(providerID)}`),
    refetchInterval: false,
  });
  const rules = useQuery({
    queryKey: ["cost-rules", providerID, media],
    queryFn: () => api<CostRule[]>(`/admin/api/cost-rules?provider=${encodeURIComponent(providerID)}&kind=${media}`),
    refetchInterval: false,
  });
  const costs = useQuery({
    queryKey: ["model-costs", providerID, media],
    queryFn: () => api<ModelCostRecord[]>(`/admin/api/model-costs?provider=${encodeURIComponent(providerID)}&kind=${media}`),
    refetchInterval: false,
  });
  const sync = useMutation({
    mutationFn: () =>
      api<PlatformModelsResponse>("/admin/api/platform-models/sync", {
        method: "POST",
        body: JSON.stringify({ provider_id: providerID, media_type: media }),
      }),
    onSuccess: (data) => client.setQueryData(["platform-models", providerID, media], data),
  });
  const rows = models.data?.data || [];
  const featured = rows.filter((r) => r.exposed);
  const needle = search.trim().toLowerCase();
  const visible = rows.filter(
    (r) =>
      (scope === "all" || r.exposed) &&
      (!needle ||
        `${r.platform.name} ${r.platform.id} ${r.platform.provider} ${r.public_id}`
          .toLowerCase()
          .includes(needle)),
  );
  const totalPages = Math.max(1, Math.ceil(visible.length / pageSize));
  const pagedModels = visible.slice((page - 1) * pageSize, page * pageSize);
  React.useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);
  const mediaName =
    media === "image" ? "图像" : media === "video" ? "视频" : "音频";
  const currentProvider = providers.data?.find((provider) => provider.id === providerID);
  const costUnit = providerCreditUnit(providerID, providers.data || []);
  const switchProvider = (nextProviderID: string) => {
    const nextProvider = providers.data?.find((provider) => provider.id === nextProviderID);
    const nextMedia = nextProvider && !providerSupports(nextProvider, media)
      ? (["image", "video", "audio"] as MediaKind[]).find((kind) => providerSupports(nextProvider, kind)) || "image"
      : media;
    setMedia(nextMedia);
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.set("provider", nextProviderID);
      return next;
    }, { replace: true });
    setPage(1);
    setSelectedModel(null);
  };
  return (
    <section>
      <div className="provider-business-header">
        <div><span className="eyebrow">Provider Workspace</span><h2>平台业务视图</h2><p>{providerDefinition(providerID, currentProvider).description}</p></div>
        <ProviderSwitcher providers={providers.data || []} value={providerID} onChange={switchProvider} />
      </div>
      <div className="model-catalog-toolbar">
        <Tabs value={media} onValueChange={(value) => { setMedia(value as MediaKind); setPage(1); }}>
          <TabsList className="model-media-tabs">
          <TabsTrigger value="image" disabled={currentProvider ? !providerSupports(currentProvider, "image") : false}>
            <ImageIcon />
            图像模型
          </TabsTrigger>
          <TabsTrigger value="video" disabled={currentProvider ? !providerSupports(currentProvider, "video") : false}>
            <CirclePlay />
            视频模型
          </TabsTrigger>
          <TabsTrigger value="audio" disabled={currentProvider ? !providerSupports(currentProvider, "audio") : false}>
            <AudioLines />
            音频模型
          </TabsTrigger>
          </TabsList>
        </Tabs>
        <div>
          <small>
            {isConfiguredCatalogSource(models.data?.catalog_source)
              ? models.data?.catalog_source === "configured_models_pending_sync"
                ? "目录来源：平台配置 · 等待首次上游同步"
                : "目录来源：平台配置 · 随应用版本生效"
              : models.data?.synced_at
                ? `最后同步：${new Date(models.data.synced_at).toLocaleString("zh-CN")}`
                : "尚未同步平台目录"}
          </small>
          <button className="secondary" onClick={() => setShowRules(true)}>
            <Coins />
            管理成本规则
          </button>
          {models.data?.sync_supported && <button onClick={() => {
            if (window.confirm(`同步会刷新当前${mediaName}模型目录和 Schema 版本，确认继续？`)) sync.mutate();
          }} disabled={sync.isPending}>
            <RefreshCw className={sync.isPending ? "spin" : ""} size={17} />
            {sync.isPending ? "同步中" : `同步平台${mediaName}模型`}
          </button>}
        </div>
      </div>
      {sync.error && <p className="error">{sync.error.message}</p>}
      {models.isLoading ? (
        <div className="loading-state">
          <RefreshCw className="spin" />
          正在读取已保存的模型目录…
        </div>
      ) : models.error ? (
        <p className="error">{models.error.message}</p>
      ) : (
        <>
          <div className="stats">
            <Metric
              icon={media === "audio" ? <AudioLines /> : <Boxes />}
              label={`${providerDisplayName(providerID, providers.data)} ${mediaName}模型`}
              value={rows.length}
            />
            <Metric
              icon={<CirclePlay />}
              label="对外开放"
              value={rows.filter((r) => r.exposed).length}
            />
            <Metric
              icon={<Activity />}
              label="目录来源"
              value={catalogSourceLabel(models.data?.catalog_source, models.data?.schema_version)}
            />
          </div>
          {featured.length > 0 && (
            <>
              <div className="section-heading">
                <div>
                  <span className="eyebrow">Public Models</span>
                  <h2>全部对外模型</h2>
                </div>
                <small>当前 API 默认可用</small>
              </div>
              <FeaturedModels rows={featured} providerID={providerID} providers={providers.data || []} />
              <CostReference
                rows={featured}
                costs={costs.data || []}
                rules={rules.data || []}
                kind={media}
                providerID={providerID}
                unit={costUnit}
                loading={rules.isLoading}
                error={rules.error}
              />
            </>
          )}
          {rows.length === 0 ? (
            <div className="empty-state">
              <Boxes />
              <strong>还没有保存模型目录</strong>
              <span>{models.data?.sync_supported ? "点击同步按钮从当前平台获取并保存目录。" : "当前平台尚未配置可用模型。"}</span>
            </div>
          ) : (
            <>
              <div className="section-heading catalog-heading">
                <div>
                  <span className="eyebrow">Platform Catalog</span>
                  <h2>完整平台目录</h2>
                </div>
                <small>默认仅显示对外模型</small>
              </div>
              <div className="catalog-controls">
                <label className="search-box">
                  <Search size={17} />
                  <input
                    aria-label="搜索模型"
                    value={search}
                    onChange={(e) => {
                      setSearchParams((current) => {
                        const next = new URLSearchParams(current);
                        if (e.target.value) next.set("search", e.target.value);
                        else next.delete("search");
                        return next;
                      }, { replace: true });
                      setPage(1);
                    }}
                    placeholder="搜索名称、模型 ID 或平台"
                  />
                </label>
                <div className="segmented compact">
                  <button
                    className={scope === "exposed" ? "active" : ""}
                    onClick={() => {
                      setScope("exposed");
                      setPage(1);
                    }}
                  >
                    仅对外
                  </button>
                  <button
                    className={scope === "all" ? "active" : ""}
                    onClick={() => {
                      setScope("all");
                      setPage(1);
                    }}
                  >
                    完整目录
                  </button>
                </div>
                <span className="result-count">{visible.length} 个结果</span>
              </div>
              <div className="table model-table">
                <div className="row model head">
                  <span>模型</span>
                  <span>平台</span>
                  <span>计价方式</span>
                  <span>
                    {media === "image"
                      ? "质量"
                      : media === "video"
                        ? "时长 / 分辨率"
                        : "时长 / 计费"}
                  </span>
                  <span>开放状态</span>
                </div>
                {pagedModels.map((r) => (
                  <div className="row model interactive-row" key={r.platform.id} role="button" tabIndex={0} onClick={() => setSelectedModel(r)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); setSelectedModel(r); } }}>
                    <span>
                      <strong>{r.platform.name}</strong>
                      <small>{r.exposed ? r.public_id : "内部平台目录"}</small>
                    </span>
                    <span>{r.platform.provider || "未知"}</span>
                    <span>{modelCost(r.platform)}</span>
                    <span className="model-options">
                      {media === "image"
                        ? r.platform.quality_options?.join(" / ") || "固定"
                        : media === "video"
                          ? videoOptions(r.platform)
                          : audioOptions(r.platform)}
                    </span>
                    <span className="exposure">
                      <i
                        className={`status ${r.exposed ? "active" : "disabled"}`}
                      ></i>
                      {r.exposed ? (
                        <span>
                          <strong>{r.public_id}</strong>
                          <small>对外可用</small>
                        </span>
                      ) : (
                        "仅平台"
                      )}
                    </span>
                  </div>
                ))}
              </div>
              <div className="pagination">
                <span>
                  共 {visible.length} 个模型 · 第 {Math.min(page, totalPages)} /{" "}
                  {totalPages} 页
                </span>
                <div>
                  <label className="page-size-control">
                    每页
                    <select
                      value={pageSize}
                      onChange={(event) => {
                        setPageSize(Number(event.target.value));
                        setPage(1);
                      }}
                      aria-label="每页模型数量"
                    >
                      <option value={10}>10</option>
                      <option value={20}>20</option>
                      <option value={40}>40</option>
                    </select>
                  </label>
                  <button
                    className="secondary"
                    disabled={page <= 1}
                    onClick={() => setPage((value) => Math.max(1, value - 1))}
                  >
                    <ChevronLeft />
                    上一页
                  </button>
                  <button
                    className="secondary"
                    disabled={page >= totalPages}
                    onClick={() =>
                      setPage((value) => Math.min(totalPages, value + 1))
                    }
                  >
                    下一页
                    <ChevronRight />
                  </button>
                </div>
              </div>
              {visible.length === 0 && (
                <div className="empty-state compact-empty">
                  <Search />
                  <strong>没有匹配的模型</strong>
                  <span>调整搜索词或筛选条件。</span>
                </div>
              )}
              <Sheet open={Boolean(selectedModel)} onOpenChange={(open) => { if (!open) setSelectedModel(null); }}>
                <SheetContent className="model-detail-sheet">
                  <SheetTitle>{selectedModel?.public_id || selectedModel?.platform.name || "模型详情"}</SheetTitle>
                  <SheetDescription>平台能力、参数选项和对外映射。</SheetDescription>
                  {selectedModel && (
                    <div className="model-detail-content">
                      <div className="request-detail-grid">
                        <span><small>模型名称</small><strong>{selectedModel.platform.name}</strong></span>
                        <span><small>接入平台</small><strong>{providerDisplayName(providerID, providers.data)}</strong></span>
                        <span><small>模型厂商</small><strong>{selectedModel.platform.provider || "未知"}</strong></span>
                        <span><small>对外 ID</small><strong>{selectedModel.public_id || "未开放"}</strong></span>
                        <span><small>上游 ID</small><strong>{selectedModel.platform.id}</strong></span>
                        <span><small>计价方式</small><strong>{modelCost(selectedModel.platform)}</strong></span>
                        <span><small>开放状态</small><Badge tone={selectedModel.exposed ? "success" : "neutral"}>{selectedModel.exposed ? "对外可用" : "仅平台"}</Badge></span>
                      </div>
                      <section><h3>能力参数</h3><p>{selectedModel.platform.description || "平台未提供说明。"}</p></section>
                      <section className="model-capability-list">
                        <span><small>质量</small><strong>{selectedModel.platform.quality_options?.join(" / ") || "固定"}</strong></span>
                        <span><small>时长</small><strong>{selectedModel.platform.duration_options?.join(" / ") || "由模型决定"}</strong></span>
                        <span><small>分辨率</small><strong>{selectedModel.platform.resolution_modes?.map((value) => value.replace("RESOLUTION_", "")).join(" / ") || "由模型决定"}</strong></span>
                      </section>
                    </div>
                  )}
                </SheetContent>
              </Sheet>
            </>
          )}
        </>
      )}
      {showRules && (
        <CostRuleDialog
          key={providerID}
          providerID={providerID}
          provider={currentProvider}
          unit={costUnit}
          kind={media}
          rules={rules.data || []}
          close={() => setShowRules(false)}
          refresh={() => client.invalidateQueries({ queryKey: ["cost-rules", providerID, media] })}
        />
      )}
    </section>
  );
}

function emptyCostRule(providerID: string, kind: MediaKind = "image"): Omit<CostRule, "id" | "created_at" | "updated_at"> {
  const provider = providerDefinition(providerID);
  const model = modelDisplayID(provider.models[kind][0] || "");
  return {
    provider_id: providerID, kind, model,
    size: kind === "image" ? "1024x1024" : "",
    quality: kind === "image" ? "low" : providerID === "adobe" && model === "kling-3.0-omni" ? "t2v" : "",
    resolution: kind === "video" ? "720p" : "",
    duration: kind === "video" ? 8 : kind === "audio" ? 1 : 0,
    unit_tokens: 0, enabled: true, price_version: "manual-v1", source: "manual",
  };
}

function CostRuleDialog({
  providerID,
  provider,
  unit,
  kind,
  rules,
  close,
  refresh,
}: {
  providerID: string;
  provider?: Provider;
  unit: string;
  kind: MediaKind;
  rules: CostRule[];
  close: () => void;
  refresh: () => void;
}) {
  const currentRules = rules.filter((rule) => rule.enabled || rule.drifted);
  const [editing, setEditing] = useState<CostRule | ReturnType<typeof emptyCostRule>>(
    () => emptyCostRule(providerID, kind),
  );
  const save = useMutation({
    mutationFn: () =>
      api<CostRule>(
        `/admin/api/cost-rules/${"id" in editing ? editing.id : ""}`,
        {
          method: "id" in editing ? "PUT" : "POST",
          body: JSON.stringify(editing),
        },
      ),
    onSuccess: (data) => {
      refresh();
      setEditing(data);
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) =>
      api(`/admin/api/cost-rules/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      refresh();
      setEditing(emptyCostRule(providerID, kind));
    },
  });
  function change(patch: Partial<CostRule>) {
    setEditing((current) => ({ ...current, ...patch }));
  }
  function changeKind(kind: MediaKind) {
    change(emptyCostRule(providerID, kind));
  }
  return (
    <div
      className="overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <section
        className="dialog cost-rule-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="cost-rule-title"
      >
        <header className="dialog-heading">
          <div>
            <span className="eyebrow">Platform Cost</span>
            <h2 id="cost-rule-title">平台成本规则</h2>
            <p><ProviderBadge providerID={providerID} providers={provider ? [provider] : []} /> 任务创建时按这些规则预留 {unit}，成功后结算，失败或取消则释放。</p>
          </div>
          <button className="icon" aria-label="关闭" onClick={close}>
            <X />
          </button>
        </header>
        <div className="cost-rule-layout">
          <div className="cost-rule-list">
            <button
              className="secondary"
              onClick={() => setEditing(emptyCostRule(providerID, kind))}
            >
              <Plus />
              新增规则
            </button>
            <small className="cost-rule-list-summary">
              当前启用 {rules.filter((rule) => rule.enabled).length} 条
            </small>
            {currentRules.map((rule) => (
              <button
                key={rule.id}
                className={
                  "id" in editing && editing.id === rule.id ? "active" : ""
                }
                onClick={() => setEditing(rule)}
              >
                <span>
                  <strong>{rule.model}</strong>
                  <small>
                    {rule.kind === "audio"
                      ? audioRuleLabel(rule.model, rule.duration)
                      : [
                          rule.size,
                          rule.kind === "video" && rule.quality
                            ? videoWorkflowLabel(rule.quality)
                            : rule.quality,
                          rule.resolution,
                          rule.duration ? `${rule.duration} 秒` : "",
                        ]
                          .filter(Boolean)
                          .join(" · ")}
                  </small>
                </span>
                <b>{rule.unit_tokens.toLocaleString()} {unit}</b>
                <i
                  className={`status ${rule.drifted ? "failed" : rule.enabled ? "active" : "disabled"}`}
                  title={rule.drifted ? rule.drift_reason || "检测到价格漂移" : undefined}
                ></i>
              </button>
            ))}
          </div>
          <form
            className="cost-rule-form"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate();
            }}
          >
            {editing.drifted ? (
              <p className="error">
                价格规则已自动熔断：{editing.drift_reason || "预计积分与账户实际扣减不一致"}
              </p>
            ) : null}
            <div className="playground-fields">
              <label>
                类型
                <select
                  value={editing.kind}
                  onChange={(e) => changeKind(e.target.value as MediaKind)}
                >
                  {(["image", "video", "audio"] as MediaKind[]).filter((kind) => !provider || providerSupports(provider, kind)).map((kind) => <option key={kind} value={kind}>{kind === "image" ? "图像" : kind === "video" ? "视频" : "音频"}</option>)}
                </select>
              </label>
              <label>
                模型
                <input
                  required
                  value={editing.model}
                  onChange={(e) => {
                    const model = e.target.value;
                    change({
                      model,
                      ...(editing.kind === "video"
                        ? { quality: providerID === "adobe" && model === "kling-3.0-omni" ? (editing.quality || "t2v") : "" }
                        : {}),
                    });
                  }}
                />
              </label>
              {editing.kind === "image" ? (
                <>
                  <label>
                    尺寸
                    <input
                      required
                      value={editing.size}
                      onChange={(e) => change({ size: e.target.value })}
                    />
                  </label>
                  <label>
                    质量
                    <input
                      value={editing.quality}
                      onChange={(e) => change({ quality: e.target.value })}
                      placeholder="固定质量留空"
                    />
                  </label>
                </>
              ) : editing.kind === "video" ? (
                <>
                  {providerID === "adobe" && editing.model === "kling-3.0-omni" ? (
                    <label>
                      工作流
                      <select
                        value={editing.quality || "t2v"}
                        onChange={(e) => change({ quality: e.target.value })}
                      >
                        <option value="t2v">文生视频</option>
                        <option value="i2v">首帧 / 首尾帧</option>
                        <option value="rtv">参考图</option>
                      </select>
                    </label>
                  ) : null}
                  <label>
                    分辨率
                    <input
                      required
                      value={editing.resolution}
                      onChange={(e) => change({ resolution: e.target.value })}
                    />
                  </label>
                  <label>
                    时长
                    <input
                      required
                      type="number"
                      min="1"
                      value={editing.duration}
                      onChange={(e) =>
                        change({ duration: Number(e.target.value) })
                      }
                    />
                  </label>
                </>
              ) : (
                <label>
                  计费维度
                  <input
                    required
                    type="number"
                    min="0"
                    value={editing.duration}
                    onChange={(e) =>
                      change({ duration: Number(e.target.value) })
                    }
                  />
                  <small>
                    dialogue-v3 填 0（每千字符）；music-v1
                    填分钟；sound-effects-v2 填秒。
                  </small>
                </label>
              )}
              <label>
                单次成本（{unit}）
                <input
                  required
                  type="number"
                  min="0"
                  value={editing.unit_tokens}
                  onChange={(e) =>
                    change({ unit_tokens: Number(e.target.value) })
                  }
                />
              </label>
              <label>
                价格版本
                <input
                  required
                  value={editing.price_version}
                  onChange={(e) => change({ price_version: e.target.value })}
                />
              </label>
              <label>
                来源
                <input
                  required
                  value={editing.source}
                  onChange={(e) => change({ source: e.target.value })}
                />
              </label>
              <label>
                状态
                <select
                  value={String(editing.enabled)}
                  onChange={(e) =>
                    change({ enabled: e.target.value === "true" })
                  }
                >
                  <option value="true">启用</option>
                  <option value="false">停用</option>
                </select>
              </label>
            </div>
            {save.error && <p className="error">{save.error.message}</p>}
            {remove.error && <p className="error">{remove.error.message}</p>}
            <footer>
              {"id" in editing ? (
                <button
                  type="button"
                  className="danger-button"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(editing.id)}
                >
                  <Trash2 />
                  删除
                </button>
              ) : (
                <span />
              )}
              <button disabled={save.isPending}>
                <Check />
                {save.isPending ? "保存中" : "保存规则"}
              </button>
            </footer>
          </form>
        </div>
      </section>
    </div>
  );
}
