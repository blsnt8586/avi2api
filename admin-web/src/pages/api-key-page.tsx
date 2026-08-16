import React, { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Ban,
  Check,
  CirclePlay,
  KeyRound,
  Plus,
  ScrollText,
  Search,
  ShieldCheck,
  Trash2,
  X,
} from "lucide-react";
import type { APIKeyRecord, APIKeysPage, Provider } from "../shared/types";
import { Metric } from "../components/metric";
import { Pagination as UIPagination, QueryStatus, TableSkeleton } from "../components/ui";
import { api } from "../shared/api";
import { providerDisplayName, providerModelGroups } from "../shared/providers";

const permissionID = (providerID: string, model: string) => `${providerID}:${model}`;
function permissionLabel(permission: string, providers: Provider[]) {
  const separator = permission.indexOf(":");
  if (separator < 0) return permission;
  const provider = permission.slice(0, separator);
  const model = permission.slice(separator + 1);
	return `${providerDisplayName(provider, providers)} · ${model}`;
}

type KeyModelGroup = ReturnType<typeof providerModelGroups>[number];

export function Keys({ setKey }: { setKey: (v: string) => void }) {
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [search, setSearch] = useState("");
  const [showCreate, setShowCreate] = useState(false);
	const [deleteTarget, setDeleteTarget] = useState<APIKeyRecord | null>(null);
	const providers = useQuery({
		queryKey: ["providers"],
		queryFn: () => api<Provider[]>("/admin/api/providers"),
	});
	const keyModelGroups = providerModelGroups(providers.data || []);
  const keys = useQuery({
    queryKey: ["api-keys-page", page, pageSize, search],
    queryFn: () =>
      api<APIKeysPage>(
        `/admin/api/api-keys?page=${page}&page_size=${pageSize}&search=${encodeURIComponent(search.trim())}`,
      ),
    refetchInterval: false,
  });
  const refresh = () => {
    client.invalidateQueries({ queryKey: ["keys"] });
    client.invalidateQueries({ queryKey: ["api-keys-page"] });
  };
  const toggle = useMutation({
    mutationFn: (k: APIKeyRecord) =>
      api(`/admin/api/api-keys/${k.id}`, {
        method: "PATCH",
        body: JSON.stringify({ enabled: !k.enabled }),
      }),
    onSuccess: refresh,
  });
  const data = keys.data?.data || [];
  const total = keys.data?.total || 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  React.useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);
  return (
    <section className="keys-workspace">
      <div className="stats key-summary-stats">
        <Metric icon={<KeyRound />} label="当前页凭据" value={data.length} detail={`全部 ${total} 个`} />
        <Metric icon={<ShieldCheck />} label="已启用" value={data.filter((key) => key.enabled && (!key.expires_at || new Date(key.expires_at) > new Date())).length} />
        <Metric icon={<Activity />} label="并发额度" value={data.reduce((sum, key) => sum + key.concurrency_limit, 0)} detail="当前页合计" />
        <Metric icon={<ScrollText />} label="累计请求" value={data.reduce((sum, key) => sum + key.request_count, 0).toLocaleString()} detail="当前页合计" />
      </div>
      <div className="keys-toolbar">
        <div>
          <span className="eyebrow">Credentials</span>
          <h2>已创建密钥</h2>
          <p>按名称、用途或密钥前缀查找凭据。</p>
        </div>
        <div className="keys-toolbar-actions">
          <QueryStatus fetching={keys.isFetching} error={keys.error} updatedAt={keys.dataUpdatedAt} />
          <label className="keys-search">
            <Search />
            <input
              aria-label="搜索 API 密钥"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
              }}
              placeholder="搜索密钥"
            />
          </label>
          <button onClick={() => setShowCreate(true)}>
            <Plus />
            创建密钥
          </button>
        </div>
      </div>
      {keys.error && <p className="error">{keys.error.message}</p>}
      <div className="table key-table">
        <div className="row head">
          <span>密钥 / 用途</span>
          <span>模型权限</span>
          <span>用量 / 并发</span>
          <span>状态</span>
          <span>操作</span>
        </div>
        {keys.isLoading && <TableSkeleton rows={5} columns={5} />}
        {data.map((k) => {
          const expired = Boolean(
            k.expires_at && new Date(k.expires_at) <= new Date(),
          );
          return (
            <div className="row" key={k.id}>
              <span>
                <strong>{k.name}</strong>
                <small>
                  {k.prefix}… · {k.description || "未填写用途"}
                </small>
              </span>
              <span>
                <strong>{k.allowed_models?.length || 0} 个模型</strong>
                <small>
				  {(k.allowed_models || []).slice(0, 2).map((permission) => permissionLabel(permission, providers.data || [])).join("、")}
                  {k.allowed_models?.length > 2 ? "…" : ""}
                </small>
              </span>
              <span>
                <strong>{k.request_count.toLocaleString()} 次</strong>
                <small>并发 {k.concurrency_limit}</small>
              </span>
              <span>
                <i
                  className={`status ${expired ? "invalid" : k.enabled ? "active" : "disabled"}`}
                ></i>
                {expired ? "已过期" : k.enabled ? "已启用" : "已禁用"}
                <small>
                  {k.expires_at
                    ? `${new Date(k.expires_at).toLocaleDateString("zh-CN")} 到期`
                    : "永不过期"}
                </small>
              </span>
              <span className="key-actions">
                <button
                  className="icon"
                  title={k.enabled ? "禁用密钥" : "启用密钥"}
                  disabled={expired || toggle.isPending}
                  onClick={() => toggle.mutate(k)}
                >
                  {k.enabled ? <Ban size={17} /> : <CirclePlay size={17} />}
                </button>
                <button
                  className="icon danger-icon"
                  title="删除密钥"
                  onClick={() => setDeleteTarget(k)}
                >
                  <Trash2 size={17} />
                </button>
              </span>
            </div>
          );
        })}
      </div>
      {!keys.isLoading && data.length === 0 && (
        <div className="empty-state compact-empty">
          <KeyRound />
          <strong>{search ? "没有匹配的密钥" : "还没有 API 密钥"}</strong>
          <span>
            {search
              ? "调整搜索内容后重试。"
              : "点击“创建密钥”配置第一个客户端凭据。"}
          </span>
        </div>
      )}
      <UIPagination
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        busy={keys.isFetching}
        onPage={setPage}
        onPageSize={(size) => {
          setPageSize(size);
          setPage(1);
        }}
      />
      {showCreate && (
		<CreateKeyDialog
		  groups={keyModelGroups}
          close={() => setShowCreate(false)}
          done={(key) => {
            setShowCreate(false);
            setKey(key);
            setPage(1);
            refresh();
          }}
        />
      )}
      {deleteTarget && (
        <DeleteKeyDialog
          value={deleteTarget}
          close={() => setDeleteTarget(null)}
          done={() => {
            setDeleteTarget(null);
            refresh();
          }}
        />
      )}
    </section>
  );
}

