#!/usr/bin/env bash
# Founder-run from a fresh checkout after migrate-state.sh. This verifies the state bucket
# is still versioned and that the delivery identity can acquire the backend lock, read the
# migrated service, and produce a guarded plan. It never applies the plan.
set -euo pipefail

for required in \
  GCP_STATE_BUCKET \
  TF_VAR_project_id \
  TF_VAR_region \
  TF_VAR_hosting_site_id \
  TF_VAR_runtime_service_account_name \
  TF_VAR_container_image \
  TF_VAR_source_commit
do
  if [ -z "${!required:-}" ]; then
    echo "error: $required must be set without writing it into tracked configuration" >&2
    exit 1
  fi
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHECK_ROOT="$(mktemp -d)"
trap 'rm -rf "$CHECK_ROOT"' EXIT

VERSIONING="$(gcloud storage buckets describe "gs://$GCP_STATE_BUCKET" \
  --format='value(versioning_enabled)')"
if [ "$(printf '%s' "$VERSIONING" | tr '[:upper:]' '[:lower:]')" != 'true' ]; then
  echo 'error: remote state bucket does not have Object Versioning enabled' >&2
  exit 1
fi

tofu -chdir="$ROOT/infra/deploy" init -input=false -reconfigure \
  -backend-config="bucket=$GCP_STATE_BUCKET"
tofu -chdir="$ROOT/infra/deploy" plan -input=false -lock-timeout=5m \
  -out="$CHECK_ROOT/delivery.tfplan"
tofu -chdir="$ROOT/infra/deploy" show -json "$CHECK_ROOT/delivery.tfplan" \
  > "$CHECK_ROOT/delivery.json"

EXPECTED_IMAGE="$TF_VAR_container_image" \
EXPECTED_COMMIT="$TF_VAR_source_commit" \
GOWORK=off GOPROXY=off \
  go -C "$ROOT" run ./internal/deployguard < "$CHECK_ROOT/delivery.json"

echo 'PASS: remote state bucket has Object Versioning enabled'
echo 'PASS: a fresh checkout acquired the delivery state lock and produced a guarded plan'
echo "STATE: gs://$GCP_STATE_BUCKET/work-tracker/delivery/default.tfstate"
