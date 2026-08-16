import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  AudioLines,
  Check,
  CirclePlay,
  Coins,
  Eye,
  EyeOff,
  Image as ImageIcon,
  KeyRound,
  Plus,
  RefreshCw,
  ScrollText,
  Upload,
  Video,
} from "lucide-react";
import { Badge, Button, Tabs, TabsList, TabsTrigger } from "../components/ui";
import type {
  CachedAudioResult,
  CachedImageResult,
  CachedVideoResult,
  MediaKind,
  PlaygroundTask,
  Provider,
  PublicVideoModel,
} from "../shared/types";
import type { PlaygroundImageModel } from "../shared/catalog";
import { imageSizes } from "../shared/catalog";
import { api, idempotencyKey, publicAPI } from "../shared/api";
import { clearPlaygroundCache, useCachedResult, useSessionState } from "../shared/cache";
import { assertPromptLength } from "../shared/text";
import {
  isRegisteredProviderID,
  modelDisplayID,
  modelProviderID,
  providerInternalModel,
  providerPublicModels,
  type RegisteredProviderID,
} from "../shared/providers";
import { GenerationProviderSelector } from "../components/generation-provider-selector";
import { PlaygroundResult } from "../components/playground-result";
import {
  PublicAudioModel,
  audioModelDocs,
  audioVoices,
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

type PlaygroundKind = MediaKind | "chat";
type GenerationProvider = RegisteredProviderID;

const imageModelsByProvider: Record<GenerationProvider, PlaygroundImageModel[]> = {
  leonardo: providerPublicModels<PlaygroundImageModel>("leonardo", "image"),
  adobe: providerPublicModels<PlaygroundImageModel>("adobe", "image"),
};

function providerImageModel(provider: GenerationProvider, model: PlaygroundImageModel): PlaygroundImageModel {
  return providerInternalModel<PlaygroundImageModel>(provider, "image", model) || model;
}

export function Playground() {
  const [media, setMedia] = useSessionState<PlaygroundKind>("media", "image");
  const [searchParams] = useSearchParams();
  const requestedMedia = searchParams.get("media");
  const requestedModel = searchParams.get("model") || undefined;
  const requestedProvider = searchParams.get("provider");
  const initialProvider = isRegisteredProviderID(requestedProvider) ? requestedProvider : undefined;
  const [apiKey, setAPIKey] = useSessionState("api-key", "");
  const [showKey, setShowKey] = useState(false);
  const providers = useQuery({
    queryKey: ["providers"],
    queryFn: () => api<Provider[]>("/admin/api/providers"),
  });
  React.useEffect(() => {
    if (requestedMedia === "image" || requestedMedia === "video" || requestedMedia === "audio" || requestedMedia === "chat") {
      setMedia(requestedMedia);
    }
  }, [requestedMedia, setMedia]);
  return (
    <section className="playground">
      <div className="playground-auth">
        <div>
          <span className="eyebrow">API Credential</span>
          <h2>测试凭据</h2>
          <p>
            密钥与参数保留在当前标签页；最近一次图像、视频和音频结果会持久缓存。
          </p>
        </div>
        <div className="playground-auth-actions">
          <label>
            API Key
            <div className="secret-field">
              <KeyRound />
              <input
                type={showKey ? "text" : "password"}
                value={apiKey}
                onChange={(e) => setAPIKey(e.target.value)}
                placeholder="leo_..."
                autoComplete="off"
              />
              <button
                type="button"
                className="icon"
                onClick={() => setShowKey((v) => !v)}
                aria-label={showKey ? "隐藏 API Key" : "显示 API Key"}
                title={showKey ? "隐藏 API Key" : "显示 API Key"}
              >
                {showKey ? <EyeOff /> : <Eye />}
              </button>
            </div>
          </label>
          <button
            className="secondary clear-cache"
            onClick={() => void clearPlaygroundCache()}
            title="清除测试密钥、参数和结果缓存"
          >
            <RefreshCw />
            清除缓存
          </button>
        </div>
      </div>
      <div className="playground-mode">
        <Tabs value={media} onValueChange={(value) => setMedia(value as PlaygroundKind)}>
          <TabsList className="playground-kind-tabs" aria-label="测试类型">
          <TabsTrigger value="image">
            <ImageIcon />
            图像测试
          </TabsTrigger>
          <TabsTrigger value="video">
            <Video />
            视频测试
          </TabsTrigger>
          <TabsTrigger value="audio">
            <AudioLines />
            音频测试
          </TabsTrigger>
          <TabsTrigger value="chat">
            <ScrollText />
            Chat
          </TabsTrigger>
          </TabsList>
        </Tabs>
        <span>
          <AlertTriangle />
          提交会调用真实接口并消耗所选平台积分
        </span>
      </div>
      {media === "image" ? (
        <ImagePlayground apiKey={apiKey} providers={providers.data || []} initialModel={requestedModel} initialProvider={initialProvider} />
      ) : media === "video" ? (
        <VideoPlayground apiKey={apiKey} providers={providers.data || []} initialModel={requestedModel} initialProvider={initialProvider} />
      ) : media === "audio" ? (
        <AudioPlayground apiKey={apiKey} initialModel={requestedModel} />
      ) : (
        <ChatPlayground apiKey={apiKey} providers={providers.data || []} />
      )}
    </section>
  );
}

function ChatPlayground({ apiKey, providers }: { apiKey: string; providers: Provider[] }) {
  const [model, setModel] = useSessionState<PlaygroundImageModel>("chat-model", "gpt-image-2");
	const [provider, setProvider] = useSessionState<GenerationProvider>("chat-provider", "leonardo");
  const [prompt, setPrompt] = useSessionState("chat-prompt", "生成一张白色背景上的产品摄影，柔和棚拍光线");
	const selectableModels = imageModelsByProvider[provider];
	const effectiveModel = providerImageModel(provider, model);
  React.useEffect(() => {
    if (!selectableModels.includes(model) && selectableModels[0]) setModel(selectableModels[0]);
  }, [model, selectableModels, setModel]);
  const mutation = useMutation({
    mutationFn: () => {
		assertPromptLength(prompt, model, imageModelDocs[effectiveModel].promptMax);
      return publicAPI<Record<string, unknown>>("/v1/chat/completions", apiKey, {
        method: "POST",
        headers: { "Idempotency-Key": idempotencyKey() },
        body: JSON.stringify({
		  provider,
          model,
          stream: false,
          messages: [{ role: "user", content: prompt }],
        }),
      });
    },
  });
  return (
    <section className="playground-workspace chat-playground">
      <form className="playground-form" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}>
        <div className="section-heading">
          <div><span className="eyebrow">Chat Compatibility</span><h2>Chat Completions 测试</h2></div>
          <Badge tone="warning">会消耗积分</Badge>
        </div>
		<GenerationProviderSelector providers={providers} value={provider} capability="image" onChange={(next) => {
		  setProvider(next);
		  if (!imageModelsByProvider[next].includes(model)) setModel(imageModelsByProvider[next][0]);
		}} />
        <label>模型
          <select value={model} onChange={(event) => setModel(event.target.value as PlaygroundImageModel)}>
			{selectableModels.map((value) => <option value={value} key={value}>{modelDisplayID(value)}</option>)}
          </select>
        </label>
        <label>用户消息
          <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} rows={6} />
        </label>
        {mutation.error && <p className="error" role="alert">{mutation.error.message}</p>}
        <Button type="submit" disabled={!apiKey.trim() || !prompt.trim() || mutation.isPending}>
          <CirclePlay size={17} />{mutation.isPending ? "提交中" : "提交 Chat 请求"}
        </Button>
      </form>
      <section className="playground-result-panel">
        <div className="section-heading"><div><span className="eyebrow">Response</span><h2>响应结果</h2></div></div>
        {mutation.data ? <pre>{JSON.stringify(mutation.data, null, 2)}</pre> : <div className="playground-empty"><ScrollText /><strong>等待请求</strong><span>填写 API Key 和消息后提交。</span></div>}
      </section>
    </section>
  );
}

