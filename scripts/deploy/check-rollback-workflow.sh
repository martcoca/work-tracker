#!/usr/bin/env bash
# Keep the manual rollback as one exact-main, keyless, compatibility-guarded release.
set -euo pipefail

WF='.github/workflows/rollback.yml'
RUNNER='scripts/deploy/rollback.mjs'
POLICY='scripts/deploy/rollback-policy.mjs'

require_literal() {
  local file="$1"
  local literal="$2"
  grep -Fq "$literal" "$file" || {
    printf 'rollback policy: %s is missing %s\n' "$file" "$literal" >&2
    exit 1
  }
}

require_literal "$WF" "github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main'"
require_literal "$WF" 'id-token: write'
require_literal "$WF" 'persist-credentials: false'
require_literal "$WF" 'fetch-depth: 0'
require_literal "$WF" 'git merge-base --is-ancestor "$TARGET_COMMIT" HEAD'
require_literal "$WF" 'workload_identity_provider: ${{ vars.GCP_WORKLOAD_IDENTITY_PROVIDER }}'
require_literal "$WF" 'service_account: ${{ vars.GCP_DEPLOYER_SERVICE_ACCOUNT }}'
require_literal "$WF" 'run: node scripts/deploy/rollback.mjs'

require_literal "$POLICY" 'release?.message === targetCommit'
require_literal "$POLICY" 'rewrite?.glob === "/api/**"'
require_literal "$POLICY" 'rewrite?.run?.serviceId === "tracker-reader"'
require_literal "$POLICY" 'rewrite?.run?.tag'
require_literal "$POLICY" 'revision?.annotations?.["source-commit"] !== expected.targetCommit'
require_literal "$RUNNER" 'method: "POST"'
require_literal "$RUNNER" 'data: { message }'

if grep -RqE 'secrets\.|BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY|gcloud run services update-traffic' \
  "$WF" "$RUNNER" "$POLICY"; then
  echo 'rollback policy: secret, key, or independent Cloud Run traffic mutation found' >&2
  exit 1
fi

POSTS="$(grep -Fc 'method: "POST"' "$RUNNER")"
[ "$POSTS" -eq 1 ] || {
  printf 'rollback policy: expected exactly one state-changing API request, found %s\n' "$POSTS" >&2
  exit 1
}

printf 'PASS: rollback is one exact-main Hosting release after commit and API coupling checks\n'

