import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  AlertTriangle,
  AudioLines,
  Boxes,
  Check,
  CirclePlay,
  Coins,
  FlaskConical,
  Image as ImageIcon,
  Images,
} from "lucide-react";
import { Tabs, TabsList, TabsTrigger } from "../components/ui";
import type { ImageCostMatrix, MediaKind, Provider, PublicModel, PublicVideoModel, VideoCostEstimate } from "../shared/types";
import { CopyCode } from "../components/copy-code";
import { ProviderBadge, ProviderSwitcher } from "../components/provider-switcher";
import { imageSizeGroups } from "../shared/catalog";
import { api } from "../shared/api";
import {
  generationRoute,
  modelsForProvider,
  providerDefinition,
  providerDisplayName,
  providerSupports,
} from "../shared/providers";
import { ImageCostCalculator } from "./pricing-components";
import {
  PublicAudioModel,
  audioModelDocs,
  defaultVideoResolution,
  imageModelDocs,
  maxAudioReferences,
  maxImageReferences,
  maxVideoReferences,
  modelVideoSizes,
  supportsVideoEndFrame,
  videoModelDocs,
  videoResolutionForSize,
  videoResolutionsForSize,
} from "./media-specs";

const imagePricingNotes: Record<PublicModel, string> = {
  "gpt-image-2": "按真实像素计价：每百万像素 7 积分，再应用 low=1、medium=8.833、high=35.167 倍率并向上取整。",
	"adobe:gpt-image-2": "使用 Adobe BKS 在账号导入时读取的 1K / 2K / 4K 与质量档价格，不套用 Leonardo 像素公式。",
	"adobe:nano-banana-2": "使用 Adobe BKS 在账号导入时读取的 1K / 2K / 4K 固定质量价格。",
  "nano-banana-2": "Small 80、Medium 120、Large 160 积分；档位按 Leonardo 的宽度枚举和 1584/3168 特例判定。",
  "nano-banana-pro": "Small/Medium 140、Large 250 积分；Large 档按 Leonardo 的宽度规则判定。",
  "seedream-5.0-pro": "常规尺寸 45 积分；达到 2K 阈值后为 90 积分。",
};

type DocParameter = readonly [
  name: string,
  type: string,
  requirement: string,
  description: string,
  accepted: string,
];

type ImageEndpoint = "generation" | "reference";

function ImageSizeCost({
  size,
  model,
  costs,
  loading,
}: {
  size: string;
  model: PublicModel;
  costs?: ImageCostMatrix["rows"][number]["costs"];
  loading: boolean;
}) {
  return (
    <span className="image-size-cost-cell">
      <strong>{size}</strong>
      {loading ? (
        <small>积分计算中</small>
      ) : model === "gpt-image-2" || model === "adobe:gpt-image-2" ? (
        <small>
          低 {costs?.low?.toLocaleString() ?? "—"} · 中 {costs?.medium?.toLocaleString() ?? "—"} · 高 {costs?.high?.toLocaleString() ?? "—"}
        </small>
      ) : (
        <small>{costs?.fixed?.toLocaleString() ?? "—"} 积分/张</small>
      )}
    </span>
  );
}

function imageParametersForModel(
  model: PublicModel,
  endpoint: ImageEndpoint,
): DocParameter[] {
  const parameters: DocParameter[] = [];
  if (endpoint === "reference") {
    parameters.push(
      ["image / image[]", "file", "必填", "参考图片。", "PNG、JPEG 或 WebP，1–6 张"],
      ["reference_strength", "string", "可选", "参考图影响强度。", "LOW、MID、HIGH；默认 MID"],
    );
  }
  parameters.push(
	["model", "string", "必填", "平台与生成模型。", `${generationRoute(model).model}；格式为 平台/模型`],
    ["prompt", "string", "必填", endpoint === "reference" ? "图片修改要求。" : "图片内容描述。", `1–${imageModelDocs[model].promptMax.toLocaleString()} 个 Unicode 字符`],
    ["size", "string", "可选", "输出尺寸。", imageModelDocs[model].size],
		["n", "integer", "可选", "生成数量。", model === "gpt-image-2" || model.startsWith("adobe-") ? "固定为 1" : "1–4；默认 1"],
    ["response_format", "string", "可选", "结果格式。", "固定为 url"],
  );
  if (model === "gpt-image-2" || model === "adobe:gpt-image-2") {
    parameters.push(["quality", "string", "可选", "生成质量。", "auto、low、medium、high；默认 auto"]);
  }
  parameters.push(
    ["background", "string", "可选", "背景模式。", "auto 或 opaque；当前结果均为不透明"],
    ["moderation", "string", "可选", "内容审核。", "固定为 auto"],
    ["Idempotency-Key", "header", "必填", "标识一次付费业务操作。", "重试同一请求时必须复用原值"],
  );
  return parameters;
}

function chatParametersForModel(model: PublicModel): DocParameter[] {
	const route = generationRoute(model);
  return [
    [
      "model",
      "string",
      "可选",
	  "平台与图片模型。",
		`${route.model}；格式为 平台/模型，默认 leonardo/gpt-image-2`,
    ],
    [
      "messages",
      "object[]",
      "必填",
      "对话消息；最后一条 user 消息用于生成。",
      "至少 1 条，并包含 user 消息",
    ],
    [
      "messages[].role",
      "string",
      "必填",
      "消息角色。",
      "system、user 或 assistant",
    ],
    [
      "messages[].content",
      "string | part[]",
      "必填",
      "文字提示词，可附一张 Base64 参考图。",
      `最终 user 提示词最多 ${imageModelDocs[model].promptMax.toLocaleString()} 个 Unicode 字符；可附 1 张不超过 25 MiB 的 Base64 图片`,
    ],
    [
      "stream",
      "boolean",
      "可选",
      "是否使用 SSE。",
      "true 或 false；默认 false",
    ],
    [
      "Idempotency-Key",
      "header",
      "建议",
      "网络重试时复用原任务。",
      "同一业务请求保持相同值",
    ],
  ];
}

