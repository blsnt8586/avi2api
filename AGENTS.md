# AIV2API Project Record

Last updated: 2026-08-11

## Project Goal

AIV2API is a self-hosted multi-provider AI gateway. It provides curated image, video, and audio APIs, asynchronous task execution, provider-specific account/session management, rate and cost controls, and a web administration console. Leonardo AI is the first production provider adapter.

## Provider Architecture

- Provider catalog and task/account/provider bindings are persisted in PostgreSQL.
- Every account, price rule, and task carries a `provider_id`; existing rows migrate to `leonardo`.
- Account selection and credit reservation are isolated by provider. Credits from different providers are never added or compared as the same unit.
- `internal/providers` is the adapter registry. Workers reject tasks whose provider adapter is not registered.
- `model_provider_configs` maps public model IDs to provider-specific upstream IDs and settings.
- The current public API defaults to Leonardo while additional provider authentication and generation adapters are implemented.
- Keep legacy `LEO_*` environment names, Redis keys, server directory, and systemd units during the compatibility phase.

The public contract intentionally hides raw Leonardo-only identifiers such as `style_ids`, `reference_ids`, `public`, upstream generation IDs, provider/account bindings, accounting snapshots, and raw stored requests. Generated content is always submitted to Leonardo with `public=false`.

## Current Deployment

- Public admin/API base URL: `http://45.59.128.219:18080`
- Server SSH: `ssh -p 2222 -i "$env:USERPROFILE\.ssh\ssk-cubecloud" root@45.59.128.219`
- Server project directory: `/opt/leonardo2api`
- Compose file: `docker-compose.server.yml` (app-only; PostgreSQL and Redis are external shared services)
- Compose environment file: `/opt/leonardo2api/.env.server`
- Runtime services: AIV2API app plus the host's shared PostgreSQL and Redis containers
- Host proxy service: Mihomo, local HTTP proxy `127.0.0.1:7890`
- AIV2API connects to the existing PostgreSQL and Redis services on host ports `15432` and `16379`. It uses a dedicated PostgreSQL database named `leonardo`; credentials remain only in `.env.server`.
- Browser fallback worker: long-running `leonardo-session-sync.service`; Patchright Chrome headless recovery followed by headed Patchright Chrome fallback
- JWT scheduler: database-backed, scans every minute and schedules accounts with 15 minutes or less remaining

Never place passwords, cookies, JWTs, API keys, `LEO_MASTER_KEY`, or `LEO_SESSION_SYNC_TOKEN` in this file, source code, logs, screenshots, or commits.

## Main Modules

### HTTP API

Location: `internal/httpapi`

Public routes:

- `GET /healthz`
- `GET /readyz`
- `GET /docs` (public developer documentation)
- `GET /openapi.json` (OpenAPI 3.1 contract)
- `GET /api-docs/cost-rules` (enabled read-only pricing rules)
- `GET /v1/models`
- `POST /v1/images/estimate`
- `POST /v1/videos/estimate`
- `POST /v1/images/generations`
- `GET /v1/images/{id}`
- `POST /v1/images/{id}/cancel`
- `POST /v1/videos/generations`
- `GET /v1/videos/{id}`
- `POST /v1/videos/{id}/cancel`
- `POST /v1/audio/generations`
- `GET /v1/audio/{id}`
- `POST /v1/audio/{id}/cancel`
- `POST /v1/chat/completions`

Authentication uses `Authorization: Bearer <API key>`. Every paid image/video/audio/chat creation requires `Idempotency-Key`; image, video, and audio creation returns a durable task immediately.

Public task reads return a dedicated DTO containing lifecycle state, queue position, model/prompt, result, sanitized failure information, retry state, and timestamps. Admin task APIs retain provider, account, reservation, cost, and upstream identifiers. `GET /v1/models` is filtered by the current API Key and reports `owned_by=aiv2api`.

### Image API

Public models:

- `gpt-image-2` -> Leonardo `gpt-image-2`
- `nano-banana-2` -> Leonardo `nano-banana-2`
- `nano-banana-pro` -> Leonardo `gemini-image-2`
- `seedream-5.0-pro` -> Leonardo `seedream-5.0-pro`

