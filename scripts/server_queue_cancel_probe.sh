#!/bin/sh
set -eu

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
PROJECT_DIR="${PROJECT_DIR:-/opt/leonardo2api}"
RUN_ID="queue-probe-$(date +%Y%m%d%H%M%S)"
WORK_DIR="$(mktemp -d)"
KEY_ID=""
TASK_IDS=""
API_KEY=""

cd "$PROJECT_DIR"

db() {
  docker compose --env-file .env.server -f docker-compose.server.yml exec -T postgres \
    psql -U leonardo -d leonardo -Atc "$1"
}

cleanup() {
  for task_id in $TASK_IDS; do
    if [ -n "$API_KEY" ]; then
      curl -sS -X POST -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/tasks/$task_id/cancel" >/dev/null 2>&1 || true
    fi
  done
  if [ -n "$KEY_ID" ]; then
    db "UPDATE api_keys SET enabled=false,deleted_at=now() WHERE id=\$\$$KEY_ID\$\$" >/dev/null 2>&1 || true
  fi
  rm -f "$WORK_DIR"/*
  rmdir "$WORK_DIR" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

API_KEY="sk-$(openssl rand -hex 24)"
KEY_HASH="$(printf '%s' "$API_KEY" | sha256sum | cut -d' ' -f1)"
KEY_PREFIX="$(printf '%s' "$API_KEY" | cut -c1-12)"
KEY_ID="$(cat /proc/sys/kernel/random/uuid)"
db "INSERT INTO api_keys(id,name,key_prefix,key_hash,enabled,concurrency_limit,allowed_models,description) VALUES (\$\$$KEY_ID\$\$,\$\$$RUN_ID\$\$,\$\$$KEY_PREFIX\$\$,decode(\$\$$KEY_HASH\$\$,'hex'),true,1,'[\"gpt-image-2\"]'::jsonb,'temporary queue and cancel probe')" >/dev/null

BEFORE="$(db "SELECT sum(subscription_tokens+rollover_tokens+paid_tokens) FROM accounts")"
i=1
while [ "$i" -le 3 ]; do
  response="$WORK_DIR/create-$i.json"
  curl -fsS -o "$response" \
    -H "Authorization: Bearer $API_KEY" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: $RUN_ID-$i" \
    -d '{"model":"gpt-image-2","prompt":"A simple centered matte blue ceramic sphere on a plain white studio background, no text","size":"1024x1024","quality":"low","response_format":"url","background":"opaque","moderation":"auto","n":1}' \
    "$BASE_URL/v1/tasks/images"
  task_id="$(jq -r .id "$response")"
  TASK_IDS="$TASK_IDS $task_id"
  jq -r '"created id="+.id+" status="+.status+" queue_position="+((.queue_position // 0)|tostring)' "$response"
  i=$((i + 1))
done

sleep 2
cancelled=0
for task_id in $TASK_IDS; do
  status="$(curl -fsS -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/tasks/$task_id" | jq -r .status)"
  if [ "$status" = "queued" ]; then
    curl -fsS -X POST -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/tasks/$task_id/cancel" >/dev/null
    cancelled=$((cancelled + 1))
  fi
done
echo "cancelled_while_queued=$cancelled"

deadline=$(( $(date +%s) + 600 ))
while :; do
  terminal=0
  for task_id in $TASK_IDS; do
    status="$(curl -fsS -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/tasks/$task_id" | jq -r .status)"
    case "$status" in
      succeeded|failed|cancelled|submission_uncertain) terminal=$((terminal + 1)) ;;
    esac
  done
  [ "$terminal" -eq 3 ] && break
  [ "$(date +%s)" -ge "$deadline" ] && { echo "probe timed out" >&2; exit 1; }
  sleep 3
done

echo "task_statuses:"
db "SELECT status||'='||count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ GROUP BY status ORDER BY status"
echo "reservation_states:"
db "SELECT r.state||'='||count(*)||',estimated='||sum(r.estimated_tokens) FROM account_reservations r JOIN tasks t ON t.id=r.task_id WHERE t.api_key_id=\$\$$KEY_ID\$\$ GROUP BY r.state ORDER BY r.state"
AFTER="$(db "SELECT sum(subscription_tokens+rollover_tokens+paid_tokens) FROM accounts")"
echo "pool_balance_before=$BEFORE after=$AFTER delta=$((BEFORE - AFTER))"
