#!/usr/bin/env bash
# Founder-run, one-time migration from the existing local foundation state.
#
# This script never applies infrastructure. It first copies the complete local state into
# runtime/, moves the existing Cloud Run address into a second local state file, then asks
# OpenTofu to migrate both files into distinct prefixes of the already-versioned GCS bucket.
set -euo pipefail

: "${GCP_STATE_BUCKET:?GCP_STATE_BUCKET must name the existing versioned state bucket}"

VERSIONING="$(gcloud storage buckets describe "gs://$GCP_STATE_BUCKET" \
  --format='value(versioning_enabled)')"
if [ "$(printf '%s' "$VERSIONING" | tr '[:upper:]' '[:lower:]')" != 'true' ]; then
  echo 'error: refusing to migrate state into a bucket without Object Versioning' >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
FOUNDATION="$ROOT/infra/identity"
DELIVERY="$ROOT/infra/deploy"
FOUNDATION_STATE="$FOUNDATION/terraform.tfstate"
DELIVERY_STATE="$DELIVERY/terraform.tfstate"
MIGRATION_RECORD="$ROOT/runtime/state-migration"

if [ ! -f "$FOUNDATION_STATE" ]; then
  echo 'error: the existing infra/identity/terraform.tfstate is not present' >&2
  exit 1
fi
if [ -e "$DELIVERY_STATE" ]; then
  echo 'error: infra/deploy/terraform.tfstate already exists; refusing to overwrite it' >&2
  exit 1
fi

mkdir -p "$MIGRATION_RECORD"
cp "$FOUNDATION_STATE" "$MIGRATION_RECORD/foundation.before.tfstate"

tofu -chdir="$FOUNDATION" state mv \
  -state="$FOUNDATION_STATE" \
  -state-out="$DELIVERY_STATE" \
  google_cloud_run_v2_service.reader \
  google_cloud_run_v2_service.reader

cp "$FOUNDATION_STATE" "$MIGRATION_RECORD/foundation.split.tfstate"
cp "$DELIVERY_STATE" "$MIGRATION_RECORD/delivery.split.tfstate"

tofu -chdir="$FOUNDATION" init -input=false -migrate-state -force-copy \
  -backend-config="bucket=$GCP_STATE_BUCKET"
tofu -chdir="$DELIVERY" init -input=false -migrate-state -force-copy \
  -backend-config="bucket=$GCP_STATE_BUCKET"

if tofu -chdir="$FOUNDATION" state list | grep -Fxq 'google_cloud_run_v2_service.reader'; then
  echo 'error: Cloud Run still appears in foundation state' >&2
  exit 1
fi
if ! tofu -chdir="$DELIVERY" state list | grep -Fxq 'google_cloud_run_v2_service.reader'; then
  echo 'error: Cloud Run is missing from delivery state' >&2
  exit 1
fi

echo 'PASS: local state split without import or recreation'
echo 'PASS: destination bucket has Object Versioning enabled'
echo 'PASS: foundation state migrated to GCS prefix work-tracker/foundation'
echo 'PASS: delivery state migrated to GCS prefix work-tracker/delivery'
echo 'Backups remain under runtime/state-migration and must not be committed.'