function ParameterTable({ parameters }: { parameters: DocParameter[] }) {
  return (
    <div className="param-table-wrap">
      <table className="param-table parameter-reference-table">
        <thead>
          <tr>
            <th>参数</th>
            <th>类型</th>
            <th>要求</th>
            <th>说明</th>
          </tr>
        </thead>
        <tbody>
          {parameters.map(([name, type, requirement, description, accepted]) => (
            <tr key={name}>
              <td><code>{name}</code></td>
              <td>{type}</td>
              <td>{requirement}</td>
              <td className="param-description">
                <span>{description}</span>
                <small>{accepted}</small>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function quickStartExample() {
  return `export BASE_URL="${window.location.origin}"\nexport AIV2API_API_KEY="leo_your_api_key"\n\n# 验证密钥并查看此密钥允许使用的模型\ncurl "$BASE_URL/v1/models" \\\n+  -H "Authorization: Bearer $AIV2API_API_KEY"`.replaceAll(
    "\n+",
    "\n",
  );
}

const imageResponseExample = `{\n  "created": 1784851200,\n  "data": [\n    {\n      "url": "https://cdn.example.com/generated/image.png",\n      "revised_prompt": "一张白色背景上的产品摄影"\n    }\n  ]\n}`;

const taskCreatedExample = `{\n  "id": "2b42f834-15dd-49ce-9c75-854302db18d0",\n  "kind": "video",\n  "status": "queued",\n  "progress": 0,\n  "queue_position": 6,\n  "model": "leonardo/kling-o3-omni",\n  "prompt": "一颗玻璃球缓慢旋转",\n  "retry_count": 0,\n  "cancel_requested": false,\n  "created_at": "2026-07-24T08:00:00Z",\n  "updated_at": "2026-07-24T08:00:00Z"\n}`;

const errorResponseExample = `{\n  "error": {\n    "message": "all eligible account queues are full",\n    "type": "account_queue_full",\n    "code": "account_queue_full"\n  }\n}`;

const taskStates = [
  ["queued", "正在本地队列等待"],
  ["processing", "正在提交或等待上游结果"],
  ["succeeded", "生成成功，从 result.data 读取结果"],
  ["failed", "生成失败，查看 error_code 和 error_message"],
  ["cancelled", "任务已取消"],
];

const publicErrors = [
  [
    "400",
    "invalid_request / invalid_task_id / idempotency_key_required / idempotency_key_too_long",
    "参数、任务 ID 不合法，或 Idempotency-Key 缺失、超过 255 个字符",
    "修正请求后重试",
  ],
  [
    "400",
    "prompt_too_long",
    "提示词超过当前模型上限；details 返回模型、实际字符数和最大字符数",
    "缩短提示词后重试；任务不会创建，也不会预留积分",
  ],
  ["401", "invalid_api_key", "API Key 缺失、无效或已停用", "更换有效密钥"],
  ["402", "insufficient_pool_balance", "可用积分不足", "补充积分后重试"],
  ["404", "not_found", "任务不存在或当前密钥无权访问", "检查任务 ID"],
  [
    "409",
    "idempotency_conflict / not_cancellable",
    "幂等键冲突，或任务已不可取消",
    "保持原请求体，或停止取消",
  ],
  [
    "413 / 415",
    "image_too_large / unsupported_image_type",
    "参考图过大或格式错误",
    "使用限制内的 PNG、JPEG、WebP",
  ],
  [
    "422",
	"cost_unavailable",
	"当前参数缺少有效价格规则",
	"更换已定价参数",
  ],
  [
    "429",
    "rate_limit_exceeded / daily_quota_exceeded",
    "API Key、IP 或每日图片配额触发限流",
    "按 Retry-After 等待并退避",
  ],
  [
    "502",
    "upstream_failed / generate_failed / image_download_failed",
    "上游明确生成失败、提交被拒绝或结果下载失败",
    "保留原 Idempotency-Key，退避后重试",
  ],
  [
    "503",
    "account_queue_full / api_key_capacity_exhausted / system_queue_full / system_overloaded / system_maintenance / provider_circuit_open / gateway_overloaded",
    "账号、API Key 或系统容量保护暂不可用",
    "按 Retry-After 重试",
  ],
];

function DeveloperOverview() {
  const quickstart = quickStartExample();
  return (
    <div className="developer-overview">
      <section id="quickstart" className="docs-overview-section">
        <div className="doc-section-title">
          <div>
            <span className="eyebrow">Start Here</span>
            <h2>快速开始</h2>
            <p>准备 API Key，所有请求都使用 Bearer Token。</p>
          </div>
          <CopyCode value={quickstart} />
        </div>
        <pre className="code-block">
          <code>{quickstart}</code>
        </pre>
        <div className="quickstart-steps">
          <div>
            <span>01</span>
            <strong>领取 API Key</strong>
            <p>
              管理员创建密钥，完整值只显示一次。
            </p>
          </div>
          <div>
            <span>02</span>
            <strong>查询模型</strong>
            <p>
              调用 <code>GET /v1/models</code>，确认密钥允许使用的模型。
            </p>
          </div>
          <div>
            <span>03</span>
            <strong>提交生成</strong>
            <p>
              提交生成，并为请求设置唯一 <code>Idempotency-Key</code>。
            </p>
          </div>
          <div>
            <span>04</span>
            <strong>读取结果</strong>
			<p>所有媒体生成都返回任务；每 3 秒查询一次直到终态。</p>
          </div>
        </div>
      </section>
      <section id="lifecycle" className="docs-overview-section">
        <div className="doc-section-title">
          <div>
            <span className="eyebrow">Async Tasks</span>
            <h2>异步任务生命周期</h2>
            <p>异步任务创建成功返回 HTTP 202；重复的幂等请求返回原任务和 HTTP 200。</p>
          </div>
        </div>
        <div className="status-reference">
          {taskStates.map(([status, meaning]) => (
            <div key={status}>
              <code>{status}</code>
              <span>{meaning}</span>
            </div>
          ))}
        </div>
        <h3>创建任务响应</h3>
        <pre className="code-block">
          <code>{taskCreatedExample}</code>
        </pre>
        <div className="doc-warning">
          <AlertTriangle />
          <div>
            <strong>轮询与取消</strong>
            <p>
              每 3 秒查询一次。只有 <code>queued</code> 可取消；进入{" "}
              <code>processing</code> 后继续查询原任务，不要重复提交。
            </p>
          </div>
        </div>
      </section>
      <section id="errors" className="docs-overview-section">
        <div className="doc-section-title">
          <div>
            <span className="eyebrow">Errors And Retries</span>
            <h2>错误与重试</h2>
            <p>非 2xx 响应统一返回 <code>error.message/type/code</code>。</p>
          </div>
        </div>
        <div className="error-contract">
          <pre className="code-block">
            <code>{errorResponseExample}</code>
          </pre>
          <p>
            429、503 和网络错误按 <code>Retry-After</code> 重试，并保留原{" "}
            <code>Idempotency-Key</code> 与请求体。
          </p>
        </div>
        <div className="param-table-wrap">
          <table className="param-table error-table">
            <thead>
              <tr>
                <th>HTTP</th>
                <th>错误码</th>
                <th>含义</th>
                <th>客户端处理</th>
              </tr>
            </thead>
            <tbody>
              {publicErrors.map(([status, code, meaning, action]) => (
                <tr key={`${status}-${code}`}>
                  <td>{status}</td>
                  <td>
                    <code>{code}</code>
                  </td>
                  <td>{meaning}</td>
                  <td>{action}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

export function APIDocs() {
  const [media, setMedia] = useState<MediaKind>("image");
  const [searchParams, setSearchParams] = useSearchParams();
  const providers = useQuery({
    queryKey: ["public-providers"],
    queryFn: () => api<Provider[]>("/api-docs/providers"),
    staleTime: 60 * 1000,
  });
  const requestedProviderID = searchParams.get("provider")?.trim().toLowerCase() || "leonardo";
  const providerID = providers.data?.find((provider) => provider.enabled && provider.id === requestedProviderID)?.id
    || (providers.data?.find((provider) => provider.enabled) || providers.data?.[0])?.id
    || requestedProviderID;
  const currentProvider = providers.data?.find((provider) => provider.id === providerID);
  const setProviderQuery = (nextProviderID: string, replace = false) => {
    const nextParams = new URLSearchParams(searchParams);
    nextParams.set("provider", nextProviderID);
    setSearchParams(nextParams, { replace });
  };
  useEffect(() => {
    if (requestedProviderID === providerID) return;
    const nextParams = new URLSearchParams(searchParams);
    nextParams.set("provider", providerID);
    setSearchParams(nextParams, { replace: true });
  }, [providerID, requestedProviderID, searchParams, setSearchParams]);
  const switchMedia = (nextMedia: MediaKind) => {
    setMedia(nextMedia);
    if (currentProvider && !providerSupports(currentProvider, nextMedia)) {
      const fallback = providers.data?.find((provider) => provider.enabled && providerSupports(provider, nextMedia));
      if (fallback) setProviderQuery(fallback.id);
    }
  };
  const switchProvider = (nextProviderID: string) => {
    setProviderQuery(nextProviderID);
    const nextProvider = providers.data?.find((provider) => provider.id === nextProviderID);
    if (nextProvider && !providerSupports(nextProvider, media)) {
      const nextMedia = (["image", "video", "audio"] as MediaKind[]).find((kind) => providerSupports(nextProvider, kind));
      if (nextMedia) setMedia(nextMedia);
    }
  };
  return (
    <Tabs value={media} onValueChange={(value) => switchMedia(value as MediaKind)} className="docs-workspace">
      <div className="docs-index">
        <span className="eyebrow">API Reference</span>
        <h2>开发者指南</h2>
        <a href="#quickstart">快速开始</a>
        <a href="#lifecycle">任务状态</a>
        <a href="#errors">错误与重试</a>
        <span className="docs-index-divider">生成接口</span>
        <TabsList className="docs-index-api-tabs" aria-label="生成接口类型">
          <TabsTrigger value="image"><ImageIcon />图像 API</TabsTrigger>
          <TabsTrigger value="video"><CirclePlay />视频 API</TabsTrigger>
          <TabsTrigger value="audio"><AudioLines />音频 API</TabsTrigger>
        </TabsList>
        <div className="docs-index-meta">
          <span>认证</span>
          <strong>Bearer Token</strong>
          <span>协议</span>
		  <strong>图像、视频与音频统一异步</strong>
          <span>规范</span>
          <a href="/openapi.json" target="_blank" rel="noreferrer">
            OpenAPI 3.1 JSON
          </a>
        </div>
      </div>
      <div className="docs-content">
        <DeveloperOverview />
        <div className="provider-business-header docs-provider-header">
          <div>
            <span className="eyebrow">Provider API</span>
            <h2>{providerDisplayName(providerID, providers.data || [])} 接口</h2>
            <p>{providerDefinition(providerID, currentProvider).description}。创建请求通过 <code>model=平台/模型</code> 自动路由，后续查询和取消只使用任务 ID。</p>
          </div>
          <ProviderSwitcher
            providers={providers.data || []}
            value={providerID}
            capability={media}
            onChange={switchProvider}
          />
        </div>
        {providers.error && <p className="error">平台目录加载失败：{providers.error.message}</p>}
        <div id="endpoints" className="endpoint-reference">
          {media === "image" ? (
            <ImageDocs key={providerID} providerID={providerID} />
          ) : media === "video" ? (
            <VideoDocs key={providerID} providerID={providerID} />
          ) : (
			<AudioDocs key={providerID} providerID={providerID} />
          )}
        </div>
      </div>
    </Tabs>
  );
}

function ImageDocs({ providerID }: { providerID: string }) {
  const availableModels = modelsForProvider(Object.keys(imageModelDocs) as PublicModel[], providerID);
	const supported = availableModels.length > 0;
  const [endpoint, setEndpoint] = useState<ImageEndpoint>("generation");
  const [model, setModel] = useState<PublicModel>(availableModels[0] || "gpt-image-2");
  const route = generationRoute(model);
  const doc = imageModelDocs[model];
  const chatParameters = chatParametersForModel(model);
  const sizeGroups = imageSizeGroups[model];
  const matrixSizes = sizeGroups.flatMap((group) => [group.small, group.medium, group.large]).filter((size): size is string => Boolean(size));
  const matrixQuery = useQuery({
    queryKey: ["public-image-cost-matrix", route.model],
    queryFn: () => api<ImageCostMatrix>("/api-docs/image-cost-matrix", {
      method: "POST",
      body: JSON.stringify({ model: route.model, sizes: matrixSizes }),
    }),
    staleTime: 5 * 60 * 1000,
	enabled: supported,
  });
  const costsBySize = new Map(matrixQuery.data?.rows.map((row) => [row.size, row.costs]) || []);
  const endpointPath = "/v1/images/generations";
  const params = imageParametersForModel(model, endpoint);
  const sample = endpoint === "generation" ? asyncImageExample(model) : editExample(model);
  const responseExample = asyncImageTaskResponseExample(model);
  const asyncPoll = imageTaskPollExample();
  const chatSample = chatCompletionExample(model);
	if (!supported) return <ProviderDocsUnavailable providerID={providerID} media="图像" />;
  return (
    <section className="api-docs">
      <div className="doc-toolbar">
        <div className="segmented" aria-label="图像接口">
          <button
            className={endpoint === "generation" ? "active" : ""}
			onClick={() => setEndpoint("generation")}
          >
            <ImageIcon />
            文生图
          </button>
          <button
            className={endpoint === "reference" ? "active" : ""}
			onClick={() => setEndpoint("reference")}
          >
            <Images />
            图生图
          </button>
        </div>
        <div className="doc-toolbar-actions">
          <code>POST {endpointPath}</code>
          <a className="doc-playground-link" href={`/playground?media=image&provider=${route.provider}&model=${route.model}`}>
            <FlaskConical />在测试中心打开
          </a>
        </div>
      </div>
      <div className="doc-intro">
        <div>
          <span className="eyebrow">认证</span>
          <strong>Authorization: Bearer $AIV2API_API_KEY</strong>
        </div>
        <div>
          <span className="eyebrow">请求格式</span>
          <strong>
			{endpoint === "generation" ? "application/json" : "multipart/form-data"}
          </strong>
        </div>
        <div>
          <span className="eyebrow">响应</span>
          <strong>
			异步 · HTTP 202 Task
          </strong>
        </div>
      </div>
      <h2>选择模型</h2>
      <div className="model-picker">
        {availableModels.map((id) => (
          <button
            key={id}
            className={model === id ? "active" : ""}
            onClick={() => setModel(id)}
          >
            <strong>{imageModelDocs[id].name}</strong>
            <small>{imageModelDocs[id].role}</small>
          </button>
        ))}
      </div>
      <div className="model-guide">
        <div>
          <span className="model-role"><ProviderBadge providerID={route.provider} />{doc.role}</span>
          <h2>{doc.name}</h2>
          <p>{doc.use}</p>
        </div>
        <div>
          <h3>适合</h3>
          <ul>
            {doc.strengths.map((v) => (
              <li key={v}>{v}</li>
            ))}
          </ul>
        </div>
        <div>
          <h3>注意</h3>
          <ul>
            {doc.limits.map((v) => (
              <li key={v}>{v}</li>
            ))}
          </ul>
        </div>
      </div>
      <div className="model-facts">
        <p>
          <strong>提示词：</strong>
          最多 {doc.promptMax.toLocaleString()} 个 Unicode 字符
        </p>
        <p>
          <strong>质量：</strong>
          {doc.quality}
        </p>
        <p>
          <strong>尺寸：</strong>
          {doc.size}
        </p>
        <p>
          <strong>单次输出：</strong>
          {doc.quantity}
        </p>
      </div>
      <div className="doc-section-title">
        <div>
          <h2>支持尺寸</h2>
          <p>表格中的值可直接传给 <code>size</code>。{doc.sizeNote}</p>
        </div>
      </div>
      <div className="param-table-wrap">
        <table className="param-table image-size-table">
          <thead>
            <tr>
              <th>比例</th>
              <th>Small</th>
              <th>Medium</th>
              <th>Large</th>
            </tr>
          </thead>
          <tbody>
            {sizeGroups.map((group) => (
              <tr key={group.ratio}>
                <td>{group.ratio}</td>
                <td>{group.small ? <ImageSizeCost size={group.small} model={model} costs={costsBySize.get(group.small)} loading={matrixQuery.isLoading} /> : "—"}</td>
                <td><ImageSizeCost size={group.medium} model={model} costs={costsBySize.get(group.medium)} loading={matrixQuery.isLoading} /></td>
                <td><ImageSizeCost size={group.large} model={model} costs={costsBySize.get(group.large)} loading={matrixQuery.isLoading} /></td>
              </tr>
            ))}
            {doc.customSize && (
              <tr className="image-size-custom-row">
                <td>自定义尺寸</td>
                <td colSpan={3}>{doc.customSize}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {matrixQuery.error && <p className="image-cost-note error">尺寸积分加载失败：{matrixQuery.error.message}</p>}
      {matrixQuery.data && (
        <p className="image-cost-note">
          单张积分来自当前计价接口 · {matrixQuery.data.price_versions.join(" / ")}
        </p>
      )}
      <div className="doc-section-title">
        <div>
          <h2>积分计算</h2>
          <p>
            <code>POST /v1/images/estimate</code>
            与生成任务共用后端校验和计价函数；只计算，不创建任务、不扣积分。
          </p>
        </div>
      </div>
      <ImageCostCalculator key={model} model={model} />
      <p className="image-cost-note">{imagePricingNotes[model]}</p>
      <div className="doc-section-title">
        <div>
          <h2>请求示例</h2>
          <p>
			{endpoint === "generation" ? "文生图使用 JSON" : "参考图使用 multipart"}
          </p>
        </div>
        <CopyCode value={sample} />
      </div>
      <pre className="code-block">
        <code>{sample}</code>
      </pre>
      <div className="doc-section-title">
        <div>
          <h2>成功响应示例</h2>
          <p>
			返回 HTTP 202 Task；通过 <code>queue_position</code> 和 <code>status</code> 跟踪执行状态。
          </p>
        </div>
        <CopyCode value={responseExample} />
      </div>
      <pre className="code-block">
        <code>{responseExample}</code>
      </pre>
		<>
          <div className="doc-section-title">
            <div>
              <h2>查询与取消</h2>
              <p>
                每 3 秒查询一次；仅 <code>queued</code> 状态可取消。
              </p>
            </div>
            <CopyCode value={asyncPoll} />
          </div>
          <pre className="code-block">
            <code>{asyncPoll}</code>
          </pre>
		</>
      <div className="doc-section-title">
        <div>
          <h2>参数</h2>
          <p>仅显示当前模型和接口支持的字段。</p>
        </div>
      </div>
      <ParameterTable parameters={params} />
      <div className="doc-warning">
        <AlertTriangle />
        <div>
          <strong>说明</strong>
          <p>
            未列出的字段会被拒绝。异步图片只返回 URL；透明背景、WebP、mask 和流式图片暂未开放。
          </p>
        </div>
      </div>
      <div className="doc-section-title">
        <div>
          <h2>Chat Completions 兼容入口</h2>
          <p>
            供只支持 OpenAI Chat Completions 的图片客户端使用；最后一条 user
            消息作为提示词，参考图仅接受 Base64 data URL。
          </p>
        </div>
        <CopyCode value={chatSample} />
      </div>
      <pre className="code-block">
        <code>{chatSample}</code>
      </pre>
      <ParameterTable parameters={chatParameters} />
      <div className="doc-validation">
        <Check />
        <div>
          <strong>响应兼容说明</strong>
          <p>
            非流式响应在 <code>choices[0].message.content</code> 中返回 Markdown
            图片链接；<code>stream=true</code> 返回 SSE，失败时在 [DONE]
            前返回 error 对象。远程图片 URL 不接受，只接受一张 Base64 data URL。
          </p>
        </div>
      </div>
    </section>
  );
}

type VideoMode = "text" | "image" | "frame" | "video" | "audio";

const videoModes: VideoMode[] = ["text", "image", "frame", "video", "audio"];

const videoModeDocs: Record<VideoMode, { label: string; description: string }> = {
  text: { label: "无参考", description: "只使用提示词和生成参数，不上传参考素材。" },
  image: { label: "参考图", description: "使用普通参考图控制主体、风格或构图。" },
  frame: { label: "首帧 / 尾帧", description: "固定视频起始画面，可选固定结束画面。" },
  video: { label: "参考视频", description: "使用已有视频指导动作、运镜或节奏。" },
  audio: { label: "参考音频", description: "使用对白、音乐或节奏作为输入参考。" },
};

function supportsVideoMode(model: PublicVideoModel, mode: VideoMode) {
  const spec = videoModelDocs[model];
  if (mode === "text") return !spec.requiresStartFrame;
  if (mode === "image") return spec.maxReferenceImages > 0;
  if (mode === "frame") return spec.supportsStartEnd;
  if (mode === "video") return maxVideoReferences(spec) > 0;
  return maxAudioReferences(spec) > 0;
}

function videoModeAvailability(model: PublicVideoModel, mode: VideoMode) {
  const spec = videoModelDocs[model];
  if (!supportsVideoMode(model, mode)) return "当前模型不支持";
  if (mode === "text") return "JSON";
  if (mode === "image") return `最多 ${spec.maxReferenceImages} 张`;
  if (mode === "frame") return "首帧必填";
  if (mode === "video") return `最多 ${maxVideoReferences(spec)} 个`;
  return `最多 ${maxAudioReferences(spec)} 个`;
}

function referenceDuration(value: number | undefined, fallback = 15) {
  return value ?? fallback;
}

function videoModeRules(model: PublicVideoModel, mode: VideoMode) {
  const spec = videoModelDocs[model];
  if (mode === "text") {
    return {
      contentType: "application/json",
      requiredMedia: "无参考文件",
      rule: "不要携带 image、start_frame、end_frame、video 或 audio。",
    };
  }
  if (mode === "image") {
    return {
      contentType: "multipart/form-data",
      requiredMedia: "image 或 image[]",
      rule: model === "veo-3.1"
        ? "最多 3 张；固定 size=1280x720、duration=8；不能与首帧或尾帧混用。"
        : `最多 ${spec.maxReferenceImages} 张；不能与首帧或尾帧混用。`,
    };
  }
  if (mode === "frame") {
    return {
      contentType: "multipart/form-data",
      requiredMedia: "start_frame",
      rule: spec.requiresStartFrame
        ? "start_frame 必填；不支持 end_frame、普通参考图、参考视频或参考音频。"
        : "start_frame 必填，end_frame 可选；不能与普通参考图、参考视频或参考音频混用。",
    };
  }
  if (mode === "video") {
    return {
      contentType: "multipart/form-data",
      requiredMedia: "video 或 video[]",
      rule: model === "kling-o3-omni"
        ? `1 个参考视频，时长 ${spec.minReferenceVideoDuration}–${spec.maxReferenceVideoDuration} 秒且须与 duration 一致；可同时上传最多 ${maxImageReferences(spec, true)} 张普通参考图；不能与首帧或尾帧混用。`
        : `最多 ${maxVideoReferences(spec)} 个，合计不超过 ${referenceDuration(spec.maxReferenceVideoDuration)} 秒；不能与首帧或尾帧混用。`,
    };
  }
  return {
    contentType: "multipart/form-data",
    requiredMedia: model === "minimax-h3" ? "audio / audio[] + image / image[]" : "audio / audio[] + image / image[] 或 video / video[]",
    rule: model === "minimax-h3"
      ? "最多 3 个音频，合计不超过 15 秒；必须同时上传普通参考图。"
      : `最多 ${maxAudioReferences(spec)} 个音频，合计不超过 ${referenceDuration(spec.maxReferenceAudioDuration)} 秒；必须同时上传参考图或参考视频。`,
  };
}

function VideoCostCalculator({ model }: { model: PublicVideoModel }) {
  const spec = videoModelDocs[model];
  const route = generationRoute(model);
  const [duration, setDuration] = useState(spec.defaultDuration);
  const [size, setSize] = useState(modelVideoSizes(model)[0].value);
  const [resolution, setResolution] = useState(defaultVideoResolution(model));
  const [generateAudio, setGenerateAudio] = useState(true);
	const [hasVideoReference, setHasVideoReference] = useState(false);
	const [referenceMode, setReferenceMode] = useState("text");
  const estimate = useMutation({
    mutationFn: () =>
      api<VideoCostEstimate>("/api-docs/video-estimate", {
        method: "POST",
        body: JSON.stringify({
          model: route.model,
          duration,
          size,
          resolution,
          generate_audio: spec.supportsGenerateAudio ? generateAudio : undefined,
		  has_video_reference: maxVideoReferences(spec) > 0 ? hasVideoReference : false,
		  reference_mode: model === "adobe:kling-3.0-omni" ? referenceMode : undefined,
        }),
      }),
  });
  const result = estimate.data;
  const resetResult = () => estimate.reset();
  const modifierLabels: Record<VideoCostEstimate["applied_modifiers"][number], string> = {
    seedance_video_reference: "Seedance 参考视频计价",
    flux_video_reference: "FLUX 3 Video 参考视频计价",
    kling_o3_video_reference: "Kling O3 Omni 参考视频计价",
    native_audio_disabled: "关闭原生音频计价",
  };
  return (
    <div className="image-cost-calculator">
      <form
        className="image-cost-form video-cost-form"
        onSubmit={(event) => {
          event.preventDefault();
          estimate.mutate();
        }}
      >
        <label>
          时长
          <select
            aria-label="视频积分计算时长"
            value={duration}
            onChange={(event) => {
              setDuration(Number(event.target.value));
              resetResult();
            }}
          >
            {spec.durationValues.map((value) => (
              <option key={value} value={value}>{value} 秒</option>
            ))}
          </select>
        </label>
        <label>
          尺寸
          <select
            aria-label="视频积分计算尺寸"
            value={size}
            onChange={(event) => {
              const nextSize = event.target.value;
              setSize(nextSize);
              const mappedResolution = videoResolutionForSize(model, nextSize);
              if (mappedResolution) setResolution(mappedResolution);
              resetResult();
            }}
          >
            {modelVideoSizes(model).map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </select>
        </label>
        <label>
          分辨率
          <select
            aria-label="视频积分计算分辨率"
            value={resolution}
            onChange={(event) => {
              setResolution(event.target.value);
              resetResult();
            }}
          >
            {videoResolutionsForSize(model, size).map((value) => (
              <option key={value} value={value}>{value}</option>
            ))}
          </select>
        </label>
        <label>
          价格选项
          <select
            aria-label="视频积分计算价格选项"
			value={model === "adobe:kling-3.0-omni" ? referenceMode : hasVideoReference ? "reference" : generateAudio ? "audio" : "silent"}
			onChange={(event) => {
			  const value = event.target.value;
			  if (model === "adobe:kling-3.0-omni") {
				setReferenceMode(value);
				resetResult();
				return;
			  }
              setHasVideoReference(value === "reference");
              setGenerateAudio(value !== "silent");
              resetResult();
            }}
          >
			{model === "adobe:kling-3.0-omni" ? (
			  <>
				<option value="text">无参考</option>
				<option value="frame">首尾帧</option>
				<option value="image">普通参考图</option>
			  </>
			) : (
			  <>
				<option value="audio">{spec.supportsGenerateAudio ? "原生音频开启" : "模型默认参数"}</option>
				{spec.supportsGenerateAudio && !spec.alwaysGenerateAudio ? <option value="silent">原生音频关闭</option> : null}
				{maxVideoReferences(spec) > 0 ? <option value="reference">包含参考视频</option> : null}
			  </>
			)}
          </select>
        </label>
        <button type="submit" disabled={estimate.isPending}>
          <Coins size={17} />
          {estimate.isPending ? "计算中" : "计算积分"}
        </button>
      </form>
      {estimate.error ? (
        <p className="error">{estimate.error.message}</p>
      ) : result ? (
        <div className="image-cost-result" aria-live="polite">
          <div>
            <span>基础规则积分</span>
            <strong>{result.base_tokens.toLocaleString()}</strong>
          </div>
          <div>
            <span>价格修饰器</span>
            <strong>{result.applied_modifiers.length}</strong>
          </div>
          <div className="total">
            <span>最终预留积分</span>
            <strong>{result.estimated_tokens.toLocaleString()}</strong>
          </div>
          <p>
            {result.applied_modifiers.length
              ? result.applied_modifiers.map((value) => modifierLabels[value]).join(" · ")
              : "当前参数没有价格修饰器"}
            {` · ${result.price_version}`}
          </p>
          <code>{result.formula}</code>
        </div>
      ) : (
        <p className="image-cost-empty">选择参数后计算，结果与任务创建时的积分预留完全一致。</p>
      )}
      <p className="image-cost-note">
        {model === "grok-imagine-1.5"
          ? "影响价格的字段为 model、duration、size 和对应的 resolution 档位；原生音频开关不改变当前 Schema 积分。"
          : model === "flux-3-video"
            ? "影响价格的字段为 model、duration、size 对应的 resolution 档位和 has_video_reference；原生音频和首尾帧不改变当前 Schema 积分。"
          : model === "kling-o3-omni"
            ? "普通生成按 duration、size 对应的 resolution 档位和 generate_audio 计价；参考视频按输入时长计价，不支持 2160p，原生音频开关不再加价。"
          : "影响价格的字段为 model、duration、resolution、generate_audio 和 has_video_reference。Seedance 的原生音频开关当前不改变 Schema 价格；普通参考图、首尾帧和参考音频也不改变积分。"}
      </p>
    </div>
  );
}

function videoReferenceSummary(model: PublicVideoModel) {
  const spec = videoModelDocs[model];
  const modes: string[] = [];
  if (spec.maxReferenceImages > 0) modes.push(spec.maxReferenceImagesWithVideo ? `参考图 ${spec.maxReferenceImages} 张（与视频同用 ${spec.maxReferenceImagesWithVideo} 张）` : `参考图 ${spec.maxReferenceImages} 张`);
  if (spec.requiresStartFrame) modes.push("首帧必填");
  else if (spec.supportsStartEnd) modes.push("首帧 / 尾帧");
  if (maxVideoReferences(spec) > 0) modes.push(`参考视频 ${maxVideoReferences(spec)} 个`);
  if (maxAudioReferences(spec) > 0) modes.push(`参考音频 ${maxAudioReferences(spec)} 个`);
  return modes.length ? modes.join(" · ") : "仅文生视频";
}

function videoParametersForMode(model: PublicVideoModel, mode: VideoMode): DocParameter[] {
  const spec = videoModelDocs[model];
  const fixedVeoImage = model === "veo-3.1" && mode === "image";
  const parameters: DocParameter[] = [
	["model", "string", "必填", "平台与视频模型。", `${generationRoute(model).model}；格式为 平台/模型`],
    ["prompt", "string", "必填", "视频内容和镜头描述。", `1–${spec.promptMax.toLocaleString()} 个 Unicode 字符`],
    ["duration", "integer", fixedVeoImage ? "固定" : "可选", "视频时长。", fixedVeoImage ? "固定为 8 秒" : `${spec.duration}；默认 ${spec.defaultDuration} 秒`],
    ["size", "string", fixedVeoImage ? "固定" : "可选", "画面方向和尺寸。", fixedVeoImage ? "固定为 1280x720" : modelVideoSizes(model).map((item) => item.value).join("、")],
    ["resolution", "string", spec.resolutions.length === 1 ? "固定" : "可选", "输出清晰度。", spec.sizes?.some((item) => item.resolution) ? `必须与 size 对应：${spec.resolutions.join("、")}` : spec.resolutions.join("、")],
  ];
  if (mode === "image") {
    parameters.push([
      "image / image[]",
      "file",
      "必填",
      "普通参考图。",
      `PNG、JPEG 或 WebP；最多 ${spec.maxReferenceImages} 张`,
    ]);
    parameters.push(["reference_strength", "string", "可选", "普通参考图的影响强度。", "LOW、MID、HIGH；默认 MID"]);
  }
  if (mode === "frame") {
    parameters.push(["start_frame", "file", "必填", "视频首帧。", "PNG、JPEG 或 WebP；1 张"]);
    if (supportsVideoEndFrame(spec)) {
      parameters.push(["end_frame", "file", "可选", "视频尾帧。", "必须与 start_frame 一起使用；最多 1 张"]);
    }
  }
  if (mode === "video") {
    if (model === "kling-o3-omni") {
      parameters.push([
        "image / image[]",
        "file",
        "可选",
        "与参考视频一起保持人物、主体或风格一致。",
        `PNG、JPEG 或 WebP；最多 ${maxImageReferences(spec, true)} 张`,
      ]);
      parameters.push(["reference_strength", "string", "使用参考图时可选", "普通参考图的影响强度。", "LOW、MID、HIGH；默认 MID"]);
    }
    parameters.push([
      "video / video[]",
      "file",
      "必填",
      "动作或镜头参考。",
      model === "kling-o3-omni"
        ? `MP4、MOV 或 WebM；1 个，${spec.minReferenceVideoDuration}–${spec.maxReferenceVideoDuration} 秒，每边 720–2160 像素；duration 必须与视频时长一致`
        : `MP4、MOV 或 WebM；最多 ${maxVideoReferences(spec)} 个，合计不超过 ${referenceDuration(spec.maxReferenceVideoDuration)} 秒`,
    ]);
  }
  if (mode === "audio" && model === "minimax-h3") {
    parameters.push([
      "image / image[]",
      "file",
      "搭配素材必填",
      "参考音频所依赖的普通参考图。",
      `PNG、JPEG 或 WebP；最多 ${spec.maxReferenceImages} 张`,
    ]);
    parameters.push([
      "audio / audio[]",
      "file",
      "必填",
      "对白、音乐或节奏参考。",
      `MP3、WAV、M4A、AAC 或 OGG；最多 ${maxAudioReferences(spec)} 个，合计不超过 ${referenceDuration(spec.maxReferenceAudioDuration)} 秒`,
    ]);
    parameters.push(["reference_strength", "string", "可选", "普通参考图的影响强度。", "LOW、MID、HIGH；默认 MID"]);
  }
  if (mode === "audio" && model !== "minimax-h3") {
    parameters.push(
      ["image / image[]", "file", "搭配素材二选一", "普通参考图。", `PNG、JPEG 或 WebP；最多 ${spec.maxReferenceImages} 张`],
      ["video / video[]", "file", "搭配素材二选一", "动作或镜头参考。", `MP4、MOV 或 WebM；最多 ${maxVideoReferences(spec)} 个，合计不超过 ${referenceDuration(spec.maxReferenceVideoDuration)} 秒`],
      ["audio / audio[]", "file", "必填", "对白、音乐或节奏参考。", `MP3、WAV、M4A、AAC 或 OGG；最多 ${maxAudioReferences(spec)} 个，合计不超过 ${referenceDuration(spec.maxReferenceAudioDuration)} 秒`],
      ["reference_strength", "string", "使用参考图时可选", "普通参考图的影响强度。", "LOW、MID、HIGH；默认 MID"],
    );
  }
  if (spec.supportsGenerateAudio) {
    parameters.push([
      "generate_audio",
      "boolean",
      spec.alwaysGenerateAudio ? "固定" : "可选",
      "是否生成输出视频的原生音轨；与 audio 参考输入不是同一个参数。",
      spec.alwaysGenerateAudio ? "固定为 true" : "true 或 false；默认 true",
    ]);
  }
  parameters.push(["Idempotency-Key", "header", "必填", "标识一次付费业务操作。", "重试同一请求时必须复用原值"]);
  return parameters;
}

function VideoDocs({ providerID }: { providerID: string }) {
  const availableModels = modelsForProvider(Object.keys(videoModelDocs) as PublicVideoModel[], providerID);
  const [model, setModel] = useState<PublicVideoModel>(availableModels[0] || "seedance-2.0-mini");
  const [mode, setMode] = useState<VideoMode>("text");
	if (availableModels.length === 0) return <ProviderDocsUnavailable providerID={providerID} media="视频" />;
  const route = generationRoute(model);
  const doc = videoModelDocs[model];
  const modeDoc = videoModeDocs[mode];
  const modeDescription = doc.requiresStartFrame && mode === "frame"
    ? "上传一张必填首帧，将静态画面生成带运动和原生音频的视频。"
    : modeDoc.description;
  const modeRules = videoModeRules(model, mode);
  const parameters = videoParametersForMode(model, mode);
  const request = videoModeExample(model, mode);
  const poll = videoPollExample();
  const selectModel = (next: PublicVideoModel) => {
    setModel(next);
    if (!supportsVideoMode(next, mode)) setMode(videoModelDocs[next].requiresStartFrame ? "frame" : "text");
  };
  return (
    <section className="api-docs">
      <div className="doc-toolbar">
        <strong>视频生成</strong>
        <div className="doc-toolbar-actions">
          <code>POST /v1/videos/generations</code>
          <a className="doc-playground-link" href={`/playground?media=video&provider=${route.provider}&model=${route.model}`}>
            <FlaskConical />在测试中心打开
          </a>
        </div>
      </div>
      <div className="doc-intro">
        <div>
          <span className="eyebrow">认证</span>
          <strong>Authorization: Bearer $AIV2API_API_KEY</strong>
        </div>
        <div>
          <span className="eyebrow">执行方式</span>
          <strong>异步任务，HTTP 202</strong>
        </div>
        <div>
          <span className="eyebrow">结果</span>
          <strong>MP4 URL 与视频尺寸</strong>
        </div>
      </div>
      <h2>选择模型</h2>
      <div className="model-picker">
        {availableModels.map((id) => (
          <button
            key={id}
            className={model === id ? "active" : ""}
            type="button"
            aria-pressed={model === id}
            onClick={() => selectModel(id)}
          >
            <strong>{videoModelDocs[id].name}</strong>
            <small>{videoModelDocs[id].role}</small>
          </button>
        ))}
      </div>
      <div className="model-guide">
        <div>
          <span className="model-role"><ProviderBadge providerID={route.provider} />{doc.role}</span>
          <h2>{doc.name}</h2>
          <p>{doc.use}</p>
        </div>
        <div>
          <h3>时长</h3>
          <p>{doc.duration}</p>
          <h3>分辨率</h3>
          <p>{doc.resolution}</p>
        </div>
        <div>
          <h3>注意</h3>
          <ul>
            {doc.limits.map((v) => (
              <li key={v}>{v}</li>
            ))}
          </ul>
        </div>
      </div>
      <div className="model-facts">
        <p><strong>提示词：</strong>最多 {doc.promptMax.toLocaleString()} 个 Unicode 字符</p>
        <p><strong>参考输入：</strong>{videoReferenceSummary(model)}</p>
        <p>
          <strong>原生音频：</strong>
          {doc.alwaysGenerateAudio ? "固定开启" : doc.supportsGenerateAudio ? "默认开启，可关闭" : "不提供开关"}
        </p>
      </div>
      <div className="doc-section-title">
        <div>
          <h2>选择请求模板</h2>
          <p>按钮只切换示例和参数表；API 没有 mode 字段。</p>
        </div>
      </div>
      <div className="video-mode-picker" role="group" aria-label="视频请求模板">
        {videoModes.map((id) => {
          const supported = supportsVideoMode(model, id);
          return (
            <button
              key={id}
              type="button"
              className={mode === id ? "active" : ""}
              aria-pressed={mode === id}
              disabled={!supported}
              onClick={() => setMode(id)}
            >
              <strong>{videoModeDocs[id].label}</strong>
              <small>{videoModeAvailability(model, id)}</small>
            </button>
          );
        })}
      </div>
      <div className="video-mode-guide" aria-live="polite">
        <div>
          <span>请求格式</span>
          <code>{modeRules.contentType}</code>
        </div>
        <div>
          <span>必填素材</span>
          <strong>{modeRules.requiredMedia}</strong>
        </div>
        <div>
          <span>组合限制</span>
          <p>{modeRules.rule}</p>
        </div>
      </div>
      <div
        className="video-combination-note"
        role="note"
        aria-label="视频参考素材组合规则"
      >
        <div className="video-combination-heading">
          <Boxes />
          <div>
            <strong>{doc.requiresStartFrame ? "Grok Imagine 1.5 只接受必填首帧" : "参考素材可以组合；首尾帧输入与其他素材互斥"}</strong>
            <p>
              API 没有 <code>mode</code> 字段，系统根据 multipart
              中实际出现的文件字段判断组合；<code>generate_audio</code>
              只控制输出原生音轨。
            </p>
          </div>
        </div>
        <div className="video-combination-rules">
          <div>
            <span>可组合</span>
            <p>Seedance 可同时提交参考图、参考视频和参考音频；MiniMax H3 可同时提交参考图和参考音频。</p>
          </div>
          <div>
            <span>互斥</span>
            <p>首帧或尾帧不能与普通参考图、参考视频、参考音频同时提交；尾帧必须搭配首帧。</p>
          </div>
          <div>
            <span>音频依赖</span>
            <p>Seedance 参考音频必须搭配参考图或参考视频；MiniMax H3 必须搭配普通参考图。</p>
          </div>
          <div>
            <span>Grok Imagine 1.5</span>
            <p>只接受必填首帧，不支持无参考、尾帧或其他参考素材。</p>
          </div>
        </div>
      </div>
      <div className="doc-section-title">
        <div>
          <h2>{modeDoc.label}请求示例</h2>
          <p>{modeDescription}</p>
        </div>
        <CopyCode value={request} />
      </div>
      <pre className="code-block">
        <code>{request}</code>
      </pre>
      <div className="doc-section-title">
        <div>
          <h2>{modeDoc.label}参数</h2>
          <p>只列当前模型在此模式下应提交的字段。未列出的素材字段不要提交。</p>
        </div>
      </div>
      <ParameterTable parameters={parameters} />
      <div className="doc-section-title">
        <div>
          <h2>积分计算</h2>
          <p>选择参数即可查看任务创建时的预留积分。</p>
        </div>
      </div>
      <VideoCostCalculator key={model} model={model} />
      <div className="doc-section-title">
        <div>
          <h2>查询与取消</h2>
          <p>
            每 3 秒查询一次；成功后从 <code>result.data[0].url</code> 读取 MP4。
          </p>
        </div>
        <CopyCode value={poll} />
      </div>
      <pre className="code-block">
        <code>{poll}</code>
      </pre>
    </section>
  );
}

function audioParametersForModel(model: PublicAudioModel): DocParameter[] {
  const parameters: DocParameter[] = [
	["model", "string", "必填", "平台与音频模型。", `leonardo/${model}；格式为 平台/模型`],
    ["prompt", "string", "必填", "朗读文本、音乐描述或音效描述。", `1–${audioModelDocs[model].promptMax.toLocaleString()} 个 Unicode 字符`],
    ["n", "integer", "可选", "生成数量。", "1–4；默认 1"],
  ];
  if (model === "dialogue-v3") {
    parameters.push(
      ["voice", "string", "可选", "语音别名。", "21 个公开别名；默认 george"],
      ["language", "string", "可选", "语言代码。", "最多 16 个字符；默认 en"],
      ["prompt_influence", "number", "可选", "提示词影响强度。", "0–1；默认 0.5"],
    );
  } else if (model === "music-v1") {
    parameters.push(
      ["duration_minutes", "integer", "可选", "音乐时长。", "1–10 分钟；默认 1"],
      ["force_instrumental", "boolean", "可选", "是否只生成纯音乐。", "true 或 false；默认 false"],
    );
  } else {
    parameters.push(
      ["duration", "integer", "可选", "音效时长。", "1–22 秒；默认 2"],
      ["loop", "boolean", "可选", "是否适合无缝循环。", "true 或 false；默认 false"],
      ["prompt_influence", "number", "可选", "提示词影响强度。", "0–1；默认 0.7"],
    );
  }
  parameters.push(["Idempotency-Key", "header", "必填", "标识一次付费业务操作。", "重试同一请求时必须复用原值"]);
  return parameters;
}

function audioGenerationExample(model: PublicAudioModel) {
  const body =
    model === "dialogue-v3"
      ? '{\n    "model": "leonardo/dialogue-v3",\n    "prompt": "Welcome to the Leonardo media studio.",\n    "voice": "george",\n    "language": "en",\n    "prompt_influence": 0.5,\n    "n": 1\n  }'
      : model === "music-v1"
        ? '{\n    "model": "leonardo/music-v1",\n    "prompt": "Warm cinematic piano and strings, slow build, no vocals",\n    "duration_minutes": 1,\n    "force_instrumental": true,\n    "n": 1\n  }'
        : '{\n    "model": "leonardo/sound-effects-v2",\n    "prompt": "Rain falling on a metal roof, seamless ambient loop",\n    "duration": 6,\n    "loop": true,\n    "prompt_influence": 0.7,\n    "n": 1\n  }';
  return `curl $BASE_URL/v1/audio/generations \\\n+  -H "Authorization: Bearer $AIV2API_API_KEY" \\\n+  -H "Content-Type: application/json" \\\n+  -H "Idempotency-Key: YOUR_IDEMPOTENCY_KEY" \\\n+  -d '${body}'`.replaceAll(
    "\n+",
    "\n",
  );
}

function audioPollExample() {
  return `curl $BASE_URL/v1/audio/TASK_ID \\\n+  -H "Authorization: Bearer $AIV2API_API_KEY"\n\n# 取消排队或未提交任务\ncurl -X POST $BASE_URL/v1/audio/TASK_ID/cancel \\\n+  -H "Authorization: Bearer $AIV2API_API_KEY"`.replaceAll(
    "\n+",
    "\n",
  );
}

function AudioDocs({ providerID }: { providerID: string }) {
  const [model, setModel] = useState<PublicAudioModel>("sound-effects-v2");
	if (providerID !== "leonardo") return <ProviderDocsUnavailable providerID={providerID} media="音频" />;
  const doc = audioModelDocs[model];
  const parameters = audioParametersForModel(model);
  const request = audioGenerationExample(model);
  const poll = audioPollExample();
  return (
    <section className="api-docs">
      <div className="doc-toolbar">
        <strong>音频生成</strong>
        <div className="doc-toolbar-actions">
          <code>POST /v1/audio/generations</code>
          <a className="doc-playground-link" href={`/playground?media=audio&model=${model}`}>
            <FlaskConical />在测试中心打开
          </a>
        </div>
      </div>
      <div className="doc-intro">
        <div>
          <span className="eyebrow">认证</span>
          <strong>Authorization: Bearer $AIV2API_API_KEY</strong>
        </div>
        <div>
          <span className="eyebrow">执行方式</span>
          <strong>异步任务，HTTP 202</strong>
        </div>
        <div>
          <span className="eyebrow">结果</span>
          <strong>音频 URL 与时长</strong>
        </div>
      </div>
      <h2>选择模型</h2>
      <div className="model-picker audio-model-picker">
        {(Object.keys(audioModelDocs) as PublicAudioModel[]).map((id) => (
          <button
            key={id}
            className={model === id ? "active" : ""}
            onClick={() => setModel(id)}
          >
            <strong>{audioModelDocs[id].name}</strong>
            <small>{audioModelDocs[id].role}</small>
          </button>
        ))}
      </div>
      <div className="model-guide">
        <div>
          <span className="model-role">{doc.role}</span>
          <h2>{doc.name}</h2>
          <p>{doc.use}</p>
        </div>
        <div>
          <h3>预估价格</h3>
          <p>{doc.price}</p>
          <h3>执行方式</h3>
          <p>创建后每 3 秒轮询任务</p>
        </div>
        <div>
          <h3>限制</h3>
          <ul>
            {doc.limits.map((v) => (
              <li key={v}>{v}</li>
            ))}
          </ul>
        </div>
      </div>
      <div className="model-facts">
        <p><strong>提示词：</strong>最多 {doc.promptMax.toLocaleString()} 个 Unicode 字符</p>
        <p><strong>生成数量：</strong>每次 1–4 条</p>
      </div>
      <div className="doc-section-title">
        <div>
          <h2>请求示例</h2>
          <p>示例只包含当前模型需要的字段。</p>
        </div>
        <CopyCode value={request} />
      </div>
      <pre className="code-block">
        <code>{request}</code>
      </pre>
      <div className="doc-section-title">
        <div>
          <h2>查询与取消</h2>
          <p>
            每 3 秒查询一次；成功后从 <code>result.data[0].url</code> 读取音频。
          </p>
        </div>
        <CopyCode value={poll} />
      </div>
      <pre className="code-block">
        <code>{poll}</code>
      </pre>
      <div className="doc-section-title">
        <div>
          <h2>参数</h2>
          <p>
            仅显示当前模型支持的字段。
          </p>
        </div>
      </div>
      <ParameterTable parameters={parameters} />
    </section>
  );
}

function ProviderDocsUnavailable({ providerID, media }: { providerID: string; media: string }) {
	return (
	  <section className="empty-state">
		<AlertTriangle />
		<strong>{providerID} 尚未提供{media}文档适配器</strong>
		<span>平台已经注册，但需要补充该媒体类型的参数说明后才会在文档和测试中心开放。</span>
	  </section>
	);
}

function asyncImageExample(model: PublicModel) {
  const route = generationRoute(model);
  const quality = model === "gpt-image-2" || model === "adobe:gpt-image-2" ? `\n    "quality": "low",` : "";
  return `curl $BASE_URL/v1/images/generations \\
  -H "Authorization: Bearer $AIV2API_API_KEY" \\
  -H "Content-Type: application/json" \\
  -H "Idempotency-Key: YOUR_IDEMPOTENCY_KEY" \\
  -d '{\n    "model": "${route.model}",\n    "prompt": "一张白色背景上的产品摄影，柔和棚拍光线",\n    "size": "1024x1024",${quality}\n    "n": 1,\n    "response_format": "url"\n  }'`;
}

function asyncImageTaskResponseExample(model: PublicModel) {
  return `{
  "id": "2b42f834-15dd-49ce-9c75-854302db18d0",
  "kind": "image",
  "status": "queued",
  "progress": 0,
  "queue_position": 2,
  "model": "${generationRoute(model).model}",
  "prompt": "一张白色背景上的产品摄影，柔和棚拍光线",
  "retry_count": 0,
  "cancel_requested": false,
  "created_at": "2026-07-24T08:00:00Z",
  "updated_at": "2026-07-24T08:00:00Z"
}`;
}

function imageTaskPollExample() {
  return `curl $BASE_URL/v1/images/TASK_ID \\
  -H "Authorization: Bearer $AIV2API_API_KEY"

# 仅 queued 任务可取消
curl -X POST $BASE_URL/v1/images/TASK_ID/cancel \\
  -H "Authorization: Bearer $AIV2API_API_KEY"`;
}

function chatCompletionExample(model: PublicModel) {
	const route = generationRoute(model);
  return `curl $BASE_URL/v1/chat/completions \\
  -H "Authorization: Bearer $AIV2API_API_KEY" \\
  -H "Content-Type: application/json" \\
  -H "Idempotency-Key: YOUR_IDEMPOTENCY_KEY" \\
  -d '{\n    "model": "${route.model}",\n    "stream": false,\n    "messages": [\n      {\n        "role": "user",\n        "content": "生成一张白色背景上的红色陶瓷方块产品照"\n      }\n    ]\n  }'`;
}

function editExample(model: PublicModel) {
  const route = generationRoute(model);
  const quality =
    model === "gpt-image-2" || model === "adobe:gpt-image-2"
      ? ` \\
  -F "quality=low"`
      : "";
  return `curl $BASE_URL/v1/images/generations \\
  -H "Authorization: Bearer $AIV2API_API_KEY" \\
  -H "Idempotency-Key: YOUR_IDEMPOTENCY_KEY" \\
  -F "image[]=@product.png" \\
  -F "image[]=@style-reference.jpg" \\
  -F "prompt=保留产品结构，转换为干净的水彩插画" \\
  -F "model=${route.model}" \\
  -F "size=1024x1024"${quality} \\
  -F "n=1" \\
  -F "reference_strength=MID" \\
  -F "response_format=url"`;
}

function videoModeExample(model: PublicVideoModel, mode: VideoMode) {
  const route = generationRoute(model);
  const spec = videoModelDocs[model];
  const selectedSize = modelVideoSizes(model)[0].value;
  const preferredResolution = spec.resolutions.includes("1080p") ? "1080p" : "720p";
  const selectedResolution = videoResolutionForSize(model, selectedSize)
    || (spec.resolutions.includes(preferredResolution) ? preferredResolution : spec.resolutions[0]);
  if (mode === "text") {
    const audio = spec.supportsGenerateAudio ? `,\n    "generate_audio": true` : "";
    return `curl $BASE_URL/v1/videos/generations \\
  -H "Authorization: Bearer $AIV2API_API_KEY" \\
  -H "Content-Type: application/json" \\
  -H "Idempotency-Key: YOUR_IDEMPOTENCY_KEY" \\
  -d '{\n    "model": "${route.model}",\n    "prompt": "一颗玻璃球在白色摄影棚中缓慢旋转，电影级光线",\n    "duration": ${spec.defaultDuration},\n    "size": "${selectedSize}",\n    "resolution": "${selectedResolution}"${audio}\n  }'`;
  }
  const fields = [
    `model=${route.model}`,
    "prompt=让主体自然向镜头走来，保持外观和动作连贯",
    `duration=${spec.defaultDuration}`,
    `size=${selectedSize}`,
    `resolution=${videoResolutionForSize(model, selectedSize) || spec.resolutions[0]}`,
  ];
  if (spec.supportsGenerateAudio) fields.push("generate_audio=true");
  if (mode === "image") {
    fields.push("image[]=@reference.png", "reference_strength=MID");
  } else if (mode === "frame") {
    fields.push("start_frame=@opening.png");
    if (supportsVideoEndFrame(spec)) fields.push("end_frame=@closing.png");
  } else if (mode === "video") {
    fields.push("video[]=@camera-motion.mp4");
    if (model === "kling-o3-omni") fields.push("image[]=@subject.png", "reference_strength=MID");
  } else if (mode === "audio") {
    fields.push("image[]=@subject.png", "audio[]=@dialogue.mp3", "reference_strength=MID");
  }
  const form = fields
    .map((field, index) => `  -F "${field}"${index === fields.length - 1 ? "" : " \\"}`)
    .join("\n");
  return `curl $BASE_URL/v1/videos/generations \\
  -H "Authorization: Bearer $AIV2API_API_KEY" \\
  -H "Idempotency-Key: YOUR_IDEMPOTENCY_KEY" \\
${form}`;
}

function videoPollExample() {
  return `curl $BASE_URL/v1/videos/TASK_ID \\
  -H "Authorization: Bearer $AIV2API_API_KEY"\n\n# 取消仍在排队的任务\ncurl -X POST $BASE_URL/v1/videos/TASK_ID/cancel \\
  -H "Authorization: Bearer $AIV2API_API_KEY"`;
}
