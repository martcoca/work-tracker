#!/usr/bin/env bash
# Every workflow must declare its own top-level `permissions:` block.
#
# The organization's default workflow token is what a workflow gets when it declares
# nothing. Raising that default to `write` — which auto-merge requires — is only safe if
# no workflow is relying on the default, because then the default is a cap rather than a
# grant. This asserts exactly that, so the org setting can be widened against a check
# instead of against a promise.
set -euo pipefail

DIR='.github/workflows'
[ -d "$DIR" ] || { printf 'no %s directory\n' "$DIR"; exit 0; }

FAILED=0
COUNT=0
for wf in "$DIR"/*.yml "$DIR"/*.yaml; do
  [ -e "$wf" ] || continue
  COUNT=$((COUNT + 1))
  # A top-level key sits at column zero.
  if ! grep -q '^permissions:' "$wf"; then
    printf 'error: %s declares no top-level permissions: block\n' "$wf" >&2
    printf '       it would inherit the organization default. Declare least privilege.\n' >&2
    FAILED=1
  fi
done

[ "$COUNT" -gt 0 ] || { printf 'error: no workflows found in %s\n' "$DIR" >&2; exit 1; }
[ "$FAILED" -eq 0 ] || exit 1
printf 'PASS: workflow-permissions (%d workflows, all explicit)\n' "$COUNT"
