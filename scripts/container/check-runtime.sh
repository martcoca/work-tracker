#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-work-tracker:check}"
FIXTURE_IMAGE='work-tracker-fixture:check'
FIXTURE_CONTAINER='wt-fixture-check'
APP_CONTAINER='wt-check'

cleanup() {
  docker rm -f "$APP_CONTAINER" >/dev/null 2>&1 || true
  docker rm -f "$FIXTURE_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

docker build --target fixture --tag "$FIXTURE_IMAGE" .
docker run --detach --name "$FIXTURE_CONTAINER" --publish 8080:8080 "$FIXTURE_IMAGE" >/dev/null

for _ in $(seq 1 20); do
  if docker exec "$FIXTURE_CONTAINER" wget -q -O /dev/null http://127.0.0.1:18080/packets.json >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$FIXTURE_CONTAINER" wget -q -O /dev/null http://127.0.0.1:18080/packets.json

docker run --detach --name "$APP_CONTAINER" \
  --network "container:$FIXTURE_CONTAINER" \
  --env PORT=8080 \
  --env FIREBASE_PROJECT_ID=project-synthetic \
  --env FIRESTORE_EMULATOR_HOST=127.0.0.1:18081 \
  --env PACKET_EXPORT_URL=http://127.0.0.1:18080/packets.json \
  --env TENANT_DIRECTORY_URL=http://127.0.0.1:18080/tenant-directory.json \
  --env AGENT_GRANTS_URL=http://127.0.0.1:18080/agent-grants.json \
  --env EXPORT_REFRESH_INTERVAL=30s \
  --env EXPORT_FETCH_TIMEOUT=2s \
  "$IMAGE" >/dev/null

for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
HEALTH="$(curl -fsS http://127.0.0.1:8080/healthz)"
echo
echo "health=$HEALTH"

IDENTITY="$(docker run --rm --entrypoint id "$IMAGE")"
case "$IDENTITY" in
  *'uid=0('*|*'gid=0('*)
    echo "error: image identity is root: $IDENTITY" >&2
    exit 1
    ;;
esac
echo "identity=$IDENTITY"
echo 'PASS: production container served verified runtime exports'
