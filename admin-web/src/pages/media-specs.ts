import type { PublicModel, PublicVideoModel } from "../shared/types";

const adobeVeoSizes = [
	{ value: "1280x720", label: "横屏 16:9 · 1280x720 · 720p", resolution: "720p" },
	{ value: "720x1280", label: "竖屏 9:16 · 720x1280 · 720p", resolution: "720p" },
	{ value: "1920x1080", label: "横屏 16:9 · 1920x1080 · 1080p", resolution: "1080p" },
	{ value: "1080x1920", label: "竖屏 9:16 · 1080x1920 · 1080p", resolution: "1080p" },
];

const adobeSeedanceSizes = [
	{ value: "1680x720", label: "电影 21:9 · 1680x720 · 720p", resolution: "720p" },
	{ value: "1280x720", label: "横屏 16:9 · 1280x720 · 720p", resolution: "720p" },
	{ value: "960x720", label: "横屏 4:3 · 960x720 · 720p", resolution: "720p" },
	{ value: "720x720", label: "方形 1:1 · 720x720 · 720p", resolution: "720p" },
	{ value: "720x960", label: "竖屏 3:4 · 720x960 · 720p", resolution: "720p" },
	{ value: "720x1280", label: "竖屏 9:16 · 720x1280 · 720p", resolution: "720p" },
	{ value: "1120x480", label: "电影 21:9 · 1120x480 · 480p", resolution: "480p" },
	{ value: "854x480", label: "横屏 16:9 · 854x480 · 480p", resolution: "480p" },
	{ value: "640x480", label: "横屏 4:3 · 640x480 · 480p", resolution: "480p" },
	{ value: "480x480", label: "方形 1:1 · 480x480 · 480p", resolution: "480p" },
	{ value: "480x640", label: "竖屏 3:4 · 480x640 · 480p", resolution: "480p" },
	{ value: "480x854", label: "竖屏 9:16 · 480x854 · 480p", resolution: "480p" },
];

const adobeSeedance1080Sizes = [
	{ value: "2520x1080", label: "电影 21:9 · 2520x1080 · 1080p", resolution: "1080p" },
	{ value: "1920x1080", label: "横屏 16:9 · 1920x1080 · 1080p", resolution: "1080p" },
	{ value: "1440x1080", label: "横屏 4:3 · 1440x1080 · 1080p", resolution: "1080p" },
	{ value: "1080x1080", label: "方形 1:1 · 1080x1080 · 1080p", resolution: "1080p" },
	{ value: "1080x1440", label: "竖屏 3:4 · 1080x1440 · 1080p", resolution: "1080p" },
	{ value: "1080x1920", label: "竖屏 9:16 · 1080x1920 · 1080p", resolution: "1080p" },
];

export const imageModelDocs: Record<
  PublicModel,
  {
    name: string;
    role: string;
    use: string;
    strengths: string[];
    limits: string[];
    quality: string;
    size: string;
    sizeNote: string;
    quantity: string;
    promptMax: number;
    customSize?: string;
  }
