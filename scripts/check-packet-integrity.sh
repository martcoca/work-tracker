#!/usr/bin/env bash
# A packet is the frozen record of what was asked. A session may set `Status:` and
# nothing else, so that scope cannot move under the work mid-flight.
#
# This compares packets/ against the merge base and fails if anything other than a
# Status: line changed. It is the contract between whoever wrote the packet and whoever
# executes it, and it is the one rule that must hold before a pull request can merge
# without a human reading it.
#
# Usage: check-packet-integrity.sh <base-ref>
set -euo pipefail

BASE_REF="${1:?usage: check-packet-integrity.sh <base-ref>}"
BASE="$(git merge-base HEAD "$BASE_REF")"
FAILED=0

CHANGED="$(git diff --name-only "$BASE" HEAD -- packets/ || true)"
if [ -z "$CHANGED" ]; then
  printf 'PASS: packet-integrity (no packet changed)\n'
  exit 0
fi

while IFS= read -r path; do
  [ -n "$path" ] || continue
  NAME="$(basename "$path")"

  # README.md is the packet index, not a packet. It is maintained alongside the queue and
  # is expected to change whenever the queue does.
  [ "$NAME" = "README.md" ] && continue

  # A brand-new packet is added by whoever writes packets, not by a session. Adding one
  # is legitimate; the check below only constrains modifications to an existing packet.
  if ! git cat-file -e "$BASE:$path" 2>/dev/null; then
    printf 'note: %s is newly added\n' "$NAME"
    continue
  fi

  # Everything except the Status: line must be byte-identical.
  if ! diff -u \
      <(git show "$BASE:$path" | grep -v '^- \*\*Status:\*\* ') \
      <(git show "HEAD:$path" | grep -v '^- \*\*Status:\*\* ') \
      > /tmp/packet-integrity.$$ 2>&1; then
    printf 'error: %s changed outside its Status: line\n' "$NAME" >&2
    printf '       a packet body is the record of what was asked and is frozen.\n' >&2
    sed -n '3,25p' /tmp/packet-integrity.$$ >&2
    FAILED=1
  fi
  rm -f /tmp/packet-integrity.$$

  STATUS="$(git show "HEAD:$path" | sed -n 's/^- \*\*Status:\*\* //p' | head -n 1)"
  case "$STATUS" in
    'not started'|'in progress'|'done'|'blocked') ;;
    '') printf 'error: %s has no Status: line\n' "$NAME" >&2; FAILED=1 ;;
    *) printf 'error: %s has invalid status: %s\n' "$NAME" "$STATUS" >&2; FAILED=1 ;;
  esac
done <<< "$CHANGED"

[ "$FAILED" -eq 0 ] || exit 1
printf 'PASS: packet-integrity\n'