function FileInput({
  files,
  onChange,
}: {
  files: File[];
  onChange: (files: File[]) => void;
}) {
  return (
    <label className="playground-file">
      <input
        type="file"
        accept="image/png,image/jpeg,image/webp"
        multiple
        onChange={(e) => onChange(Array.from(e.target.files || []).slice(0, 6))}
      />
      <Upload />
      <span>
        <strong>
          {files.length ? `已选择 ${files.length} 张参考图` : "选择参考图"}
        </strong>
        <small>
          {files.length
            ? files.map((f) => f.name).join("、")
            : "PNG、JPEG 或 WebP，最多 6 张"}
        </small>
      </span>
    </label>
  );
}

function ImagePlayground({ apiKey, providers, initialModel, initialProvider }: { apiKey: string; providers: Provider[]; initialModel?: string; initialProvider?: GenerationProvider }) {
  const [mode, setMode] = useSessionState<"generation" | "edit">(
    "image-mode",
    "generation",
  );
  const [model, setModel] = useSessionState<PlaygroundImageModel>(
    "image-model",
    "gpt-image-2",
  );
  const [provider, setProvider] = useSessionState<GenerationProvider>("image-provider", "leonardo");
  React.useEffect(() => {
    if (!initialModel) return;
    const inferredProvider = initialProvider || modelProviderID(initialModel);
    if (!isRegisteredProviderID(inferredProvider)) return;
    const publicModel = modelDisplayID(initialModel) as PlaygroundImageModel;
    if (!imageModelsByProvider[inferredProvider].includes(publicModel)) return;
    setProvider(inferredProvider);
    setModel(publicModel);
  }, [initialModel, initialProvider, setModel, setProvider]);
  const [prompt, setPrompt] = useSessionState("image-prompt", "");
  const [size, setSize] = useSessionState("image-size", "1024x1024");
  const [quality, setQuality] = useSessionState("image-quality", "low");
  const [count, setCount] = useSessionState("image-count", 1);
  const [background, setBackground] = useSessionState<"auto" | "opaque">(
    "image-background",
    "opaque",
  );
  const [strength, setStrength] = useSessionState("image-strength", "MID");
  const [files, setFiles] = useState<File[]>([]);
  const effectiveModel = providerImageModel(provider, model);
  const selectableImageModels = imageModelsByProvider[provider];
  React.useEffect(() => {
    if (!selectableImageModels.includes(model) && selectableImageModels[0]) setModel(selectableImageModels[0]);
  }, [model, selectableImageModels, setModel]);
  const [cachedResult, setCachedResult, cacheReady] =
    useCachedResult<CachedImageResult>("latest-image");
  const [taskID, setTaskID] = useState("");
  const create = useMutation({
    mutationFn: async () => {
      if (!apiKey.trim()) throw new Error("请先填写 API Key");
      if (!prompt.trim()) throw new Error("请输入图像描述");
      assertPromptLength(prompt, model, imageModelDocs[effectiveModel].promptMax);
      if (mode === "edit" && !files.length)
        throw new Error("图生图至少需要 1 张参考图");
      const common = {
		provider,
        model,
        prompt: prompt.trim(),
        size,
		n: provider === "adobe" || model === "gpt-image-2" ? 1 : count,
		response_format: "url",
        background,
        moderation: "auto",
      };
      if (mode === "generation")
		return publicAPI<PlaygroundTask>("/v1/images/generations", apiKey, {
          method: "POST",
          headers: { "Idempotency-Key": idempotencyKey() },
          body: JSON.stringify({
            ...common,
            quality: model === "gpt-image-2" ? quality : undefined,
          }),
        });
      const form = new FormData();
      Object.entries({
        ...common,
        quality: model === "gpt-image-2" ? quality : undefined,
        reference_strength: strength,
      }).forEach(([key, value]) => {
        if (value !== undefined) form.append(key, String(value));
      });
      files.forEach((file) => form.append("image[]", file));
		return publicAPI<PlaygroundTask>("/v1/images/generations", apiKey, {
        method: "POST",
        headers: { "Idempotency-Key": idempotencyKey() },
        body: form,
      });
    },
    onSuccess: (task) => setTaskID(task.id),
  });
  const task = useQuery({
    queryKey: ["playground-image", taskID],
    queryFn: () => publicAPI<PlaygroundTask>(`/v1/images/${taskID}`, apiKey),
    enabled: Boolean(taskID && apiKey),
    refetchInterval: (query) =>
      terminalStatuses.includes(query.state.data?.status || "") ? false : 3000,
  });
  const liveCurrent = task.data || create.data;
  React.useEffect(() => {
    if (liveCurrent?.status === "succeeded" && liveCurrent.result?.data?.length)
      setCachedResult({ task: liveCurrent, saved_at: Date.now() });
  }, [liveCurrent, setCachedResult]);
  const current = create.isPending
    ? undefined
    : liveCurrent || cachedResult?.task;
  const cancel = useMutation({
    mutationFn: () =>
      publicAPI<{ id: string; status: string }>(
        `/v1/images/${taskID}/cancel`,
        apiKey,
        { method: "POST" },
      ),
    onSuccess: () => task.refetch(),
  });
  function selectModel(next: PlaygroundImageModel) {
    setModel(next);
    setSize(imageSizes(providerImageModel(provider, next))[0]);
    if (next === "gpt-image-2") setCount(1);
  }
  function selectProvider(next: GenerationProvider) {
    const supported = imageModelsByProvider[next];
    const nextModel = supported.includes(model) ? model : supported[0];
    if (!nextModel) return;
    setProvider(next);
    setModel(nextModel);
    setSize(imageSizes(providerImageModel(next, nextModel))[0]);
    if (next === "adobe") setCount(1);
  }
  function clear() {
	setTaskID("");
	create.reset();
    setCachedResult(null);
  }
  return (
    <div className="playground-shell">
      <form
        className="playground-form"
        onSubmit={(e) => {
          e.preventDefault();
		  setTaskID("");
		  create.reset();
		  create.mutate();
        }}
      >
        <div className="playground-form-heading">
          <div>
            <span className="eyebrow">Image API</span>
            <h2>{mode === "generation" ? "文生图" : "图生图"}</h2>
          </div>
          <div className="segmented compact">
            <button
              type="button"
              className={mode === "generation" ? "active" : ""}
              onClick={() => setMode("generation")}
            >
              文生图
            </button>
            <button
              type="button"
              className={mode === "edit" ? "active" : ""}
              onClick={() => setMode("edit")}
            >
              图生图
            </button>
          </div>
        </div>
        <div className="playground-section-label">
          <strong>生成配置</strong>
          <span>必填与常用参数</span>
        </div>
        <GenerationProviderSelector providers={providers} value={provider} capability="image" onChange={selectProvider} />
        <label>
          模型
          <select
            value={model}
            onChange={(e) =>
              selectModel(e.target.value as PlaygroundImageModel)
            }
          >
            {selectableImageModels.map((v) => (
              <option key={v} value={v}>{modelDisplayID(v)}</option>
            ))}
          </select>
        </label>
        <label>
          提示词
          <textarea
            rows={5}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="描述主体、场景、光线、构图和风格…"
          />
        </label>
        {mode === "edit" && (
          <>
            <FileInput files={files} onChange={setFiles} />
            <label>
              参考强度
              <select
                value={strength}
                onChange={(e) => setStrength(e.target.value)}
              >
                <option value="LOW">LOW · 弱参考</option>
                <option value="MID">MID · 平衡</option>
                <option value="HIGH">HIGH · 强参考</option>
              </select>
            </label>
          </>
        )}
        <div className="playground-fields">
          <label>
            尺寸
            <select value={size} onChange={(e) => setSize(e.target.value)}>
              {imageSizes(effectiveModel).map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
          {(model === "gpt-image-2" || model === "adobe:gpt-image-2") && (
            <label>
              质量
              <select
                value={quality}
                onChange={(e) => setQuality(e.target.value)}
              >
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
              </select>
            </label>
          )}
          <label>
            数量
            <input
              type="number"
              min="1"
			  max={provider === "adobe" || model === "gpt-image-2" ? 1 : 4}
			  value={provider === "adobe" || model === "gpt-image-2" ? 1 : count}
			  disabled={provider === "adobe" || model === "gpt-image-2"}
              onChange={(e) => setCount(Number(e.target.value))}
            />
          </label>
        </div>
		<details className="playground-advanced">
          <summary>
            <span>
              <strong>高级参数</strong>
			  <small>异步交付与背景</small>
            </span>
            <Plus />
          </summary>
		  <div className="playground-fields">
            <label>
              背景
              <select
                value={background}
                onChange={(e) =>
                  setBackground(e.target.value as "auto" | "opaque")
                }
              >
                <option value="opaque">Opaque · 不透明</option>
                <option value="auto">Auto · 平台决定</option>
              </select>
            </label>
          </div>
          <div className="playground-contract">
            <span>
              <code>response_format</code>
			  <strong>url</strong>
            </span>
            <span>
              <code>moderation</code>
              <strong>auto</strong>
            </span>
            <span>
              <code>public</code>
              <strong>false</strong>
            </span>
            <span>
              <code>Idempotency-Key</code>
              <strong>自动生成</strong>
            </span>
          </div>
        </details>
		{create.error && <p className="error">{create.error.message}</p>}
		<button className="playground-submit" disabled={create.isPending}>
		  <ImageIcon />
		  {create.isPending ? "正在提交…" : "创建图像任务"}
        </button>
      </form>
      <PlaygroundResult
        kind="image"
		pending={create.isPending}
        restoring={!cacheReady}
		error={create.error?.message || task.error?.message}
		outputs={current?.result?.data}
		task={current}
        cachedAt={cachedResult?.saved_at}
        onClear={clear}
		onCancel={
		  liveCurrent?.status === "queued" && !cancel.isPending
			? () => cancel.mutate()
			: undefined
		}
      />
    </div>
  );
}

type PlaygroundVideoModel = PublicVideoModel | "kling-3.0-omni";

const videoModelsByProvider: Record<GenerationProvider, PlaygroundVideoModel[]> = {
  leonardo: providerPublicModels<PlaygroundVideoModel>("leonardo", "video"),
  adobe: providerPublicModels<PlaygroundVideoModel>("adobe", "video"),
};

function providerVideoModel(provider: GenerationProvider, model: PlaygroundVideoModel): PublicVideoModel {
  return (providerInternalModel<PublicVideoModel>(provider, "video", model) || model) as PublicVideoModel;
}

const terminalStatuses = [
  "succeeded",
  "failed",
  "cancelled",
];

function VideoMediaInput({
  label,
  hint,
  accept,
  files,
  max,
  multiple = false,
  onChange,
}: {
  label: string;
  hint: string;
  accept: string;
  files: File[];
  max: number;
  multiple?: boolean;
  onChange: (files: File[]) => void;
}) {
  return (
    <label className="playground-file video-reference-file">
      <input
        type="file"
        accept={accept}
        multiple={multiple}
        onChange={(event) =>
          onChange(Array.from(event.target.files || []).slice(0, max))
        }
      />
      <Upload />
      <span>
        <strong>{files.length ? files.map((file) => file.name).join("、") : label}</strong>
        <small>{files.length ? `${files.length} 个文件已选择` : hint}</small>
      </span>
    </label>
  );
}

function VideoPlayground({ apiKey, providers, initialModel, initialProvider }: { apiKey: string; providers: Provider[]; initialModel?: string; initialProvider?: GenerationProvider }) {
  const [mode, setMode] = useSessionState<"generation" | "reference">(
    "video-mode",
    "generation",
  );
  const [model, setModel] = useSessionState<PlaygroundVideoModel>(
    "video-model",
    "seedance-2.0-mini",
  );
  const [provider, setProvider] = useSessionState<GenerationProvider>("video-provider", "leonardo");
  React.useEffect(() => {
    if (!initialModel) return;
    const inferredProvider = initialProvider || modelProviderID(initialModel);
    if (!isRegisteredProviderID(inferredProvider)) return;
    const publicModel = modelDisplayID(initialModel) as PlaygroundVideoModel;
    if (!videoModelsByProvider[inferredProvider].includes(publicModel)) return;
    setProvider(inferredProvider);
    setModel(publicModel);
  }, [initialModel, initialProvider, setModel, setProvider]);
  const [prompt, setPrompt] = useSessionState("video-prompt", "");
  const [duration, setDuration] = useSessionState("video-duration", 8);
  const [size, setSize] = useSessionState("video-size", "1280x720");
  const [resolution, setResolution] = useSessionState(
    "video-resolution",
    "720p",
  );
  const [strength, setStrength] = useSessionState("video-strength", "MID");
  const [referenceImages, setReferenceImages] = useState<File[]>([]);
  const [startFrame, setStartFrame] = useState<File[]>([]);
  const [endFrame, setEndFrame] = useState<File[]>([]);
  const [referenceVideos, setReferenceVideos] = useState<File[]>([]);
  const [referenceAudio, setReferenceAudio] = useState<File[]>([]);
  const [generateAudio, setGenerateAudio] = useSessionState(
    "video-generate-audio",
    true,
  );
  const effectiveModel = providerVideoModel(provider, model);
  const selectableVideoModels = videoModelsByProvider[provider];
  React.useEffect(() => {
    if (!selectableVideoModels.includes(model) && selectableVideoModels[0]) setModel(selectableVideoModels[0]);
  }, [model, selectableVideoModels, setModel]);
  const selectedVideoSpec = videoModelDocs[effectiveModel];
  const referenceMode = Boolean(selectedVideoSpec.requiresStartFrame || mode === "reference");
  const [taskID, setTaskID] = useState("");
  const [cachedResult, setCachedResult, cacheReady] =
    useCachedResult<CachedVideoResult>("latest-video");
  const create = useMutation({
    mutationFn: async () => {
      if (!apiKey.trim()) throw new Error("请先填写 API Key");
      if (!prompt.trim()) throw new Error("请输入视频描述");
      assertPromptLength(prompt, model, selectedVideoSpec.promptMax);
      if (selectedVideoSpec.requiresStartFrame && !startFrame.length)
        throw new Error("Grok Imagine 1.5 必须上传首帧");
      if (!supportsVideoEndFrame(selectedVideoSpec) && endFrame.length)
        throw new Error("当前模型不支持尾帧");
      if (referenceImages.length && (startFrame.length || endFrame.length))
        throw new Error("普通参考图不能与首帧、尾帧同时使用");
      if (referenceVideos.length && (startFrame.length || endFrame.length))
        throw new Error("参考视频不能与首帧、尾帧同时使用");
      if (referenceImages.length > maxImageReferences(selectedVideoSpec, referenceVideos.length > 0))
        throw new Error(`当前参考模式最多支持 ${maxImageReferences(selectedVideoSpec, referenceVideos.length > 0)} 张普通参考图`);
      if (referenceAudio.length && !referenceImages.length && !referenceVideos.length)
        throw new Error("参考音频需要同时提供普通参考图或参考视频");
      const hasReference =
        referenceImages.length ||
        startFrame.length ||
        endFrame.length ||
        referenceVideos.length ||
        referenceAudio.length;
      if (referenceMode && !hasReference)
        throw new Error("多模态参考模式至少需要上传 1 个参考文件");
      const fields = {
		provider,
        model,
        prompt: prompt.trim(),
        duration,
        size,
        resolution,
        reference_strength: strength,
        generate_audio: selectedVideoSpec.supportsGenerateAudio
          ? selectedVideoSpec.alwaysGenerateAudio || generateAudio
          : undefined,
      };
      if (!referenceMode)
        return publicAPI<PlaygroundTask>("/v1/videos/generations", apiKey, {
          method: "POST",
          headers: { "Idempotency-Key": idempotencyKey() },
          body: JSON.stringify(fields),
        });
      const form = new FormData();
      Object.entries(fields).forEach(([key, value]) => {
        if (value !== undefined) form.append(key, String(value));
      });
      referenceImages.forEach((file) => form.append("image[]", file));
      if (startFrame[0]) form.append("start_frame", startFrame[0]);
      if (endFrame[0]) form.append("end_frame", endFrame[0]);
      referenceVideos.forEach((file) => form.append("video[]", file));
      referenceAudio.forEach((file) => form.append("audio[]", file));
      return publicAPI<PlaygroundTask>("/v1/videos/generations", apiKey, {
        method: "POST",
        headers: { "Idempotency-Key": idempotencyKey() },
        body: form,
      });
    },
    onSuccess: (task) => setTaskID(task.id),
  });
  const task = useQuery({
    queryKey: ["playground-video", taskID],
    queryFn: () => publicAPI<PlaygroundTask>(`/v1/videos/${taskID}`, apiKey),
    enabled: Boolean(taskID && apiKey),
    refetchInterval: (query) =>
      terminalStatuses.includes(query.state.data?.status || "") ? false : 3000,
  });
  const liveCurrent = task.data || create.data;
  React.useEffect(() => {
    if (liveCurrent?.status === "succeeded" && liveCurrent.result?.data?.length)
      setCachedResult({ task: liveCurrent, saved_at: Date.now() });
  }, [liveCurrent, setCachedResult]);
  const current = create.isPending
    ? undefined
    : liveCurrent || cachedResult?.task;
  const cancel = useMutation({
    mutationFn: () =>
      publicAPI<{ id: string; status: string }>(
        `/v1/videos/${taskID}/cancel`,
        apiKey,
        { method: "POST" },
      ),
    onSuccess: () => task.refetch(),
  });
  function selectModel(next: PlaygroundVideoModel) {
    const routed = providerVideoModel(provider, next);
    const spec = videoModelDocs[routed];
    setModel(next);
    setResolution(defaultVideoResolution(routed));
    setSize(modelVideoSizes(routed)[0].value);
    setDuration(spec.defaultDuration);
    setReferenceImages((current) => current.slice(0, maxImageReferences(spec, referenceVideos.length > 0)));
    if (!spec.supportsStartEnd) {
      setStartFrame([]);
      setEndFrame([]);
    } else if (!supportsVideoEndFrame(spec)) {
      setEndFrame([]);
    }
    if (maxVideoReferences(spec) === 0) {
      setReferenceVideos([]);
    }
    setReferenceAudio((current) => current.slice(0, maxAudioReferences(spec)));
    if (spec.alwaysGenerateAudio) setGenerateAudio(true);
    if (spec.requiresStartFrame) setMode("reference");
  }
  function selectProvider(next: GenerationProvider) {
    const supported = videoModelsByProvider[next];
    const nextModel = supported.includes(model) ? model : supported[0];
    if (!nextModel) return;
    setProvider(next);
    const routed = providerVideoModel(next, nextModel);
    const spec = videoModelDocs[routed];
    setModel(nextModel);
    setResolution(defaultVideoResolution(routed));
    setSize(modelVideoSizes(routed)[0].value);
    setDuration(spec.defaultDuration);
    setReferenceImages([]);
    setStartFrame([]);
    setEndFrame([]);
    setReferenceVideos([]);
    setReferenceAudio([]);
    if (spec.alwaysGenerateAudio) setGenerateAudio(true);
  }
  function resetLive() {
    setTaskID("");
    create.reset();
  }
  function clear() {
    resetLive();
    setCachedResult(null);
  }
  return (
    <div className="playground-shell">
      <form
        className="playground-form"
        onSubmit={(e) => {
          e.preventDefault();
          resetLive();
          create.mutate();
        }}
      >
        <div className="playground-form-heading">
          <div>
            <span className="eyebrow">Video API</span>
            <h2>{selectedVideoSpec.requiresStartFrame ? "首帧生视频" : !referenceMode ? "文生视频" : "多模态参考"}</h2>
          </div>
          <div className="segmented compact">
            <button
              type="button"
              className={!referenceMode ? "active" : ""}
              disabled={Boolean(selectedVideoSpec.requiresStartFrame)}
              onClick={() => setMode("generation")}
            >
              文生视频
            </button>
            <button
              type="button"
              className={referenceMode ? "active" : ""}
              onClick={() => setMode("reference")}
            >
              多模态参考
            </button>
          </div>
        </div>
        <div className="playground-section-label">
          <strong>生成配置</strong>
          <span>视频任务为异步执行</span>
        </div>
        <GenerationProviderSelector providers={providers} value={provider} capability="video" onChange={selectProvider} />
        <label>
          模型
          <select
            value={model}
            onChange={(e) =>
              selectModel(e.target.value as PlaygroundVideoModel)
            }
          >
            {selectableVideoModels.map((v) => (
              <option key={v} value={v}>{modelDisplayID(v)}</option>
            ))}
          </select>
        </label>
        <label>
          提示词
          <textarea
            rows={5}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="描述画面、动作、运镜、节奏和氛围…"
          />
        </label>
        {referenceMode && (
          <>
            <div className="video-reference-grid">
              {selectedVideoSpec.maxReferenceImages > 0 && (
                <VideoMediaInput
                  label="普通参考图"
                  hint={`最多 ${maxImageReferences(selectedVideoSpec, referenceVideos.length > 0)} 张 PNG、JPEG 或 WebP`}
                  accept="image/png,image/jpeg,image/webp"
                  files={referenceImages}
                  max={maxImageReferences(selectedVideoSpec, referenceVideos.length > 0)}
                  multiple
                  onChange={setReferenceImages}
                />
              )}
              {selectedVideoSpec.supportsStartEnd && (
                <>
                  <VideoMediaInput
                    label="首帧"
                    hint="锁定开场画面，最多 1 张"
                    accept="image/png,image/jpeg,image/webp"
                    files={startFrame}
                    max={1}
                    onChange={setStartFrame}
                  />
                  {supportsVideoEndFrame(selectedVideoSpec) && (
                    <VideoMediaInput
                      label="尾帧"
                      hint="锁定结束画面，最多 1 张"
                      accept="image/png,image/jpeg,image/webp"
                      files={endFrame}
                      max={1}
                      onChange={setEndFrame}
                    />
                  )}
                </>
              )}
              {maxVideoReferences(selectedVideoSpec) > 0 && (
                  <VideoMediaInput
                    label="参考视频"
                    hint={`最多 ${maxVideoReferences(selectedVideoSpec)} 个，合计不超过 ${selectedVideoSpec.maxReferenceVideoDuration ?? 15} 秒`}
                    accept="video/mp4,video/quicktime,video/webm,.mp4,.mov,.webm"
                    files={referenceVideos}
                    max={maxVideoReferences(selectedVideoSpec)}
                    multiple
                    onChange={(files) => {
                      setReferenceVideos(files);
                      if (files.length) setReferenceImages((current) => current.slice(0, maxImageReferences(selectedVideoSpec, true)));
                    }}
                  />
              )}
              {maxAudioReferences(selectedVideoSpec) > 0 && (
                  <VideoMediaInput
                    label="参考音频"
                    hint={`需同时提供参考图或视频，最多 ${maxAudioReferences(selectedVideoSpec)} 个，合计不超过 ${selectedVideoSpec.maxReferenceAudioDuration ?? 15} 秒`}
                    accept="audio/mpeg,audio/wav,audio/mp4,audio/aac,audio/ogg,.mp3,.wav,.m4a,.aac,.ogg"
                    files={referenceAudio}
                    max={maxAudioReferences(selectedVideoSpec)}
                    multiple={maxAudioReferences(selectedVideoSpec) > 1}
                    onChange={setReferenceAudio}
                  />
              )}
            </div>
            {referenceImages.length > 0 && (
              <label>
                普通参考图强度
                <select
                  value={strength}
                  onChange={(e) => setStrength(e.target.value)}
                >
                  <option value="LOW">LOW · 弱参考</option>
                  <option value="MID">MID · 平衡</option>
                  <option value="HIGH">HIGH · 强参考</option>
                </select>
              </label>
            )}
          </>
        )}
        <div className="playground-fields">
          <label>
            画幅
            <select
              value={size}
              onChange={(e) => {
                const nextSize = e.target.value;
                setSize(nextSize);
                const mappedResolution = videoResolutionForSize(effectiveModel, nextSize);
                if (mappedResolution) setResolution(mappedResolution);
              }}
            >
              {modelVideoSizes(effectiveModel).map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label>
            分辨率
            <select
              value={resolution}
              onChange={(e) => setResolution(e.target.value)}
            >
              {videoResolutionsForSize(effectiveModel, size).map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
          <label>
            时长
            <select
              value={duration}
              onChange={(e) => setDuration(Number(e.target.value))}
            >
              {selectedVideoSpec.durationValues.map((value) => (
                <option key={value} value={value}>{value} 秒</option>
              ))}
            </select>
          </label>
        </div>
        {selectedVideoSpec.supportsGenerateAudio && !selectedVideoSpec.alwaysGenerateAudio && (
          <label className="audio-toggle video-audio-toggle">
            <input
              type="checkbox"
              checked={generateAudio}
              onChange={(event) => setGenerateAudio(event.target.checked)}
            />
            <span>
              <strong>生成原生音频</strong>
              <small>对白、旁白、音效与环境声；默认开启并计入预留积分</small>
            </span>
          </label>
        )}
        {selectedVideoSpec.alwaysGenerateAudio && (
          <div className="audio-toggle video-audio-toggle">
            <Check />
            <span>
              <strong>原生音频固定开启</strong>
              <small>MiniMax H3 上游契约不支持关闭原生音频</small>
            </span>
          </div>
        )}
        <div className="playground-contract">
          <span>
            <code>返回方式</code>
            <strong>异步 Task</strong>
          </span>
          <span>
            <code>轮询间隔</code>
            <strong>3 秒</strong>
          </span>
          <span>
            <code>public</code>
            <strong>false</strong>
          </span>
          <span>
            <code>Idempotency-Key</code>
            <strong>自动生成</strong>
          </span>
        </div>
        {create.error && <p className="error">{create.error.message}</p>}
        <button
          className="playground-submit"
          disabled={
            create.isPending ||
            Boolean(current && !terminalStatuses.includes(current.status))
          }
        >
          <CirclePlay />
          {create.isPending ? "正在提交…" : "生成视频"}
        </button>
      </form>
      <PlaygroundResult
        kind="video"
        pending={create.isPending || (task.isFetching && !current)}
        restoring={!cacheReady}
        error={create.error?.message || task.error?.message}
        outputs={current?.result?.data}
        task={current}
        cachedAt={cachedResult?.saved_at}
        onClear={clear}
        onCancel={
          liveCurrent?.status === "queued" && !cancel.isPending
            ? () => cancel.mutate()
            : undefined
        }
      />
    </div>
  );
}

function AudioPlayground({ apiKey, initialModel }: { apiKey: string; initialModel?: string }) {
  const [model, setModel] = useSessionState<PublicAudioModel>(
    "audio-model",
    "sound-effects-v2",
  );
  React.useEffect(() => {
    if (initialModel && initialModel in audioModelDocs) setModel(initialModel as PublicAudioModel);
  }, [initialModel, setModel]);
  const [prompt, setPrompt] = useSessionState("audio-prompt", "");
  const [count, setCount] = useSessionState("audio-count", 1);
  const [voice, setVoice] = useSessionState("audio-voice", "george");
  const [language, setLanguage] = useSessionState("audio-language", "en");
  const [duration, setDuration] = useSessionState("audio-duration", 6);
  const [durationMinutes, setDurationMinutes] = useSessionState(
    "audio-duration-minutes",
    1,
  );
  const [promptInfluence, setPromptInfluence] = useSessionState(
    "audio-prompt-influence",
    0.7,
  );
  const [forceInstrumental, setForceInstrumental] = useSessionState(
    "audio-force-instrumental",
    true,
  );
  const [loop, setLoop] = useSessionState("audio-loop", true);
  const [taskID, setTaskID] = useState("");
  const [cachedResult, setCachedResult, cacheReady] =
    useCachedResult<CachedAudioResult>("latest-audio");
  const create = useMutation({
    mutationFn: async () => {
      if (!apiKey.trim()) throw new Error("请先填写 API Key");
      if (!prompt.trim()) throw new Error("请输入音频描述");
      assertPromptLength(prompt, model, audioModelDocs[model].promptMax);
      const common = { provider: "leonardo", model, prompt: prompt.trim(), n: count };
      const body =
        model === "dialogue-v3"
          ? { ...common, voice, language, prompt_influence: promptInfluence }
          : model === "music-v1"
            ? {
                ...common,
                duration_minutes: durationMinutes,
                force_instrumental: forceInstrumental,
              }
            : { ...common, duration, loop, prompt_influence: promptInfluence };
      return publicAPI<PlaygroundTask>("/v1/audio/generations", apiKey, {
        method: "POST",
        headers: { "Idempotency-Key": idempotencyKey() },
        body: JSON.stringify(body),
      });
    },
    onSuccess: (task) => setTaskID(task.id),
  });
  const task = useQuery({
    queryKey: ["playground-audio", taskID],
    queryFn: () => publicAPI<PlaygroundTask>(`/v1/audio/${taskID}`, apiKey),
    enabled: Boolean(taskID && apiKey),
    refetchInterval: (query) =>
      terminalStatuses.includes(query.state.data?.status || "") ? false : 3000,
  });
  const liveCurrent = task.data || create.data;
  React.useEffect(() => {
    if (liveCurrent?.status === "succeeded" && liveCurrent.result?.data?.length)
      setCachedResult({ task: liveCurrent, saved_at: Date.now() });
  }, [liveCurrent, setCachedResult]);
  const current = create.isPending
    ? undefined
    : liveCurrent || cachedResult?.task;
  const cancel = useMutation({
    mutationFn: () =>
      publicAPI<{ id: string; status: string }>(
        `/v1/audio/${taskID}/cancel`,
        apiKey,
        { method: "POST" },
      ),
    onSuccess: () => task.refetch(),
  });
  function selectModel(next: PublicAudioModel) {
    setModel(next);
    setCount(1);
    if (next === "dialogue-v3") {
      setPromptInfluence(0.5);
      setDuration(6);
    } else if (next === "music-v1") {
      setDurationMinutes(1);
    } else {
      setPromptInfluence(0.7);
      setDuration(6);
    }
  }
  function resetLive() {
    setTaskID("");
    create.reset();
  }
  function clear() {
    resetLive();
    setCachedResult(null);
  }
  const title =
    model === "dialogue-v3"
      ? "文本转语音"
      : model === "music-v1"
        ? "生成音乐"
        : "生成音效";
  return (
    <div className="playground-shell">
      <form
        className="playground-form"
        onSubmit={(e) => {
          e.preventDefault();
          resetLive();
          create.mutate();
        }}
      >
        <div className="playground-form-heading">
          <div>
            <span className="eyebrow">Audio API</span>
            <h2>{title}</h2>
          </div>
          <AudioLines />
        </div>
        <div className="playground-section-label">
          <strong>生成配置</strong>
          <span>音频任务为异步执行</span>
        </div>
        <label>
          模型
          <select
            value={model}
            onChange={(e) => selectModel(e.target.value as PublicAudioModel)}
          >
            {(Object.keys(audioModelDocs) as PublicAudioModel[]).map(
              (value) => (
                <option key={value}>{value}</option>
              ),
            )}
          </select>
        </label>
        <label>
          提示词
          <textarea
            rows={5}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder={
              model === "dialogue-v3"
                ? "输入需要朗读的文本…"
                : model === "music-v1"
                  ? "描述曲风、情绪、乐器、节奏和是否有歌词…"
                  : "描述声音来源、环境、材质、节奏和变化…"
            }
          />
        </label>
        {model === "dialogue-v3" ? (
          <>
            <div className="playground-fields">
              <label>
                语音
                <select
                  value={voice}
                  onChange={(e) => setVoice(e.target.value)}
                >
                  {audioVoices.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </select>
              </label>
              <label>
                语言
                <input
                  maxLength={16}
                  value={language}
                  onChange={(e) => setLanguage(e.target.value)}
                  placeholder="en"
                />
              </label>
              <label>
                提示词强度
                <input
                  type="number"
                  min="0"
                  max="1"
                  step="0.1"
                  value={promptInfluence}
                  onChange={(e) => setPromptInfluence(Number(e.target.value))}
                />
              </label>
            </div>
          </>
        ) : model === "music-v1" ? (
          <div className="playground-fields">
            <label>
              时长（分钟）
              <input
                type="number"
                min="1"
                max="10"
                value={durationMinutes}
                onChange={(e) => setDurationMinutes(Number(e.target.value))}
              />
            </label>
            <label className="audio-toggle">
              <input
                type="checkbox"
                checked={forceInstrumental}
                onChange={(e) => setForceInstrumental(e.target.checked)}
              />
              <span>
                <strong>仅器乐</strong>
                <small>不生成歌词或人声</small>
              </span>
            </label>
          </div>
        ) : (
          <div className="playground-fields">
            <label>
              时长（秒）
              <input
                type="number"
                min="1"
                max="22"
                value={duration}
                onChange={(e) => setDuration(Number(e.target.value))}
              />
            </label>
            <label>
              提示词强度
              <input
                type="number"
                min="0"
                max="1"
                step="0.1"
                value={promptInfluence}
                onChange={(e) => setPromptInfluence(Number(e.target.value))}
              />
            </label>
            <label className="audio-toggle">
              <input
                type="checkbox"
                checked={loop}
                onChange={(e) => setLoop(e.target.checked)}
              />
              <span>
                <strong>循环</strong>
                <small>适合无缝环境声</small>
              </span>
            </label>
          </div>
        )}
        <div className="playground-fields">
          <label>
            数量
            <input
              type="number"
              min="1"
              max="4"
              value={count}
              onChange={(e) => setCount(Number(e.target.value))}
            />
          </label>
          <div className="playground-cost-hint">
            <Coins />
            <span>
              {audioModelDocs[model].price}
              <small>积分由后端规则计算并预留</small>
            </span>
          </div>
        </div>
        <div className="playground-contract">
          <span>
            <code>返回方式</code>
            <strong>异步 Task</strong>
          </span>
          <span>
            <code>轮询间隔</code>
            <strong>3 秒</strong>
          </span>
          <span>
            <code>public</code>
            <strong>false</strong>
          </span>
          <span>
            <code>Idempotency-Key</code>
            <strong>自动生成</strong>
          </span>
        </div>
        {create.error && <p className="error">{create.error.message}</p>}
        <button
          className="playground-submit"
          disabled={
            create.isPending ||
            Boolean(current && !terminalStatuses.includes(current.status))
          }
        >
          <AudioLines />
          {create.isPending ? "正在提交…" : title}
        </button>
      </form>
      <PlaygroundResult
        kind="audio"
        pending={create.isPending || (task.isFetching && !current)}
        restoring={!cacheReady}
        error={create.error?.message || task.error?.message}
        outputs={current?.result?.data}
        task={current}
        cachedAt={cachedResult?.saved_at}
        onClear={clear}
        onCancel={
          liveCurrent?.status === "queued" && !cancel.isPending
            ? () => cancel.mutate()
            : undefined
        }
      />
    </div>
  );
}