> = {
  "gpt-image-2": {
    name: "GPT Image 2",
    role: "通用首选",
    use: "适合大多数文生图、精确编辑、多参考图融合、人物一致性和图中文字。建议先用 low 打样，确认构图后再提高质量。",
    strengths: ["精确遵循编辑指令", "最多 6 张参考图", "文字与品牌素材表现好"],
    limits: ["当前账号 n 固定为 1", "高质量积分消耗明显增加"],
    quality: "auto / low / medium / high；auto 在本服务中按 low 执行",
    size: "默认 1024x1024；宽高必须命中 Leonardo 枚举，比例不超过 3:1，总像素为 655,360–8,294,400",
    sizeNote: "表内是常用预设；还可使用符合上述条件的 Leonardo 枚举宽高组合，不能传任意像素。",
    quantity: "固定 1 张",
    promptMax: 9999,
  },
	"adobe:gpt-image-2": {
    name: "Adobe · GPT Image 2",
    role: "Firefly 多供应商路由",
    use: "通过 Adobe Firefly 的异步接口生成或编辑图片，积分和 Leonardo 账号池完全隔离。",
    strengths: ["Adobe Firefly 异步任务", "最多 6 张参考图", "BKS 实时价格规则"],
    limits: ["当前 n 固定为 1", "仅开放 1K / 2K / 4K 方形价格档"],
    quality: "auto / low / medium / high；auto 按 low 执行",
    size: "1024x1024、2048x2048 或 2880x2880",
    sizeNote: "三档分别映射 Adobe 1K、2K、4K outputResolution，并使用账号导入时读取的 BKS 价格。",
    quantity: "固定 1 张",
    promptMax: 9999,
	},
	"adobe:nano-banana-2": {
		name: "Adobe · Nano Banana 2",
		role: "Firefly 文字与编辑",
		use: "通过 Adobe Firefly 的异步接口生成或编辑图片，适合文字、品牌素材和多参考图组合。",
		strengths: ["Nano Banana 2 当前 Firefly 版本", "最多 6 张参考图", "BKS 实时价格规则"],
		limits: ["当前 n 固定为 1", "不接受 quality 参数"],
		quality: "固定，由 Adobe Firefly 模型决定",
		size: "1K / 2K / 4K 的十种常用画幅；最大 6336x2688 或 3072x5504",
		sizeNote: "尺寸表与 Firefly 当前 Nano Banana 2 输出档一致，价格按 1K、2K、4K BKS 档读取。",
		quantity: "固定 1 张",
		promptMax: 9999,
	},
  "nano-banana-2": {
    name: "Nano Banana 2",
    role: "文字与品牌",
    use: "适合 Logo、字标、海报文案、包装与带文字的品牌素材，也可结合参考图延续风格。",
    strengths: ["文字渲染", "品牌与排版", "参考图风格延续"],
    limits: ["定点修改精度通常低于 GPT Image 2", "不接受 quality 参数"],
    quality: "固定，由 Leonardo 模型决定",
    size: "默认 1024x1024；宽高必须命中 Leonardo 枚举，标准预设最大为 6336x2688 或 3072x5504",
    sizeNote: "表内是常用预设；还可组合 Leonardo 已开放的宽、高枚举，不能传任意像素。",
    quantity: "1–4 张；默认 1 张",
    promptMax: 9999,
  },
  "nano-banana-pro": {
    name: "Nano Banana Pro",
    role: "复杂品牌画面",
    use: "对外别名映射到 Leonardo 的 gemini-image-2，适合更复杂的版式、多个视觉元素和参考图组合。",
    strengths: ["复杂构图", "多元素组合", "最多 6 张参考图"],
    limits: ["当前代理不开放独立质量档位", "不接受 quality 参数"],
    quality: "固定，由 Leonardo 模型决定",
    size: "默认 1024x1024；使用与 Nano Banana 2 相同的平台尺寸体系",
    sizeNote: "表内是常用预设；还可组合 Leonardo 已开放的宽、高枚举，不能传任意像素。",
    quantity: "1–4 张；默认 1 张",
    promptMax: 9999,
  },
  "seedream-5.0-pro": {
    name: "Seedream 5.0 Pro",
    role: "高保真排版与文字",
    use: "适合高保真文生图、多语言文字、海报排版和需要精确布局的画面。支持图像参考。",
    strengths: ["多语言文字渲染", "密集排版与布局", "最多 4 张输出、6 张参考图"],
    limits: ["每条边 768–2048 像素", "2K 阈值价格为 90 积分", "不接受 quality 参数"],
    quality: "固定，由 Leonardo 模型决定",
    size: "默认 1024x1024；支持自定义 768–2048 像素边长，标准画幅见尺寸表",
    sizeNote: "表内包含常用预设和完整的连续自定义范围。",
    quantity: "1–4 张；默认 1 张",
    promptMax: 9999,
    customSize: "WIDTHxHEIGHT；宽、高均可填写 768–2048 的整数像素，例如 1600x1200",
  },
};

