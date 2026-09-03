import type { MediaKind, Provider } from "./types";

export type ProviderPresentation = {
  id: string;
  displayName: string;
  shortName: string;
  description: string;
  capabilities: readonly MediaKind[];
  modelAliases: Record<string, string>;
  models: Record<MediaKind, readonly string[]>;
};

export const providerRegistry = {
  leonardo: {
    id: "leonardo",
    displayName: "Leonardo AI",
    shortName: "Leonardo",
    description: "Leonardo 账号池、Schema 目录与本地积分账本",
    capabilities: ["image", "video", "audio"],
    modelAliases: {},
    models: {
      image: ["gpt-image-2", "nano-banana-2", "nano-banana-pro", "seedream-5.0-pro"],
      video: ["gemini-omni-flash", "happy-horse-1.1", "kling-3.0", "kling-3.0-turbo", "kling-o3-omni", "hailuo-2.3", "wan-2.7"],
      audio: ["dialogue-v3", "music-v1", "sound-effects-v2"],
    },
  },
  adobe: {
    id: "adobe",
    displayName: "Adobe Firefly",
    shortName: "Adobe",
    description: "Adobe Firefly 多供应商生成接口与独立 BKS 计价",
    capabilities: ["image", "video"],
    modelAliases: {
      "adobe:gpt-image-2": "gpt-image-2",
      "adobe:nano-banana-2": "nano-banana-2",
      "adobe:kling-3.0-omni": "kling-3.0-omni",
      "adobe:veo-3.1": "veo-3.1",
      "adobe:veo-3.1-fast": "veo-3.1-fast",
      "adobe:seedance-2.0": "seedance-2.0",
      "adobe:seedance-2.0-fast": "seedance-2.0-fast",
    },
    models: {
      image: ["adobe:gpt-image-2", "adobe:nano-banana-2"],
      video: ["adobe:kling-3.0-omni", "adobe:veo-3.1", "adobe:veo-3.1-fast", "adobe:seedance-2.0", "adobe:seedance-2.0-fast"],
      audio: [],
    },
  },
  creativefabrica: {
    id: "creativefabrica",
    displayName: "Creative Fabrica Studio",
    shortName: "Creative Fabrica",
    description: "Creative Fabrica Studio 账号池、Flow/Media Matrix 异步生成与 Coins 账本",
    capabilities: ["image", "video"],
    modelAliases: {},
    models: {
      image: [
        "creativefabrica:flux-2-max",
        "creativefabrica:flux-2-pro",
        "creativefabrica:flux-2-klein",
        "creativefabrica:grok",
        "creativefabrica:ideogram-p-image",
        "creativefabrica:ideogram-v4",
        "creativefabrica:kling-image-o1",
        "creativefabrica:kling-v2-1-image",
        "creativefabrica:kling-v3-image",
        "creativefabrica:kling-v3-omni-image",
        "creativefabrica:nano-banana",
        "creativefabrica:nano-banana-2",
        "creativefabrica:nano-banana-flash-lite",
        "creativefabrica:nano-banana-pro",
        "creativefabrica:openai-gpt-image-2",
        "creativefabrica:qwen-image-2-0-pro",
        "creativefabrica:qwen-image-3",
        "creativefabrica:qwen-image-3-pro",
        "creativefabrica:qwen-image-edit-spicy",
        "creativefabrica:recraft-v4",
        "creativefabrica:recraft-v4-svg",
        "creativefabrica:seedream-4-0",
        "creativefabrica:seedream-4-5",
        "creativefabrica:seedream-5-lite",
        "creativefabrica:seedream-5-pro",
        "creativefabrica:wan-2-7-pro-image",
        "creativefabrica:z-image-spicy",
        "creativefabrica:quiverai-arrow-1-1",
      ],
      video: [
        "creativefabrica:alibaba_happy_horse_v1",
        "creativefabrica:alibaba_happy_horse_v1_1",
        "creativefabrica:alibaba_wan_2_7",
        "creativefabrica:berry_1_0",
        "creativefabrica:berry_1_0_pro",
        "creativefabrica:fal_ltx_2_3",
        "creativefabrica:fal_pixverse_c1",
        "creativefabrica:fal_pixverse_v6",
        "creativefabrica:flux_3",
        "creativefabrica:gemini_omni_flash",
        "creativefabrica:gemini_omni_flash_1_1",
        "creativefabrica:gemini_omni_flash_1_1_video_extended",
        "creativefabrica:grok_imagine_video",
        "creativefabrica:grok_imagine_video_1_5",
        "creativefabrica:heygen_avatar_5",
        "creativefabrica:kling_ai_v3",
        "creativefabrica:kling_ai_v3_motion_control",
        "creativefabrica:kling_ai_v3_turbo",
        "creativefabrica:kling_v3_omni",
        "creativefabrica:luma_ray_2",
        "creativefabrica:luma_ray_3_2",
        "creativefabrica:minimax_hailuo_v3",
        "creativefabrica:minimax_hailuo_v3_max",
        "creativefabrica:mulerouter_wan_2_7_spicy",
        "creativefabrica:pika_v2_5",
        "creativefabrica:runway_gen_4_5",
        "creativefabrica:seedance_one_five_pro",
        "creativefabrica:seedance_two_point_zero",
        "creativefabrica:seedance_v2_5",
        "creativefabrica:seedance_v2_fast",
        "creativefabrica:seedance_v2_mini",
        "creativefabrica:veo_31_fast_generate_preview",
        "creativefabrica:wan_3_0",
        "creativefabrica:wan_3_0_prime",
      ],
      audio: [],
    },
  },
} as const satisfies Record<string, ProviderPresentation>;

