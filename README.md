# AIV2API

AIV2API is a multi-provider gateway for image, video, and audio generation. Leonardo AI is the first provider adapter; additional providers use isolated authentication, balance, pricing, submission, polling, and result adapters behind the same public API.

Go service that exposes curated Leonardo image, video, and audio generation through a media-specific asynchronous API. It uses PostgreSQL for durable state, Redis/Asynq for distributed execution, and a React administration UI embedded in the Go binary.

## Implemented

- Symmetric asynchronous image, video, and audio endpoints
- Leonardo session refresh, GraphQL generation, result polling, and balance refresh
- Real image upload flow: `UploadImage` GraphQL, presigned multipart upload, moderation polling
- Encrypted account Cookie and access-token storage with AES-256-GCM
- Multi-account scheduling with PostgreSQL-atomic credit reservations and execution leases
- Idempotent asynchronous tasks and conservative `submission_uncertain` handling
- Worker crash recovery without resubmitting requests that may already have reached Leonardo
- Fenced balance reconciliation and an append-only per-task API-key usage ledger
- Admin UI and APIs for accounts, tasks, API keys, models, and schema version
- Asynchronous text-to-video generation with a curated Leonardo model set
- Asynchronous speech, music, and sound-effect generation with three curated Leonardo audio models
- Prometheus metrics, structured logs, OpenTelemetry HTTP instrumentation, Compose, and Helm

## Quick Start

Requirements: Docker and Docker Compose.

```powershell
Copy-Item .env.example .env
$key = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Fill($key)
$masterKey = [Convert]::ToBase64String($key)
(Get-Content .env) -replace 'replace-with-base64-encoded-32-byte-key', $masterKey |
  Set-Content .env
```

Set a strong `LEO_ADMIN_PASSWORD` in `.env`, then start the stack:

```powershell
docker compose up -d --build
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). Add an account with an authenticated Leonardo Cookie/session. The production session scheduler reuses isolated browser profiles and encrypted sessions; account passwords are not stored by the Go service.

### Browser Login Capture

An optional local helper can log in through an isolated Edge profile and export a complete Chrome/Patchright Cookie JSON array:

```powershell
python -m venv .venv-login
.\.venv-login\Scripts\python.exe -m pip install -r requirements-login.txt
$env:LEONARDO_EMAIL = "account@example.com"
$env:LEONARDO_PASSWORD = "password"
.\.venv-login\Scripts\python.exe scripts\leonardo_login.py
```

Sensitive output is written to `artifacts/leonardo-login/`, which is ignored by Git. Use `cookies.json` when adding or recovering an account. It preserves the session token, `session_data` fragments, domain, path, expiry and security attributes required to restore a browser session. The helper also records a HAR file and a sanitized API summary for debugging. Its isolated browser profile is stored in `artifacts/leonardo-profile/` so a valid session can be reused without entering the password again. Add `--solve-captcha` only when a visible Cloudflare challenge appears.

The administration API stages a complete Cookie JSON upload and validates it through the browser worker before replacing an existing session. It does not accept an AT or a Cookie Header for new account creation or recovery.

For unattended runs, complete one headed login first, then reuse the authenticated profile in headless mode:

```powershell
.\.venv-login\Scripts\python.exe scripts\leonardo_login.py --headless
```

A brand-new headless profile may trigger Cloudflare before the email or password step. The click solver does not reliably handle Leonardo's closed-shadow-DOM Turnstile widget, so headless mode is intended for reusing an existing authenticated profile rather than bootstrapping a new session.

To bootstrap a Linux server from cookies exported on another machine:

```bash
.venv-login/bin/python scripts/leonardo_login.py \
  --headless \
  --channel chrome \
  --import-cookies /run/secrets/leonardo-cookies.json \
  --profile-dir /data/leonardo-profile \
  --output-dir /data/leonardo-login
