#!/usr/bin/env bash
# Produce the existing refresh-disabled synthetic foundation plan without initializing its
# committed remote backend. A temporary copy omits only backend.tf; all resource code,
# variables, outputs, provider locks, and test inputs are unchanged.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHECK_ROOT="$(mktemp -d)"
trap 'rm -rf "$CHECK_ROOT"' EXIT

for source in "$ROOT"/infra/identity/*.tf; do
  [ "$(basename "$source")" = 'backend.tf' ] && continue
  cp "$source" "$CHECK_ROOT/"
done
cp "$ROOT/infra/identity/.terraform.lock.hcl" "$CHECK_ROOT/"

tofu -chdir="$CHECK_ROOT" init -backend=false
set +e
GOOGLE_OAUTH_ACCESS_TOKEN=synthetic-ci-not-a-credential \
  tofu -chdir="$CHECK_ROOT" plan -refresh=false -input=false -lock=false -no-color \
  -detailed-exitcode \
  -var-file="$ROOT/config/example/identity.tfvars" \
  -out="$CHECK_ROOT/foundation.tfplan"
PLAN_EXIT=$?
set -e
case "$PLAN_EXIT" in
  0|2) printf 'PASS: identity plan detailed exit %s\n' "$PLAN_EXIT" ;;
  *) printf 'error: identity plan returned forbidden exit %s\n' "$PLAN_EXIT" >&2; exit "$PLAN_EXIT" ;;
esac
tofu -chdir="$CHECK_ROOT" show -json "$CHECK_ROOT/foundation.tfplan" \
  > "$CHECK_ROOT/foundation.json"
GOWORK=off GOPROXY=off go -C "$ROOT" run ./internal/costguard < "$CHECK_ROOT/foundation.json"
