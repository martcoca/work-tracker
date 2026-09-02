#!/usr/bin/env bash
# Every path doctrine forbids committing must be unignorable by accident.
#
# The organization's entrypoint says: never commit anything from config/local/, runtime/,
# or .agentic/. That was a rule an agent had to remember, and on 2026-09-02 one did not —
# a saved OpenTofu plan reached a PUBLIC repository carrying an Identity Platform OAuth
# client secret. Nothing failed, because .gitignore covered *.tfstate and not *.tfplan.
# A .tfplan is a zip archive containing tfstate: the same bytes in a different wrapper.
#
# So the rule stops being a reminder and becomes a check. This asserts the ignore file
# covers every forbidden path, and — more importantly — proves it by asking git, not by
# reading the file, so an entry that is present but overridden later still fails.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

FAILED=0

# Each probe is a path that must be ignored. They are never created; git check-ignore
# answers on the pattern alone.
while IFS='|' read -r probe why; do
  [ -n "$probe" ] || continue
  if ! git check-ignore -q "$probe" 2>/dev/null; then
    printf 'error: %s is not ignored\n' "$probe" >&2
    printf '       %s\n' "$why" >&2
    FAILED=1
  fi
done <<'PROBES'
config/local/probe|Local configuration holds real values and credentials.
runtime/probe|Runtime state is never committed; it describes what is happening now.
.agentic/probe|Worker dispatch state is machine-local and often names a real target.
probe.tfstate|State records every sensitive variable that produced it.
probe.tfplan|A saved plan is a zip containing tfstate. This is how a secret was published.
probe.tfvars|Variable files carry the values the plan would consume.
PROBES

# A tracked file that matches a forbidden pattern is worse than a missing rule: the
# ignore entry looks correct while the content is already published.
#
# config/example/ is exempt by design, not by oversight. Doctrine puts redacted examples
# there precisely so a reader can see the shape of an input without its value, and those
# files are meant to be committed. The exemption is the path, never the extension.
TRACKED="$(git ls-files -- '*.tfstate' '*.tfplan' '*.tfvars' 'config/local/*' 'runtime/*' '.agentic/*' 2>/dev/null \
  | grep -v '^config/example/' || true)"
if [ -n "$TRACKED" ]; then
  printf 'error: forbidden paths are tracked despite being ignored:\n' >&2
  printf '%s\n' "$TRACKED" | sed 's/^/       /' >&2
  printf '       Ignoring a path does not untrack it. Use git rm --cached, and treat any\n' >&2
  printf '       credential inside as published: rotate it rather than deleting the file.\n' >&2
  FAILED=1
fi

if [ "$FAILED" -ne 0 ]; then
  exit 1
fi

printf 'PASS: ignore-contract (6 forbidden patterns ignored, none tracked)\n'
