import React, { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { BadgeDollarSign, Calculator, Coins, Save, TrendingUp } from "lucide-react";
import { Metric } from "../components/metric";
import { Badge, DataTable, Pagination } from "../components/ui";
import { api } from "../shared/api";
import type {
  CostRule,
  MediaKind,
  Provider,
  SalePricingItemQuote,
  SalePricingProfile,
  SalePricingQuoteResponse,
  SalePricingSettings,
  SalePricingVideoRateQuote,
} from "../shared/types";

type PricingForm = {
  provider_id: string;
  currency: string;
  account_cost: number;
  included_credits: number;
  usable_percent: number;
  overhead_percent: number;
  payment_fee_percent: number;
  target_margin_percent: number;
  rounding_step: number;
};

type QuoteRow = {
  rule: CostRule;
  result?: SalePricingItemQuote;
};

type VideoRateGroup = {
  model: string;
  resolution: string;
  durations: number[];
};

type VideoRateRow = {
  group: VideoRateGroup;
  result?: SalePricingVideoRateQuote;
};

const defaultRates = {
  usable_percent: 90,
  overhead_percent: 5,
  payment_fee_percent: 3,
  target_margin_percent: 35,
  rounding_step: 0.01,
};

function formForProvider(providerID: string, profile?: SalePricingProfile): PricingForm {
  return {
    provider_id: providerID,
    currency: profile?.currency || "CNY",
    account_cost: profile?.account_cost || 0,
    included_credits: profile?.included_credits || 0,
    usable_percent: profile ? profile.usable_credit_rate * 100 : defaultRates.usable_percent,
    overhead_percent: profile ? profile.overhead_rate * 100 : defaultRates.overhead_percent,
    payment_fee_percent: profile ? profile.payment_fee_rate * 100 : defaultRates.payment_fee_percent,
    target_margin_percent: profile ? profile.target_margin * 100 : defaultRates.target_margin_percent,
    rounding_step: profile?.rounding_step || defaultRates.rounding_step,
  };
}

function profileFromForm(form: PricingForm): SalePricingProfile {
  return {
    provider_id: form.provider_id,
    currency: form.currency.trim().toUpperCase(),
    account_cost: form.account_cost,
    included_credits: Math.round(form.included_credits),
    usable_credit_rate: form.usable_percent / 100,
    overhead_rate: form.overhead_percent / 100,
    payment_fee_rate: form.payment_fee_percent / 100,
    target_margin: form.target_margin_percent / 100,
    rounding_step: form.rounding_step,
  };
}

function validForm(form: PricingForm) {
  const numericValues = [
    form.account_cost,
    form.included_credits,
    form.usable_percent,
    form.overhead_percent,
    form.payment_fee_percent,
    form.target_margin_percent,
    form.rounding_step,
  ];
  return (
    numericValues.every(Number.isFinite) &&
    /^[A-Za-z]{3}$/.test(form.currency.trim()) &&
    form.account_cost > 0 &&
    form.account_cost <= 1_000_000_000 &&
    Number.isInteger(form.included_credits) &&
    form.included_credits >= 1 &&
    form.included_credits <= 1_000_000_000_000 &&
    form.usable_percent > 0 &&
    form.usable_percent <= 100 &&
    form.overhead_percent >= 0 &&
    form.overhead_percent <= 1000 &&
    form.payment_fee_percent >= 0 &&
    form.payment_fee_percent < 100 &&
    form.target_margin_percent >= 0 &&
    form.target_margin_percent < 100 &&
    form.payment_fee_percent + form.target_margin_percent < 99 &&
    form.rounding_step > 0 &&
    form.rounding_step <= 1000
  );
}

function useDebouncedValue<T>(value: T, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [delay, value]);
  return debounced;
}

