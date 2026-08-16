import { useState } from "react";
import {
  AlertTriangle,
  Check,
  Download,
  FlaskConical,
  RefreshCw,
  Square,
} from "lucide-react";
import type { MediaKind, PlaygroundOutput, PlaygroundTask } from "../shared/types";
import { statusText } from "../shared/status";

const terminalStatuses = ["succeeded", "failed", "cancelled"];

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
    <div className={`playground-result-media ${single ? "single" : "multiple"} ${viewMode} ${kind}`}>
      {outputs.map((output, index) => {
        const src = output.url || (output.b64_json ? `data:image/${imageFormat};base64,${output.b64_json}` : "");
        const extension = kind === "video" ? "mp4" : kind === "audio" ? "mp3" : imageFormat;
        const label = kind === "video" ? "视频" : kind === "audio" ? "音频" : "图像";
        return (
          <figure key={output.id || index}>
            {output.nsfw && (
              <div className="playground-content-warning">
                <AlertTriangle />
                <span>
                  <strong>上游标记为显式内容</strong>
                  <small>{output.moderation_classifications?.length ? output.moderation_classifications.join(" · ") : "请按业务内容政策决定是否展示"}</small>
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
                <strong>{label} {index + 1}</strong>
                <small>
                  {output.width && output.height ? `${output.width} × ${output.height}` : "生成结果"}
                  {output.duration ? ` · ${output.duration} 秒` : ""}
                </small>
              </span>
              {src && (
                <a href={src} download={`aiv2api-${kind}-${index + 1}.${extension}`} target="_blank" rel="noreferrer">
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

export function PlaygroundResult({
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
  const succeeded = task ? task.status === "succeeded" : hasMedia;
  const activeTask = Boolean(task && !terminalStatuses.includes(task.status));
  return (
    <section className="playground-preview">
      <div className="playground-preview-heading">
        <div><span className="eyebrow">Live Result</span><h2>生成结果</h2></div>
        <div className="playground-preview-actions">
          {cachedAt && <span className="playground-cache-badge" title={`缓存于 ${new Date(cachedAt).toLocaleString("zh-CN")}`}><Check />最近结果</span>}
          {hasMedia && kind !== "audio" && (
            <div className="segmented compact" aria-label="媒体显示方式">
              <button className={viewMode === "fit" ? "active" : ""} onClick={() => setViewMode("fit")}>适应</button>
              <button className={viewMode === "fill" ? "active" : ""} onClick={() => setViewMode("fill")}>铺满</button>
            </div>
          )}
          {(hasMedia || task) && <button className="icon" onClick={onClear} title="清空结果和对应缓存" aria-label="清空结果"><Square /></button>}
        </div>
      </div>
      {task && (
        <div className="playground-status">
          <span><i className={`status ${task.status}`}></i>{statusText(task.status)}</span>
          {task.status !== "processing" && <><strong>{task.progress || 0}%</strong><div><i style={{ width: `${task.progress || 0}%` }}></i></div></>}
          <code>{task.id}</code>
        </div>
      )}
      <div className={`playground-stage ${hasMedia ? "has-media" : ""}`}>
        {restoring && !pending && <div className="playground-empty loading"><RefreshCw className="spin" /><strong>正在恢复最近结果</strong><span>媒体缓存在当前浏览器中</span></div>}
        {pending && <div className="playground-empty loading"><RefreshCw className="spin" /><strong>{kind === "image" ? "正在提交图像任务" : kind === "video" ? "正在提交视频任务" : "正在提交音频任务"}</strong><span>请保持页面打开</span></div>}
        {!restoring && !pending && !error && !hasMedia && !task && <div className="playground-empty"><FlaskConical /><strong>结果将在这里显示</strong><span>配置参数并提交一次真实测试</span></div>}
        {error && <div className="playground-empty error-state"><AlertTriangle /><strong>请求失败</strong><span>{error}</span></div>}
        {task?.error_message && (
          <div className="playground-empty error-state">
            <AlertTriangle /><strong>{statusText(task.status)}</strong><span>{task.error_message}</span>
            <code>{[task.error_code, task.error_details?.provider_error_code].filter(Boolean).join(" · ")}</code>
            {task.generation_id && <small>Generation ID：{task.generation_id}</small>}
          </div>
        )}
        {succeeded && hasMedia && <MediaGallery kind={kind} imageFormat={imageFormat} outputs={outputs || []} viewMode={viewMode} />}
        {succeeded && !hasMedia && <div className="playground-empty error-state"><AlertTriangle /><strong>任务完成但没有媒体结果</strong><span>上游渠道未返回可展示的文件地址。</span></div>}
        {activeTask && !pending && (
          <div className="playground-progress-note">
            <RefreshCw className="spin" />
            <span>每 3 秒自动更新，完成后会显示{kind === "image" ? "图像" : kind === "video" ? "视频" : "音频"}。</span>
            {onCancel && <button className="secondary" onClick={onCancel}><Square />取消排队</button>}
          </div>
        )}
      </div>
    </section>
  );
}
