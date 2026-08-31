#!/usr/bin/env bash
set -euo pipefail

WORKFLOW='.github/workflows/image.yml'

require_literal() {
  local expected="$1"
  grep -Fq -- "$expected" "$WORKFLOW" || {
    echo "error: image workflow is missing required policy: $expected" >&2
    exit 1
  }
}

require_literal "github.ref == 'refs/heads/main'"
require_literal "github.event_name == 'push' || github.event_name == 'workflow_dispatch'"
require_literal 'id-token: write'
require_literal 'create_credentials_file: false'
require_literal 'push: true'
require_literal '${{ vars.GCP_PROJECT_ID }}'
require_literal '${{ vars.GCP_ARTIFACT_REPOSITORY }}'
require_literal '${{ vars.GCP_WORKLOAD_IDENTITY_PROVIDER }}'
require_literal '${{ vars.GCP_PUBLISHER_SERVICE_ACCOUNT }}'

if grep -Eq '\$\{\{[[:space:]]*secrets\.' "$WORKFLOW"; then
  echo 'error: image publication must use keyless repository variables, not a stored secret' >&2
  exit 1
fi

PUBLISH_LINE="$(grep -n '^  publish:' "$WORKFLOW" | cut -d: -f1)"
AUTH_LINE="$(grep -n 'uses: google-github-actions/auth@' "$WORKFLOW" | cut -d: -f1)"
[ -n "$PUBLISH_LINE" ] && [ -n "$AUTH_LINE" ] && [ "$AUTH_LINE" -gt "$PUBLISH_LINE" ] || {
  echo 'error: cloud authentication must remain confined to the publish job' >&2
  exit 1
}

echo 'PASS: image publication is keyless and confined to merged main revisions'
