#!/bin/sh
set -eu

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
PROJECT_DIR="${PROJECT_DIR:-/opt/leonardo2api}"
TOTAL="${TOTAL:-30}"
RUN_ID="account-queue-probe-$(date +%Y%m%d%H%M%S)"
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

ELIGIBLE="$(db "SELECT count(*) FROM accounts WHERE provider_id='leonardo' AND status='active' AND last_checked_at>now()-interval '10 minutes'")"
CAPACITY="$(db "SELECT COALESCE(sum(image_concurrency+queue_capacity),0) FROM accounts WHERE provider_id='leonardo' AND status='active' AND last_checked_at>now()-interval '10 minutes'")"
[ "$ELIGIBLE" -ge 1 ] || {
  echo "preflight_failed eligible=$ELIGIBLE capacity=$CAPACITY requested=$TOTAL" >&2
  exit 1
}

API_KEY="sk-$(openssl rand -hex 24)"
KEY_HASH="$(printf '%s' "$API_KEY" | sha256sum | cut -d' ' -f1)"
KEY_PREFIX="$(printf '%s' "$API_KEY" | cut -c1-12)"
KEY_ID="$(cat /proc/sys/kernel/random/uuid)"
db "INSERT INTO api_keys(id,name,key_prefix,key_hash,enabled,concurrency_limit,allowed_models,description) VALUES (\$\$$KEY_ID\$\$,\$\$$RUN_ID\$\$,\$\$$KEY_PREFIX\$\$,decode(\$\$$KEY_HASH\$\$,'hex'),true,$TOTAL,'[\"gpt-image-2\"]'::jsonb,'temporary 30-image account queue probe')" >/dev/null

EXPECTED_ACCEPTS="$CAPACITY"
if [ "$TOTAL" -lt "$CAPACITY" ]; then
  EXPECTED_ACCEPTS="$TOTAL"
fi
db "SELECT id||'|'||name||'|'||(subscription_tokens+rollover_tokens+paid_tokens) FROM accounts WHERE provider_id='leonardo' ORDER BY created_at" >"$WORK_DIR/before"
echo "run_id=$RUN_ID requests=$TOTAL eligible_accounts=$ELIGIBLE total_capacity=$CAPACITY expected_accepts=$EXPECTED_ACCEPTS"
echo "balances_before:"
cut -d'|' -f2- "$WORK_DIR/before"

i=1
while [ "$i" -le "$TOTAL" ]; do
  (
    curl -sS --max-time 30 -o "$WORK_DIR/response-$i.json" \
      -w '%{http_code}|%{time_total}\n' \
      -H "Authorization: Bearer $API_KEY" \
      -H 'Content-Type: application/json' \
      -H "Idempotency-Key: $RUN_ID-$i" \
      -d "{\"model\":\"gpt-image-2\",\"prompt\":\"A simple centered matte ceramic cube on a plain white studio background, request $i, no text\",\"size\":\"1024x1024\",\"quality\":\"low\",\"response_format\":\"url\",\"background\":\"opaque\",\"moderation\":\"auto\",\"n\":1}" \
      "$BASE_URL/v1/tasks/images" >"$WORK_DIR/meta-$i" || echo '000|30' >"$WORK_DIR/meta-$i"
  ) &
  i=$((i + 1))
done
wait

echo "submission_http:"
cut -d'|' -f1 "$WORK_DIR"/meta-* | sort | uniq -c | awk '{print "http_"$2"="$1}'
awk -F'|' '{sum+=$2; if(min==0 || $2<min)min=$2; if($2>max)max=$2; n++} END {printf "submission_latency_seconds min=%.3f avg=%.3f max=%.3f\n",min,sum/n,max}' "$WORK_DIR"/meta-*
echo "submission_errors:"
for response in "$WORK_DIR"/response-*.json; do
  jq -r 'select(.error.code != null) | (.error.code+"|"+.error.message)' "$response" 2>/dev/null || true
done | sort | uniq -c

CREATED="$(db "SELECT count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$")"
echo "created_tasks=$CREATED"
echo "initial_account_distribution:"
db "SELECT a.name||'='||count(*) FROM tasks t JOIN accounts a ON a.id=t.account_id WHERE t.api_key_id=\$\$$KEY_ID\$\$ GROUP BY a.name ORDER BY a.name"

deadline=$(( $(date +%s) + 1200 ))
last_snapshot=""
peak_running=0
peak_queued=0
while :; do
  snapshot="$(db "SELECT status||'='||count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ GROUP BY status ORDER BY status" | tr '\n' ' ')"
  if [ "$snapshot" != "$last_snapshot" ]; then
    echo "snapshot $(date +%H:%M:%S) $snapshot"
    last_snapshot="$snapshot"
  fi
  running="$(db "SELECT count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ AND status IN ('reserving','uploading','submitted','polling')")"
  queued="$(db "SELECT count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ AND status='queued'")"
  [ "$running" -le "$peak_running" ] || peak_running="$running"
  [ "$queued" -le "$peak_queued" ] || peak_queued="$queued"
  terminal="$(db "SELECT count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ AND status IN ('succeeded','failed','cancelled','submission_uncertain')")"
  [ "$terminal" -eq "$CREATED" ] && break
  [ "$(date +%s)" -lt "$deadline" ] || {
    echo "probe_timeout terminal=$terminal created=$CREATED" >&2
    break
  }
  sleep 3
done

echo "peak_running=$peak_running peak_queued=$peak_queued"
echo "final_statuses:"
db "SELECT status||'='||count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ GROUP BY status ORDER BY status"
echo "final_by_account:"
db "SELECT a.name||'|'||t.status||'='||count(*) FROM tasks t JOIN accounts a ON a.id=t.account_id WHERE t.api_key_id=\$\$$KEY_ID\$\$ GROUP BY a.name,t.status ORDER BY a.name,t.status"
echo "reservations:"
db "SELECT r.state||'='||count(*)||'|estimated='||sum(r.estimated_tokens) FROM account_reservations r JOIN tasks t ON t.id=r.task_id WHERE t.api_key_id=\$\$$KEY_ID\$\$ GROUP BY r.state ORDER BY r.state"
echo "errors:"
db "SELECT COALESCE(NULLIF(error_code,''),'none')||'|'||COALESCE(NULLIF(error_message,''),'none')||'='||count(*) FROM tasks WHERE api_key_id=\$\$$KEY_ID\$\$ AND status<>'succeeded' GROUP BY error_code,error_message ORDER BY count(*) DESC"

echo "balances_after:"
while IFS='|' read -r account_id account_name before; do
  after="$(db "SELECT subscription_tokens+rollover_tokens+paid_tokens FROM accounts WHERE id=\$\$$account_id\$\$")"
  echo "$account_name|before=$before|after=$after|delta=$((before - after))"
done <"$WORK_DIR/before"
