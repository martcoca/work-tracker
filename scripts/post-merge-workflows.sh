#!/usr/bin/env bash
# Print the workflows that must be dispatched by hand after an auto-merge.
#
# GitHub does not raise events for actions taken with the default GITHUB_TOKEN. When
# auto-merge merges a pull request, the resulting push to the default branch therefore
# triggers nothing: every `push: branches: [main]` workflow is skipped, silently, on
# exactly the commits nobody reviewed.
#
# That is the "green by absence" failure this organization keeps meeting. The gate did not
# pass — it was never asked. It is worse than the earlier cases because the skipped
# workflows are the ones reserved for the default branch: the plan against real cloud
# state, the full-length suite too slow for a pull request.
#
# Auto-merge therefore dispatches them explicitly after merging. A workflow is eligible
# when it triggers on push to the default branch AND accepts workflow_dispatch. One that
# takes push but not workflow_dispatch cannot be recovered and is a hard failure.
set -euo pipefail

DIR='.github/workflows'
MODE="${1:-list}"     # list | check
FAILED=0

for wf in "$DIR"/*.yml "$DIR"/*.yaml; do
  [ -e "$wf" ] || continue
  BASE="$(basename "$wf")"
  [ "$BASE" = 'auto-merge.yml' ] && continue

  # The trigger block runs from `on:` to the next top-level key.
  TRIGGERS="$(awk '/^on:/{f=1;next} /^[a-zA-Z_-]+:/{f=0} f' "$wf")"
  printf '%s\n' "$TRIGGERS" | grep -q '^[[:space:]]*push:' || continue

  if printf '%s\n' "$TRIGGERS" | grep -q '^[[:space:]]*workflow_dispatch:'; then
    [ "$MODE" = list ] && printf '%s\n' "$BASE"
  else
    printf 'error: %s runs on push but has no workflow_dispatch.\n' "$wf" >&2
    printf '       After an auto-merge its push never fires, and it cannot be\n' >&2
    printf '       dispatched to recover. Add workflow_dispatch: to its on: block.\n' >&2
    FAILED=1
  fi
done

[ "$FAILED" -eq 0 ] || exit 1
[ "$MODE" = check ] && printf 'PASS: post-merge-workflows (all push workflows are dispatchable)\n'
exit 0