`POST /v1/images/generations` accepts JSON for text-to-image and `multipart/form-data` with `image` or repeated `image[]` for reference-image generation.

`POST /v1/images/generations` is URL-only asynchronous delivery. It accepts `response_format=url`, rejects `b64_json`, `output_format`, and `output_compression`, and stores multipart reference images as temporary task assets until terminal cleanup.

Public image parameters:

- `model`, `prompt`, `size`, `n`
- `quality` for `gpt-image-2`
- `response_format`: `url` only for asynchronous image tasks
- `background`: `auto` or `opaque`; Leonardo output is currently opaque
- `moderation`: only `auto`
- multipart-only `image`/`image[]` and `reference_strength`

`reference_strength` is backed by Leonardo schema and accepts only `LOW`, `MID`, or `HIGH`; default is `MID`.

Gateway limits:

- Prompt: non-empty, maximum 9,999 Unicode characters, matching Leonardo schema `1.232.1`
- Images per request: `1..4`; `gpt-image-2` is restricted to `n=1` on the current account
- Reference images: maximum 6, PNG/JPEG/WebP input
- GPT Image 2 sizes must match Leonardo width/height enums, remain within a `3:1` ratio, and contain `655,360..8,294,400` pixels
- Nano Banana 2 and Pro sizes must match Leonardo's positive width/height enums; the standard Large presets extend beyond 4096 to `6336x2688` and `3072x5504`
- Seedream 5.0 Pro accepts custom `768..2048` per edge; Schema 1.247.2 lists standard UI presets and a 45-credit base with a 90-credit 2K threshold
- WebP output encoding, masks, streaming, partial images, and adjustable `input_fidelity` are not exposed

Gateway delivery is URL-only for asynchronous image tasks; `response_format` is normalized locally and no output transcoding parameters are exposed. These delivery fields are not sent as Leonardo generation parameters.

`POST /v1/images/estimate` accepts `model`, `size`, `quality`, and `n`, runs the same model validation and pricing path as task admission, and does not create a task or reserve credits. It returns per-output and total estimated credits, normalized parameters, pricing basis/tier, formula, active price version, and the fields that affect cost. GPT Image 2 uses its exact pixel-and-quality formula; Nano Banana 2/Pro map every legal size to the Leonardo Small/Medium/Large price tier; Seedream 5.0 Pro uses the Schema 1.247.2 45/90-credit size threshold. Delivery parameters, moderation, reference media, and reference strength do not currently change image credits.

### Video API

Public models:

- `flux-3-video` -> Leonardo `bfl/flux-3-video`
- `seedance-2.0`
- `seedance-2.0-fast`
- `seedance-2.0-mini`
- `seedance-2.5` -> Leonardo `bytedance/seedance-2.5`
- `veo-3.1` -> Leonardo `veo-3.1-generate-001`
- `veo-3.1-fast` -> Leonardo `veo-3.1-fast-generate-001`
- `kling-o3-omni` -> Leonardo `kling-video-o-3`
- `minimax-h3` -> Leonardo `hailuo-03`
- `grok-imagine-1.5` -> Leonardo `grok-imagine-1.5`

The video API is an asynchronous gateway extension; it does not claim field-for-field OpenAI Videos compatibility.

JSON video requests accept only the documented generation fields. All image/video/audio reference media must use multipart upload fields; clients cannot submit internal `SourceMedia` paths or stored provider fields.

Public video parameters:

- `model`, `prompt`, `duration`, `size`, `resolution`
- optional reference `image`/`image[]`
- optional model-specific `start_frame`, `end_frame`, `video`/`video[]`, and `audio`/`audio[]`
- optional model-specific `generate_audio`; default `true`; MiniMax H3 rejects `false`
- `reference_strength`: `LOW`, `MID`, or `HIGH`; default `MID`
- `Idempotency-Key` header

Model constraints:

