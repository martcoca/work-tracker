# Product Specification: Agentic Work Tracker

- **Initiative:** 0004 — Agentic Work Tracker
- **Accountable function:** CPO
- **Unique portfolio proof:** A human, Claude Code, and Codex can share a
  provenance-aware view of intent, attempts, evidence, and decisions without flattening
  their different ownership and lifecycles.

## Strategic fit

The organization already creates durable intent, immutable attempt evidence, git
history, checks, reviews, human approvals, and chief-of-staff handoffs. Those facts are
distributed across systems and are difficult to understand across provider switches or
interrupted sessions.

This product makes the relationships navigable while preserving canonical ownership.
It is useful to the organization and demonstrates agent-aware work traceability that a
generic ticket CRUD system does not.

Its category is **intent-to-evidence traceability for agent-run engineering**. It is
not positioned as a general issue tracker or agent-observability product: the former
implies ticket-workflow ownership, while the latter implies prompt, model, and runtime
telemetry that this product deliberately does not collect.

## Users and actors

| Actor | Does | Authority |
|---|---|---|
| Founder | Signs in, navigates initiatives, reads packets and their history, comments | Full, within their tenant |
| Chief-of-staff | Creates and issues packets, supersedes wrong ones, reviews what returns | Authoring and issue |
| Agent session | Authenticates, reads its packet as a file, comments, transitions status | **Only what its grant says** |
| Public reader | Sees that the product exists and what it claims | None |

**A session is a first-class actor here, not an event source.** The earlier draft said workers
"do not operate the product"; they now hold scoped credentials-free identity and write to it.
That is the change the Founder's direction made, and it is what makes this product 0000's
first real consumer.

**Nobody edits a packet body.** Not the Founder, not the chief-of-staff, not a session. A
packet whose scope was wrong is superseded, and the original stays as the record of what was
asked.

## Founder direction, 2026-08-27

The Founder set the product's shape directly, superseding the attempt-tracking framing the
earlier draft inherited from the dispatcher:

1. **Sign in as myself**, and
2. **select an initiative and see every task packet in it**, and
3. **the chief-of-staff creates task packets in the app**, and
4. **any ChatGPT or Claude Code session authenticates and can read, comment on, and update
   the status of every task packet.**

Two of those change the product materially. The earlier draft said workers "do not operate
the product"; requirement 4 makes them first-class authenticated actors. And requirement 3
makes the app a **write** surface for packets, which the earlier draft did not contemplate.

### The decision this forces: what is authoritative for a packet

**Founder direction: repository packets are a stop-gap. When this product goes live, they
are removed.** The app becomes the only home for a packet; `packets/` directories disappear
from every target repository.

That is the right end state and it removes real cost. A packet currently exists **twice** —
authored in the brain's initiative tree, copied into a target repository — and a script
exists solely to prove the copy is faithful. One home makes that whole class of drift
impossible rather than detectable.

But the file-based arrangement was doing three jobs, and the app must do all three or the
migration loses something that has already caught defects:

| What the repository gave | What the app must provide |
|---|---|
| A packet body **frozen** once a session takes it, enforced in CI | Scope immutable after issue; status and comments mutable |
| An **append-only, attributable history** — git, free | An event history that cannot be edited, only appended |
| A packet readable **with no network and no credential** | See the conflict below |

The freeze is not ceremony. Scope moving under a session mid-flight is corrupting, and the
check that enforces it has fired in anger.

### The architectural conflict, and how it resolves

