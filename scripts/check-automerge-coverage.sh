#!/usr/bin/env bash
# Auto-merge must wait for every workflow in this repository.
#
# `workflow_run` needs an explicit `workflows:` list. Omitting it does not mean "all" —
# it matches nothing, and auto-merge silently never fires. That failure is invisible:
# the pull request simply sits green and unmerged, and it took a live test to notice.
#
# The list therefore has to be maintained by hand, so this asserts it is complete. A
# packet that adds a workflow and forgets to list it fails here instead of quietly
# disabling the merge gate — or worse, merging before its checks have run.
set -euo pipefail

DIR='.github/workflows'
AM="$DIR/auto-merge.yml"
[ -f "$AM" ] || { printf 'error: %s is missing\n' "$AM" >&2; exit 1; }

LISTED="$(sed -n 's/^[[:space:]]*workflows:[[:space:]]*\[\(.*\)\].*/\1/p' "$AM" | head -n 1)"
if [ -z "$LISTED" ]; then
  printf 'error: %s has no inline `workflows: [...]` list.\n' "$AM" >&2
  printf '       An absent list matches nothing and auto-merge never fires.\n' >&2
  exit 1
fi

FAILED=0
COUNT=0
for wf in "$DIR"/*.yml "$DIR"/*.yaml; do
  [ -e "$wf" ] || continue
  NAME="$(sed -n 's/^name:[[:space:]]*//p' "$wf" | head -n 1 | sed 's/^"\(.*\)"$/\1/')"
  [ -n "$NAME" ] || { printf 'error: %s has no top-level name:\n' "$wf" >&2; FAILED=1; continue; }
  [ "$wf" = "$AM" ] && continue        # auto-merge must not wait on itself
  COUNT=$((COUNT + 1))
  case "$LISTED" in
    *"\"$NAME\""*|*"'$NAME'"*) ;;
    *)
      printf 'error: workflow "%s" (%s) is not in auto-merge.yml workflows: [...]\n' \
        "$NAME" "$wf" >&2
      printf '       auto-merge would merge without waiting for it.\n' >&2
      FAILED=1
      ;;
  esac
done

[ "$FAILED" -eq 0 ] || exit 1
printf 'PASS: automerge-coverage (%d workflow(s) awaited)\n' "$COUNT"
