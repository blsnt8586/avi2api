import type {
  CostRule,
  ImageSizeGroup,
  MediaKind,
  ModelCostRecord,
  PlatformModel,
  PublicModel,
} from "./types";

function currentCostRules(rules: CostRule[], kind: MediaKind) {
  const seen = new Set<string>();
  return [...rules]
    .filter((rule) => rule.enabled && rule.kind === kind)
    .sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime() ||
        b.id - a.id,
    )
    .filter((rule) => {
      const key = [
        rule.model,
        rule.size,
        rule.quality,
        rule.resolution,
        rule.duration,
      ].join("|");
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
}

export function audioRuleLabel(model: string, duration: number) {
  if (model === "dialogue-v3") return "每 1000 字符";
  return `${duration} ${model === "music-v1" ? "分钟" : "秒"}`;
}

export function costEstimates(rules: CostRule[], kind: MediaKind) {
  const active = currentCostRules(rules, kind);
  return [...new Set(active.map((rule) => rule.model))].map((model) => {
    const modelRules = active.filter((rule) => rule.model === model);
    if (kind === "image") {
      const columns = [...new Set(modelRules.map((rule) => rule.size))];
      const qualities = [
        ...new Set(modelRules.map((rule) => rule.quality || "固定质量")),
      ];
      return {
        model,
        columns,
        rows: qualities.map((label) => ({
          label:
            label === "固定质量"
              ? label
              : label[0].toUpperCase() + label.slice(1),
          values: columns.map(
            (size) =>
              modelRules.find(
                (rule) =>
                  rule.size === size && (rule.quality || "固定质量") === label,
              )?.unit_tokens ?? 0,
          ),
        })),
        note: `${modelRules[0]?.price_version || "未定价"} · 单张，n 按数量乘算`,
      };
    }
    if (kind === "video") {
      const columns = [...new Set(modelRules.map((rule) => rule.resolution))];
      const durations = [
        ...new Set(modelRules.map((rule) => rule.duration)),
      ].sort((a, b) => a - b);
      return {
        model,
        columns,
        rows: durations.map((duration) => ({
          label: `${duration} 秒`,
          values: columns.map(
            (resolution) =>
              modelRules.find(
                (rule) =>
                  rule.duration === duration && rule.resolution === resolution,
              )?.unit_tokens ?? 0,
          ),
        })),
        note: `${modelRules[0]?.price_version || "未定价"} · 单个视频`,
      };
    }
    return {
      model,
      columns: ["单条"],
      rows: modelRules
        .sort((a, b) => a.duration - b.duration)
        .map((rule) => ({
          label: audioRuleLabel(model, rule.duration),
          values: [rule.unit_tokens],
        })),
      note: `${modelRules[0]?.price_version || "未定价"} · n 按数量乘算`,
    };
  });
}

export const imageModels = [
  "gpt-image-2",
  "nano-banana-2",
  "nano-banana-pro",
  "seedream-5.0-pro",
] as const;

export type PlaygroundImageModel = (typeof imageModels)[number];

export function imageSizes(model: PlaygroundImageModel) {
  return imageSizeGroups[model].flatMap((group) =>
    [group.small, group.medium, group.large].filter(
      (value): value is string => Boolean(value),
    ),
  );
}

export function costDimensions(cost: ModelCostRecord) {
  const duration = cost.duration
    ? `${cost.duration}${cost.kind === "audio" && cost.model === "music-v1" ? "分钟" : "秒"}`
    : "";
  const parts = [cost.size, cost.quality, cost.resolution, duration].filter(
    Boolean,
  );
  return parts.length ? parts.join(" · ") : "默认参数";
}

export function modelCost(model: PlatformModel) {
  if (!model.base_token_cost || model.base_token_cost < 1) return "动态";
  return `${model.base_token_cost}${model.cost_type === "per_megapixel" ? "/MP" : "/次"}`;
}

export function videoOptions(model: PlatformModel) {
  const duration = model.duration_options?.length
    ? `${model.duration_options.join("/")} 秒`
    : model.default_duration
      ? `${model.default_duration} 秒`
      : "动态";
  const modes =
    model.resolution_modes
      ?.map((v) => v.replace("RESOLUTION_", ""))
      .join("/") || "平台控制";
  return `${duration} · ${modes}`;
}

export function audioOptions(model: PlatformModel) {
  const duration = model.duration_options?.length
    ? `${model.duration_options.join("/")} 秒`
    : model.default_duration
      ? `${model.default_duration} 秒`
      : "平台控制";
  return `${duration} · ${model.cost_type === "per_minute" ? "按分钟计费" : model.cost_type === "per_character" ? "按字符计费" : "音频参数由模型决定"}`;
}

const gptImage2SizeGroups: ImageSizeGroup[] = [
  { ratio: "1:1", small: "1024x1024", medium: "2048x2048", large: "2880x2880" },
  { ratio: "2:3", small: "848x1264", medium: "1376x2048", large: "2336x3504" },
  { ratio: "3:2", small: "1264x848", medium: "2048x1376", large: "3504x2336" },
  { ratio: "3:4", small: "896x1200", medium: "1536x2048", large: "2448x3264" },
  { ratio: "4:3", small: "1200x896", medium: "2048x1536", large: "3264x2448" },
  { ratio: "4:5", small: "928x1152", medium: "1648x2048", large: "2560x3200" },
  { ratio: "5:4", small: "1152x928", medium: "2048x1648", large: "3200x2560" },
  { ratio: "9:16", small: "768x1376", medium: "1136x2048", large: "2016x3584" },
  { ratio: "16:9", small: "1376x768", medium: "2048x1136", large: "3584x2016" },
  { ratio: "21:9", small: "1584x672", medium: "2048x864", large: "3808x1632" },
];

const nanoBananaSizeGroups: ImageSizeGroup[] = [
  { ratio: "1:1", small: "1024x1024", medium: "2048x2048", large: "4096x4096" },
  { ratio: "2:3", small: "848x1264", medium: "1696x2528", large: "3392x5056" },
  { ratio: "3:2", small: "1264x848", medium: "2528x1696", large: "5056x3392" },
  { ratio: "3:4", small: "896x1200", medium: "1792x2400", large: "3584x4800" },
  { ratio: "4:3", small: "1200x896", medium: "2400x1792", large: "4800x3584" },
  { ratio: "4:5", small: "928x1152", medium: "1856x2304", large: "3712x4608" },
  { ratio: "5:4", small: "1152x928", medium: "2304x1856", large: "4608x3712" },
  { ratio: "9:16", small: "768x1376", medium: "1536x2752", large: "3072x5504" },
  { ratio: "16:9", small: "1376x768", medium: "2752x1536", large: "5504x3072" },
  { ratio: "21:9", small: "1584x672", medium: "3168x1344", large: "6336x2688" },
];

const seedream50ProSizeGroups: ImageSizeGroup[] = [
  { ratio: "1:1", small: "1024x1024", medium: "1536x1536", large: "2048x2048" },
  { ratio: "2:3", small: "1024x1536", medium: "1152x1728", large: "1344x2016" },
  { ratio: "16:9", small: "1368x768", medium: "1792x1008", large: "2048x1152" },
  { ratio: "4:3", small: "1184x888", medium: "1536x1152", large: "2048x1536" },
  { ratio: "4:5", small: "1024x1280", medium: "1296x1620", large: "1632x2040" },
  { ratio: "9:16", small: "768x1368", medium: "1008x1792", large: "1152x2048" },
  { ratio: "2:1", small: "1536x768", medium: "1792x896", large: "2048x1024" },
  { ratio: "1.85:1", small: "1424x768", medium: "1776x960", large: "2048x1104" },
  { ratio: "2.4:1", small: "1840x768", medium: "1920x800", large: "2048x848" },
  { ratio: "21:9", small: "1806x774", medium: "1890x810", large: "2016x864" },
  { ratio: "3:2", small: "1344x896", medium: "1536x1024", large: "2040x1360" },
  { ratio: "3:4", small: "888x1184", medium: "1152x1536", large: "1536x2048" },
  { ratio: "5:4", small: "1280x1024", medium: "1620x1296", large: "2040x1632" },
  { ratio: "5:6", small: "960x1152", medium: "1280x1536", large: "1680x2016" },
  { ratio: "6:5", small: "1152x960", medium: "1536x1280", large: "2016x1680" },
];

export const imageSizeGroups: Record<PublicModel, ImageSizeGroup[]> = {
  "gpt-image-2": gptImage2SizeGroups,
  "nano-banana-2": nanoBananaSizeGroups,
  "nano-banana-pro": nanoBananaSizeGroups,
  "seedream-5.0-pro": seedream50ProSizeGroups,
};

export const keyModelGroups = [
  {
    label: "图像模型",
    models: ["gpt-image-2", "nano-banana-2", "nano-banana-pro", "seedream-5.0-pro"],
  },
  {
    label: "视频模型",
    models: [
      "flux-3-video",
      "seedance-2.0",
      "seedance-2.0-fast",
      "seedance-2.0-mini",
      "seedance-2.5",
      "veo-3.1",
      "veo-3.1-fast",
      "kling-o3-omni",
      "minimax-h3",
      "grok-imagine-1.5",
    ],
  },
  {
    label: "音频模型",
    models: ["dialogue-v3", "music-v1", "sound-effects-v2"],
  },
];
