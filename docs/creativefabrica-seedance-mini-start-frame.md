# Creative Fabrica Seedance 2.0 Mini 首帧测试

日期：2026-09-14

## 请求

- AIV2API：`POST /v1/videos/generations`
- Content-Type：`multipart/form-data`
- 模型：`creativefabrica/seedance_v2_mini`
- 文件字段：`start_frame`
- 输入：此前 Z Image Spicy 生成的蓝色杯子 PNG
- 参数：4 秒、480p、864x496
- 官方请求前报价：2440 Coins
- AIV2API 任务：`874d802e-e1cc-4089-9186-cd175d6f6a4d`

## 结果

- 状态：succeeded
- 输出：MP4，4 秒，864x496
- 结果资源 HEAD：HTTP 200，`video/mp4`
- 结论：multipart 解析、临时素材存储、首帧上传、异步上游提交、
  轮询及结果返回链路可用。

## 测试中发现并修复的问题

首帧上传成功后，任务状态错误地从 `submitted` 回写到 `uploading`。
当执行租约恢复时，`isSubmittedGenerationTask` 只识别 submitted/polling，
因此任务被当作未提交并重复执行 InitiateSession。

本次一共出现三次上游提交。账号余额从 9080 降到 1760，
差值 7320，等于 3 × 2440。AIV2API 本地任务仅结算一次，
随后通过官方余额刷新校准为 1760。

修复：

1. 素材上传成功后的任务状态保持 `submitted`。
2. `uploading + generation_id` 也视作已创建上游任务，只恢复轮询，
   不再重新提交 mutation。
3. 增加回归测试，确认 pre-submission uploading 仍可首次提交，
   而带 generation ID 的 uploading 禁止重新提交。
4. 全量 Go 测试通过，生产重新构建，`healthz` 与 `readyz` 通过。

本轮没有为了验证修复再次发起付费生成。临时计价规则和测试 API Key 已停用。
