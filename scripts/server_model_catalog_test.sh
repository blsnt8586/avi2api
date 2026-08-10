#!/bin/sh
set -eu

BASE_URL="${LEO_SMOKE_BASE_URL:-http://127.0.0.1:18080}"
ENV_FILE="${LEO_SMOKE_ENV_FILE:-/opt/leonardo2api/.env.server}"
COOKIE_JAR="$(mktemp)"
LOGIN_JSON="$(mktemp)"
KEY_JSON="$(mktemp)"
KEY_ID=""

cleanup() {
  if [ -n "$KEY_ID" ]; then
    curl -fsS -b "$COOKIE_JAR" -H 'Content-Type: application/json' \
      -d '{"enabled":false}' -X PATCH "$BASE_URL/admin/api/api-keys/$KEY_ID" >/dev/null || true
  fi
  rm -f "$COOKIE_JAR" "$LOGIN_JSON" "$KEY_JSON"
}
trap cleanup EXIT INT TERM

set -a
. "$ENV_FILE"
set +a

python3 -c 'import json,os; print(json.dumps({"username": os.environ["LEO_ADMIN_USERNAME"], "password": os.environ["LEO_ADMIN_PASSWORD"]}))' >"$LOGIN_JSON"
curl -fsS -c "$COOKIE_JAR" -H 'Content-Type: application/json' --data-binary @"$LOGIN_JSON" "$BASE_URL/admin/login" >/dev/null
curl -fsS -b "$COOKIE_JAR" -H 'Content-Type: application/json' \
  -d '{"name":"codex-model-catalog-test","concurrency_limit":1}' "$BASE_URL/admin/api/api-keys" >"$KEY_JSON"

API_KEY="$(jq -r .key "$KEY_JSON")"
KEY_ID="$(jq -r .id "$KEY_JSON")"
PUBLIC_MODELS="$(curl -fsS -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/models")"
PLATFORM_MODELS="$(curl -fsS -b "$COOKIE_JAR" "$BASE_URL/admin/api/platform-models")"

EXPECTED='["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro"]'
ACTUAL="$(printf '%s' "$PUBLIC_MODELS" | jq -c '.data | map(.id) | sort')"
if [ "$ACTUAL" != "$(printf '%s' "$EXPECTED" | jq -c 'sort')" ]; then
  echo "unexpected public models: $ACTUAL" >&2
  exit 1
fi

PLATFORM_COUNT="$(printf '%s' "$PLATFORM_MODELS" | jq '.data | length')"
EXPOSED_COUNT="$(printf '%s' "$PLATFORM_MODELS" | jq '[.data[] | select(.exposed)] | length')"
if [ "$PLATFORM_COUNT" -lt 4 ] || [ "$EXPOSED_COUNT" -ne 4 ]; then
  echo "unexpected platform catalog counts: platform=$PLATFORM_COUNT exposed=$EXPOSED_COUNT" >&2
  exit 1
fi

echo "public=$ACTUAL platform_count=$PLATFORM_COUNT exposed_count=$EXPOSED_COUNT temporary_key=disabled-on-exit"