- Seedance 2.0 duration: `4..15` seconds, default 8
- FLUX 3 Video duration: `5..20` seconds, default 8
- Kling O3 Omni duration: `3..15` seconds, default 5
- Seedance 2.0 resolution: `480p`, `720p`, `1080p`, `2160p`
- Seedance 2.0 Fast resolution: `480p`, `720p`
- Seedance 2.0 Mini supports `480p` and `720p`
- Seedance 2.5 supports `480p` and `720p` through twelve exact landscape, portrait, square, and cinema sizes
- FLUX 3 Video supports `720p` and `1080p` through fourteen exact aspect-ratio dimensions
- Veo 3.1 and Veo 3.1 Fast support `720p`, `1080p`, and `2160p`
- Kling O3 Omni supports `720p`, `1080p`, and `2160p` through nine exact landscape, portrait, and square sizes; default is `1920x1080` at `1080p`
- MiniMax H3 is fixed to `1440p` and accepts `3360x1440`, `2560x1440`, `1920x1440`, `1440x1440`, `1440x1920`, or `1440x2560`
- Grok Imagine 1.5 supports `480p`, `720p`, and `1080p` price tiers through nine exact landscape, portrait, and square sizes
- Quantity is fixed to one video
- Prompt limits: Seedance 5,000; Veo 3.1/Fast 9,999; Kling O3 Omni 2,500; MiniMax H3 2,000 Unicode characters

Veo 3.1 models accept only 4, 6, or 8 seconds. Both support start/end frames and native audio generation; only Veo 3.1 accepts ordinary image references, with a maximum of three, and that combination requires `size=1280x720` and `duration=8`. Default pricing tables include native audio, while `generate_audio=false` is adjusted from the same schema modifiers by the shared pricing estimator.
- Kling O3 Omni accepts up to seven ordinary images, one start frame with optional end frame, or one reference video plus up to four images. Frames are exclusive with ordinary image/video references. Reference video accepts one 3–10.05 second file with each edge between 720 and 2160 pixels; the integer `duration` must match within 0.05 seconds, and 2160p is unavailable for this path. Native audio defaults to enabled. Schema `1.255.2` prices native-audio generation at 224/280/420 credits per second for 720p/1080p/2160p; reference video is 252 credits per input second.
- MiniMax H3 accepts 5 through 15 seconds and limits prompts to 2,000 characters. It accepts five ordinary image references, one start frame, one end frame, and three reference audio files totaling at most 15 seconds. Reference audio requires an ordinary image reference. Native audio is always enabled, so `generate_audio=false` is rejected. Leonardo schema `1.247.2` prices it at 140 credits per second.
- Grok Imagine 1.5 accepts 3 through 15 seconds and limits prompts to 5,000 characters. It requires exactly one `start_frame` guidance input and rejects `end_frame`, ordinary image references, reference video, and reference audio. Native audio defaults to enabled and may be disabled without changing the current Schema token price. Its exact dimensions map to 100, 165, or 290 credits per second for the 480p, 720p, and 1080p tiers.
- Seedance accepts at most 4 ordinary reference images, one start frame, one end frame, 3 reference videos, and one reference audio file
- Seedance 2.5 accepts at most 30 ordinary reference images, one start frame, one end frame, 10 reference videos, and 10 reference audio files. Reference video and audio totals are independently capped at 30.2 seconds; audio requires an ordinary image or video reference. Frames are exclusive with ordinary image, video, and audio references.
- Seedance reference videos may total at most 15 seconds
- Seedance reference audio may total at most 15 seconds
- FLUX 3 Video accepts one start frame, one optional end frame, or one reference video up to 50 MB and 15.05 seconds; these modes are mutually exclusive. Native audio may be disabled without changing the current Schema price.
- FLUX 3 Video costs 215 credits per second at 720p and 366 credits per second at 1080p; a reference video uses 543 and 682 credits per second respectively.

Reference images and frames use Leonardo init-image upload and moderation. Reference video/audio use the `UploadImage` presigned upload flow, poll `uploaded_media` to `COMPLETE`, and submit the verified `image_reference`, `start_frame`, `end_frame`, `video_reference_base`, and `audio_reference` guidance shapes from schema `1.247.2`. Large media files are stored temporarily under `LEO_TASK_ASSET_DIR`, removed at task terminal state, and purged after 24 hours if orphaned. A paid end-to-end generation covering every video/audio guidance combination has not yet been run.

