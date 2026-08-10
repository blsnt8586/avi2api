import React, { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  AudioLines,
  Check,
  CirclePlay,
  Coins,
  Download,
  Eye,
  EyeOff,
  FlaskConical,
  Image as ImageIcon,
  KeyRound,
  Plus,
  RefreshCw,
  ScrollText,
  Square,
  Upload,
  Video,
} from "lucide-react";
import { Badge, Button, Tabs, TabsList, TabsTrigger } from "../components/ui";
import type {
  CachedAudioResult,
  CachedImageResult,
  CachedVideoResult,
  ImageResponse,
  MediaKind,
  PlaygroundOutput,
  PlaygroundTask,
  PublicVideoModel,
} from "../shared/types";
import type { PlaygroundImageModel } from "../shared/catalog";
import { imageModels, imageSizes } from "../shared/catalog";
import { idempotencyKey, publicAPI } from "../shared/api";
import { clearPlaygroundCache, useCachedResult, useSessionState } from "../shared/cache";
import { statusText } from "../shared/status";
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

export function Playground() {
  const [media, setMedia] = useSessionState<PlaygroundKind>("media", "image");
  const [searchParams] = useSearchParams();
  const requestedMedia = searchParams.get("media");
  const requestedModel = searchParams.get("model") || undefined;
  const [apiKey, setAPIKey] = useSessionState("api-key", "");
  const [showKey, setShowKey] = useState(false);
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
          提交会调用真实接口并消耗 Leonardo 积分
        </span>
      </div>
      {media === "image" ? (
        <ImagePlayground apiKey={apiKey} initialModel={requestedModel} />
      ) : media === "video" ? (
        <VideoPlayground apiKey={apiKey} initialModel={requestedModel} />
      ) : media === "audio" ? (
        <AudioPlayground apiKey={apiKey} initialModel={requestedModel} />
      ) : (
        <ChatPlayground apiKey={apiKey} />
      )}
    </section>
  );
}

function ChatPlayground({ apiKey }: { apiKey: string }) {
  const [model, setModel] = useSessionState("chat-model", "gpt-image-2");
  const [prompt, setPrompt] = useSessionState("chat-prompt", "生成一张白色背景上的产品摄影，柔和棚拍光线");
  const mutation = useMutation({
    mutationFn: () => publicAPI<Record<string, unknown>>("/v1/chat/completions", apiKey, {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey() },
      body: JSON.stringify({
        model,
        stream: false,
        messages: [{ role: "user", content: prompt }],
      }),
    }),
  });
  return (
    <section className="playground-workspace chat-playground">
      <form className="playground-form" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}>
        <div className="section-heading">
          <div><span className="eyebrow">Chat Compatibility</span><h2>Chat Completions 测试</h2></div>
          <Badge tone="warning">会消耗积分</Badge>
        </div>
        <label>模型
          <select value={model} onChange={(event) => setModel(event.target.value)}>
            {imageModels.map((value) => <option value={value} key={value}>{value}</option>)}
          </select>
        </label>
        <label>用户消息
          <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} maxLength={9999} rows={6} />
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