```

Create an API key in the administration UI. The plaintext key is displayed once.

Interactive developer documentation is available without administrator credentials at `/docs`. The OpenAPI 3.1 contract is available at `/openapi.json`; enabled public pricing rules are exposed read-only at `/api-docs/cost-rules`. Exact image and video estimators are available at `POST /v1/images/estimate` and `POST /v1/videos/estimate`; both reuse task-admission validation and pricing without creating a task or reserving credits.

## API

Set the plaintext key shown once by the administration UI:

```bash
export AIV2API_API_KEY="leo_your_api_key"
```

Text-to-image is asynchronous:

```bash
curl http://127.0.0.1:8080/v1/images/generations \
  -H "Authorization: Bearer $AIV2API_API_KEY" \
  -H "Idempotency-Key: image-example-001" \
  -H "Content-Type: application/json" \
  -d '{"provider":"adobe","model":"gpt-image-2","prompt":"a red ceramic teapot on a white table","size":"1024x1024","quality":"low","n":1}'
```

The response is a queued task. Poll `GET /v1/images/{id}` until `status=succeeded`, then read `result.data[].url`. Image tasks accept `response_format=url`; Base64 delivery and local output transcoding are not part of the asynchronous contract. For `gpt-image-2`, `quality=auto|low|medium|high` is accepted and `auto` is intentionally normalized to `low`: Leonardo defaults this model to the much more expensive `MEDIUM` tier.

Select the upstream only when creating or estimating a task: `provider=leonardo|adobe`, defaulting to `leonardo`. Polling and cancellation use only the returned task ID. Public model names remain provider-neutral: Adobe image routes use `gpt-image-2` or `nano-banana-2` together with `provider=adobe`; provider-prefixed model aliases are rejected. Adobe accounts and BKS price rules are isolated from Leonardo. Adobe GPT Image 2 exposes 1024x1024, 2048x2048, and 2880x2880 with low/medium/high quality; Adobe Nano Banana 2 exposes the current Firefly `gemini-flash@nano-banana-3` 1K/2K/4K size tiers. Both Adobe image models are asynchronous, fixed to `n=1`, and accept up to six multipart reference images.

Image-to-image:

```bash
curl http://127.0.0.1:8080/v1/images/generations \
  -H "Authorization: Bearer $AIV2API_API_KEY" \
  -H "Idempotency-Key: image-edit-example-001" \
  -F "image[]=@product.png" \
  -F "image[]=@style-reference.png" \
  -F "prompt=turn this into a watercolor illustration" \
  -F "provider=adobe" \
  -F "model=gpt-image-2" \
  -F "size=1024x1024" \
  -F "quality=low" \
  -F "reference_strength=MID"
```

`POST /v1/images/generations` accepts JSON for text-only generation and multipart for reference-image generation. It accepts either `image` or repeated `image[]` fields; all four public models support up to six reference images. `reference_strength=LOW|MID|HIGH` defaults to `MID`. Uploaded references are stored as temporary task assets and removed at terminal state. Mask editing is not exposed because these Leonardo model schemas only publish image-reference guidance.

Text-to-video is asynchronous:

```bash
curl http://127.0.0.1:8080/v1/videos/generations \
  -H "Authorization: Bearer $AIV2API_API_KEY" \
  -H "Idempotency-Key: video-example-001" \
  -H "Content-Type: application/json" \
  -d '{"provider":"adobe","model":"seedance-2.0-fast","prompt":"a slow cinematic orbit around a glass sculpture","duration":4,"size":"1280x720","resolution":"720p"}'