### Audio API

Public models:

- `dialogue-v3`: text-to-speech
- `music-v1`: prompt-to-music
- `sound-effects-v2`: prompt-to-sound-effect

The audio API is asynchronous. Create with `POST /v1/audio/generations`, poll `GET /v1/audio/{id}`, and cancel with `POST /v1/audio/{id}/cancel`.

Audio JSON decoding uses a public DTO and rejects internal fields such as `public`; the worker always submits private output.

Public audio parameters:

- common: `model`, `prompt`, `n`, `Idempotency-Key`
- `dialogue-v3`: `voice`, `language`, `prompt_influence`
- `music-v1`: `duration_minutes`, `force_instrumental`
- `sound-effects-v2`: `duration`, `loop`, `prompt_influence`

Constraints and default costs:

- Quantity: `1..4`, default 1
- Dialogue: prompt up to 5,000 Unicode characters; 21 public voice aliases; `prompt_influence=0..1`; 90 credits per 1,000 characters per output
- Music: prompt up to 9,999 Unicode characters; `duration_minutes=1..10`; 700 credits per minute per output
- Sound effects: prompt up to 9,999 Unicode characters; `duration=1..22` seconds; `prompt_influence=0..1`; 2 credits per second per output
- Audio results are extracted from the verified Leonardo `generated_images.urls.asset` field, with legacy `generated_images.url` and audio-semantic metadata URLs as fallbacks

Schema and HAR confirm the model IDs and submit parameter shapes. A paid 2026-07-24 `sound-effects-v2` probe at 1 second and `n=1` completed end to end: the task returned one downloadable `audio/mpeg`, estimated and actual cost were both 2 credits, and the reservation settled as `consumed`. Leonardo returned an empty legacy `generated_images.url`; the MP3 was present in `generated_images.urls.asset`.

### Leonardo Client

Location: `internal/leonardo`

Responsibilities:

- Session retrieval from `app.leonardo.ai/api/auth/get-session`
- GraphQL requests to `api.leonardo.ai/v1/graphql`
- Image/video/audio generation submission
- Init image upload and moderation polling
- Generation polling and cancellation support
- Audio result URL extraction, including the verified `generated_images.urls.asset` shape
- Token balance retrieval
- Platform model schema synchronization

Upstream errors use `leonardo.HTTPError` with the HTTP status and truncated response body.

### Task Workers

Location: `internal/jobs`

Responsibilities:

- Database-backed task execution leases and lease renewal
- Transactional account credit reservations and account selection at task creation
- Database-atomic account/API-key execution-slot claims when a queued task starts
- Price-rule fencing at admission and immediately before upstream submission
- Transactional Outbox dispatch with Redis as a delivery transport only
- Reference image upload
- Image/video/audio generation submission
- Polling and result persistence
- Token cost capture
- Circuit breaker and upstream cooldown handling
- Cleanup of stored source-image payloads

Task states include `queued`, `reserving`, `uploading`, `submitted`, `polling`, `succeeded`, `failed`, `cancelled`, and `submission_uncertain`.

Credit reservation and execution capacity are intentionally separate. Each account has an execution limit in `image_concurrency` and a waiting limit in `queue_capacity`. Concurrency 5 plus queue capacity 5 allows 5 executing and 5 waiting. Full accounts are skipped during routing; only an entirely full eligible account pool returns `account_queue_full`. `queue_position` is returned while queued. Workers atomically claim at most `accounts.image_concurrency` tasks per account and `api_keys.concurrency_limit` tasks per API key. Each API key can hold at most its execution limit plus `LEO_API_KEY_QUEUE_MULTIPLIER` waiting multiples; the default is 20 executing plus 40 waiting. Cancelling a queued task releases its held credits in the same PostgreSQL transaction. The Asynq dispatcher defaults to 128 goroutines and PostgreSQL remains bounded separately by `LEO_DATABASE_MAX_CONNS`, default 64.

