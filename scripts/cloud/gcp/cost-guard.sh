#!/usr/bin/env bash
set -euo pipefail

PLAN_PATH="${1:?usage: scripts/cloud/gcp/cost-guard.sh <saved-plan>}"
PLAN_DIRECTORY="$(cd "$(dirname "$PLAN_PATH")" && pwd)"
PLAN_ABSOLUTE="$PLAN_DIRECTORY/$(basename "$PLAN_PATH")"
TEMPORARY="$(mktemp)"
trap 'rm -f "$TEMPORARY"' EXIT

tofu -chdir=infra/identity show -json "$PLAN_ABSOLUTE" > "$TEMPORARY"
GOWORK=off GOPROXY=off go run ./internal/costguard < "$TEMPORARY"