function money(value: number, currency: string, maxDigits = 4) {
  try {
    return new Intl.NumberFormat("zh-CN", {
      style: "currency",
      currency: currency.toUpperCase(),
      minimumFractionDigits: 2,
      maximumFractionDigits: maxDigits,
    }).format(Number.isFinite(value) ? value : 0);
  } catch {
    return `${currency.toUpperCase()} ${(Number.isFinite(value) ? value : 0).toFixed(2)}`;
  }
}

function ruleParameters(rule: CostRule) {
  if (rule.kind === "image") return [rule.size, rule.quality || "固定质量"].filter(Boolean).join(" · ");
  if (rule.kind === "video") return [rule.resolution, rule.duration ? `${rule.duration} 秒` : ""].filter(Boolean).join(" · ");
  return rule.duration ? `${rule.duration} ${rule.model === "music-v1" ? "分钟" : "秒"}` : "模型默认参数";
}

function kindLabel(kind: MediaKind) {
  return kind === "image" ? "图片" : kind === "video" ? "视频" : "音频";
}

function videoRateKey(model: string, resolution: string) {
  return `${model}\u0000${resolution}`;
}

function groupVideoRules(rules: CostRule[]): VideoRateGroup[] {
  const groups = new Map<string, VideoRateGroup>();
  for (const rule of rules) {
    if (rule.kind !== "video" || !rule.duration) continue;
    const key = videoRateKey(rule.model, rule.resolution);
    const group = groups.get(key) || { model: rule.model, resolution: rule.resolution, durations: [] };
    if (!group.durations.includes(rule.duration)) group.durations.push(rule.duration);
    groups.set(key, group);
  }
  return [...groups.values()]
    .map((group) => ({ ...group, durations: group.durations.sort((a, b) => a - b) }))
    .sort((a, b) => a.model.localeCompare(b.model) || a.resolution.localeCompare(b.resolution));
}

function durationRange(durations: number[]) {
  if (!durations.length) return "时长待核算";
  const continuous = durations.every((value, index) => index === 0 || value === durations[index - 1] + 1);
  if (continuous && durations.length > 1) return `${durations[0]}–${durations[durations.length - 1]} 秒`;
  return `${durations.join("/")} 秒`;
}

function creditsPerSecond(value: number) {
  const roundedUp = Math.ceil((value - Number.EPSILON) * 1000) / 1000;
  return `${roundedUp.toLocaleString("zh-CN", { maximumFractionDigits: 3 })} /秒`;
}

const pricingModifierLabels: Record<string, string> = {
  reference_video: "参考视频",
  seedance_video_reference: "Seedance 参考视频",
  flux_video_reference: "FLUX 3 Video 参考视频",
  kling_o3_video_reference: "Kling O3 Omni 参考视频",
  native_audio_disabled: "关闭原生音频",
};