export type RegisteredProviderID = keyof typeof providerRegistry;

export function isRegisteredProviderID(value: string | null | undefined): value is RegisteredProviderID {
  return Boolean(value && value in providerRegistry);
}

const mediaLabels: Record<MediaKind, string> = { image: "图像", video: "视频", audio: "音频" };

export function providerDefinition(providerID: string, provider?: Provider): ProviderPresentation {
  const known = providerRegistry[providerID as keyof typeof providerRegistry];
  if (known) return known;
  const capabilities = (provider?.capabilities || []).filter((item): item is MediaKind => item === "image" || item === "video" || item === "audio");
  return {
    id: providerID,
    displayName: provider?.display_name || providerID,
    shortName: provider?.display_name || providerID,
    description: "已注册的外部生成平台",
    capabilities,
    modelAliases: {},
    models: { image: [], video: [], audio: [] },
  };
}

export function providerDisplayName(providerID: string, providers: Provider[] = []) {
  return providerDefinition(providerID, providers.find((provider) => provider.id === providerID)).displayName;
}

export function modelDisplayID(model: string) {
	const publicParts = model.split("/");
	if (publicParts.length === 2 && publicParts[0] && publicParts[1]) return publicParts[1];
	const qualifiedParts = model.split(":");
	if (qualifiedParts.length === 2 && isRegisteredProviderID(qualifiedParts[0]) && qualifiedParts[1]) return qualifiedParts[1];
  for (const provider of Object.values(providerRegistry)) {
    const canonical = (provider.modelAliases as Record<string, string>)[model];
    if (canonical) return canonical;
  }
  return model;
}

export function providerPublicModels<T extends string>(providerID: RegisteredProviderID, kind: MediaKind): T[] {
  const provider = providerRegistry[providerID];
  return provider.models[kind].map((model) => modelDisplayID(model) as T);
}

export function providerInternalModel<T extends string>(providerID: RegisteredProviderID, kind: MediaKind, publicModel: string): T | undefined {
  const provider = providerRegistry[providerID];
  return provider.models[kind].find((model) => modelDisplayID(model) === publicModel) as T | undefined;
}

export function providerCapabilityText(provider: { capabilities: readonly string[] }) {
  return provider.capabilities.map((kind) => mediaLabels[kind as MediaKind] || kind).join(" · ") || "未开放生成能力";
}

export function providerSupports(provider: { capabilities: readonly string[] } | undefined, kind: MediaKind) {
  return Boolean(provider?.capabilities.includes(kind));
}

export function modelProviderID(model: string) {
	const publicParts = model.split("/");
	if (publicParts.length === 2 && isRegisteredProviderID(publicParts[0])) return publicParts[0];
	const qualifiedParts = model.split(":");
	if (qualifiedParts.length === 2 && isRegisteredProviderID(qualifiedParts[0])) return qualifiedParts[0];
  for (const provider of Object.values(providerRegistry)) {
    if (([...provider.models.image, ...provider.models.video, ...provider.models.audio] as readonly string[]).includes(model)) return provider.id;
  }
  return "";
}

export function modelsForProvider<T extends string>(models: readonly T[], providerID: string) {
  return models.filter((model) => modelProviderID(model) === providerID);
}

export function generationRoute(model: string) {
  const providerID = modelProviderID(model);
  const provider = providerDefinition(providerID);
	const canonicalModel = provider.modelAliases[model] || modelDisplayID(model);
	return { provider: providerID, model: `${providerID}/${canonicalModel}`, internalModel: model };
}

export function providerModelGroups(configured: Provider[] = []) {
  if (configured.length > 0) {
    return configured.flatMap((provider) => {
      const presentation = providerDefinition(provider.id, provider);
      return (["image", "video", "audio"] as MediaKind[])
        .filter((kind) => (provider.models?.[kind] || []).length > 0)
        .map((kind) => ({
          provider_id: provider.id,
          label: `${presentation.shortName} · ${mediaLabels[kind]}`,
          kind,
          models: provider.models[kind],
        }));
    });
  }
	return Object.values(providerRegistry).flatMap((provider) =>
    (["image", "video", "audio"] as MediaKind[])
      .filter((kind) => provider.models[kind].length > 0)
      .map((kind) => ({
        provider_id: provider.id,
        label: `${provider.shortName} · ${mediaLabels[kind]}`,
        kind,
        models: provider.models[kind].map((model) => (provider.modelAliases as Record<string, string>)[model] || model),
      })),
  );
}

type CreditProvider = { id?: string; provider_id?: string; credit_unit: string };

export function providerCreditUnit(providerID: string, providers: CreditProvider[] = []) {
  const raw = providers.find((provider) => (provider.id || provider.provider_id) === providerID)?.credit_unit || "credits";
  const normalized = raw.trim().toLowerCase();
  if (normalized === "bks") return "BKS";
  if (normalized === "credits" || normalized === "tokens") return "积分";
  return raw.trim() || "积分";
}
