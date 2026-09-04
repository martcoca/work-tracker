#!/usr/bin/env bash
# Authorized real-store proof. The worker does not run this without an existing project
# and authority to create the test's small, append-only namespace.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

: "${FIRESTORE_INTEGRATION_PROJECT_ID:?set FIRESTORE_INTEGRATION_PROJECT_ID to an existing project}"

GOWORK=off go -C "$ROOT" test ./eventstore \
  -run '^TestFirestorePersistsAndSerializesPacketEvents$' -count=1 -v
