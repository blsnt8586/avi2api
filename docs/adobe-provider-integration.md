# Adobe Firefly provider integration

This repository keeps Adobe Firefly behind the same asynchronous task system
used by Leonardo. The Adobe repository is used as an upstream protocol
reference; its FastAPI routes, JSON files, token pool, and local job store are
not copied into AIV2API.

## Current status

- `provider_id=adobe` is enabled in the provider catalog and registered in the
  worker registry.
- Admin accounts accept either an Adobe Access Token or complete Adobe Cookie
  JSON. AT-only accounts remain usable until the token expires; Cookie-backed
  accounts refresh ATs through the existing database scheduler and Go Cookie
  workers without entering the Leonardo browser worker.
- Account creation verifies the token scope, Adobe Profile, and current credit
  balance. The observed account used for the read-only integration probe had
  50,000 total credits, 0 used, and 50,000 available at the time of validation.
- Adobe BKS is queried during account import to create provider-specific active
  price rules. Price rules are not shared with Leonardo.
- Public models use canonical IDs such as `gpt-image-2` and `veo-3.1-fast` together with `provider=adobe`. Both use the
  same durable asynchronous task, lease, reservation, polling, and settlement
  system as the existing provider.

## Upstream contract captured from adobe2api

Image and video generation use separate asynchronous endpoints:

```text
POST https://firefly-3p.ff.adobe.io/v2/3p-images/generate-async
POST https://firefly-3p.ff.adobe.io/v2/3p-videos/generate-async
GET  <x-override-status-link or links.result.href>
```

Reference media is uploaded first:

```text
POST https://firefly-3p.ff.adobe.io/v2/storage/image
POST https://firefly-3p.ff.adobe.io/v2/storage/video
POST https://firefly-3p.ff.adobe.io/v2/storage/audio
```

The returned media ID is then placed in the model-specific `referenceBlobs`
or `referenceImages` field. Completed image/video jobs expose a presigned URL
under `outputs[*].image.presignedUrl` or `outputs[*].video.presignedUrl`.

Required upstream headers are an Adobe access token and the Firefly web API
key, plus the Firefly origin/referer headers. The access token must be treated
as an encrypted account credential; the API key belongs in deployment
configuration, never in a migration, source file, log, or task payload.

Adobe account and pricing reads use:

```text
GET  https://ims-na1.adobelogin.com/ims/profile/v1
GET  https://firefly.adobe.io/v1/credits/balance
POST https://bks.adobe.io/v2/credits/cost
POST https://adobeid-na1.services.adobe.com/ims/check/v6/token
```

The observed AT lifetime was about 24 hours. Runtime scheduling always follows
the JWT timestamps rather than assuming a fixed duration. Complete Cookie JSON
is encrypted in full so later cookie attributes remain available for renewal.

## AIV2API mapping

```text
public async request
  -> existing admission, idempotency, provider pricing and reservation
  -> account selected with provider_id=adobe
  -> Adobe media upload (if references exist)
  -> Adobe async submit
  -> task.status=submitted/polling
  -> presigned URL result
  -> existing terminal settlement and source cleanup
```

`gpt-image-2` on provider Adobe currently exposes 1024x1024, 2048x2048, and 2880x2880
pricing tiers with low, medium, and high quality. `veo-3.1-fast` on provider Adobe exposes
4, 6, and 8 seconds at 720p or 1080p, landscape or portrait, with optional
start/end frame guidance. Native audio is disabled in the current public model
contract so its BKS price and upstream payload remain aligned. Firefly's
current Veo 3.1 Fast cost builder sends only `videoDuration` and
`generateAudio`; it does not send a resolution field, so the synchronized 720p
and 1080p rules intentionally share the same per-duration BKS value.

Profile, credit balance, token renewal, BKS price reads, payload construction,
and mock submit/poll coverage do not consume generation credits. A real paid
image or video generation remains a separate explicit production probe.

## Repository and license note

The GitHub metadata for `leik1000/adobe2api` currently reports no license. Use
its endpoint/payload behavior as protocol evidence, but do not copy its Python
implementation or package wholesale until the upstream licensing and
attribution terms are clarified.
