#!/bin/sh
set -eu

BASE_URL="${LEO_SMOKE_BASE_URL:-http://127.0.0.1:18080}"
PROJECT_DIR="${LEO_SMOKE_PROJECT_DIR:-/opt/leonardo2api}"
RUN_ID="codex-seedream5-smoke-$(date +%Y%m%d%H%M%S)"
WORK_DIR="$(mktemp -d)"
KEY_ID=""

cd "$PROJECT_DIR"

db() {
  docker compose --env-file .env.server -f docker-compose.server.yml exec -T postgres \
    psql -U leonardo -d leonardo -Atc "$1" </dev/null
}

cleanup() {
  if [ -n "$KEY_ID" ]; then
    db "UPDATE api_keys SET enabled=false,deleted_at=now() WHERE id=\$\$$KEY_ID\$\$" >/dev/null 2>&1 || true
  fi
  rm -f "$WORK_DIR"/*
  rmdir "$WORK_DIR" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

ACTIVE="$(db "SELECT count(*) FROM tasks WHERE status IN ('queued','reserving','uploading','submitted','polling')")"
HELD="$(db "SELECT count(*) FROM account_reservations WHERE state='held'")"
[ "$ACTIVE" = "0" ] && [ "$HELD" = "0" ] || {
  echo "preflight_failed active=$ACTIVE held=$HELD" >&2
  exit 1
}

API_KEY="sk-$(openssl rand -hex 24)"
KEY_HASH="$(printf '%s' "$API_KEY" | sha256sum | cut -d' ' -f1)"
KEY_PREFIX="$(printf '%s' "$API_KEY" | cut -c1-12)"
KEY_ID="$(cat /proc/sys/kernel/random/uuid)"
db "INSERT INTO api_keys(id,name,key_prefix,key_hash,enabled,concurrency_limit,allowed_models,description)
  VALUES (\$\$$KEY_ID\$\$,\$\$$RUN_ID\$\$,\$\$$KEY_PREFIX\$\$,decode(\$\$$KEY_HASH\$\$,'hex'),true,1,
  '[\"seedream-5.0-pro\"]'::jsonb,'temporary Seedream 5.0 Pro production smoke test')" >/dev/null

curl -fsS -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/models" \
  | jq -e '.data | map(.id) == ["seedream-5.0-pro"]' >/dev/null

curl -fsS -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
  -d '{"model":"seedream-5.0-pro","size":"2048x1152","n":1}' \
  "$BASE_URL/v1/images/estimate" >"$WORK_DIR/estimate.json"
jq -e '.model == "seedream-5.0-pro" and .size == "2048x1152" and .unit_tokens == 45 and .estimated_tokens == 45' \
  "$WORK_DIR/estimate.json" >/dev/null

HTTP_STATUS="$(curl -sS -o "$WORK_DIR/create.json" -w '%{http_code}' \
  -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $RUN_ID" \
  -d '{"model":"seedream-5.0-pro","prompt":"A colossal neon phoenix made of liquid chrome and electric cyan plasma rising above a rain-soaked cyberpunk megacity at midnight, ancient floating temples woven between skyscrapers, holographic calligraphy and luminous street markets, cinematic anamorphic composition, dramatic volumetric lighting, ultra-detailed feathers reflecting the city, dynamic rooftop perspective, rich magenta and teal contrast, premium sci-fi concept art, breathtaking scale, crisp geometric details, no logos, no watermark","size":"2048x1152","response_format":"url","n":1}' \
  "$BASE_URL/v1/images/generations")"
[ "$HTTP_STATUS" = "202" ] || [ "$HTTP_STATUS" = "200" ] || {
  jq . "$WORK_DIR/create.json" >&2
  exit 1
}

TASK_ID="$(jq -r '.id // empty' "$WORK_DIR/create.json")"
[ -n "$TASK_ID" ] || {
  jq . "$WORK_DIR/create.json" >&2
  exit 1
}

ACCOUNT_ROW="$(db "SELECT a.id||'|'||a.name||'|'||(a.subscription_tokens+a.rollover_tokens+a.paid_tokens)
  FROM tasks t JOIN accounts a ON a.id=t.account_id WHERE t.id=\$\$$TASK_ID\$\$")"
ACCOUNT_ID="$(printf '%s' "$ACCOUNT_ROW" | cut -d'|' -f1)"
ACCOUNT_NAME="$(printf '%s' "$ACCOUNT_ROW" | cut -d'|' -f2)"
BALANCE_BEFORE="$(printf '%s' "$ACCOUNT_ROW" | cut -d'|' -f3)"

deadline=$(( $(date +%s) + 600 ))
last_status=""
while :; do
  curl -fsS -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/images/$TASK_ID" >"$WORK_DIR/task.json"
  status="$(jq -r '.status' "$WORK_DIR/task.json")"
  if [ "$status" != "$last_status" ]; then
    echo "task_status=$status progress=$(jq -r '.progress' "$WORK_DIR/task.json")"
    last_status="$status"
  fi
  case "$status" in
    succeeded|failed|cancelled|submission_uncertain) break ;;
  esac
  [ "$(date +%s)" -lt "$deadline" ] || {
    echo "smoke_timeout task_id=$TASK_ID status=$status" >&2
    exit 1
  }
  sleep 2
done

sleep 1
FINAL_ROW="$(db "SELECT t.status||'|'||t.generation_id||'|'||t.estimated_tokens||'|'||
  r.state||'|'||COALESCE(r.settled_tokens,0)||'|'||COALESCE(t.error_code,'')||'|'||COALESCE(t.error_message,'')
  FROM tasks t JOIN account_reservations r ON r.task_id=t.id WHERE t.id=\$\$$TASK_ID\$\$")"
BALANCE_AFTER="$(db "SELECT subscription_tokens+rollover_tokens+paid_tokens FROM accounts WHERE id=\$\$$ACCOUNT_ID\$\$")"
RESULT_URL="$(jq -r '.result.data[0].url // .result[0].url // empty' "$WORK_DIR/task.json")"

echo "task_id=$TASK_ID"
echo "account=$ACCOUNT_NAME"
echo "balance_before=$BALANCE_BEFORE balance_after=$BALANCE_AFTER delta=$((BALANCE_BEFORE - BALANCE_AFTER))"
echo "final=$FINAL_ROW"
echo "result_url=$RESULT_URL"

[ "$status" = "succeeded" ] || exit 1
[ "$(printf '%s' "$FINAL_ROW" | cut -d'|' -f3-5)" = "45|consumed|45" ] || exit 1
[ $((BALANCE_BEFORE - BALANCE_AFTER)) -eq 45 ] || exit 1
[ -n "$RESULT_URL" ] || exit 1