export const videoModelDocs: Record<
  PublicVideoModel,
  {
    name: string;
    role: string;
    use: string;
    duration: string;
    resolution: string;
    limits: string[];
    durationValues: number[];
    defaultDuration: number;
    resolutions: string[];
    sizes?: { value: string; label: string; resolution?: string }[];
    maxReferenceImages: number;
    maxReferenceImagesWithVideo?: number;
    supportsStartEnd: boolean;
    supportsEndFrame?: boolean;
    requiresStartFrame?: boolean;
    supportsVideoAudioReferences: boolean;
    maxReferenceVideos?: number;
    maxReferenceAudios?: number;
    maxReferenceVideoDuration?: number;
    minReferenceVideoDuration?: number;
    maxReferenceAudioDuration?: number;
    supportsGenerateAudio: boolean;
    alwaysGenerateAudio?: boolean;
    promptMax: number;
  }
> = {
	"adobe:kling-3.0-omni": {
		name: "Adobe · Kling 3.0 Omni",
		role: "Firefly 全模态视频",
		use: "通过 Adobe Firefly 异步生成 5、10 或 15 秒视频，支持首尾帧和普通参考图。",
		duration: "5 / 10 / 15 秒",
		resolution: "720p / 1080p（尺寸与积分档绑定）",
		limits: ["普通参考图最多 3 张", "当前公开契约固定关闭原生音频"],
		durationValues: [5, 10, 15], defaultDuration: 5, resolutions: ["720p", "1080p"],
		sizes: [
			{ value: "1280x720", label: "横屏 16:9 · 1280x720 · 720p", resolution: "720p" },
			{ value: "720x1280", label: "竖屏 9:16 · 720x1280 · 720p", resolution: "720p" },
			{ value: "720x720", label: "方形 1:1 · 720x720 · 720p", resolution: "720p" },
			{ value: "1920x1080", label: "横屏 16:9 · 1920x1080 · 1080p", resolution: "1080p" },
			{ value: "1080x1920", label: "竖屏 9:16 · 1080x1920 · 1080p", resolution: "1080p" },
			{ value: "1080x1080", label: "方形 1:1 · 1080x1080 · 1080p", resolution: "1080p" },
		],
		maxReferenceImages: 3, supportsStartEnd: true, supportsVideoAudioReferences: false, supportsGenerateAudio: false, promptMax: 2500,
	},
	"adobe:veo-3.1": {
		name: "Adobe · Veo 3.1",
		role: "Firefly Google 旗舰",
		use: "通过 Adobe Firefly 异步生成 4、6 或 8 秒视频，支持首尾帧和最多 3 张普通参考图。",
		duration: "4 / 6 / 8 秒", resolution: "720p / 1080p（尺寸与积分档绑定）",
		limits: ["普通参考图最多 3 张", "当前公开契约固定关闭原生音频"],
		durationValues: [4, 6, 8], defaultDuration: 8, resolutions: ["720p", "1080p"], sizes: adobeVeoSizes,
		maxReferenceImages: 3, supportsStartEnd: true, supportsVideoAudioReferences: false, supportsGenerateAudio: false, promptMax: 9999,
	},
	"adobe:veo-3.1-fast": {
    name: "Adobe · Veo 3.1 Fast",
    role: "Firefly 快速视频",
    use: "通过 Adobe Firefly 异步视频任务生成 4、6 或 8 秒视频，支持首尾帧控制。",
    duration: "4 / 6 / 8 秒",
    resolution: "720p / 1080p（尺寸与积分档绑定）",
    limits: ["不支持普通参考图", "当前公开契约固定关闭原生音频"],
    durationValues: [4, 6, 8],
    defaultDuration: 8,
    resolutions: ["720p", "1080p"],
		sizes: adobeVeoSizes,
    maxReferenceImages: 0,
    supportsStartEnd: true,
    supportsVideoAudioReferences: false,
    supportsGenerateAudio: false,
		promptMax: 9999,
	},
	"adobe:seedance-2.0": {
		name: "Adobe · Seedance 2.0", role: "Firefly 多模态视频",
		use: "通过 Adobe Firefly 异步生成 4–15 秒视频，支持多画幅、参考图、首尾帧、视频与音频参考。",
		duration: "4–15 秒", resolution: "480p / 720p / 1080p（尺寸与积分档绑定）",
		limits: ["图片、视频和音频合计最多 12 个", "当前公开契约固定关闭原生音频"],
		durationValues: Array.from({ length: 12 }, (_, index) => index + 4), defaultDuration: 8,
		resolutions: ["480p", "720p", "1080p"], sizes: [...adobeSeedanceSizes, ...adobeSeedance1080Sizes],
		maxReferenceImages: 9, supportsStartEnd: true, supportsVideoAudioReferences: true,
		maxReferenceVideos: 3, maxReferenceAudios: 3, maxReferenceVideoDuration: 15, maxReferenceAudioDuration: 15,
		supportsGenerateAudio: false, promptMax: 5000,
	},
	"adobe:seedance-2.0-fast": {
		name: "Adobe · Seedance 2.0 Fast", role: "Firefly 快速多模态",
		use: "通过 Adobe Firefly 快速异步生成 4–15 秒视频，支持参考图、首尾帧、视频与音频参考。",
		duration: "4–15 秒", resolution: "480p / 720p（尺寸与积分档绑定）",
		limits: ["图片、视频和音频合计最多 12 个", "当前公开契约固定关闭原生音频"],
		durationValues: Array.from({ length: 12 }, (_, index) => index + 4), defaultDuration: 8,
		resolutions: ["480p", "720p"], sizes: adobeSeedanceSizes,
		maxReferenceImages: 9, supportsStartEnd: true, supportsVideoAudioReferences: true,
		maxReferenceVideos: 3, maxReferenceAudios: 3, maxReferenceVideoDuration: 15, maxReferenceAudioDuration: 15,
		supportsGenerateAudio: false, promptMax: 5000,
	},
  "flux-3-video": {
    name: "FLUX 3 Video",
    role: "原生音频长视频",
    use: "适合强提示词遵循、同步原生音频、首尾帧插值和既有视频续写。",
    duration: "5–20 秒",
    resolution: "720p / 1080p（尺寸与积分档绑定）",
    limits: ["首尾帧模式与参考视频模式互斥", "参考视频最多 1 段、50 MB，时长不超过 15.05 秒"],
    durationValues: Array.from({ length: 16 }, (_, index) => index + 5),
    defaultDuration: 8,
    resolutions: ["720p", "1080p"],
    sizes: [
      { value: "1470x630", label: "电影 21:9 · 1470x630 · 720p", resolution: "720p" },
      { value: "1360x680", label: "宽屏 2:1 · 1360x680 · 720p", resolution: "720p" },
      { value: "1280x720", label: "横屏 16:9 · 1280x720 · 720p", resolution: "720p" },
      { value: "1112x834", label: "横屏 4:3 · 1112x834 · 720p", resolution: "720p" },
      { value: "960x960", label: "方形 1:1 · 960x960 · 720p", resolution: "720p" },
      { value: "834x1112", label: "竖屏 3:4 · 834x1112 · 720p", resolution: "720p" },
      { value: "720x1280", label: "竖屏 9:16 · 720x1280 · 720p", resolution: "720p" },
      { value: "2520x1080", label: "电影 21:9 · 2520x1080 · 1080p", resolution: "1080p" },
      { value: "2160x1080", label: "宽屏 2:1 · 2160x1080 · 1080p", resolution: "1080p" },
      { value: "1920x1080", label: "横屏 16:9 · 1920x1080 · 1080p", resolution: "1080p" },
      { value: "1440x1080", label: "横屏 4:3 · 1440x1080 · 1080p", resolution: "1080p" },
      { value: "1440x1440", label: "方形 1:1 · 1440x1440 · 1080p", resolution: "1080p" },
      { value: "1080x1440", label: "竖屏 3:4 · 1080x1440 · 1080p", resolution: "1080p" },
      { value: "1080x1920", label: "竖屏 9:16 · 1080x1920 · 1080p", resolution: "1080p" },
    ],
    maxReferenceImages: 0,
    supportsStartEnd: true,
    supportsVideoAudioReferences: false,
    maxReferenceVideos: 1,
    maxReferenceAudios: 0,
    maxReferenceVideoDuration: 15.05,
    supportsGenerateAudio: true,
    promptMax: 5000,
  },
  "seedance-2.0": {
    name: "Seedance 2.0",
    role: "高质量主模型",
    use: "适合强调画面质量、运动连贯性和高分辨率输出的视频。",
    duration: "4–15 秒",
    resolution: "480p / 720p / 1080p / 2160p",
    limits: ["高分辨率和长时长会明显增加积分消耗", "支持参考图、首尾帧、视频与音频参考"],
    durationValues: Array.from({ length: 12 }, (_, index) => index + 4),
    defaultDuration: 8,
    resolutions: ["480p", "720p", "1080p", "2160p"],
    maxReferenceImages: 4,
    supportsStartEnd: true,
    supportsVideoAudioReferences: true,
    supportsGenerateAudio: true,
    promptMax: 5000,
  },
  "seedance-2.0-fast": {
    name: "Seedance 2.0 Fast",
    role: "快速生成",
    use: "适合快速预览、镜头方案验证和较高频率的短视频生成。",
    duration: "4–15 秒",
    resolution: "480p / 720p",
    limits: ["最高开放到 720p", "支持参考图、首尾帧、视频与音频参考"],
    durationValues: Array.from({ length: 12 }, (_, index) => index + 4),
    defaultDuration: 8,
    resolutions: ["480p", "720p"],
    maxReferenceImages: 4,
    supportsStartEnd: true,
    supportsVideoAudioReferences: true,
    supportsGenerateAudio: true,
    promptMax: 5000,
  },
  "seedance-2.0-mini": {
    name: "Seedance 2.0 Mini",
    role: "低成本打样",
    use: "适合低成本测试提示词、构图和运动方向，再切换主模型生成。",
    duration: "4–15 秒",
    resolution: "480p / 720p",
    limits: ["支持参考图、首尾帧、视频与音频参考", "复杂运动和细节能力低于主模型"],
    durationValues: Array.from({ length: 12 }, (_, index) => index + 4),
    defaultDuration: 8,
    resolutions: ["480p", "720p"],
    maxReferenceImages: 4,
    supportsStartEnd: true,
    supportsVideoAudioReferences: true,
    supportsGenerateAudio: true,
    promptMax: 5000,
  },
  "seedance-2.5": {
    name: "Seedance 2.5",
    role: "长时长全模态",
    use: "适合精确运镜、角色一致性、长达 30 秒的原生音频视频和多模态参考编辑。",
    duration: "4–30 秒",
    resolution: "480p / 720p（尺寸与积分档绑定）",
    limits: [
      "提示词最多 5,000 个 Unicode 字符",
      "支持参考图、首尾帧、参考视频和参考音频；首尾帧不能与其他参考媒体混用",
      "参考视频和参考音频各最多 10 个，合计时长不超过 30.2 秒；音频需要普通参考图或参考视频",
    ],
    durationValues: Array.from({ length: 27 }, (_, index) => index + 4),
    defaultDuration: 8,
    resolutions: ["480p", "720p"],
    sizes: [
      { value: "1280x720", label: "横屏 16:9 · 1280x720 · 720p", resolution: "720p" },
      { value: "1470x630", label: "电影 21:9 · 1470x630 · 720p", resolution: "720p" },
      { value: "1112x834", label: "横屏 4:3 · 1112x834 · 720p", resolution: "720p" },
      { value: "960x960", label: "方形 1:1 · 960x960 · 720p", resolution: "720p" },
      { value: "834x1112", label: "竖屏 3:4 · 834x1112 · 720p", resolution: "720p" },
      { value: "720x1280", label: "竖屏 9:16 · 720x1280 · 720p", resolution: "720p" },
      { value: "992x432", label: "电影 21:9 · 992x432 · 480p", resolution: "480p" },
      { value: "864x496", label: "宽屏 · 864x496 · 480p", resolution: "480p" },
      { value: "752x560", label: "横屏 4:3 · 752x560 · 480p", resolution: "480p" },
      { value: "640x640", label: "方形 1:1 · 640x640 · 480p", resolution: "480p" },
      { value: "560x752", label: "竖屏 3:4 · 560x752 · 480p", resolution: "480p" },
      { value: "496x864", label: "竖屏 9:16 · 496x864 · 480p", resolution: "480p" },
    ],
    maxReferenceImages: 30,
    supportsStartEnd: true,
    supportsVideoAudioReferences: true,
    maxReferenceVideos: 10,
    maxReferenceAudios: 10,
    maxReferenceVideoDuration: 30.2,
    maxReferenceAudioDuration: 30.2,
    supportsGenerateAudio: true,
    promptMax: 5000,
  },
  "veo-3.1": {
    name: "Veo 3.1",
    role: "Google 旗舰",
    use: "适合电影感叙事、首尾帧控制和高质量含音频视频。",
    duration: "4 / 6 / 8 秒",
    resolution: "720p / 1080p / 2160p",
    limits: ["普通参考图最多 3 张，且只支持 1280x720、8 秒", "支持首帧、尾帧和原生音频开关"],
    durationValues: [4, 6, 8],
    defaultDuration: 8,
    resolutions: ["720p", "1080p", "2160p"],
    maxReferenceImages: 3,
    supportsStartEnd: true,
    supportsVideoAudioReferences: false,
    supportsGenerateAudio: true,
    promptMax: 9999,
  },
  "veo-3.1-fast": {
    name: "Veo 3.1 Fast",
    role: "快速高质量",
    use: "适合以较低积分快速验证 Veo 镜头方案和成片方向。",
    duration: "4 / 6 / 8 秒",
    resolution: "720p / 1080p / 2160p",
    limits: ["不支持普通参考图", "支持首帧、尾帧和原生音频开关"],
    durationValues: [4, 6, 8],
    defaultDuration: 8,
    resolutions: ["720p", "1080p", "2160p"],
    maxReferenceImages: 0,
    supportsStartEnd: true,
    supportsVideoAudioReferences: false,
    supportsGenerateAudio: true,
    promptMax: 9999,
  },
  "kling-o3-omni": {
    name: "Kling Video O3 Omni",
    role: "全模态视频",
    use: "适合长视频、原生音频、多图角色一致性、首尾帧控制和视频续写。",
    duration: "3–15 秒",
    resolution: "720p / 1080p / 2160p（尺寸与积分档绑定）",
    limits: ["参考图最多 7 张；与视频同时使用时最多 4 张", "参考视频限 1 个、3–10.05 秒、每边 720–2160 像素；不支持 2160p 视频参考"],
    durationValues: Array.from({ length: 13 }, (_, index) => index + 3),
    defaultDuration: 5,
    resolutions: ["720p", "1080p", "2160p"],
    sizes: [
      { value: "1920x1080", label: "横屏 16:9 · 1920x1080 · 1080p", resolution: "1080p" },
      { value: "1080x1920", label: "竖屏 9:16 · 1080x1920 · 1080p", resolution: "1080p" },
      { value: "1440x1440", label: "方形 1:1 · 1440x1440 · 1080p", resolution: "1080p" },
      { value: "1280x720", label: "横屏 16:9 · 1280x720 · 720p", resolution: "720p" },
      { value: "720x1280", label: "竖屏 9:16 · 720x1280 · 720p", resolution: "720p" },
      { value: "960x960", label: "方形 1:1 · 960x960 · 720p", resolution: "720p" },
      { value: "3840x2160", label: "横屏 16:9 · 3840x2160 · 2160p", resolution: "2160p" },
      { value: "2160x3840", label: "竖屏 9:16 · 2160x3840 · 2160p", resolution: "2160p" },
      { value: "2880x2880", label: "方形 1:1 · 2880x2880 · 2160p", resolution: "2160p" },
    ],
    maxReferenceImages: 7,
    maxReferenceImagesWithVideo: 4,
    supportsStartEnd: true,
    supportsVideoAudioReferences: false,
    maxReferenceVideos: 1,
    minReferenceVideoDuration: 3,
    maxReferenceVideoDuration: 10.05,
    supportsGenerateAudio: true,
    promptMax: 2500,
  },
  "minimax-h3": {
    name: "MiniMax H3",
    role: "原生音频 2K",
    use: "适合需要 2K 画面、原生音频和多张图像或音频参考的成片视频。",
    duration: "5–15 秒",
    resolution: "固定 1440p（2K）",
    limits: ["提示词最多 2,000 字符", "原生音频始终开启，最多 3 段参考音频且合计不超过 15 秒"],
    durationValues: Array.from({ length: 11 }, (_, index) => index + 5),
    defaultDuration: 5,
    resolutions: ["1440p"],
    sizes: [
      { value: "2560x1440", label: "横屏 16:9 · 2560x1440" },
      { value: "3360x1440", label: "电影 21:9 · 3360x1440" },
      { value: "1920x1440", label: "横屏 4:3 · 1920x1440" },
      { value: "1440x1440", label: "方形 1:1 · 1440x1440" },
      { value: "1440x1920", label: "竖屏 3:4 · 1440x1920" },
      { value: "1440x2560", label: "竖屏 9:16 · 1440x2560" },
    ],
    maxReferenceImages: 5,
    supportsStartEnd: true,
    supportsVideoAudioReferences: false,
    maxReferenceVideos: 0,
    maxReferenceAudios: 3,
    supportsGenerateAudio: true,
    alwaysGenerateAudio: true,
    promptMax: 2000,
  },
  "grok-imagine-1.5": {
    name: "Grok Imagine 1.5",
    role: "首帧电影化",
    use: "适合将单张静态图转为带运动、电影感细节和原生音频的视频。",
    duration: "3–15 秒",
    resolution: "480p / 720p / 1080p（尺寸与积分档绑定）",
    limits: ["必须上传 start_frame，不支持纯文生视频", "不支持尾帧、普通参考图、参考视频或参考音频"],
    durationValues: Array.from({ length: 13 }, (_, index) => index + 3),
    defaultDuration: 6,
    resolutions: ["480p", "720p", "1080p"],
    sizes: [
      { value: "736x400", label: "横屏 16:9 · 736x400 · 480p", resolution: "480p" },
      { value: "400x736", label: "竖屏 9:16 · 400x736 · 480p", resolution: "480p" },
      { value: "544x544", label: "方形 1:1 · 544x544 · 480p", resolution: "480p" },
      { value: "1280x720", label: "横屏 16:9 · 1280x720 · 720p", resolution: "720p" },
      { value: "720x1280", label: "竖屏 9:16 · 720x1280 · 720p", resolution: "720p" },
      { value: "960x960", label: "方形 1:1 · 960x960 · 720p", resolution: "720p" },
      { value: "1888x1072", label: "横屏 16:9 · 1888x1072 · 1080p", resolution: "1080p" },
      { value: "1072x1888", label: "竖屏 9:16 · 1072x1888 · 1080p", resolution: "1080p" },
      { value: "1424x1424", label: "方形 1:1 · 1424x1424 · 1080p", resolution: "1080p" },
    ],
    maxReferenceImages: 0,
    supportsStartEnd: true,
    supportsEndFrame: false,
    requiresStartFrame: true,
    supportsVideoAudioReferences: false,
    maxReferenceVideos: 0,
    maxReferenceAudios: 0,
    supportsGenerateAudio: true,
    promptMax: 5000,
  },
};

