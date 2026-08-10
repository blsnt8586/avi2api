#!/bin/sh
set -eu

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
PROJECT_DIR="${PROJECT_DIR:-/opt/leonardo2api}"
ENV_FILE="${ENV_FILE:-$PROJECT_DIR/.env.server}"
TOTAL="${TOTAL:-30}"
TARGET_ACCOUNT="${TARGET_ACCOUNT:-leonardo-main}"
RUN_ID="probe-$(date +%Y%m%d%H%M%S)"
WORK_DIR="$(mktemp -d)"
KEY_ID=""
DISABLED_IDS=""
TIMER_WAS_ACTIVE="false"

cd "$PROJECT_DIR"

db() {
  docker compose --env-file .env.server -f docker-compose.server.yml exec -T postgres \
    psql -U leonardo -d leonardo -Atc "$1"
}

cleanup() {
  for account_id in $DISABLED_IDS; do
    db "UPDATE accounts SET status='active',cooldown_until=NULL,last_error='' WHERE id=\$\$$account_id\$\$ AND status='disabled'" >/dev/null 2>&1 || true
  done
  if [ "$TIMER_WAS_ACTIVE" = "true" ]; then
    systemctl start leonardo-session-sync.timer >/dev/null 2>&1 || true
  fi
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
db "INSERT INTO api_keys(id,name,key_prefix,key_hash,enabled,concurrency_limit,allowed_models,description) VALUES (\$\$$KEY_ID\$\$,\$\$$RUN_ID\$\$,\$\$$KEY_PREFIX\$\$,decode(\$\$$KEY_HASH\$\$,'hex'),true,$TOTAL,'[\"gpt-image-2\"]'::jsonb,'temporary image concurrency probe')" >/dev/null

if systemctl is-active --quiet leonardo-session-sync.timer; then
  TIMER_WAS_ACTIVE="true"
fi
systemctl stop leonardo-session-sync.timer
DISABLED_IDS="$(db "SELECT id FROM accounts WHERE name<>\$\$$TARGET_ACCOUNT\$\$ AND status='active'")"
db "UPDATE accounts SET status='disabled' WHERE name<>\$\$$TARGET_ACCOUNT\$\$ AND status='active'" >/dev/null

TARGET_ID="$(db "SELECT id FROM accounts WHERE name=\$\$$TARGET_ACCOUNT\$\$")"
BEFORE="$(db "SELECT subscription_tokens+rollover_tokens+paid_tokens FROM accounts WHERE id=\$\$$TARGET_ID\$\$")"
echo "run_id=$RUN_ID target=$TARGET_ACCOUNT requests=$TOTAL balance_before=$BEFORE"

i=1
while [ "$i" -le "$TOTAL" ]; do
  (
    curl -sS --max-time 600 -o "$WORK_DIR/response-$i.json" \
      -w '%{http_code}|%{time_total}\n' \
      -H "Authorization: Bearer $API_KEY" \
      -H 'Content-Type: application/json' \
      -H "Idempotency-Key: $RUN_ID-$i" \
      -d '{"model":"gpt-image-2","prompt":"A simple centered matte red ceramic cube on a plain white studio background, no text","size":"1024x1024","quality":"low","response_format":"url","background":"opaque","moderation":"auto","n":1}' \
      "$BASE_URL/v1/images/generations" >"$WORK_DIR/meta-$i" || echo '000|600' >"$WORK_DIR/meta-$i"
  ) &
  i=$((i + 1))
done
wait

cat "$WORK_DIR"/meta-* | cut -d'|' -f1 | sort | uniq -c | awk '{print "http_"$2"="$1}'
awk -F'|' '{sum+=$2; if(min==0 || $2<min)min=$2; if($2>max)max=$2; n++} END {printf "latency_seconds_min=%.3f avg=%.3f max=%.3f\n",min,sum/n,max}' "$WORK_DIR"/meta-*
for response in "$WORK_DIR"/response-*.json; do
  jq -r 'select(.error.code != null) | .error.code' "$response" 2>/dev/null || true
done | sort | uniq -c | awk '{print "error_"$2"="$1}'
echo "error_messages:"
for response in "$WORK_DIR"/response-*.json; do
  jq -r 'select(.error.message != null) | .error.message' "$response" 2>/dev/null || true
done | sort | uniq -c

echo "task_statuses:"
db "SELECT status||'='||count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ GROUP BY status ORDER BY status"
echo "reservation_states:"
db "SELECT r.state||'='||count(*)||',estimated='||sum(r.estimated_tokens) FROM account_reservations r JOIN tasks t ON t.id=r.task_id WHERE t.api_key_id=\$\$$KEY_ID\$\$ GROUP BY r.state ORDER BY r.state"
echo "task_costs:"
db "SELECT COALESCE(tokens_before::text,'')||'|'||COALESCE(tokens_after::text,'')||'|'||estimated_tokens||'|'||count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ GROUP BY tokens_before,tokens_after,estimated_tokens ORDER BY tokens_before NULLS LAST"

AFTER="$(db "SELECT subscription_tokens+rollover_tokens+paid_tokens FROM accounts WHERE id=\$\$$TARGET_ID\$\$")"
echo "balance_after=$AFTER balance_delta=$((BEFORE - AFTER))"
