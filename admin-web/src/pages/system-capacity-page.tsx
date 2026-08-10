import React, { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, Check, Gauge, RefreshCw, ScrollText, Server } from "lucide-react";
import { Metric } from "../components/metric";
import type { SystemCapacity, SystemCapacityResponse } from "../shared/types";
import { api } from "../shared/api";
import { formatCapacityWait } from "../shared/status";

type SystemCapacityForm = Pick<
  SystemCapacity,
  | "max_executing"
  | "max_queued"
  | "queue_high_watermark"
  | "queue_resume_watermark"
  | "queue_timeout_seconds"
  | "maintenance_mode"
  | "execution_paused"
>;

export function SystemCapacityPanel() {
  const client = useQueryClient();
  const capacity = useQuery({
    queryKey: ["system-capacity"],
    queryFn: () => api<SystemCapacityResponse>("/admin/api/system-capacity"),
    refetchInterval: 3000,
  });
  const [form, setForm] = useState<SystemCapacityForm | null>(null);
  const revision = capacity.data?.capacity?.revision;
  React.useEffect(() => {
    const current = capacity.data?.capacity;
    if (!current) return;
    setForm({
      max_executing: current.max_executing,
      max_queued: current.max_queued,
      queue_high_watermark: current.queue_high_watermark,
      queue_resume_watermark: current.queue_resume_watermark,
      queue_timeout_seconds: current.queue_timeout_seconds,
      maintenance_mode: current.maintenance_mode,
      execution_paused: current.execution_paused,
    });
  }, [revision]);
  const save = useMutation({
    mutationFn: () =>
      api<{ capacity: SystemCapacity }>("/admin/api/system-capacity", {
        method: "PUT",
        body: JSON.stringify(form),
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["system-capacity"] });
      void client.invalidateQueries({ queryKey: ["audit-page"] });
    },
  });
  if (capacity.isPending) {
    return <div className="capacity-loading"><RefreshCw className="spin" />正在读取系统容量</div>;
  }
  if (capacity.isError) {
    return <p className="error">系统容量读取失败：{capacity.error.message}</p>;
  }
  const current = capacity.data?.capacity;
  const runtime = capacity.data?.runtime;
  if (!current || !runtime || !form) {
    return <p className="error">系统容量接口返回数据不完整。</p>;
  }
  const mode = current.execution_paused
    ? ["执行暂停", "paused"]
    : current.maintenance_mode
      ? ["维护排空", "maintenance"]
      : current.overload_active
        ? ["过载保护", "overload"]
        : ["正常接单", "normal"];
  const valid =
    form.max_executing >= 1 &&
    form.max_queued >= 1 &&
    form.queue_high_watermark >= 1 &&
    form.queue_high_watermark <= form.max_queued &&
    form.queue_resume_watermark >= 0 &&
    form.queue_resume_watermark < form.queue_high_watermark &&
    form.queue_timeout_seconds >= 60 &&
    (!form.execution_paused || form.maintenance_mode);
  const persistedForm: SystemCapacityForm = {
    max_executing: current.max_executing,
    max_queued: current.max_queued,
    queue_high_watermark: current.queue_high_watermark,
    queue_resume_watermark: current.queue_resume_watermark,
    queue_timeout_seconds: current.queue_timeout_seconds,
    maintenance_mode: current.maintenance_mode,
    execution_paused: current.execution_paused,
  };
  const dirty = JSON.stringify(form) !== JSON.stringify(persistedForm);
  return (
    <section className="capacity-workspace">
      <div className="capacity-summary">
        <Metric
          icon={<Activity />}
          label="系统执行"
          value={`${current.executing}/${current.effective_execution_limit}`}
          detail={`硬上限 ${current.max_executing} · 剩余 ${current.execution_headroom}`}
          tone={current.execution_headroom === 0 ? "warning" : "normal"}
        />
        <Metric
          icon={<ScrollText />}
          label="等待队列"
          value={`${current.queued}/${current.max_queued}`}
          detail={`高水位 ${current.queue_high_watermark} · 剩余 ${current.queue_headroom}`}
          tone={current.queued >= current.queue_high_watermark ? "warning" : "normal"}
        />
        <Metric
          icon={<Server />}
          label="可用账号槽位"
          value={current.eligible_execution_slots}
          detail={`${current.eligible_accounts} 个账号 · 队列 ${current.eligible_queue_slots}`}
        />
        <Metric
          icon={<Gauge />}
          label="最老等待"
          value={formatCapacityWait(current.oldest_queued_seconds)}
          detail={`模式：${mode[0]}`}
          tone={current.oldest_queued_seconds > 300 ? "warning" : "normal"}
        />
      </div>

      <div className="capacity-columns">
        <section className="capacity-section">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Live Capacity</span>
              <h2>实时容量</h2>
            </div>
            <span className={`capacity-mode ${mode[1]}`}>{mode[0]}</span>
          </div>
          <div className="capacity-bars">
            <CapacityLine label="图片执行" value={current.executing_images} max={current.max_executing} />
            <CapacityLine label="视频执行" value={current.executing_videos} max={current.max_executing} />
            <CapacityLine label="音频执行" value={current.executing_audio} max={current.max_executing} />
            <CapacityLine label="等待队列" value={current.queued} max={current.max_queued} warning={current.queue_high_watermark} />
          </div>
          <div className="capacity-runtime">
            <span><small>Dispatcher</small><strong>{runtime.task_dispatcher_concurrency}</strong></span>
            <span><small>数据库连接</small><strong>{runtime.database_max_connections}</strong></span>
            <span><small>HTTP 准入</small><strong>{runtime.gateway_inflight}/{runtime.gateway_inflight_limit}</strong></span>
            <span><small>上传槽位</small><strong>{runtime.multipart_inflight}/{runtime.multipart_inflight_limit}</strong></span>
            <span><small>同步槽位</small><strong>{runtime.sync_inflight}/{runtime.sync_inflight_limit}</strong></span>
            <span><small>Key 队列倍数</small><strong>{runtime.api_key_queue_multiplier}</strong></span>
          </div>
        </section>

        <section className="capacity-section capacity-policy">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Admission Policy</span>
              <h2>容量策略</h2>
            </div>
            <span className="capacity-revision">版本 {current.revision}</span>
          </div>
          <div className="capacity-form-grid">
            <label>系统执行上限<input type="number" min={1} max={10000} value={form.max_executing} onChange={(e) => setForm({ ...form, max_executing: Number(e.target.value) })} /></label>
            <label>系统队列上限<input type="number" min={1} max={100000} value={form.max_queued} onChange={(e) => setForm({ ...form, max_queued: Number(e.target.value) })} /></label>
            <label>过载高水位<input type="number" min={1} max={form.max_queued} value={form.queue_high_watermark} onChange={(e) => setForm({ ...form, queue_high_watermark: Number(e.target.value) })} /></label>
            <label>恢复水位<input type="number" min={0} max={Math.max(0, form.queue_high_watermark - 1)} value={form.queue_resume_watermark} onChange={(e) => setForm({ ...form, queue_resume_watermark: Number(e.target.value) })} /></label>
            <label>队列超时（分钟）<input type="number" min={1} max={1440} value={Math.round(form.queue_timeout_seconds / 60)} onChange={(e) => setForm({ ...form, queue_timeout_seconds: Number(e.target.value) * 60 })} /></label>
          </div>
          <div className="capacity-toggles">
            <label>
              <input type="checkbox" checked={form.maintenance_mode} onChange={(e) => setForm({ ...form, maintenance_mode: e.target.checked, execution_paused: e.target.checked ? form.execution_paused : false })} />
              <span><strong>维护排空</strong><small>停止接收新任务，现有任务继续执行</small></span>
            </label>
            <label>
              <input type="checkbox" checked={form.execution_paused} onChange={(e) => setForm({ ...form, execution_paused: e.target.checked, maintenance_mode: e.target.checked || form.maintenance_mode })} />
              <span><strong>暂停新执行</strong><small>排队任务停止领取，已提交任务继续轮询</small></span>
            </label>
          </div>
          {save.error && <p className="error">{save.error.message}</p>}
          <div className="capacity-actions">
            <small>{dirty ? "有未保存的修改" : `更新于 ${new Date(current.updated_at).toLocaleString("zh-CN")}`}</small>
            <button disabled={!valid || !dirty || save.isPending} onClick={() => {
              const highImpact = form.maintenance_mode !== current.maintenance_mode || form.execution_paused !== current.execution_paused;
              if (highImpact && !window.confirm("此修改会影响新任务准入或队列执行，确认保存？")) return;
              save.mutate();
            }}>
              <Check />
              {save.isPending ? "保存中" : "保存容量策略"}
            </button>
          </div>
        </section>
      </div>
    </section>
  );
}

function CapacityLine({ label, value, max, warning }: { label: string; value: number; max: number; warning?: number }) {
  const ratio = max > 0 ? Math.min(100, (value / max) * 100) : 0;
  const warn = warning !== undefined ? value >= warning : ratio >= 90;
  return (
    <div className="capacity-line">
      <span>{label}</span>
      <div><i className={warn ? "warning" : ""} style={{ width: `${ratio}%` }}></i></div>
      <strong>{value}/{max}</strong>
    </div>
  );
}