export const standardVideoSizes: { value: string; label: string; resolution?: string }[] = [
  { value: "1280x720", label: "横屏 16:9" },
  { value: "720x1280", label: "竖屏 9:16" },
];

export function modelVideoSizes(model: PublicVideoModel) {
  return videoModelDocs[model].sizes || standardVideoSizes;
}

export function supportsVideoEndFrame(spec: (typeof videoModelDocs)[PublicVideoModel]) {
  return spec.supportsEndFrame ?? spec.supportsStartEnd;
}

export function videoResolutionForSize(model: PublicVideoModel, size: string) {
  return modelVideoSizes(model).find((option) => option.value === size)?.resolution;
}

export function defaultVideoResolution(model: PublicVideoModel) {
  const firstSize = modelVideoSizes(model)[0];
  return firstSize.resolution || videoModelDocs[model].resolutions[0];
}

export function videoResolutionsForSize(model: PublicVideoModel, size: string) {
  const resolution = videoResolutionForSize(model, size);
  return resolution ? [resolution] : videoModelDocs[model].resolutions;
}

export function maxVideoReferences(spec: (typeof videoModelDocs)[PublicVideoModel]) {
  return spec.maxReferenceVideos ?? (spec.supportsVideoAudioReferences ? 3 : 0);
}

