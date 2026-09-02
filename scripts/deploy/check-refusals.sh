#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK_ROOT="$(mktemp -d)"
trap 'rm -rf "$CHECK_ROOT"' EXIT
export GOCACHE="$CHECK_ROOT/go-cache"

printf '%s\n' '{"planned_values":{"root_module":{"resources":[{"address":"google_cloud_run_v2_service.reader","type":"google_cloud_run_v2_service","values":{"deletion_protection":true,"template":[{"scaling":[{"min_instance_count":1}]}]}}]}}}' \
  > "$CHECK_ROOT/non-zero-cost.json"
if COST_OUTPUT="$(GOWORK=off GOPROXY=off go -C "$ROOT" run ./internal/costguard \
  < "$CHECK_ROOT/non-zero-cost.json" 2>&1)"; then
  echo 'error: non-zero idle cost passed the cost guard' >&2
  exit 1
fi
case "$COST_OUTPUT" in
  *'configures min_instance_count=1'*) ;;
  *) printf '%s\n' "$COST_OUTPUT" >&2; exit 1 ;;
esac
printf 'PASS: non-zero idle cost refused: %s\n' "$COST_OUTPUT"

printf '%s\n' '{"resource_changes":[{"address":"google_cloud_run_v2_service.reader","type":"google_cloud_run_v2_service","change":{"actions":["delete"],"after":null}}]}' \
  > "$CHECK_ROOT/destructive-plan.json"
if DESTROY_OUTPUT="$(EXPECTED_IMAGE='registry.invalid/tracker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
  EXPECTED_COMMIT='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
  GOWORK=off GOPROXY=off go -C "$ROOT" run ./internal/deployguard \
  < "$CHECK_ROOT/destructive-plan.json" 2>&1)"; then
  echo 'error: destructive delivery passed the deploy guard' >&2
  exit 1
fi
case "$DESTROY_OUTPUT" in
  *'forbidden actions [delete]'*) ;;
  *) printf '%s\n' "$DESTROY_OUTPUT" >&2; exit 1 ;;
esac
printf 'PASS: destructive delivery refused: %s\n' "$DESTROY_OUTPUT"

if AUTHORITY_OUTPUT="$(
  PACKET_EXPORT_URL='http://127.0.0.1:1/packets.json' \
  TENANT_DIRECTORY_URL='http://127.0.0.1:1/tenant-directory.json' \
  AGENT_GRANTS_URL='http://127.0.0.1:1/agent-grants.json' \
  EXPORT_FETCH_TIMEOUT='100ms' \
  GOWORK=off GOPROXY=off \
    go -C "$ROOT" run ./cmd/verify-runtime-exports 2>&1
)"; then
  echo 'error: unreachable authority exports passed deployment verification' >&2
  exit 1
fi
case "$AUTHORITY_OUTPUT" in
  *'fetch request failed'*) ;;
  *) printf '%s\n' "$AUTHORITY_OUTPUT" >&2; exit 1 ;;
esac
printf 'PASS: unverifiable authority refused: %s\n' "$AUTHORITY_OUTPUT"