```

The response is a queued task. Poll `GET /v1/videos/{id}` until `status=succeeded`; the result contains an MP4 URL. Adobe video routes use provider-neutral model IDs with `provider=adobe`: `kling-o3-omni`, `veo-3.1`, `veo-3.1-fast`, `seedance-2.0`, and `seedance-2.0-fast`. They use the same durable asynchronous task and reservation flow as every other video route. Veo supports 4/6/8 seconds, Kling supports 5/10/15 seconds, and Seedance supports 4–15 seconds. Current Adobe routes keep native audio disabled; reference frames and model-supported image/video/audio uploads remain asynchronous multipart inputs. Video quantity is fixed at one.

Video prompt limits are model-specific: Seedance and Grok Imagine 1.5 are 5,000 Unicode characters; Kling O3 Omni is 2,500; Veo 3.1/Fast are 9,999; MiniMax H3 is 2,000. `POST /v1/videos/estimate` includes native-audio and video-reference pricing modifiers. Kling O3 Omni costs 224, 280, or 420 credits per second at 720p, 1080p, or 2160p with native audio; a reference video costs 252 credits per input second and does not support 2160p. Grok dimensions map to fixed 480p, 720p, or 1080p price tiers at 100, 165, or 290 credits per second. Seedance and Grok accept `generate_audio=false`, but the current Leonardo schema price is unchanged by that flag.

Audio generation is asynchronous:

```bash
curl http://127.0.0.1:8080/v1/audio/generations \
  -H "Authorization: Bearer $AIV2API_API_KEY" \
  -H "Idempotency-Key: audio-example-001" \
  -H "Content-Type: application/json" \
  -d '{"provider":"leonardo","model":"sound-effects-v2","prompt":"rain falling on a metal roof","duration":6,"loop":true,"prompt_influence":0.7,"n":1}'
```

Poll `GET /v1/audio/{id}` until `status=succeeded`, then read `result.data[0].url`; cancel a queued task with `POST /v1/audio/{id}/cancel`. Curated models are `dialogue-v3` for text-to-speech, `music-v1` for music, and `sound-effects-v2` for sound effects. `dialogue-v3` accepts `voice`, `language`, and `prompt_influence`; `music-v1` accepts `duration_minutes=1..10` and `force_instrumental`; `sound-effects-v2` accepts `duration=1..22`, `loop`, and `prompt_influence`. Prompt limits are 5,000 Unicode characters for Dialogue and 9,999 for Music and Sound Effects. Quantity is `1..4`; every paid creation request requires `Idempotency-Key`.

Seedance video requests may use multipart fields `image[]`, `start_frame`, `end_frame`, `video[]`, `audio[]`, `reference_strength`, and `generate_audio`. Kling O3 Omni accepts up to seven images, one start frame with optional end frame, or one 3–10.05 second reference video plus up to four images; frame inputs are exclusive with image/video inputs, and reference-video duration must match the integer `duration` within 0.05 seconds. FLUX 3 Video accepts `start_frame` and optional `end_frame`, or one `video` continuation file up to 50 MB and 15.05 seconds; these modes are mutually exclusive. Seedance 2.0 models accept up to 4 images, 3 videos totaling 15 seconds, and one audio file up to 15 seconds. Veo 3.1 ordinary image references require `size=1280x720` and `duration=8`. Grok Imagine 1.5 accepts only `start_frame` plus generation parameters; its nine exact size/resolution pairs are documented at `/docs` and in `/openapi.json`. Video and audio references are streamed through temporary files under `LEO_TASK_ASSET_DIR`; defaults are 200 MiB per video and 50 MiB per audio file.

Asynchronous image task:

```bash
curl http://127.0.0.1:8080/v1/images/generations \
  -H "Authorization: Bearer $AIV2API_API_KEY" \
  -H "Idempotency-Key: example-001" \
  -H "Content-Type: application/json" \
  -d '{"model":"nano-banana-2","prompt":"architectural concept sketch"}'
