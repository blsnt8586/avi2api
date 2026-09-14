import React, { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "../shared/api";
import type { AccountsPage, MediaKind } from "../shared/types";
import catalogJSON from "../shared/creativefabrica-pricing-catalog.json";
import evidenceJSON from "../shared/creativefabrica-pricing-evidence.json";

type Scalar = string | number | boolean;
type OptionSpec = { allowedValues?: Scalar[]; defaultValue?: Scalar; min?: Scalar; max?: Scalar };
type QuoteModel = {
  options: Record<string, Record<string, OptionSpec>>;
  pricing_inputs: { options?: string[]; media?: { role: string; requiredMetadata?: string[] }[] };
};
type Quote = {
  coins: number; credit_unit: string; checked_at: string; notice: string;
  resolved_options?: unknown; model: string; pricing_type: string;
  request: { options: unknown; input_media: unknown };
  default_flow_total?: number; default_flow_quantity?: number; default_flow_notice?: string;
};
const catalog = catalogJSON as unknown as { checked_at: string; models: Record<string, QuoteModel> };
const evidence = evidenceJSON as unknown as Record<string, {
  checked_at: string; static_coins?: number;
  quotes: { options: Record<string, Scalar>; coins: number; aspects: string[] }[];
  flow_counts?: Record<string, number>; flow_checked_at?: string;
}>;
const labels: Record<string, string> = {
  duration_seconds: "输出时长（秒）", resolution: "分辨率", imageSize: "图像尺寸",
  aspect_ratio: "画面比例", aspectRatio: "画面比例", quality: "质量", mode: "模式",
  outputCount: "输出张数", generate_audio: "生成音频", output_format: "输出格式",
};

function initialOptions(model?: QuoteModel) {
  const result: Record<string, Scalar> = {};
  for (const [key, definition] of Object.entries(model?.options || {})) {
    const [type, spec] = Object.entries(definition)[0] || [];
    if (!spec) continue;
    const value = spec.defaultValue ?? spec.allowedValues?.[0] ?? spec.min;
    if (value === undefined) continue;
    result[key] = type === "integerType" || type === "numberType" ? Number(value) : value;
  }
  return result;
}

export function CreativeFabricaCostQuote({ kind, models }: { kind: MediaKind; models: string[] }) {
  const [accountID, setAccountID] = useState("");
  const [modelID, setModelID] = useState("");
  const [options, setOptions] = useState<Record<string, Scalar>>({});
  const [media, setMedia] = useState("[]");
  const model = catalog.models[modelID];
  const [accountPage, setAccountPage] = useState(1);
  const accounts = useQuery({
    queryKey: ["cf-quote-accounts", accountPage],
    queryFn: () => api<AccountsPage>(`/admin/api/accounts?provider=creativefabrica&page_size=100&page=${accountPage}`),
  });
  const quote = useMutation({
    mutationFn: async () => {
      const inputMedia: unknown = JSON.parse(media);
      if (!Array.isArray(inputMedia)) throw new Error("素材元数据必须为 JSON 数组");
      return api<Quote>(`/admin/api/accounts/${accountID}/cost-quote`, {
        method: "POST",
        body: JSON.stringify({ kind, model: modelID, options, input_media: inputMedia }),
      });
    },
  });
  const changeModel = (id: string) => {
    setModelID(id);
    setOptions(initialOptions(catalog.models[id]));
    setMedia("[]");
    quote.reset();
  };
  return (
    <details className="panel" style={{ margin: "16px 0", padding: 16 }}>
      <summary style={{ cursor: "pointer" }}>Creative Fabrica 官方即时报价 · 不生成、不扣积分</summary>
      <p>选择账号和具体参数，向官方计算器查询 Coins。报价不自动覆盖本地计费规则，参考素材只填写计费元数据，无需上传文件。</p>
      <fieldset disabled={quote.isPending} style={{ border: 0, padding: 0, margin: 0 }}>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 12 }}>
          <label>查询账号
            <select value={accountID} onChange={(e) => { setAccountID(e.target.value); quote.reset(); }} style={{ display: "block", width: "100%" }}>
              <option value="">请选择账号</option>
              {accounts.data?.data.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}
            </select>
          </label>
          <label>模型
            <select value={modelID} onChange={(e) => changeModel(e.target.value)} style={{ display: "block", width: "100%" }}>
              <option value="">请选择模型</option>
              {models.map((id) => <option key={id} value={id}>{id}</option>)}
            </select>
          </label>
        </div>
        {accounts.error && <p role="alert">账号列表读取失败：{accounts.error.message}</p>}
        {(accounts.data?.total || 0) > 100 && <div>
          <button type="button" disabled={accountPage === 1} onClick={() => { setAccountPage(accountPage - 1); setAccountID(""); quote.reset(); }}>上一页账号</button>
          <span>第 {accountPage} 页</span>
          <button type="button" disabled={accountPage * 100 >= (accounts.data?.total || 0)} onClick={() => { setAccountPage(accountPage + 1); setAccountID(""); quote.reset(); }}>下一页账号</button>
        </div>}
        {modelID && !model && <p role="alert">该型号尚无已核对的参数定义，请先更新目录。</p>}
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 12, marginTop: 16 }}>
          {Object.entries(model?.options || {}).map(([name, definition]) => {
            const [type, spec] = Object.entries(definition)[0] || [];
            if (!spec || (!(name in options) && !spec.allowedValues)) return null;
            const update = (value: Scalar) => { setOptions({ ...options, [name]: value }); quote.reset(); };
            return <label key={name}>{labels[name] || name}
              {spec.allowedValues ? (
                <select value={String(options[name] ?? "")} onChange={(e) => update(type === "integerType" || type === "numberType" ? Number(e.target.value) : e.target.value)} style={{ display: "block", width: "100%" }}>
                  {spec.allowedValues.map((value) => <option key={String(value)} value={String(value)}>{String(value)}</option>)}
                </select>
              ) : type === "booleanType" ? (
                <input type="checkbox" checked={Boolean(options[name])} onChange={(e) => update(e.target.checked)} />
              ) : (
                <input type={type === "integerType" || type === "numberType" ? "number" : "text"}
                  min={spec.min === undefined ? undefined : Number(spec.min)} max={spec.max === undefined ? undefined : Number(spec.max)}
                  step={type === "integerType" ? 1 : "any"} value={String(options[name] ?? "")}
                  onChange={(e) => update(type === "integerType" || type === "numberType" ? Number(e.target.value) : e.target.value)}
                  style={{ display: "block", width: "100%" }} />
              )}
            </label>;
          })}
        </div>
        {(model?.pricing_inputs.media || []).length > 0 && <div style={{ marginTop: 16 }}>
          <label>参考素材计费元数据（JSON 数组）
            <textarea rows={5} value={media} onChange={(e) => { setMedia(e.target.value); quote.reset(); }} style={{ display: "block", width: "100%", fontFamily: "monospace" }} />
          </label>
          <p>durationSeconds 为输入时长，widthPx/heightPx 为像素尺寸。源/参考视频按官网 Math.round 四舍五入；音频的小数秒换算尚未核实，目前接受整数元数据。结果中可查看实际计价值。</p>
          {model?.pricing_inputs.media?.map((item) => <button type="button" key={item.role} onClick={() => {
            setMedia(JSON.stringify([{ role: item.role, metadata: Object.fromEntries((item.requiredMetadata || []).map((key) => [key, key === "durationSeconds" ? 5 : key === "widthPx" ? 1280 : key === "fps" ? 24 : 720])) }], null, 2));
            quote.reset();
          }}>填入 {item.role} 示例</button>)}
        </div>}
        <p><small>参数定义核对时间：{new Date(catalog.checked_at).toLocaleString()}。提交时服务端会重新核对账号当前目录。</small></p>
        <button type="button" className="primary" disabled={!accountID || !model || !models.includes(modelID)} onClick={() => quote.mutate()}>
          {quote.isPending ? "正在查询官方报价…" : "查询官方 Coins"}
        </button>
      </fieldset>
      {quote.error && <p role="alert">{quote.error.message}</p>}
      {quote.data && <div role="status" style={{ marginTop: 16 }}>
        <strong>通用模型计算器报价：{quote.data.coins.toLocaleString()} Coins</strong>
        <p>{quote.data.model} · {new Date(quote.data.checked_at).toLocaleString()}</p>
        <p>{quote.data.notice}</p>
        {quote.data.default_flow_total !== undefined && <strong>默认 Flow 整单（{quote.data.default_flow_quantity} 张）：{quote.data.default_flow_total.toLocaleString()} Coins</strong>}
        {quote.data.default_flow_notice && <p>{quote.data.default_flow_notice}</p>}
        <details><summary>官方实际采用的参数</summary><pre style={{ overflow: "auto" }}>{JSON.stringify({ options: quote.data.resolved_options || quote.data.request.options, input_media: quote.data.request.input_media }, null, 2)}</pre></details>
      </div>}
      {modelID && evidence[modelID] && <details style={{ marginTop: 16 }}>
        <summary style={{ cursor: "pointer" }}>查看已核对的参数报价与默认整单价</summary>
        <p>通用计算器快照：{new Date(evidence[modelID].checked_at).toLocaleString()}。这是上游估价证据，不等于本地已启用价格规则，也不必然等于 Flow 链路成本。</p>
        {evidence[modelID].static_coins !== undefined ? (
          <p>官方 STATIC 报价：<strong>{evidence[modelID].static_coins?.toLocaleString()} Coins</strong>。已查参数轴返回同一模型报价；多张输出的整单费用仍待账单核对。</p>
        ) : <div style={{ overflowX: "auto", maxHeight: 360 }}>
          <table style={{ width: "100%", textAlign: "left", borderCollapse: "collapse" }}>
            <thead><tr><th>参数</th><th>已查比例</th><th>Coins</th></tr></thead>
            <tbody>{evidence[modelID].quotes.map((row, index) => <tr key={index}>
              <td style={{ padding: 8 }}>{Object.entries(row.options).map(([name, value]) => `${labels[name] || name}=${value}`).join("；")}</td>
              <td style={{ padding: 8 }}>{row.aspects.join(" / ")}</td>
              <td style={{ padding: 8, whiteSpace: "nowrap" }}>{row.coins.toLocaleString()}</td>
            </tr>)}</tbody>
          </table>
        </div>}
        {evidence[modelID].flow_counts && <div style={{ overflowX: "auto", maxHeight: 280, marginTop: 12 }}>
          <p>官网默认 Flow 整单报价（无素材、未指定额外参数）。当前图片生成走 Flow 链路，请勿混用上方通用计算器单价。</p>
          <table style={{ width: "100%", textAlign: "left" }}>
            <thead><tr><th>张数</th><th>整单 Coins</th></tr></thead>
            <tbody>{Object.entries(evidence[modelID].flow_counts || {}).sort(([a], [b]) => Number(a) - Number(b)).map(([count, coins]) => (
              <tr key={count}><td>{count}</td><td>{coins.toLocaleString()}</td></tr>
            ))}</tbody>
          </table>
        </div>}
      </details>}
    </details>
  );
}
