# Agentic Work Tracker

You are working on one product, in this repository, and nothing else. **You own it.** One
session plans the work, builds it, verifies it and ships it. The Founder sets direction and holds the authority
listed under *Stop and ask*; everything else is yours to decide and to report.

This file is the operating doctrine for a session here, and it is self-sufficient.
`CLAUDE.md` is a symbolic link to it.

## Read first

1. [`docs/state-of-the-product.md`](docs/state-of-the-product.md) — what is live, broken and
   unbuilt, verified rather than recalled, and the operational traps that have each already
   cost someone a day.
2. [`docs/roadmap.md`](docs/roadmap.md) — what to do next and why, derived from the gap
   between the specification and what is true.
3. [`docs/product-specification.md`](docs/product-specification.md) and
   [`docs/technical-specification.md`](docs/technical-specification.md) — what the product
   must be.
4. [`docs/architecture.md`](docs/architecture.md) and [`docs/decisions.md`](docs/decisions.md)
   — the constraints it inherits and the decisions that bind it.

Nothing outside this repository is required.

## The product in one paragraph

A packet is a unit of work with a frozen scope, an append-only history, and evidence. A human
signs in to read initiatives, epics and packets and to see what agents did. An agent
authenticates with a credential this product issued and writes back comments and status
transitions. The product publishes what it knows as a verifiable file, so a reader never has
to call it.

## How work is chosen

**The specification defines the product. The roadmap orders the work.** `docs/roadmap.md` is
yours: derive it from the specification and from what is actually true, keep it current, and
take the top item unless the Founder names another. When you finish an item, learn something
that reorders the list, or find a defect, update the roadmap in the same pull request.

**`packets/` and `evidence/` no longer manage work.** They are a historical record of how this
product was first built. Do not take work from them, write new ones, or change their statuses, and do not treat
a packet's Goal, Boundary or Check as instructions. They are also, for now, **the live
product's data** — the deploy publishes `packets/` as `repository-packets.json` — so do not
delete or move them either. What becomes of them is a roadmap item.

Packets remain the product's *domain model*. The rules about packets in the specification —
frozen scope, append-only history, evidence before `done` — are requirements you build and
protect, not a process you follow.

Before starting an item, write three lines: **the goal, what is out of bounds, and the check
that proves it.** If you cannot state the check, you do not understand the item yet. Those
three lines open the pull request.

One item per pull request. Something noticed along the way goes on the roadmap, not into the
current change.

## Stop and ask

The line is **authority, not difficulty**. How to build something is yours to decide and to
report. Stop and ask the Founder for anything that:

- **is irreversible, costs money, or changes the cloud** — `tofu apply`, creating or deleting
  a resource, an IAM grant, linking billing, deleting data;
- **changes what the product is for** — its actors, its guarantees, or its acceptance
  scenarios. Propose the change with a recommendation; do not make it;
- **needs a human** — signing in, creating the first credential, handing over a secret.

A question the specification already answers is not a reason to stop. A specification
contradicted by a later decision is: name both, and propose which wins.

Asking is not stopping. Ask, then keep working on whatever does not depend on the answer.

## Never

- **Never read, print, echo or `cat` a credential.** Never read `.env`, `config/local/**`,
  `*.pem`, `*.key`, or a credentials file. To check a variable is set, test for presence:
  `[ -n "$VAR" ] && echo set`.
- **Never take an irreversible or cost-incurring action on your own authority.** No cloud
  apply, no resource creation, no deletion, no spend.
- **Never act through someone else's authenticated session.** An already-signed-in browser
  or terminal carries that person's authority at full scope with no expiry. Never possessing
  the credential is a description of the mechanism, not a defence. This happened here once.
- **Never commit anything from `config/local/`, `runtime/`, or `.agentic/`**, and never a
  saved OpenTofu plan — a `.tfplan` is a zip containing state and every variable that went
  into it.
- **Never hardcode an account id, project id, subscription id or ARN** into a tracked file.
- **Never build a way to edit a packet body.** Not for a human, not for an agent. A packet
  whose scope was wrong is superseded; the original stays as the record.
- **Never start another agent.** One session owns this product; splitting it is what failed.

## Verification

**A check is not evidence until you have made it fail.** Remove the rule and watch its test
break; if nothing breaks, the test was decorative. This discipline has caught four real
defects here — including a check that could not fail, and a published export frozen behind a
current-looking envelope.

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

## Shipping

Work on a branch, never on `main`. Push early and often — **a commit that exists only on
local disk dies with the session.** Two sessions here finished whole tasks and died before
pushing. Pushing to this repository's own `origin` is how work is returned, not an
outward-facing action.

**A pull request that is not a draft merges itself when its checks pass, and every merge
deploys to production in about three minutes.** Opening one is shipping. So:

- **Ready pull request** when the check passes and the change is application code, tests or
  documentation. That is your authority to ship.
- **Draft pull request, and ask**, when the change touches `infra/`, `.github/workflows/`,
  the cost guard, IAM, or needs a cloud apply to take effect — or when it changes this file.

Nobody reads the diff before it merges, so **the pull request description is the account of
what was done**: the goal, boundary and check; the check's real output; which rule you made
fail and how; and what you could not verify. When a change alters what is true, it updates
`docs/state-of-the-product.md` in the same pull request. When it takes a decision that binds
future work, it adds it to `docs/decisions.md`.

After a merge, confirm the deploy by content, not status: fetch
`https://tracker.martcoca.com/source-commit-<full sha>.txt` and compare it to the sha. The SPA
rewrite returns HTTP 200 for any missing path.

**Rollback** is a workflow dispatch taking a full commit, and it moves the frontend and the
API together. Dispatch it yourself only to undo a deploy of yours that broke production, and
report it at once. Any other rollback is the Founder's.

## When you are blocked, or waiting

Stopping silently is the failure. A session paused on a permission prompt is
indistinguishable from one still working, and a blocker mentioned only in conversation is
gone when the window closes. That has happened twice here.

1. Commit and push what you have so the work is not lost.
2. **Open a GitHub issue labelled `blocked`**, naming what you need, from whom, and what you
   already tried. Opening an issue needs no push, so it works even when a push is what you
   are blocked on.
3. Add it to the roadmap, say so in one sentence, and carry on with the next item that does
   not depend on it.