```

Async image, video, and audio requests reserve their estimated credits at creation. Each account has an execution limit (`image_concurrency`) and a separate waiting limit (`queue_capacity`). The system also enforces a database-backed global execution limit, queue hard limit, overload high/resume watermarks, maintenance drain mode, and optional execution pause. With concurrency 5 and queue capacity 5, at most 5 tasks execute and 5 wait. A full account routes new work to another eligible account; creation returns `account_queue_full` only when every eligible account is full. Global protection returns `system_queue_full`, `system_overloaded`, or `system_maintenance` before creating a task. Cancelling a queued task releases its reservation immediately.

Chat compatibility uses the same durable image task path and may wait for its compatibility response budget. The public image endpoints themselves always return tasks immediately.

`POST /v1/images/generations` always returns URL-based task results. It accepts `response_format=url` only and rejects `b64_json`, `output_format`, and `output_compression`. Reference images use multipart `image`/`image[]` plus optional `reference_strength`. Public task responses omit provider/account IDs, credit ledger fields, upstream generation IDs, and raw request payloads.

Other endpoints:

- `GET /v1/models`
- `GET /v1/images/{id}`
- `POST /v1/images/{id}/cancel`
- `POST /v1/videos/generations`
- `GET /v1/videos/{id}`
- `POST /v1/videos/{id}/cancel`
- `POST /v1/audio/generations`
- `GET /v1/audio/{id}`
- `POST /v1/audio/{id}/cancel`
- `POST /v1/chat/completions`
- `GET /healthz`, `GET /readyz`, `GET /metrics`

Asynchronous image results are URL-only. Reference uploads are limited by `LEO_MAX_IMAGE_BYTES` (25 MiB by default) and accept PNG, JPEG, and WebP. Chat Completions accepts the same `provider` selector as image creation, accepts at most one Base64 PNG/JPEG/WebP data URL, and rejects remote image URLs.

## Configuration

See [.env.example](.env.example). Required values:

- `LEO_MASTER_KEY`: base64-encoded 32-byte key. The service refuses to start without it.
- `LEO_ADMIN_PASSWORD`: administrator password.
- `LEO_SESSION_SYNC_TOKEN`: random token used by the loopback-only session worker and browser-session importer. Keep it in the server environment; browser sync does not depend on the mutable administrator password.
- `LEO_DATABASE_URL` and `LEO_REDIS_ADDR`: PostgreSQL and Redis connections.
- `LEO_DATABASE_MAX_CONNS`: application PostgreSQL pool limit; default 64.
- `LEO_TASK_ASSET_DIR`: persistent temporary media directory shared by API and workers.
- `LEO_HISTORY_CLEANUP_INTERVAL` and `LEO_HISTORY_CLEANUP_BATCH`: indexed cleanup cadence and batch size for non-financial history.
- `LEO_TASK_EVENT_RETENTION`, `LEO_OUTBOX_RETENTION`, `LEO_SESSION_JOB_RETENTION`, `LEO_AUDIT_LOG_RETENTION`, and `LEO_RECONCILIATION_RETENTION`: history retention windows. Per-task reservations and API-key usage ledgers remain immutable.

The active Leonardo `x-leo-schema-version` is stored in PostgreSQL and can be changed through `PUT /admin/api/settings/schema-version` without restarting workers.

Session renewal is expiration-driven and designed for large account pools. The application scans once per minute, schedules accounts whose JWT has 15 minutes or less remaining with a deterministic 0–600 second account jitter, and uses 32 database-leased Cookie workers by default. A Cookie refresh that does not return at least 20 minutes of remaining JWT lifetime is handed to the separately deployed browser worker. Browser workers use `FOR UPDATE SKIP LOCKED` leases and can run on multiple hosts without refreshing the same account twice. Optional email/password credentials are encrypted separately from Cookie state; when configured, a headed fallback uses an ephemeral profile and deletes it after the job. `browser_worker_group` pins each account to a worker/proxy shard, so a node only claims accounts available in its group.

Relevant settings are `LEO_SESSION_REFRESH_AHEAD`, `LEO_SESSION_REFRESH_SCAN_INTERVAL`, `LEO_SESSION_REFRESH_MIN_FRESH`, `LEO_SESSION_REFRESH_WORKERS`, `LEO_SESSION_REFRESH_BATCH`, `LEO_SESSION_REFRESH_LEASE`, `LEO_SESSION_BROWSER_LEASE`, `LEO_BALANCE_REFRESH_WORKERS`, `LEO_BALANCE_REFRESH_PER_PROXY`, `LEO_PROXY_CONTROL_MIN_INTERVAL`, and `LEO_PROXY_CONTROL_LEASE`. The worker pool is global; control-plane traffic is serialized and paced per configured proxy URL, so multiple proxy groups still refresh in parallel. Keep `LEO_SESSION_WORKER_ALLOW_REMOTE=false` for a colocated worker. Remote browser workers require a private network or TLS and an explicit opt-in.

Account-protection defaults are intentionally conservative:

- 60 requests/minute and 1,000 requests/hour per API key
- 100 generated images/day per API key
- 120 requests/minute per source IP
- 1,024 internal dispatcher goroutines and a 64-connection PostgreSQL pool; neither changes per-account business concurrency
- 8 seconds between submissions on the same Leonardo account
- General accounts use a 40-task local waiting queue by default. Routing uses cost-aware Best-Fit placement so low-cost work consumes the smallest suitable balance first.
- Accounts can be marked `video_reserved` with protected credits and reserved video slots. Ordinary work may borrow idle execution capacity but cannot spend protected credits, and queued video reclaims the configured slots with aging protection for old ordinary tasks.
- 2-minute account cooldown after an upstream 429
- 10-minute circuit cooldown after 3 consecutive submission failures

All values are configurable through `LEO_RATE_LIMIT_*`, `LEO_DAILY_IMAGE_LIMIT`, `LEO_TASK_DISPATCHER_CONCURRENCY`, `LEO_DATABASE_MAX_CONNS`, `LEO_ACCOUNT_SUBMIT_INTERVAL`, `LEO_CIRCUIT_*`, and `LEO_UPSTREAM_429_COOLDOWN` in [.env.example](.env.example). Business execution concurrency is controlled per account in PostgreSQL.

## Development

Open the repository in its Dev Container, or run tools in containers:

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.25-alpine sh -c "go fmt ./... && go test ./..."
docker compose build
```