export function SalePricing() {
  const client = useQueryClient();
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => api<Provider[]>("/admin/api/providers"),
  });
  const rules = useQuery({
    queryKey: ["cost-rules"],
    queryFn: () => api<CostRule[]>("/admin/api/cost-rules"),
  });
  const settings = useQuery({
    queryKey: ["sale-pricing"],
    queryFn: () => api<SalePricingSettings>("/admin/api/sale-pricing"),
  });
  const [providerID, setProviderID] = useState("");
  const [form, setForm] = useState<PricingForm>(() => formForProvider(""));
  const [manualCredits, setManualCredits] = useState(1000);
  const [kind, setKind] = useState<MediaKind>("image");
  const [model, setModel] = useState("");
  const [videoAudioMode, setVideoAudioMode] = useState<"default" | "off">("default");
  const [hasVideoReference, setHasVideoReference] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  useEffect(() => {
    if (providerID || !providers.data?.length) return;
    setProviderID((providers.data.find((provider) => provider.enabled) || providers.data[0]).id);
  }, [providerID, providers.data]);

  const persistedProfile = settings.data?.profiles.find((profile) => profile.provider_id === providerID);
  useEffect(() => {
    if (!providerID || settings.isPending) return;
    setForm(formForProvider(providerID, persistedProfile));
  }, [providerID, settings.isPending, persistedProfile]);

  const persistedForm = formForProvider(providerID, persistedProfile);
  const dirty = JSON.stringify(form) !== JSON.stringify(persistedForm);
  const currency = form.currency.trim().toUpperCase() || "CNY";

  const save = useMutation({
    mutationFn: (profile: SalePricingProfile | null) => {
      const profiles = (settings.data?.profiles || []).filter((item) => item.provider_id !== providerID);
      if (profile) profiles.push(profile);
      return api<SalePricingSettings>("/admin/api/sale-pricing", {
        method: "PUT",
        body: JSON.stringify({ profiles }),
      });
    },
    onSuccess: (data) => {
      client.setQueryData(["sale-pricing"], data);
      void client.invalidateQueries({ queryKey: ["audit-page"] });
    },
  });

  const activeRules = (rules.data || []).filter(
    (rule) => rule.enabled && !rule.drifted && (rule.provider_id || "leonardo") === providerID,
  );
  const models = [...new Set(activeRules.filter((rule) => rule.kind === kind).map((rule) => rule.model))].sort();
  const filteredRules = activeRules
    .filter((rule) => rule.kind === kind && kind !== "video" && (!model || rule.model === model))
    .sort((a, b) => a.model.localeCompare(b.model) || a.unit_tokens - b.unit_tokens || a.id - b.id);
  const filteredVideoGroups = groupVideoRules(activeRules).filter((group) => !model || group.model === model);
  const resultCount = kind === "video" ? filteredVideoGroups.length : filteredRules.length;
  const totalPages = Math.max(1, Math.ceil(resultCount / pageSize));
  const currentPage = Math.min(page, totalPages);
  const pagedRules = filteredRules.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const pagedVideoGroups = filteredVideoGroups.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const quotePayload = validForm(form) ? {
    profile: profileFromForm(form),
    manual_credits: [...new Set([1000, manualCredits])],
    items: pagedRules.map((rule) => ({ rule_id: rule.id })),
    video_rates: pagedVideoGroups.map((group) => ({
      model: group.model,
      resolution: group.resolution,
      ...(videoAudioMode === "off" ? { generate_audio: false } : {}),
      ...(hasVideoReference ? { has_video_reference: true } : {}),
    })),
  } : undefined;
  const quotePayloadJSON = quotePayload ? JSON.stringify(quotePayload) : "";
  const debouncedPayloadJSON = useDebouncedValue(quotePayloadJSON, 250);
  const quote = useQuery({
    queryKey: ["sale-pricing-quote", providerID, debouncedPayloadJSON],
    queryFn: () => api<SalePricingQuoteResponse>("/admin/api/sale-pricing/quote", {
      method: "POST",
      body: debouncedPayloadJSON,
    }),
    enabled: Boolean(debouncedPayloadJSON),
    placeholderData: undefined,
    retry: false,
  });
  const quoteIsCurrent = Boolean(quotePayloadJSON) && quotePayloadJSON === debouncedPayloadJSON && !quote.isFetching;
  const quoteData = quoteIsCurrent ? quote.data : undefined;
  const itemQuotes = new Map(quoteData?.items.map((item) => [item.rule_id, item]) || []);
  const rows: QuoteRow[] = pagedRules.map((rule) => ({ rule, result: itemQuotes.get(rule.id) }));
  const videoRateQuotes = new Map((quoteData?.video_rates || []).map((item) => [videoRateKey(item.model, item.resolution), item]));
  const videoRows: VideoRateRow[] = pagedVideoGroups.map((group) => ({ group, result: videoRateQuotes.get(videoRateKey(group.model, group.resolution)) }));
  const calculated = quoteData?.economics;
  const manualQuote = quoteData?.manual_quotes.find((item) => item.credits === manualCredits);
  const thousandQuote = quoteData?.manual_quotes.find((item) => item.credits === 1000);

  useEffect(() => {
    setPage(1);
    setModel("");
    setVideoAudioMode("default");
    setHasVideoReference(false);
  }, [providerID, kind]);

  useEffect(() => setPage(1), [model, pageSize]);

  const columns = useMemo<ColumnDef<QuoteRow>[]>(() => [
    {
      header: "模型",
      cell: ({ row }) => <span className="pricing-model-cell"><strong>{row.original.rule.model}</strong><Badge tone="neutral">{kindLabel(row.original.rule.kind)}</Badge></span>,
    },
    {
      header: "参数组合",
      cell: ({ row }) => <span className="pricing-parameter-cell"><strong>{ruleParameters(row.original.rule)}</strong>{row.original.result?.applied_modifiers.map((modifier) => <small key={modifier}>{pricingModifierLabels[modifier] || modifier}</small>)}</span>,
    },
    {
      header: "最终积分",
      cell: ({ row }) => row.original.result?.available
        ? <strong>{row.original.result.base_credits === row.original.result.effective_credits ? row.original.result.effective_credits.toLocaleString() : `${row.original.result.base_credits.toLocaleString()} → ${row.original.result.effective_credits.toLocaleString()}`}</strong>
        : <span className="pricing-unavailable">{row.original.result?.error || "核算中"}</span>,
    },
    { header: "内部成本", cell: ({ row }) => row.original.result?.quote ? money(row.original.result.quote.cost, currency) : "—" },
    { header: "建议售价", cell: ({ row }) => row.original.result?.quote ? <strong className="pricing-price">{money(row.original.result.quote.price, currency, 2)}</strong> : "—" },
    { header: "支付手续费", cell: ({ row }) => row.original.result?.quote ? money(row.original.result.quote.payment_fee, currency) : "—" },
    { header: "净利润", cell: ({ row }) => row.original.result?.quote ? money(row.original.result.quote.profit, currency) : "—" },
    { header: "实际毛利", cell: ({ row }) => row.original.result?.quote ? `${(row.original.result.quote.margin * 100).toFixed(1)}%` : "—" },
  ], [currency]);

  const videoColumns = useMemo<ColumnDef<VideoRateRow>[]>(() => [
    {
      header: "模型",
      cell: ({ row }) => <span className="pricing-model-cell"><strong>{row.original.group.model}</strong><Badge tone="neutral">视频</Badge></span>,
    },
    {
      header: "分辨率 / 时长",
      cell: ({ row }) => <span className="pricing-parameter-cell"><strong>{row.original.group.resolution}</strong><small>{durationRange(row.original.result?.durations || row.original.group.durations)}</small>{row.original.result?.applied_modifiers.map((modifier) => <small key={modifier}>{pricingModifierLabels[modifier] || modifier}</small>)}</span>,
    },
    {
      header: "每秒积分",
      cell: ({ row }) => row.original.result?.available
        ? <strong>{row.original.result.base_credits_per_second === row.original.result.effective_credits_per_second
          ? creditsPerSecond(row.original.result.effective_credits_per_second)
          : `${creditsPerSecond(row.original.result.base_credits_per_second)} → ${creditsPerSecond(row.original.result.effective_credits_per_second)}`}</strong>
        : <span className="pricing-unavailable">{row.original.result?.error || "核算中"}</span>,
    },
    { header: "内部成本 / 秒", cell: ({ row }) => row.original.result?.quote ? money(row.original.result.quote.cost, currency) : "—" },
    { header: "建议售价 / 秒", cell: ({ row }) => row.original.result?.quote ? <strong className="pricing-price">{money(row.original.result.quote.price, currency)}</strong> : "—" },
    { header: "支付手续费 / 秒", cell: ({ row }) => row.original.result?.quote ? money(row.original.result.quote.payment_fee, currency) : "—" },
    { header: "净利润 / 秒", cell: ({ row }) => row.original.result?.quote ? money(row.original.result.quote.profit, currency) : "—" },
    { header: "实际毛利", cell: ({ row }) => row.original.result?.quote ? `${(row.original.result.quote.margin * 100).toFixed(1)}%` : "—" },
  ], [currency]);

  const switchProvider = (next: string) => {
    if (dirty && !window.confirm("当前平台有未保存修改，确认切换？")) return;
    setProviderID(next);
  };

  if (providers.isPending || rules.isPending || settings.isPending) {
    return <div className="loading-state"><Calculator className="spin" /><strong>正在加载成本与价格规则</strong></div>;
  }
  if (providers.isError || rules.isError || settings.isError) {
    return <p className="error">成本核算数据加载失败。</p>;
  }
  if (!providers.data?.length) {
    return <p className="error">当前没有已配置的平台。</p>;
  }

  return (
    <section className="pricing-workspace">
      <div className="pricing-summary">
        <Metric icon={<Coins />} label="可售积分" value={calculated ? Math.round(calculated.usable_credits).toLocaleString() : "待配置"} detail={`名义积分 ${form.included_credits.toLocaleString()}`} />
        <Metric icon={<Calculator />} label="每千积分成本" value={calculated ? money(calculated.cost_per_credit * 1000, currency) : "待配置"} detail="含账号损耗与运营成本" />
        <Metric icon={<BadgeDollarSign />} label="每千积分售价" value={thousandQuote ? money(thousandQuote.price, currency, 2) : "待配置"} detail={`目标毛利 ${form.target_margin_percent}%`} />
        <Metric icon={<TrendingUp />} label="单账号预计利润" value={calculated ? money(calculated.projected_profit, currency) : "待配置"} detail={calculated ? `预计收入 ${money(calculated.projected_revenue, currency)}` : "填写有效成本后计算"} />
      </div>

      <div className="pricing-columns">
        <section className="pricing-section pricing-profile">
          <div className="section-heading">
            <div><span className="eyebrow">Provider Economics</span><h2>平台成本画像</h2></div>
            <select aria-label="选择平台" value={providerID} onChange={(event) => switchProvider(event.target.value)}>
              {providers.data.map((provider) => <option key={provider.id} value={provider.id}>{provider.display_name}</option>)}
            </select>
          </div>
          <div className="pricing-form-grid">
            <NumberField label="账号采购价" value={form.account_cost} min={0} step={0.01} onChange={(account_cost) => setForm({ ...form, account_cost })} />
            <label>结算币种<input maxLength={3} value={form.currency} onChange={(event) => setForm({ ...form, currency: event.target.value.toUpperCase() })} /></label>
            <NumberField label="账号名义积分" value={form.included_credits} min={0} step={1} onChange={(included_credits) => setForm({ ...form, included_credits })} />
            <NumberField label="积分可售率（%）" value={form.usable_percent} min={0.01} max={100} step={0.1} onChange={(usable_percent) => setForm({ ...form, usable_percent })} />
            <NumberField label="运营成本率（%）" value={form.overhead_percent} min={0} step={0.1} onChange={(overhead_percent) => setForm({ ...form, overhead_percent })} />
            <NumberField label="支付费率（%）" value={form.payment_fee_percent} min={0} max={98} step={0.1} onChange={(payment_fee_percent) => setForm({ ...form, payment_fee_percent })} />
            <NumberField label="目标毛利率（%）" value={form.target_margin_percent} min={0} max={98} step={0.1} onChange={(target_margin_percent) => setForm({ ...form, target_margin_percent })} />
            <NumberField label="售价取整步长" value={form.rounding_step} min={0.0001} step={0.01} onChange={(rounding_step) => setForm({ ...form, rounding_step })} />
          </div>
          {!validForm(form) && <p className="error">请填写账号采购价、积分和有效费率；支付费率与目标毛利率之和必须低于 99%。</p>}
          {save.error && <p className="error">保存失败：{save.error.message}</p>}
          <div className="pricing-actions">
            <small>{persistedProfile ? (dirty ? "有未保存修改" : "当前画像已保存") : "当前平台尚未保存成本画像"}</small>
            {persistedProfile && <button className="secondary" disabled={save.isPending} onClick={() => window.confirm("确认删除当前平台成本画像？") && save.mutate(null)}>删除画像</button>}
            <button disabled={!validForm(form) || !dirty || save.isPending} onClick={() => save.mutate(profileFromForm(form))}><Save />{save.isPending ? "保存中" : "保存成本画像"}</button>
          </div>
        </section>

        <section className="pricing-section pricing-quick">
          <div className="section-heading"><div><span className="eyebrow">Quick Quote</span><h2>单次积分快速核算</h2></div></div>
          <label>任务积分<input type="number" min={1} step={1} value={manualCredits} onChange={(event) => setManualCredits(Math.max(1, Math.round(Number(event.target.value))))} /></label>
          <div className="pricing-quick-result">
            <span><small>内部成本</small><strong>{manualQuote ? money(manualQuote.cost, currency) : "—"}</strong></span>
            <span><small>建议售价</small><strong>{manualQuote ? money(manualQuote.price, currency, 2) : "—"}</strong></span>
            <span><small>到账净利润</small><strong>{manualQuote ? money(manualQuote.profit, currency) : "—"}</strong></span>
            <span><small>实际毛利</small><strong>{manualQuote ? `${(manualQuote.margin * 100).toFixed(1)}%` : "—"}</strong></span>
          </div>
          <code>净利润 = 售价 - 支付手续费 - 含损耗成本</code>
          {(quote.isFetching || (quotePayloadJSON && !quoteIsCurrent)) && <small className="pricing-calculating">正在按最新输入重新核算</small>}
          {quote.error && <p className="error">报价核算失败：{quote.error.message}</p>}
        </section>
      </div>

      <section className="pricing-rule-section">
        <div className="section-heading">
          <div><span className="eyebrow">Model Quotes</span><h2>模型对外报价</h2></div>
          <div className="pricing-filters">
            <select aria-label="媒体类型" value={kind} onChange={(event) => setKind(event.target.value as MediaKind)}>
              <option value="image">图片</option><option value="video">视频</option><option value="audio">音频</option>
            </select>
            <select aria-label="筛选模型" value={model} onChange={(event) => setModel(event.target.value)}>
              <option value="">全部模型</option>{models.map((item) => <option key={item} value={item}>{item}</option>)}
            </select>
            {kind === "video" && <>
              <select aria-label="原生音频场景" value={videoAudioMode} onChange={(event) => setVideoAudioMode(event.target.value as "default" | "off")}>
                <option value="default">默认原生音频</option><option value="off">关闭原生音频</option>
              </select>
              <label className="pricing-reference-toggle"><input type="checkbox" checked={hasVideoReference} onChange={(event) => setHasVideoReference(event.target.checked)} />包含参考视频</label>
            </>}
          </div>
        </div>
        {kind === "video"
          ? <DataTable className="pricing-rule-table" data={videoRows} columns={videoColumns} rowKey={(row) => videoRateKey(row.group.model, row.group.resolution)} empty="当前平台没有匹配的视频秒价" loading={quote.isFetching} />
          : <DataTable className="pricing-rule-table" data={rows} columns={columns} rowKey={(row) => String(row.rule.id)} empty="当前平台没有匹配的有效积分规则" loading={quote.isFetching} />}
        <Pagination page={currentPage} totalPages={totalPages} total={resultCount} pageSize={pageSize} onPage={setPage} onPageSize={setPageSize} />
      </section>
    </section>
  );
}

function NumberField({ label, value, min, max, step, onChange }: { label: string; value: number; min?: number; max?: number; step?: number; onChange: (value: number) => void }) {
  return <label>{label}<input type="number" value={value} min={min} max={max} step={step} onChange={(event) => onChange(Number(event.target.value))} /></label>;
}