Global task protection is persisted in the singleton `system_capacity_config` row introduced by embedded migration `00034_system_capacity.sql`. PostgreSQL serializes admission and execution claims against this row, so Redis and worker goroutine counts are not treated as capacity facts. Defaults are 100 executing tasks, 1,000 queued tasks, a 900-task overload watermark, a 700-task resume watermark, and a 30-minute queue deadline. Hysteresis prevents overload flapping. Maintenance drain rejects new non-idempotent admissions while existing tasks continue; execution pause additionally stops queued tasks from claiming new execution slots but never interrupts already submitted or polling tasks. Public overload errors are `system_queue_full`, `system_overloaded`, and `system_maintenance`.

### Account And Session Management

Locations: `internal/accounts`, `scripts/leonardo_login.py`, `scripts/leonardo_sync_session.py`

Account cookies, access tokens, and optional email/password login credentials are stored as separate AES-GCM ciphertexts. The encryption key comes from `LEO_MASTER_KEY`; plaintext passwords are only released to a currently leased browser worker and are never returned by admin account APIs.

Automatic refresh flow:

1. The Go scheduler scans JWT expiry and inserts at most one active `session_refresh_jobs` row per due account.
2. JWT renewal moves the leased job directly to the browser stage because Vercel checkpoints direct HTTP calls to `get-session`.
3. Long-running `leonardo-session-sync.service` workers claim browser jobs with `FOR UPDATE SKIP LOCKED` and lease heartbeats.
4. The worker imports the encrypted-at-rest Better Auth Cookie into a private, ephemeral Patchright Chrome headless profile. It calls `get-session` through page JavaScript so the request uses Chrome's network stack; a successful recovery imports the fresh session and completes the lease.
5. Headless failure deletes its temporary profile and artifacts. Accounts with saved login credentials then run Patchright in a second ephemeral headed profile using child-process environment variables; legacy accounts without saved credentials retain their persistent-profile fallback. CAPTCHA handling is enabled only for this headed fallback.
6. Browser failures use capped database backoff. Expired worker leases are recovered automatically.

The scheduler defaults to a one-minute scan, 15-minute refresh-ahead window, 32 Cookie workers, and a 1,000-account scheduling batch. Each account receives a deterministic 0–600 second refresh jitter to avoid expiry bursts. Leonardo currently issues Better Auth session cookies with an observed lifetime of about 45 days and JWTs with an observed lifetime of about 60 minutes; scheduling always uses returned expiry timestamps rather than fixed assumptions. Browser renewal, authentication-time balance calibration, and session import share a Redis control window per proxy URL: default minimum spacing is 20 seconds with a 90-second lease. There is no periodic or post-generation upstream balance polling. An upstream HTTP 429 opens a two-minute retry window by default; valid JWTs and locally tracked balances remain usable. The current proxy group runs one browser job at a time; larger pools scale horizontally with additional private worker/proxy groups instead of increasing concurrency on one exit. `browser_worker_group` shards claims by node/proxy group. Both browser stages use Patchright with the system Chrome installation. `scripts/leonardo_sync_all_sessions.py` and the static JSON account list are legacy migration tools, not the production scheduling path. Workers remove inherited `LEONARDO_EMAIL` and `LEONARDO_PASSWORD` values. Temporary profiles, HAR, screenshots, cookies, storage state, Cookie headers, plaintext credentials, and session tokens are deleted after every browser job.

Account status meanings:

- `active`: usable
- `disabled`: manually disabled
- `invalid`: Leonardo returned 401/403; login credentials/session need renewal
- `rate_limited`: Leonardo/Vercel returned 429; UI shows `429 临时风控`, with a two-minute default retry window
- `cooldown`: temporary network/upstream failure; retry later

A successful browser session sync always restores the account to `active`.

When an admin adds an account, `image_concurrency` is the legacy storage/API name for the account-wide image, video, and audio generation capacity. Leonardo accounts default to 5 and accept 1 through 5 because the current Leonardo BASIC account was verified at a maximum of five pending jobs. Account creation validates the upstream session and balance before completion; if validation fails, the newly inserted account row is deleted so retries do not accumulate unusable duplicates.

### Persistence

Location: `internal/store`, migrations in `internal/migrate/sql`

