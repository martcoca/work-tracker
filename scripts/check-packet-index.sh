#!/usr/bin/env bash
# The packet index must agree with the packets.
#
# `packets/README.md` carries a Status column, and it is what a session reads to choose its
# next work. The packet file carries the real status. Nothing kept them in step, and they
# drifted: four packets were `done` in their files while the table still offered all of them
# as `not started`. A resuming session would have redone finished work.
#
# The table is a projection of the packet files. A projection that can disagree with its
# source is worse than no projection, because it is the one people read.
set -euo pipefail

DIR='packets'
INDEX="$DIR/README.md"
[ -f "$INDEX" ] || { printf 'no %s; nothing to check\n' "$INDEX"; exit 0; }

FAILED=0
COUNT=0

for packet in "$DIR"/*.md; do
  [ -e "$packet" ] || continue
  NAME="$(basename "$packet")"
  [ "$NAME" = 'README.md' ] && continue
  ID="${NAME%.md}"

  FILE_STATUS="$(sed -n 's/^- \*\*Status:\*\* //p' "$packet" | head -n 1)"
  [ -n "$FILE_STATUS" ] || { printf 'error: %s has no Status: line\n' "$NAME" >&2; FAILED=1; continue; }

  ROW="$(grep -F "($NAME)" "$INDEX" | head -n 1 || true)"
  if [ -z "$ROW" ]; then
    printf 'error: %s is not listed in %s\n' "$NAME" "$INDEX" >&2
    printf '       a packet nobody can find in the index will not be taken.\n' >&2
    FAILED=1
    continue
  fi

  # The status is the third pipe-delimited cell.
  ROW_STATUS="$(printf '%s' "$ROW" | awk -F'|' '{gsub(/^[ \t]+|[ \t]+$/,"",$4); print $4}')"
  if [ "$ROW_STATUS" != "$FILE_STATUS" ]; then
    printf 'error: %s says %q; the index says %q\n' "$NAME" "$FILE_STATUS" "$ROW_STATUS" >&2
    printf '       a session picks work from the index, so it would act on the wrong one.\n' >&2
    FAILED=1
    continue
  fi
  COUNT=$((COUNT + 1))
done

[ "$FAILED" -eq 0 ] || exit 1
printf 'PASS: packet-index (%d packet(s) agree with the index)\n' "$COUNT"