For a Linux host where Mihomo listens on `127.0.0.1:7890`, use the server Compose file. The application uses host networking so only Leonardo traffic configured with that proxy leaves through Mihomo:

Production hosts can install `deploy/aiv2api-storage-maintenance.timer`. It removes only artifacts and unused Docker build/image cache older than seven days, never Docker volumes or database backups.

```bash
docker compose -f docker-compose.server.yml --env-file .env.server up -d --build
```

The admin frontend source is in `admin-web/`; the production Docker build compiles it and embeds the output into the Go binary.

## Security Notes

- Leonardo Cookies and access tokens are encrypted at rest. Email passwords are never stored.
- API keys are stored only as SHA-256 hashes.
- Reference-image bytes are hidden from API responses and removed from task JSON after terminal processing.
- Mutating admin requests require a valid same-site session and an allowed `Origin`.
- JSON video/audio endpoints accept only documented public fields; media references must use multipart upload fields.
- When traffic arrives through a loopback reverse proxy, rate limiting uses the validated `X-Real-IP`/`X-Forwarded-For` client address. Direct public requests cannot spoof those headers.
- Keep `LEO_MASTER_KEY`, administrator credentials, `.env`, database backups, and proxy credentials out of source control.
- Production deployments must use HTTPS and set `LEO_COOKIE_SECURE=true`. An OpenResty template is provided at `deploy/openresty/aiv2api.conf.example`; bind the Go service to loopback with `LEO_HTTP_ADDR=127.0.0.1:18080` after a domain and certificate are configured.

## Verification

```bash
go test ./...
docker compose up -d --build
curl http://127.0.0.1:8080/readyz
k6 run loadtest/k6.js
```

Live tests are intentionally opt-in because they consume Leonardo credits. Import a dedicated low-balance test account and submit one 1024x1024 image while recording estimated/settled credits from the administrator task detail; public task responses intentionally hide accounting fields.