- PostgreSQL stores accounts, encrypted sessions, API keys, tasks, model configs, platform model catalog, audit logs, and settings.
- `system_capacity_config` stores the global execution limit, queue hard limit, overload high/resume watermarks, queue timeout, maintenance drain, and execution-pause state. Runtime capacity snapshots combine this policy with live task and eligible-account counts.
- Redis stores queues, rate-limit counters, API-key slots, refresh locks, and task notifications.
- `tasks.execution_lease_*` makes Worker ownership explicit. Only the lease owner may change task state, submit upstream work, or settle a reservation.
- `task_outbox` is written in the same transaction as a newly reserved task. Redis enqueue failures leave the task pending for scheduler redispatch instead of releasing capacity or marking a false failure.
- Queued reservations retain the 24-hour financial expiry and also carry a 30-minute absolute queue deadline by default. Submitted generations carry a 30-minute absolute upstream deadline. Deadline expiry never resubmits a mutation: queued work fails and releases, while submitted work becomes `submission_uncertain` and keeps its reservation for reconciliation.
- Authentication-time account balance reads use a database fencing version. A superseded request can update a newer JWT but cannot overwrite the newer calibration snapshot.
- Successful tasks atomically consume their reservation and subtract the immutable estimated price from the local account balance. Failed or cancelled tasks release the reservation without changing the local balance; `submission_uncertain` remains held.
- The next successful authentication replaces the local balance with Leonardo's current balance, correcting external usage or upstream pricing drift without periodic polling.
- `api_key_usage_ledger` is append-only and receives exactly one database-triggered terminal entry for every released or consumed reservation. Settled reservations and ledger rows reject later mutation.
- Only explicit upstream 4xx submission rejections release a pre-generation reservation. HTTP 408/5xx, GraphQL mutation errors, malformed responses, and transport errors become `submission_uncertain`.
- Price changes create a new immutable rule row and disable the replaced version. A partial unique index allows only one enabled rule per parameter combination.
- Goose migrations are embedded from `internal/migrate/sql`. The top-level `migrations` directory is only a secondary copy and is not the runtime migration source.
- New embedded migrations must use the next unused numeric version. Check both migration directories before choosing a number.

### Admin Web

Location: `admin-web/src`

Stack: React, TypeScript, Vite, TanStack Query, Lucide icons.

Views:

- Operations overview
- System capacity and protection controls
- Account pool and session status
- Task history and token consumption
- API key management
- Curated/platform model catalog and synchronization
- Image/video/audio API documentation and test center
- Admin password change
- Audit logs

The account view uses server-side pagination and search, while the operations overview reads SQL aggregates plus four account summaries. It does not fetch or render the complete account pool every five seconds.

The developer documentation is also rendered publicly at `/docs` without exposing admin APIs or credentials. It includes quick start, complete request/response examples, asynchronous task states, stable error codes, retry guidance, live enabled pricing, and an OpenAPI 3.1 download.

The OpenAPI contract must pass a standard OpenAPI linter and JSON Schema 2020-12 example validation. It documents the URL-only async image contract, strict video/audio JSON fields, one Base64 Chat reference image, sanitized task responses, and `api_key_capacity_exhausted` alongside account queue errors.

The API documentation table separates parameter meaning from accepted values. Keep examples executable and aligned with backend validation.

### Security And Abuse Controls

- Bearer API keys are stored as SHA-256 hashes; plaintext is displayed only once.
- Admin password is stored as an Argon2-derived hash in application settings.
- Admin sessions use HTTP-only cookies and CSRF/origin checks.
- Global, API-key, and IP rate limits are configurable.
- Behind the loopback OpenResty proxy, client IP rate limiting trusts `X-Real-IP`/the first `X-Forwarded-For` address only when the direct peer is loopback; direct public clients cannot override their source address with proxy headers.
- Daily request limits, per-minute/hour request limits, worker concurrency, account submit spacing, circuit breaker, and 429 cooldown are configurable. Defaults are 60 requests/minute per API key, 1,000/hour, and 120/minute per source IP. Task capacity is configured per account.
- Leonardo generation is always private (`public=false`).
- Platform moderation remains enabled.
- A successful result consumes its reservation and local account credits in the same PostgreSQL transaction as the terminal task state.

### Platform Model Catalog And Costs

