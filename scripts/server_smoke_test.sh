#!/bin/sh
set -eu

BASE_URL="${LEO_SMOKE_BASE_URL:-http://127.0.0.1:18080}"
ENV_FILE="${LEO_SMOKE_ENV_FILE:-/opt/leonardo2api/.env.server}"
COOKIE_JAR="$(mktemp)"
LOGIN_JSON="$(mktemp)"
KEY_JSON="$(mktemp)"
RESULT_JSON="$(mktemp)"
KEY_ID=""

cleanup() {
  if [ -n "$KEY_ID" ]; then
    curl -fsS -b "$COOKIE_JAR" -H 'Content-Type: application/json' \
      -d '{"enabled":false}' -X PATCH "$BASE_URL/admin/api/api-keys/$KEY_ID" >/dev/null || true
  fi
  rm -f "$COOKIE_JAR" "$LOGIN_JSON" "$KEY_JSON" "$RESULT_JSON"
}
trap cleanup EXIT INT TERM

set -a
. "$ENV_FILE"
set +a

python3 -c 'import json,os; print(json.dumps({"username": os.environ["LEO_ADMIN_USERNAME"], "password": os.environ["LEO_ADMIN_PASSWORD"]}))' >"$LOGIN_JSON"
curl -fsS -c "$COOKIE_JAR" -H 'Content-Type: application/json' --data-binary @"$LOGIN_JSON" "$BASE_URL/admin/login" >/dev/null
curl -fsS -b "$COOKIE_JAR" -H 'Content-Type: application/json' \
  -d '{"name":"codex-smoke-test","concurrency_limit":1}' "$BASE_URL/admin/api/api-keys" >"$KEY_JSON"

API_KEY="$(jq -r .key "$KEY_JSON")"
KEY_ID="$(jq -r .id "$KEY_JSON")"

MODELS="$(curl -fsS -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/models")"
printf '%s' "$MODELS" | jq -e '.data | map(.id) | index("gpt-image-2") != null' >/dev/null

HTTP_STATUS="$(curl -sS -o "$RESULT_JSON" -w '%{http_code}' \
  -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: codex-gpt-image-2-smoke-v1' \
  -d '{"model":"gpt-image-2","prompt":"A minimal studio product photo of one matte red ceramic cube on a plain white background, no text","size":"1024x1024","quality":"low","output_format":"png","background":"opaque","moderation":"auto","n":1}' \
  "$BASE_URL/v1/images/generations")"

if [ "$HTTP_STATUS" != "200" ]; then
  jq . "$RESULT_JSON" >&2
  exit 1
fi

B64_LENGTH="$(jq -r '.data[0].b64_json | length' "$RESULT_JSON")"
if [ "$B64_LENGTH" -lt 1000 ]; then
  echo "generated image payload is unexpectedly small" >&2
  exit 1
fi

echo "models=gpt-image-2-present status=$HTTP_STATUS b64_length=$B64_LENGTH temporary_key=disabled-on-exit"
