#!/usr/bin/env bash
# A packet marked done must arrive with its evidence file.
#
# Pull requests here merge without a human reading the diff, so the evidence file is the
# only durable account of what was actually done and what was not. The requirement lived
# in doctrine prose and in a per-repository README, and the README half of it was silently
# lost in one repository for two days without anyone noticing — which is precisely the
# argument for checking it rather than writing it down a third time.
#
# Usage: check-evidence.sh <base-ref>
set -euo pipefail

BASE_REF="${1:?usage: check-evidence.sh <base-ref>}"
BASE="$(git merge-base HEAD "$BASE_REF")"
MIN_LINES=15
FAILED=0
CHECKED=0

CHANGED="$(git diff --name-only "$BASE" HEAD -- packets/ || true)"
[ -n "$CHANGED" ] || { printf 'PASS: evidence (no packet changed)\n'; exit 0; }

while IFS= read -r path; do
  [ -n "$path" ] || continue
  [ "$(basename "$path")" = 'README.md' ] && continue

  STATUS="$(git show "HEAD:$path" 2>/dev/null | sed -n 's/^- \*\*Status:\*\* //p' | head -n 1)"
  [ "$STATUS" = 'done' ] || continue

  # Only require evidence when this pull request is what marks it done.
  WAS="$(git show "$BASE:$path" 2>/dev/null | sed -n 's/^- \*\*Status:\*\* //p' | head -n 1 || true)"
  [ "$WAS" = 'done' ] && continue

  ID="$(git show "HEAD:$path" | sed -n 's/^- \*\*Packet id:\*\* `\([^`]*\)`.*/\1/p' | head -n 1)"
  if [ -z "$ID" ]; then
    printf 'error: %s has no packet id; cannot locate its evidence\n' "$path" >&2
    FAILED=1
    continue
  fi

  CHECKED=$((CHECKED + 1))
  EV="evidence/$ID.md"
  if ! git cat-file -e "HEAD:$EV" 2>/dev/null; then
    printf 'error: %s is marked done but %s is missing.\n' "$path" "$EV" >&2
    printf '       Nobody reads this diff before it merges. Without the evidence file\n' >&2
    printf '       there is no record of what was run, or of what was not verified.\n' >&2
    FAILED=1
    continue
  fi

  LINES="$(git show "HEAD:$EV" | grep -c '' || true)"
  if [ "$LINES" -lt "$MIN_LINES" ]; then
    printf 'error: %s is %s lines; a real account needs more than a placeholder.\n' \
      "$EV" "$LINES" >&2
    printf '       State the Check you ran, its output, and what you could not verify.\n' >&2
    FAILED=1
  fi
done <<< "$CHANGED"

[ "$FAILED" -eq 0 ] || exit 1
printf 'PASS: evidence (%d packet(s) completed with evidence)\n' "$CHECKED"