- Admin can synchronize current image/video/audio schemas from Leonardo `GetRelease`.
- The default catalog view shows only public curated models; the full catalog remains searchable.
- Static cost tables were sampled from the Leonardo UI without submitting generation. GPT Image 2 custom sizes are estimated from the schema's 7-credits-per-megapixel formula and quality multipliers; Nano tiers use Leonardo's width modifiers rather than square-pixel buckets.
- Settled task credits are stored per immutable accepted price rule. Authentication-time upstream balances are calibration snapshots; task accounting uses the local append-only ledger.
- Leonardo may change schema and prices. Re-sync and re-sample before making pricing guarantees.

## Evidence And Research Artifacts

Relevant artifacts live under `artifacts/`, including Leonardo HAR captures, screenshots, live image results, and UI visual checks.

The strongest current upstream evidence is Leonardo `GetRelease` schema version `1.258.0`, read through `publicJsonSchemaRegistry.release` and an active account-scoped catalog. It confirms Seedance 2.5 account availability, ID, exact sizes, 4-30 second duration range, reference combinations, native audio, and 180/292 credits-per-second base rates with 258/466 credits-per-second video-reference rates. Schema 1.255.2 remains the source for FLUX 3 Video and Kling O3 Omni; earlier Schema 1.247.2 evidence remains the source for the existing image, MiniMax H3, and Grok Imagine 1.5 contracts.

Do not commit fresh HAR files or browser state without checking for Authorization headers, cookies, JWTs, signed URLs, email addresses, or other secrets.

## Development Commands

Go formatting and tests, using Docker when Go is not installed on Windows:

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.25-bookworm sh -lc "/usr/local/go/bin/go fmt ./... && /usr/local/go/bin/go test ./..."
```

Frontend build with the Codex bundled Node runtime when `npm` is not on PATH:

```powershell
$env:PATH='C:\Users\zhang\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin;'+$env:PATH
Set-Location admin-web
.\node_modules\.bin\tsc.cmd -b
.\node_modules\.bin\vite.cmd build
```

Server deployment:

```powershell
ssh -p 2222 -i "$env:USERPROFILE\.ssh\ssk-cubecloud" root@45.59.128.219 `
  "cd /opt/leonardo2api && docker compose --env-file .env.server -f docker-compose.server.yml build app && docker compose --env-file .env.server -f docker-compose.server.yml up -d --no-deps app"
```

Server health check:

```powershell
Invoke-WebRequest -UseBasicParsing http://45.59.128.219:18080/healthz
```

Session refresh diagnostics:

```bash
systemctl status leonardo-session-sync.service
journalctl -u leonardo-session-sync.service -n 100 --no-pager
```

Install the production Patchright driver and Chrome:

```bash
/opt/leonardo2api/.venv-login/bin/pip install -r /opt/leonardo2api/requirements-login.txt
/opt/leonardo2api/.venv-login/bin/patchright install chrome
```

## Required Verification

Before claiming a change works:

1. Run targeted tests for the changed package.
2. Run `go test ./...` for backend/shared contract changes.
3. Run TypeScript and Vite production builds for frontend changes.
4. Deploy only the intended files; preserve `.env.server`, secrets, browser profiles, and user data.
5. Rebuild and restart only the app unless infrastructure changes require more.
6. Check `/healthz`, container logs, and relevant database state.
7. For frontend changes, verify the production asset contains the new behavior and visually inspect desktop/mobile when layout changed.
8. Never submit a paid Leonardo generation merely to test UI or schema unless the user explicitly approves the cost.

## Known Limitations And Follow-ups