If a session must call this product to learn what work it has, then **every session in the
organization acquires a synchronous dependency on one service.** That is a shared spine, and
[ADR-0005](decisions.md#adr-0005) rejects one; layer 1 of the
architecture forbids exactly this coupling. It would also mean this product being down stops
all work everywhere — the blast radius the portfolio is designed not to have.

**Resolution: the app is authoritative, and a session receives its packet as a published
export.** The same pattern [0000](architecture.md#the-identity-product) established
for authority — a versioned file with provenance, a digest, and a freshness bound, read
without calling the product that published it.

So the app owns the packet, and a session reads a file. No session calls the tracker to find
its work, the tracker never becomes a spine, and the freeze survives as a property of the
export rather than of a git diff.

**Writes are the other direction and may be synchronous.** A session commenting or moving a
status is an outbound call that can fail without stopping the work — the packet it holds
stays valid. That asymmetry is what keeps the dependency safe: reads are files, writes are
best-effort.

### Migration, which is not a footnote

`packets/` exists in five repositories today and sessions are working from it right now. It
is removed **when the app is live and a session has demonstrably executed a packet delivered
as an export** — not when the app is merely deployed. Until then the two coexist, with the
repository authoritative, because a half-migrated packet convention is worse than either
arrangement.

### What this makes true elsewhere

**This product becomes the operating model's own substrate**, not merely a view of it. When
`packets/` is removed, the way work reaches a session runs through here. That raises the bar:
a tracker that is merely useful can be unreliable, and one that dispatches the organization's
work cannot. The export pattern above is what keeps that bearable — the tracker can be down
and every session keeps working from the last export it holds.

**0004 becomes 0000's second consumer**, and the first real one. A session authenticating
with a scoped, expiring, revocable grant to read and comment on packets is exactly
acceptance scenarios 2, 3 and 4 of Identity and Tenancy — a grant honored by a product that
never calls the product that issued it, and a revocation that stops it.

Those scenarios have been recorded as *unschedulable* throughout 0000 because no second
product had code. This is that product. Neither initiative can finish its central claim
without the other, and that dependency is now real rather than aspirational.

## Problems and outcomes

The product addresses:

- loss of context across Claude Code and Codex sessions;
- ambiguity between intent, current execution, evidence, and accepted result;
- retries that can obscure or overwrite prior attempts;
- disconnected repository and GitHub evidence;
- human decisions mixed into agent events; and
- inability to explain how a product claim follows from work and checks.

The outcome is a rebuildable, provenance-aware work view where a reader can navigate
from portfolio intent to an initiative, epic, task, attempt, evidence, and decision,
with every projected fact identifying its source.

## Primary workflows

1. **Author and issue.** The chief-of-staff composes a packet against an initiative and epic,
   names its target and its tenant, and issues it. Its scope freezes at that moment.
2. **A session takes it.** The session authenticates, reads the packet from a published
   export, and moves it to `in progress`.
3. **A session reports.** It comments as it works and attaches evidence. Comments append;
   nothing overwrites.
4. **A session finishes or stops.** It transitions to `done` with evidence attached, or to
   `blocked` with what it needs — and a blocker is a record here rather than a sentence in a
   chat window.
5. **The Founder looks.** Signs in, selects an initiative, sees every packet in it and what
   is waiting on whom.
6. **Scope turns out to be wrong.** The packet is superseded by a new one naming its parent.
   The original is never edited and never deleted.

## Capabilities and requirements

### The packet, as a record

- Author a packet in the app: goal, boundary, done-when, check, context, and its initiative,
  epic and target.
- **Freeze its scope at issue.** A packet's body cannot be edited afterwards; a changed goal
  supersedes it with a new packet naming its parent.
- Carry a tenant on every packet, validated against the published tenant directory.
- Keep an append-only history — issued, taken, commented, transitioned, closed — that can be
  replayed to reconstruct any past state.

### Navigation, for a human

- Sign in, and see the initiatives that exist.
- Select an initiative and see **every packet in it**, grouped by epic, with its current
  status.
- Open a packet and read its full body, its history, and its comments.
- See what is waiting: packets blocked, packets open for review, packets nobody has taken.

### Authoring and dispatch

- The chief-of-staff **creates packets in the app**, and the app writes them.
- Issue a packet to a target repository so a session can find it.
- Supersede a packet whose scope was wrong, preserving the original as the record of what was
  asked.

### What an agent session can do

- **Authenticate** as a federated workload identity holding a scoped, expiring grant.
- **Read** the packet assigned to it — as a published file, without calling this product.
- **Comment** on a packet, appended and attributed, never overwriting.
- **Transition status** through legal states, with evidence required to reach `done`.
- Be **refused** anything outside its grant, and refused everything once the grant is revoked.

### What nothing can do

- **No route edits a packet body**, for any actor, human or agent. That route does not exist.
- No session reaches a packet outside its tenant.
- No status reaches `done` without evidence attached.

## Product-owned records and correction experience

The product owns three kinds of record, and the distinction decides what may change.

**The packet body is frozen.** Goal, boundary, done-when, check, context. Written once at
issue, never edited, by anyone. This is the record of what was asked, and a system where it
can move is a system where nobody can later tell what was asked.

**Status is a transition, not a field.** Each change is an event with an actor, a time and,
for `done`, evidence. The current status is derived from replaying them; it is never stored
as the truth.

**Comments append.** They are attributed and ordered and never overwritten. A correction is a
new comment saying what was wrong, not an edit that makes the mistake disappear.

### Correcting a packet that was wrong

**Supersede, never edit.** A new packet naming its parent, and the original closed as
superseded. The chain stays readable in both directions.

This is deliberately more awkward than editing, because the awkwardness is doing work: a
packet quietly rewritten mid-flight means a session executed something nobody can now
reconstruct. That happened often enough with files that a CI check exists to prevent it, and
this product must not reintroduce it in a nicer interface.

## First-slice experiences

### Resume Capsule

A chief-of-staff receives a compact, bounded view of the selected initiative/task,
attempt and parent/replacement chain, latest authoritative source revisions, checks and
evidence references, unresolved decisions/findings, freshness, and next permitted
action. The same representation serves Claude Code and Codex. It contains no provider
transcript and grants no write authority.

### Portfolio Review

The human navigates the full Portfolio → Initiative → Epic → Task → Attempt hierarchy,
including all attempts, provenance, evidence, contradictions, decisions, corrections,
and supersession history. Summary views never hide that a source is partial, stale,
conflicting, or unavailable.

### Degraded and offline behavior

Accepted history remains readable when a source is unavailable and carries its last
observation, coverage, and freshness. An optional redacted Resume Capsule expires after
24 hours and is clearly labelled cached. Product unavailability does not stop dispatch,
target-local evidence, git work, or chief-of-staff handoff.

## User-visible quality

- A non-expert reader can tell intent, attempt, evidence, and decision apart.
- Provenance is visible without forcing every view to expose implementation detail.
- Interrupted or unavailable source systems produce freshness/degraded indicators.
- Keyboard, semantic, responsive, and accessibility behavior applies to primary
  navigation and evidence views.
- Potentially sensitive evidence is private by default.
- A source disagreement is explainable and never represented as silent certainty.
- Every state and ingestion/reconciliation result is keyboard reachable, announced to
  assistive technology when it changes, and distinguishable without color alone.

## Boundary and non-goals

- Not a general-purpose clone of GitHub Issues, Linear, or Jira.
- Not initially the canonical owner of initiative intent, operating-model improvements,
  attempt events, git history, or GitHub checks.
- No worker dispatch, autonomous planning, or worker-to-worker communication.
- No mutation of source Markdown or immutable attempt evidence.
- No live status committed into this doctrine repository.
- No raw prompts, credentials, secret-bearing logs, or private source copied for
  convenience.
- **No ingestion from external trackers.** This product is where packets live, not a mirror of somewhere else. Linear and Jira are admitted only on observed need, and importing work from them would recreate the two-homes problem this design removes.
- No mandatory dependency for dispatch or chief-of-staff continuation.

## Public and private behavior

Private views may expose redacted operational relationships needed by the human and
chief-of-staff. Public export is a separate allowlisted projection containing only
portfolio-safe aggregates or evidence references.

Cloud identifiers, internal paths, private repository content, raw transcripts,
credentials, prompts, and detailed operational topology never enter the public
projection. A private fact is not made public merely because the product can display
it.

Raw prompts, model/command output, source text, credentials, local paths, personal
identifiers, cloud/account identifiers, and internal topology are not retained by the
private product either. Accepted structured evidence is durable by purpose; purgeable
human text may be deleted while its tombstone and digest remain. Operational logs are
metadata-only and bounded to 30 days. The first slice has no retention lock, WORM,
managed backup, or PITR.

## Success measures

- A human can explain a real task and all of its attempts without reading provider
  transcripts.
- Claude Code and Codex consume the same provider-neutral scoped context.
- Replaying the event log produces an identical projection, so no derived view can drift from the events that produced it.
- Rebuilding from append-only inputs produces equivalent views.
- Every projected fact identifies a canonical source.
- Contradictions remain visible until an authorized owner resolves them.
- The product can dogfood real work without becoming required for its own construction.

## Product acceptance scenarios

1. The chief-of-staff creates a packet in the app; it appears in the published export; a
   session executes it **without ever calling this product**.
2. A session with a valid grant comments and transitions a packet; the comment is attributed
   and appended.
3. A session's grant is revoked; **within the freshness bound** it is refused, and the refusal
   is distinguishable from "packet not found".
4. A session attempts to edit a packet body by any route the product exposes. It is refused —
   the route does not exist.
5. A session attempts to transition a packet to `done` with no evidence. Refused.
6. The product is taken **entirely offline**; sessions holding a current export keep working,
   and their comments are reported as unsent rather than lost.
7. **0000 is taken entirely offline**; this product keeps serving from the last tenant
   directory and grant exports it holds, and says how old they are.
8. A packet naming a tenant that does not exist is refused at issue; one naming a `retired`
   tenant is refused differently.
9. The Founder signs in, selects an initiative, and sees every packet in it — including one
   `blocked`, with what it needs.
10. The projection is dropped and rebuilt from the event log; the result is identical.

## Capability roadmap

| Increment | Capability |
|---|---|
| 1 | Packets as records: author, issue, freeze, append-only history — local, no cloud |
| 2 | Published packet exports a session can read as a file |
| 3 | The human surface: sign in, initiative → epic → packet, history |
| 4 | Authoring in the app, writing to the store |
| 5 | The session API: authenticate, comment, transition — **consuming 0000's grants** |
| 6 | Repository `packets/` removed, once a session has executed from an export |

Increments 1 and 2 need no cloud account. **Increment 5 cannot start until 0000 publishes
grants**, and increment 6 is gated on demonstrated execution rather than deployment.

## Assumptions and open product questions

- ADR-0031 fixes the product-owned record and correction boundary.
- Resume Capsule and Portfolio Review are the two first-slice experiences.
- Private-by-default, 24-hour offline context, explicit freshness, and continued
  repository operation define degraded behavior.
- GitHub evidence — the pull request a packet produced — is linked, not ingested. Linear or Jira is admitted only when a
  real initiative has canonical evidence there that GitHub/repository ingestion cannot
  represent; feature-parity pressure is not sufficient.
- **Founder checkpoint:** approve the public product/repository name after a fresh
  legal, registry, domain, and handle check. `IntentTrail` / `intent-trail` is the CPO
  recommendation; it is not yet the accepted brand.

The name is the only open first-slice product question. The preceding items are
accepted decisions, not prompts to reopen the definition.