function ImagePlayground({ apiKey, initialModel }: { apiKey: string; initialModel?: string }) {
  const [mode, setMode] = useSessionState<"generation" | "edit">(
    "image-mode",
    "generation",
  );
  const [model, setModel] = useSessionState<PlaygroundImageModel>(
    "image-model",
    "gpt-image-2",
  );
  React.useEffect(() => {
    if (initialModel && initialModel in imageModelDocs) setModel(initialModel as PlaygroundImageModel);
  }, [initialModel, setModel]);
  const [prompt, setPrompt] = useSessionState("image-prompt", "");
  const [size, setSize] = useSessionState("image-size", "1024x1024");
  const [quality, setQuality] = useSessionState("image-quality", "low");
  const [count, setCount] = useSessionState("image-count", 1);
  const [format, setFormat] = useSessionState<"png" | "jpeg">(
    "image-format",
    "jpeg",
  );
  const [compression, setCompression] = useSessionState(
    "image-compression",
    90,
  );
  const [background, setBackground] = useSessionState<"auto" | "opaque">(
    "image-background",
    "opaque",
  );
  const [strength, setStrength] = useSessionState("image-strength", "MID");
  const [files, setFiles] = useState<File[]>([]);
  const [cachedResult, setCachedResult, cacheReady] =
    useCachedResult<CachedImageResult>("latest-image");
  const mutation = useMutation({
    mutationFn: async () => {
      if (!apiKey.trim()) throw new Error("请先填写 API Key");
      if (!prompt.trim()) throw new Error("请输入图像描述");
      if (mode === "edit" && !files.length)
        throw new Error("图生图至少需要 1 张参考图");
      const common = {
        model,
        prompt: prompt.trim(),
        size,
        n: model === "gpt-image-2" ? 1 : count,
        response_format: "b64_json",
        output_format: format,
        output_compression: format === "jpeg" ? compression : undefined,
        background,
        moderation: "auto",
      };
      if (mode === "generation")
        return publicAPI<ImageResponse>("/v1/images/generations", apiKey, {
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
      return publicAPI<ImageResponse>("/v1/images/edits", apiKey, {
        method: "POST",
        headers: { "Idempotency-Key": idempotencyKey() },
        body: form,
      });
    },
    onSuccess: (response) =>
      setCachedResult({ response, format, saved_at: Date.now() }),
  });
  function selectModel(next: PlaygroundImageModel) {
    setModel(next);
    setSize(imageSizes(next)[0]);
    if (next === "gpt-image-2") setCount(1);
  }
  function clear() {
    mutation.reset();
    setCachedResult(null);
  }
  const cachedOutputs =
    mutation.isPending || mutation.error
      ? undefined
      : cachedResult?.response.data;
  return (
    <div className="playground-shell">
      <form
        className="playground-form"
        onSubmit={(e) => {
          e.preventDefault();
          mutation.mutate();
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
        <label>
          模型
          <select
            value={model}
            onChange={(e) =>
              selectModel(e.target.value as PlaygroundImageModel)
            }
          >
            {imageModels.map((v) => (
              <option key={v}>{v}</option>
            ))}
          </select>
        </label>
        <label>
          提示词
          <textarea
            rows={5}
            maxLength={9999}
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
              {imageSizes(model).map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
          {model === "gpt-image-2" && (
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
              max={model === "gpt-image-2" ? 1 : 4}
              value={model === "gpt-image-2" ? 1 : count}
              disabled={model === "gpt-image-2"}
              onChange={(e) => setCount(Number(e.target.value))}
            />
          </label>
        </div>
        <details className="playground-advanced">
          <summary>
            <span>
              <strong>高级参数</strong>
              <small>输出格式、压缩与背景</small>
            </span>
            <Plus />
          </summary>
          <div className="playground-fields">
            <label>
              输出格式
              <select
                value={format}
                onChange={(e) => setFormat(e.target.value as "png" | "jpeg")}
              >
                <option value="jpeg">JPEG</option>
                <option value="png">PNG</option>
              </select>
            </label>
            {format === "jpeg" && (
              <label>
                JPEG 压缩质量
                <input
                  type="number"
                  min="0"
                  max="100"
                  value={compression}
                  onChange={(e) => setCompression(Number(e.target.value))}
                />
              </label>
            )}
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
              <strong>b64_json</strong>
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
        {mutation.error && <p className="error">{mutation.error.message}</p>}
        <button className="playground-submit" disabled={mutation.isPending}>
          <ImageIcon />
          {mutation.isPending ? "正在生成…" : "生成图像"}
        </button>
      </form>
      <PlaygroundResult
        kind="image"
        imageFormat={cachedResult?.format || format}
        pending={mutation.isPending}
        restoring={!cacheReady}
        error={mutation.error?.message}
        outputs={cachedOutputs}
        cachedAt={cachedResult?.saved_at}
        onClear={clear}
      />
    </div>
  );
}

const videoModels = Object.keys(videoModelDocs) as PublicVideoModel[];

type PlaygroundVideoModel = PublicVideoModel;

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

function VideoPlayground({ apiKey, initialModel }: { apiKey: string; initialModel?: string }) {
  const [mode, setMode] = useSessionState<"generation" | "reference">(
    "video-mode",
    "generation",
  );
  const [model, setModel] = useSessionState<PlaygroundVideoModel>(
    "video-model",
    "seedance-2.0-mini",
  );
  React.useEffect(() => {
    if (initialModel && initialModel in videoModelDocs) setModel(initialModel as PlaygroundVideoModel);
  }, [initialModel, setModel]);
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
  const selectedVideoSpec = videoModelDocs[model];
  const referenceMode = Boolean(selectedVideoSpec.requiresStartFrame || mode === "reference");
  const [taskID, setTaskID] = useState("");
  const [cachedResult, setCachedResult, cacheReady] =
    useCachedResult<CachedVideoResult>("latest-video");
  const create = useMutation({
    mutationFn: async () => {
      if (!apiKey.trim()) throw new Error("请先填写 API Key");
      if (!prompt.trim()) throw new Error("请输入视频描述");
      if (prompt.length > selectedVideoSpec.promptMax)
        throw new Error(`当前模型提示词最多 ${selectedVideoSpec.promptMax} 字符`);
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
        model,
        prompt: prompt.trim(),
        duration,
        size,
        resolution,
        reference_strength: strength,
        generate_audio: videoModelDocs[model].supportsGenerateAudio
          ? videoModelDocs[model].alwaysGenerateAudio || generateAudio
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
    const spec = videoModelDocs[next];
    setModel(next);
    setResolution(defaultVideoResolution(next));
    setSize(modelVideoSizes(next)[0].value);
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
        <label>
          模型
          <select
            value={model}
            onChange={(e) =>
              selectModel(e.target.value as PlaygroundVideoModel)
            }
          >
            {videoModels.map((v) => (
              <option key={v}>{v}</option>
            ))}
          </select>
        </label>
        <label>
          提示词
          <textarea
            rows={5}
            maxLength={selectedVideoSpec.promptMax}
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
                const mappedResolution = videoResolutionForSize(model, nextSize);
                if (mappedResolution) setResolution(mappedResolution);
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
              value={resolution}
              onChange={(e) => setResolution(e.target.value)}
            >
              {videoResolutionsForSize(model, size).map((v) => (
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
      const common = { model, prompt: prompt.trim(), n: count };
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
            maxLength={model === "dialogue-v3" ? 5000 : 10000}
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

function MediaGallery({
  kind,
  imageFormat,
  outputs,
  viewMode,
}: {
  kind: MediaKind;
  imageFormat: "png" | "jpeg";
  outputs: PlaygroundOutput[];
  viewMode: "fit" | "fill";
}) {
  const single = outputs.length === 1;
  return (
    <div
      className={`playground-result-media ${single ? "single" : "multiple"} ${viewMode} ${kind}`}
    >
      {outputs.map((output, index) => {
        const src =
          output.url ||
          (output.b64_json
            ? `data:image/${imageFormat};base64,${output.b64_json}`
            : "");
        const extension =
          kind === "video" ? "mp4" : kind === "audio" ? "mp3" : imageFormat;
        const label =
          kind === "video" ? "视频" : kind === "audio" ? "音频" : "图像";
        return (
          <figure key={output.id || index}>
            {output.nsfw && (
              <div className="playground-content-warning">
                <AlertTriangle />
                <span>
                  <strong>上游标记为显式内容</strong>
                  <small>
                    {output.moderation_classifications?.length
                      ? output.moderation_classifications.join(" · ")
                      : "请按业务内容政策决定是否展示"}
                  </small>
                </span>
              </div>
            )}
            <div className="playground-media-frame">
              {kind === "video" ? (
                <video src={src} controls playsInline preload="metadata" />
              ) : kind === "audio" ? (
                <audio src={src} controls preload="metadata" />
              ) : (
                <img src={src} alt={`生成结果 ${index + 1}`} />
              )}
            </div>
            <figcaption>
              <span>
                <strong>
                  {label} {index + 1}
                </strong>
                <small>
                  {output.width && output.height
                    ? `${output.width} × ${output.height}`
                    : "生成结果"}
                  {output.duration ? ` · ${output.duration} 秒` : ""}
                </small>
              </span>
              {src && (
                <a
                  href={src}
                  download={`leonardo-${kind}-${index + 1}.${extension}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  <Download />
                  下载
                </a>
              )}
            </figcaption>
          </figure>
        );
      })}
    </div>
  );
}

function PlaygroundResult({
  kind,
  imageFormat = "jpeg",
  pending,
  restoring = false,
  error,
  outputs,
  task,
  cachedAt,
  onClear,
  onCancel,
}: {
  kind: MediaKind;
  imageFormat?: "png" | "jpeg";
  pending: boolean;
  restoring?: boolean;
  error?: string;
  outputs?: PlaygroundOutput[];
  task?: PlaygroundTask;
  cachedAt?: number;
  onClear: () => void;
  onCancel?: () => void;
}) {
  const [viewMode, setViewMode] = useState<"fit" | "fill">("fit");
  const hasMedia = Boolean(outputs?.length);
  const succeeded = kind === "image" ? hasMedia : task?.status === "succeeded";
  const activeTask = Boolean(task && !terminalStatuses.includes(task.status));
  return (
    <section className="playground-preview">
      <div className="playground-preview-heading">
        <div>
          <span className="eyebrow">Live Result</span>
          <h2>生成结果</h2>
        </div>
        <div className="playground-preview-actions">
          {cachedAt && (
            <span
              className="playground-cache-badge"
              title={`缓存于 ${new Date(cachedAt).toLocaleString("zh-CN")}`}
            >
              <Check />
              最近结果
            </span>
          )}
          {hasMedia && kind !== "audio" && (
            <div className="segmented compact" aria-label="媒体显示方式">
              <button
                className={viewMode === "fit" ? "active" : ""}
                onClick={() => setViewMode("fit")}
              >
                适应
              </button>
              <button
                className={viewMode === "fill" ? "active" : ""}
                onClick={() => setViewMode("fill")}
              >
                铺满
              </button>
            </div>
          )}
          {(hasMedia || task) && (
            <button
              className="icon"
              onClick={onClear}
              title="清空结果和对应缓存"
              aria-label="清空结果"
            >
              <Square />
            </button>
          )}
        </div>
      </div>
      {task && (
        <div className="playground-status">
          <span>
            <i className={`status ${task.status}`}></i>
            {statusText(task.status)}
          </span>
          {task.status !== "processing" && (
            <>
              <strong>{task.progress || 0}%</strong>
              <div>
                <i style={{ width: `${task.progress || 0}%` }}></i>
              </div>
            </>
          )}
          <code>{task.id}</code>
        </div>
      )}
      <div className={`playground-stage ${hasMedia ? "has-media" : ""}`}>
        {restoring && !pending && (
          <div className="playground-empty loading">
            <RefreshCw className="spin" />
            <strong>正在恢复最近结果</strong>
            <span>媒体缓存在当前浏览器中</span>
          </div>
        )}
        {pending && (
          <div className="playground-empty loading">
            <RefreshCw className="spin" />
            <strong>
              {kind === "image"
                ? "正在等待图像生成"
                : kind === "video"
                  ? "正在提交视频任务"
                  : "正在提交音频任务"}
            </strong>
            <span>请保持页面打开</span>
          </div>
        )}
        {!restoring && !pending && !error && !hasMedia && !task && (
          <div className="playground-empty">
            <FlaskConical />
            <strong>结果将在这里显示</strong>
            <span>配置参数并提交一次真实测试</span>
          </div>
        )}
        {error && (
          <div className="playground-empty error-state">
            <AlertTriangle />
            <strong>请求失败</strong>
            <span>{error}</span>
          </div>
        )}
        {task?.error_message && (
          <div className="playground-empty error-state">
            <AlertTriangle />
            <strong>{statusText(task.status)}</strong>
            <span>{task.error_message}</span>
            <code>
              {[task.error_code, task.error_details?.provider_error_code]
                .filter(Boolean)
                .join(" · ")}
            </code>
            {task.generation_id && (
              <small>Generation ID：{task.generation_id}</small>
            )}
          </div>
        )}
        {succeeded && hasMedia && (
          <MediaGallery
            kind={kind}
            imageFormat={imageFormat}
            outputs={outputs || []}
            viewMode={viewMode}
          />
        )}{" "}
        {succeeded && !hasMedia && (
          <div className="playground-empty error-state">
            <AlertTriangle />
            <strong>任务完成但没有媒体结果</strong>
            <span>上游渠道未返回可展示的文件地址。</span>
          </div>
        )}
        {activeTask && !pending && (
          <div className="playground-progress-note">
            <RefreshCw className="spin" />
            <span>
              每 3 秒自动更新，完成后会显示{kind === "video" ? "视频" : "音频"}
              。
            </span>
            {onCancel && (
              <button className="secondary" onClick={onCancel}>
                <Square />
                取消排队
              </button>
            )}
          </div>
        )}
      </div>
    </section>
  );
}
