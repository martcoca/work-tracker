# Agentic Work Tracker

You are working on one product, in this repository, and nothing else. This file is the
operating doctrine for a session here and it is self-sufficient.

## Read first

[`docs/README.md`](docs/README.md) gives the reading order. Start with
[`docs/state-of-the-product.md`](docs/state-of-the-product.md) — it says what is live, what
is broken and what is unbuilt, verified rather than recalled, and it lists the operational
traps that have each already cost someone a day.

The product is defined by [`docs/product-specification.md`](docs/product-specification.md)
and [`docs/technical-specification.md`](docs/technical-specification.md). The constraints it
inherits and the decisions that bind it are in
[`docs/architecture.md`](docs/architecture.md) and
[`docs/decisions.md`](docs/decisions.md). Nothing outside this repository is required.

## The product in one paragraph

A packet is a unit of work with a frozen scope, an append-only history, and evidence. A human
signs in to read initiatives, epics and packets and to see what agents did. An agent
authenticates with a credential this product issued and writes back comments and status
transitions. The product publishes what it knows as a verifiable file, so a reader never has
to call it.

## How work arrives

A task arrives as a written brief: a goal, a boundary, what "done" means, and the check that
proves it. Restate the goal, the boundary and the check before touching anything. **If you
cannot restate the check, you do not understand the task yet.**

Everything in the repository at the time you start is the current state, including its
defects. `docs/state-of-the-product.md` is the fastest way to learn them.

## Scope, and the line that matters

**The brief is the scope.** Work outside it is drift, not initiative.

If the brief asks for the **wrong thing** — wrong goal, wrong boundary, authority you do not
hold, a step that contradicts another step — say so and stop. Three briefs in this
product's history were impossible as written and were caught exactly this way. That is the
system working, not a delay.

**But a step that cannot be performed as written is not the same thing.** Where the intent is
clear and only the mechanics are impossible — a command that cannot run here, a file the
brief misnames, a demonstration that contradicts how the tool actually behaves — do the
nearest thing that satisfies the stated intent and say plainly what you changed and why.

The line is **authority, not difficulty**. Stop for anything irreversible, cost-incurring, or
that changes what the task is *for*. Proceed, and report, on how to carry it out.

## Never

- **Never read, print, echo or `cat` a credential.** Never read `.env`, `config/local/**`,
  `*.pem`, `*.key`, or a credentials file. To check a variable is set, test for presence:
  `[ -n "$VAR" ] && echo set`.
- **Never take an irreversible or cost-incurring action on your own authority.** No cloud
  apply, no resource creation, no deletion, no publish, no spend.
- **Never act through someone else's authenticated session.** An already-signed-in browser
  or terminal carries that person's authority at full scope with no expiry. Never possessing
  the credential is a description of the mechanism, not a defence. This happened here once.
- **Never commit anything from `config/local/`, `runtime/`, or `.agentic/`**, and never a
  saved OpenTofu plan — a `.tfplan` is a zip containing state and every variable that went
  into it.
- **Never hardcode an account id, project id, subscription id or ARN** into a tracked file.
- **Never edit a packet body** in the product model. Not as a human, not as an agent. A
  packet whose scope was wrong is superseded; the original stays as the record.
- **Never start another agent.**

## Verification

**A check is not evidence until you have made it fail.** Remove the rule and watch its test
break; if nothing breaks, the test was decorative. This is the one inherited discipline kept
from the operating model, because it has caught four real defects here — including a check
that could not fail, and a published export frozen behind a current-looking envelope.

```bash
GOWORK=off go test ./... -count=1
GOWORK=off go vet ./...
npm test
npm run build
```

**A pipeline reports the last command's status.** `curl -sf <url> | head` exits 0 on an HTTP
403. Fetch to a file and inspect it separately, or the check verifies the pipe.

**Report facts.** If a check did not run, say so. If it failed, say so and paste the output.
Success you did not confirm is worse than plain failure: failure gets handled, false success
gets built upon.

## Returning work

Commit to a branch, never to `main`. Push early and often — **a commit that exists only on
local disk dies with the session.** Two sessions here finished whole tasks and died before
pushing.

Open a pull request when the check passes. A pull request asserts two things: the work is
complete, and you ran the check yourself and it passed. If you cannot reach that state, push
what you have, mark the pull request a draft, and say exactly where you stopped.

Pushing to this repository's own `origin` is how work is returned, not an outward-facing
action. If your runtime asks permission, ask once for the whole session and name the
destination.

## When you are blocked, or waiting

Stopping is correct. Stopping *silently* is not — nobody is watching this session, and a
blocker mentioned only in conversation is gone when the window closes. **Waiting counts.** A
session paused on a permission prompt is indistinguishable from one still working; that has
happened twice here and both times a human noticed the window by chance.

1. Commit what you have so the work is not lost.
2. **Open a GitHub issue labelled `blocked`**, naming what you need and what you already
   tried. Opening an issue needs no push, so it works even when a push is what you are
   blocked on.
3. Say so in one sentence.

## Deploy and rollback

Deploy is keyless on merge to `main` and takes about three minutes. Rollback is a workflow
dispatch taking a full commit, and it moves the frontend and the API together. Neither is
yours to trigger without being asked.
