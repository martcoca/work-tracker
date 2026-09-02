#!/usr/bin/env bash
set -euo pipefail

WORKFLOW='.github/workflows/deploy.yml'
TRUST='infra/trust/main.tf'

require_literal() {
  local expected="$1"
  local path="${2:-$WORKFLOW}"
  grep -Fq -- "$expected" "$path" || {
    echo "error: $path is missing required delivery policy: $expected" >&2
    exit 1
  }
}

require_literal "github.ref == 'refs/heads/main'"
require_literal "github.event_name == 'push' || github.event_name == 'workflow_dispatch'"
require_literal 'id-token: write'
require_literal 'contents: read'
require_literal 'create_credentials_file: false'
require_literal 'create_credentials_file: true'
require_literal '${{ vars.GCP_PUBLISHER_SERVICE_ACCOUNT }}'
require_literal '${{ vars.GCP_DEPLOYER_SERVICE_ACCOUNT }}'
require_literal 'go run ./cmd/verify-runtime-exports'
require_literal 'go run ./internal/deployguard'
require_literal 'firebase deploy --only hosting'
require_literal 'source-commit-$GITHUB_SHA.txt'

require_literal 'var.repository_ref == "refs/heads/main"' 'infra/trust/variables.tf'
require_literal 'roles/run.developer' "$TRUST"
require_literal 'roles/artifactregistry.reader' "$TRUST"
require_literal 'roles/iam.serviceAccountUser' "$TRUST"
require_literal 'roles/storage.objectAdmin' "$TRUST"
require_literal 'roles/serviceusage.apiKeysViewer' "$TRUST"

if grep -rEq 'secrets\.|BEGIN [A-Z ]*PRIVATE KEY|service_account.*\.json' .github/workflows; then
  echo 'error: workflows must contain no stored-secret reference or key material' >&2
  exit 1
fi

line_of() {
  grep -nF -- "$1" "$WORKFLOW" | head -n 1 | cut -d: -f1
}

BUILD_LINE="$(line_of 'docker build --tag "$IMAGE_TAG" .')"
SCAN_LINE="$(line_of 'scripts/container/check-image.sh "$IMAGE_TAG"')"
PUSH_LINE="$(line_of 'docker push "$IMAGE_TAG"')"
VERIFY_LINE="$(line_of 'go run ./cmd/verify-runtime-exports')"
GUARD_LINE="$(line_of 'go run ./internal/deployguard')"
APPLY_LINE="$(line_of 'tofu -chdir=infra/deploy apply')"
HOSTING_LINE="$(line_of 'firebase deploy --only hosting')"

if [ -z "$BUILD_LINE" ] || [ -z "$SCAN_LINE" ] || [ -z "$PUSH_LINE" ] ||
   [ -z "$VERIFY_LINE" ] || [ -z "$GUARD_LINE" ] || [ -z "$APPLY_LINE" ] || [ -z "$HOSTING_LINE" ]; then
  echo 'error: ordered delivery steps could not be located' >&2
  exit 1
fi
if ! [ "$BUILD_LINE" -lt "$SCAN_LINE" ] || ! [ "$SCAN_LINE" -lt "$PUSH_LINE" ] ||
   ! [ "$PUSH_LINE" -lt "$VERIFY_LINE" ] || ! [ "$VERIFY_LINE" -lt "$GUARD_LINE" ] ||
   ! [ "$GUARD_LINE" -lt "$APPLY_LINE" ] || ! [ "$APPLY_LINE" -lt "$HOSTING_LINE" ]; then
  echo 'error: delivery must build, scan, push, verify, guard, apply, then release Hosting' >&2
  exit 1
fi

if [ "$(grep -Fc 'docker build --tag "$IMAGE_TAG" .' "$WORKFLOW")" -ne 1 ]; then
  echo 'error: the production image must be built exactly once in the deploy workflow' >&2
  exit 1
fi
if grep -Fq 'docker/build-push-action' "$WORKFLOW"; then
  echo 'error: deploy must push the inspected local image, not rebuild it in a push action' >&2
  exit 1
fi

echo 'PASS: deploy is keyless, exact-main, ordered, guarded, and digest-preserving'
