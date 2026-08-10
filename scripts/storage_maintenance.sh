#!/bin/sh
set -eu

artifact_dir=${AIV2API_ARTIFACT_DIR:-/opt/leonardo2api/artifacts}
resolved=$(readlink -f "$artifact_dir")
if [ "$resolved" != "/opt/leonardo2api/artifacts" ]; then
  echo "unexpected artifact directory: $resolved" >&2
  exit 1
fi

find "$resolved" -xdev -type f -mtime +7 -delete
find "$resolved" -xdev -depth -mindepth 1 -type d -empty -delete
docker builder prune -af --filter until=168h
docker image prune -f
journalctl --vacuum-time=14d --vacuum-size=300M