function CreateKeyDialog({
	groups,
	close,
  done,
}: {
	groups: KeyModelGroup[];
	close: () => void;
  done: (key: string) => void;
}) {
	const allKeyModels = groups.flatMap((group) => group.models.map((model) => permissionID(group.provider_id, model)));
	const [form, setForm] = useState({
    name: "",
    description: "",
    concurrency_limit: 20,
    expires_in_days: 0,
    allowed_models: [...allKeyModels],
  });
  const m = useMutation({
    mutationFn: () =>
      api<{ key: string }>("/admin/api/api-keys", {
        method: "POST",
        body: JSON.stringify(form),
      }),
    onSuccess: (r) => done(r.key),
  });
  function toggleModel(model: string) {
    setForm((current) => ({
      ...current,
      allowed_models: current.allowed_models.includes(model)
        ? current.allowed_models.filter((value) => value !== model)
        : [...current.allowed_models, model],
    }));
  }
  return (
    <div
      className="overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <form
        className="dialog key-dialog"
        onSubmit={(e) => {
          e.preventDefault();
          m.mutate();
        }}
      >
        <header className="dialog-heading">
          <div className="key-create-intro">
            <span className="key-create-icon">
              <KeyRound />
            </span>
            <div>
              <span className="eyebrow">New Credential</span>
              <h2>创建 API 密钥</h2>
              <p>填写用途和权限，确认后才会创建。完整密钥只显示一次。</p>
            </div>
          </div>
          <button
            type="button"
            className="icon"
            aria-label="关闭"
            onClick={close}
          >
            <X />
          </button>
        </header>
        <div className="key-create-fields">
          <label>
            名称
            <input
              autoFocus
              required
              maxLength={80}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="例如：生产环境"
            />
          </label>
          <label>
            用途说明
            <input
              maxLength={240}
              value={form.description}
              onChange={(e) =>
                setForm({ ...form, description: e.target.value })
              }
              placeholder="例如：内容生产服务"
            />
          </label>
          <label>
            并发上限
            <input
              type="number"
              min="1"
              max="1000"
              value={form.concurrency_limit}
              onChange={(e) =>
                setForm({ ...form, concurrency_limit: Number(e.target.value) })
              }
            />
            <small>限制同时执行数量；超出的已预留任务继续排队。</small>
          </label>
          <label>
            有效期
            <select
              value={form.expires_in_days}
              onChange={(e) =>
                setForm({ ...form, expires_in_days: Number(e.target.value) })
              }
            >
              <option value="0">永不过期</option>
              <option value="7">7 天</option>
              <option value="30">30 天</option>
              <option value="90">90 天</option>
              <option value="365">1 年</option>
            </select>
          </label>
        </div>
        <div className="key-model-access">
          <div>
            <strong>允许模型</strong>
            <span>
              已选择 {form.allowed_models.length} / {allKeyModels.length}
            </span>
          </div>
		  {groups.map((group) => (
            <fieldset key={group.label}>
              <legend>{group.label}</legend>
              <div>
                {group.models.map((model) => (
                  <label className="check-chip" key={permissionID(group.provider_id, model)}>
                    <input
                      type="checkbox"
                      checked={form.allowed_models.includes(permissionID(group.provider_id, model))}
                      onChange={() => toggleModel(permissionID(group.provider_id, model))}
                    />
                    <span>
                      <Check />
					  {model}
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>
          ))}
        </div>
        {m.error && <p className="error">{m.error.message}</p>}
        <footer>
          <span className="key-security-note">
            <ShieldCheck />
            密钥以 SHA-256 哈希保存
          </span>
          <div>
            <button type="button" className="secondary" onClick={close}>
              取消
            </button>
            <button
              disabled={
                m.isPending ||
                !form.name.trim() ||
                form.allowed_models.length === 0
              }
            >
              <Plus />
              {m.isPending ? "正在创建" : "确认创建"}
            </button>
          </div>
        </footer>
      </form>
    </div>
  );
}

function DeleteKeyDialog({
  value,
  close,
  done,
}: {
  value: APIKeyRecord;
  close: () => void;
  done: () => void;
}) {
  const m = useMutation({
    mutationFn: () =>
      api(`/admin/api/api-keys/${value.id}`, { method: "DELETE" }),
    onSuccess: done,
  });
  return (
    <div
      className="overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <section
        className="dialog delete-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="delete-key-title"
      >
        <span className="delete-dialog-icon">
          <Trash2 />
        </span>
        <h2 id="delete-key-title">删除 API 密钥？</h2>
        <p>
          <strong>{value.name}</strong>（{value.prefix}
          …）将立即失效，已产生的任务、用量和审计记录会保留。
        </p>
        {m.error && <p className="error">{m.error.message}</p>}
        <footer>
          <button className="secondary" onClick={close}>
            取消
          </button>
          <button
            className="danger-button"
            disabled={m.isPending}
            onClick={() => m.mutate()}
          >
            <Trash2 />
            {m.isPending ? "正在删除" : "删除密钥"}
          </button>
        </footer>
      </section>
    </div>
  );
}