export function maxImageReferences(spec: (typeof videoModelDocs)[PublicVideoModel], withVideo = false) {
  return withVideo ? (spec.maxReferenceImagesWithVideo ?? spec.maxReferenceImages) : spec.maxReferenceImages;
}

export function maxAudioReferences(spec: (typeof videoModelDocs)[PublicVideoModel]) {
  return spec.maxReferenceAudios ?? (spec.supportsVideoAudioReferences ? 1 : 0);
}

export type PublicAudioModel = "dialogue-v3" | "music-v1" | "sound-effects-v2";

export const audioModelDocs: Record<
  PublicAudioModel,
  { name: string; role: string; use: string; price: string; limits: string[]; promptMax: number }
> = {
  "dialogue-v3": {
    name: "Dialogue V3",
    role: "文本转语音",
    use: "将文本转换为单人语音，使用公开语音别名选择声音。",
    price: "90 积分 / 1000 字符 / 条",
    promptMax: 5000,
    limits: [
      "文本最长 5,000 个字符",
      "每次 1–4 条；voice、language、prompt_influence 可用",
    ],
  },
  "music-v1": {
    name: "Music V1",
    role: "提示词配乐",
    use: "根据文字描述生成音乐，可要求纯音乐。时长以整分钟计费。",
    price: "700 积分 / 分钟 / 条",
    promptMax: 9999,
    limits: [
      "duration_minutes 仅支持 1–10",
      "提示词最多 9,999 个 Unicode 字符",
      "每次 1–4 条；仅接受 force_instrumental",
    ],
  },
  "sound-effects-v2": {
    name: "Sound Effects V2",
    role: "提示词音效",
    use: "生成短音效或环境声；可选择循环输出。",
    price: "2 积分 / 秒 / 条",
    promptMax: 9999,
    limits: [
      "duration 仅支持 1–22 秒",
      "提示词最多 9,999 个 Unicode 字符",
      "每次 1–4 条；支持 loop、prompt_influence",
    ],
  },
};

export const audioVoices = [
  "roger",
  "sarah",
  "laura",
  "charlie",
  "george",
  "callum",
  "river",
  "harry",
  "liam",
  "alice",
  "matilda",
  "will",
  "jessica",
  "eric",
  "bella",
  "chris",
  "brian",
  "daniel",
  "lily",
  "adam",
  "bill",
];
