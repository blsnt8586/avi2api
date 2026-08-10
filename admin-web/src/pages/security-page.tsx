import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import {
  Activity,
  Check,
  ChevronRight,
  Gauge,
  KeyRound,
  RefreshCw,
  ScrollText,
  Settings2,
  ShieldCheck,
} from "lucide-react";
import type { SystemCapacityResponse } from "../shared/types";
import { api } from "../shared/api";

const passwordFormSchema = z.object({
  current_password: z.string().min(1, "请输入当前密码"),
  new_password: z.string().min(8, "新密码至少 8 个字符"),
  confirmation: z.string().min(8, "请再次输入新密码"),
}).refine((values) => values.new_password === values.confirmation, {
  path: ["confirmation"],
  message: "两次输入的新密码不一致",
});

type PasswordForm = z.infer<typeof passwordFormSchema>;

export function Security() {
  const navigate = useNavigate();
  const settings = useQuery({
    queryKey: ["admin-settings"],
    queryFn: () => api<Record<string, unknown>>("/admin/api/settings"),
  });
  const capacity = useQuery({
    queryKey: ["system-capacity"],
    queryFn: () => api<SystemCapacityResponse>("/admin/api/system-capacity"),
  });
  const [message, setMessage] = useState("");
  const passwordForm = useForm<PasswordForm>({
    resolver: zodResolver(passwordFormSchema),
    mode: "onChange",
    defaultValues: { current_password: "", new_password: "", confirmation: "" },
  });
  const mutation = useMutation({
    mutationFn: (values: PasswordForm) =>
      api<{ ok: boolean; reauthenticate: boolean }>(
        "/admin/api/settings/password",
        {
          method: "PUT",
          body: JSON.stringify({
            current_password: values.current_password,
            new_password: values.new_password,
          }),
        },
      ),
    onSuccess: () => {
      setMessage("密码已更新，正在返回登录页…");
      window.setTimeout(() => window.location.reload(), 900);
    },
  });
  return (
    <section className="settings-workspace">
      <div className="settings-summary-grid">
        <section><Settings2 /><span><small>Schema</small><strong>{String(settings.data?.schema_version || "读取中")}</strong></span></section>
        <section><Activity /><span><small>全局执行</small><strong>{capacity.data?.capacity ? `${capacity.data.capacity.executing}/${capacity.data.capacity.effective_execution_limit}` : "读取中"}</strong></span></section>
        <section><ScrollText /><span><small>等待队列</small><strong>{capacity.data?.capacity ? `${capacity.data.capacity.queued}/${capacity.data.capacity.max_queued}` : "读取中"}</strong></span></section>
        <section><ShieldCheck /><span><small>会话安全</small><strong>HTTP-only Cookie</strong></span></section>
      </div>
      <div className="settings-sections">
        <button onClick={() => navigate("/capacity")}><Gauge /><span><strong>容量与准入策略</strong><small>并发、队列、水位和维护模式</small></span><ChevronRight /></button>
        <button onClick={() => navigate("/accounts?status=attention")}><RefreshCw /><span><strong>会话刷新状态</strong><small>查看等待恢复、429 和自动登录账号</small></span><ChevronRight /></button>
        <button onClick={() => navigate("/keys")}><KeyRound /><span><strong>API Key 限制</strong><small>并发、模型权限和凭据状态</small></span><ChevronRight /></button>
      </div>
      <div className="security-layout">
      <form
        className="ops-panel password-form"
        onSubmit={passwordForm.handleSubmit((values) => mutation.mutate(values))}
      >
        <div className="panel-heading">
          <div>
            <span className="eyebrow">Admin Credential</span>
            <h2>修改登录密码</h2>
          </div>
          <ShieldCheck />
        </div>
        <p className="security-note">
          新密码至少 8 个字符。更新成功后，所有管理后台会话都会退出。
        </p>
        <label>
          当前密码
          <input
            type="password"
            autoComplete="current-password"
            {...passwordForm.register("current_password")}
          />
          {passwordForm.formState.errors.current_password && <small className="field-error">{passwordForm.formState.errors.current_password.message}</small>}
        </label>
        <label>
          新密码
          <input
            type="password"
            autoComplete="new-password"
            {...passwordForm.register("new_password")}
          />
          {passwordForm.formState.errors.new_password && <small className="field-error">{passwordForm.formState.errors.new_password.message}</small>}
        </label>
        <label>
          确认新密码
          <input
            type="password"
            autoComplete="new-password"
            {...passwordForm.register("confirmation")}
          />
          {passwordForm.formState.errors.confirmation && <small className="field-error">{passwordForm.formState.errors.confirmation.message}</small>}
        </label>
        {mutation.error && <p className="error">{mutation.error.message}</p>}
        {message && (
          <p className="success-message">
            <Check />
            {message}
          </p>
        )}
        <button
          disabled={mutation.isPending || !passwordForm.formState.isValid}
        >
          {mutation.isPending ? "正在更新" : "更新密码"}
        </button>
      </form>
      <div className="security-guidance">
        <span className="eyebrow">Security Policy</span>
        <h2>凭据存储</h2>
        <p>新密码使用独立随机盐和 Argon2id 哈希保存，数据库中不存储明文。</p>
        <div>
          <strong>修改后</strong>
          <span>旧密码立即失效</span>
        </div>
        <div>
          <strong>会话</strong>
          <span>全部强制重新登录</span>
        </div>
        <div>
          <strong>审计</strong>
          <span>记录修改时间，不记录密码内容</span>
        </div>
      </div>
      </div>
    </section>
  );
}