- A 2026-07-23 paid `gpt-image-2` low-cost probe confirmed Leonardo `BASIC` reports `maxPendingJobs=5`. Five overlapping generations succeeded; the sixth returned GraphQL `RATE_LIMIT_EXCEEDED` with `currentPendingJobs=5` and was not charged. Keep production account concurrency below five unless the plan or upstream behavior is revalidated.
- An isolated six-image repro after more than five idle minutes produced the same `currentPendingJobs=5 / maxPendingJobs=5` rejection, confirming it was not leftover work from earlier batches. Repeated probe batches later caused a separate Vercel Security Checkpoint HTTP 429 during balance refresh; distinguish this transient security cooldown from the deterministic GraphQL pending-job limit.
- A historical paid video probe submitted five overlapping `gemini-omni-flash` jobs at 3 seconds/720p. All five succeeded, each reserved and consumed 300 credits, and all five were submitted before the earliest completion. This retired public model still confirms that the five-pending-job BASIC limit also accommodates five concurrent video generations.
- A paid audio probe submitted `sound-effects-v2` at 1 second and `n=1`. The first generation exposed a parser gap because Leonardo left `generated_images.url` empty while populating `generated_images.urls.asset`; after fixing that mapping, a second probe succeeded with `audio/mpeg`, a downloadable MP3, a 2-credit actual delta, and a consumed/reconciled reservation.
- Seedance ordinary image, start/end frame, video, and audio reference generation is schema-verified but still needs an approved paid end-to-end test.
- FLUX 3 Video parameters, exact sizes, reference limits, account-scoped availability, and price modifiers are schema-verified against Leonardo `1.255.2`; no paid end-to-end FLUX 3 Video generation has been run through AIV2API.
- Seedance 2.5 is enabled from Schema 1.258.0. Its parameters and pricing are schema-verified; no paid end-to-end generation probe has been run through AIV2API.
- Veo 3.1 and Veo 3.1 Fast submission shapes and costs are verified against Leonardo schema `1.232.1`; neither has received a paid end-to-end generation probe through AIV2API yet.
- Kling O3 Omni parameters, nine dimensions, reference combinations, native-audio modifiers, and reference-video pricing are schema-verified against Leonardo `1.255.2`; no paid end-to-end generation probe has been run through AIV2API.
- MiniMax H3 parameters, guidance limits, six 2K sizes, forced native audio, and 140-credits-per-second price are schema-verified against Leonardo `1.247.2`; no paid end-to-end MiniMax H3 generation has been run through AIV2API.
- Grok Imagine 1.5 parameters, mandatory start frame, nine size/tier mappings, optional native audio, and 100/165/290-credits-per-second prices are schema-verified against Leonardo `1.247.2`; no paid end-to-end Grok Imagine 1.5 generation has been run through AIV2API.
- Leonardo/Vercel may return transient 429 checkpoints. These are rate-limited states, not account deletion.
- Balance reconciliation assumes each provider account is consumed only through AIV2API. External Leonardo usage is treated as unexplained spend and can open the price-rule circuit.
- `submission_uncertain` reservations remain held until the generation is recovered or an administrator explicitly reconciles them; they are never automatically retried on another account.
- Cookie session refresh may reuse a JWT until Leonardo issues a new one; tokens below the configured minimum remaining lifetime are handed to a leased browser worker.
- Image prompts allow 9,999 Unicode characters. Video limits are Seedance/Grok 5,000, Kling O3 Omni 2,500, Veo 9,999, and MiniMax H3 2,000. Audio limits are Dialogue 5,000 and Music/Sound Effects 9,999.
- WebP output encoding is not exposed.
- Transparent backgrounds, mask-based editing, streaming, partial images, and adjustable moderation/input fidelity are not exposed.
- Server HTTP is currently public over plain HTTP. Production hardening should add TLS and restrict the admin surface through a reverse proxy or firewall policy.
- `deploy/openresty/aiv2api.conf.example` is the TLS reverse-proxy template. After a dedicated domain and certificate exist, bind the app with `LEO_HTTP_ADDR=127.0.0.1:18080`, set `LEO_PUBLIC_BASE_URL=https://<domain>`, and enable `LEO_COOKIE_SECURE=true`.

## Change Discipline

- Keep public API docs synchronized with actual validation and transformation code.
- Distinguish Leonardo-native parameters from gateway delivery parameters.
- Do not expose raw Leonardo-only IDs unless the product decision changes explicitly.
- Prefer schema/HAR evidence over assumptions about undocumented upstream fields.
- Preserve account data, encrypted credentials, task history, volumes, and unrelated user changes.
- Do not run destructive Git or Docker cleanup commands unless storage pressure is confirmed and the target is explicit.
