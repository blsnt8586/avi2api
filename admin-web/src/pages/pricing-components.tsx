import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Coins } from "lucide-react";
import { api } from "../shared/api";
import type { ImageCostEstimate, PlatformEstimate, PublicModel } from "../shared/types";
import { imageSizes } from "../shared/catalog";

export function EstimateTable({
  estimate,
  compact = false,
}: {
  estimate: PlatformEstimate;
  compact?: boolean;
}) {
  return (
    <div className={`estimate-table-wrap ${compact ? "compact" : ""}`}>
      <table className="estimate-table">
        <thead>
          <tr>
            <th>参数</th>
            {estimate.columns.map((c) => (
              <th key={c}>{c}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {estimate.rows.map((row) => (
            <tr key={row.label}>
              <th>{row.label}</th>
              {row.values.map((value, index) => (
                <td key={`${row.label}-${estimate.columns[index]}`}>
                  {value.toLocaleString()}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <p>{estimate.note}</p>
    </div>
  );
}

export function ImageCostCalculator({ model }: { model: PublicModel }) {
  const [size, setSize] = useState("1024x1024");
  const [quality, setQuality] = useState("low");
  const [quantity, setQuantity] = useState(1);
  const estimate = useMutation({
    mutationFn: () =>
      api<ImageCostEstimate>("/api-docs/image-estimate", {
        method: "POST",
        body: JSON.stringify({
          model,
          size: size.trim(),
          quality: model === "gpt-image-2" ? quality : undefined,
          n: model === "gpt-image-2" ? 1 : quantity,
        }),
      }),
  });
  const result = estimate.data;
  const basis = result
    ? {
        pixel_formula: "按像素与质量倍率",
        size_tier: `按 ${result.pricing_tier || "尺寸"} 档`,
        size_threshold: `按 ${result.pricing_tier || "尺寸阈值"} 档`,
        fixed: "固定单价",
      }[result.pricing_basis]
    : "";
  const resetResult = () => estimate.reset();
  return (
    <div className="image-cost-calculator">
      <form
        className="image-cost-form"
        onSubmit={(event) => {
          event.preventDefault();
          estimate.mutate();
        }}
      >
        <label>
          尺寸
          <input
            aria-label="积分计算尺寸"
            list={`image-cost-sizes-${model}`}
            value={size}
            onChange={(event) => {
              setSize(event.target.value);
              resetResult();
            }}
            placeholder="例如 1536x2048"
          />
          <datalist id={`image-cost-sizes-${model}`}>
            {imageSizes(model).map((value) => (
              <option key={value} value={value} />
            ))}
          </datalist>
        </label>
        {model === "gpt-image-2" ? (
          <label>
            质量
            <select
              aria-label="积分计算质量"
              value={quality}
              onChange={(event) => {
                setQuality(event.target.value);
                resetResult();
              }}
            >
              <option value="low">Low</option>
              <option value="medium">Medium</option>
              <option value="high">High</option>
            </select>
          </label>
        ) : (
          <label>
            质量
            <input value="模型固定" disabled aria-label="积分计算质量固定" />
          </label>
        )}
        <label>
          数量
          <input
            aria-label="积分计算数量"
            type="number"
            min="1"
            max={model === "gpt-image-2" ? 1 : 4}
            value={model === "gpt-image-2" ? 1 : quantity}
            disabled={model === "gpt-image-2"}
            onChange={(event) => {
              setQuantity(Number(event.target.value));
              resetResult();
            }}
          />
        </label>
        <button type="submit" disabled={estimate.isPending || !size.trim()}>
          <Coins size={17} />
          {estimate.isPending ? "计算中" : "计算积分"}
        </button>
      </form>
      {estimate.error ? (
        <p className="error">{estimate.error.message}</p>
      ) : result ? (
        <div className="image-cost-result" aria-live="polite">
          <div>
            <span>单张积分</span>
            <strong>{result.unit_tokens.toLocaleString()}</strong>
          </div>
          <div>
            <span>数量</span>
            <strong>× {result.quantity}</strong>
          </div>
          <div className="total">
            <span>总预留积分</span>
            <strong>{result.estimated_tokens.toLocaleString()}</strong>
          </div>
          <p>
            {basis}
            {result.quality_multiplier
              ? ` · 质量倍率 ${result.quality_multiplier}`
              : ""}
            {result.pricing_anchor
              ? ` · 计价锚点 ${result.pricing_anchor}`
              : ""}
            {` · ${result.price_version}`}
          </p>
          <code>{result.formula}</code>
        </div>
      ) : (
        <p className="image-cost-empty">
          输入任意合法尺寸即可得到与任务预留完全相同的积分结果。
        </p>
      )}
      <p className="image-cost-note">
        当前影响积分的参数由结果中的 cost_parameters 给出；response_format、output_format、output_compression、background、moderation、参考图和 reference_strength 不改变积分。
      </p>
    </div>
  );
}
