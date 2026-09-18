# Product Specification: Agentic Work Tracker

- **Initiative:** 0004 — Agentic Work Tracker
- **Accountable function:** CPO
- **Unique portfolio proof:** A human, Claude Code, and Codex can share a
  provenance-aware view of intent, attempts, evidence, and decisions without flattening
  their different ownership and lifecycles.

## Strategic fit

Agent-run engineering produces durable intent, immutable attempt evidence, git history,
checks, reviews, human approvals, and handoffs between sessions. Those facts are distributed
across systems and are difficult to understand across provider switches or interrupted
sessions.

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
| Agent session | Authenticates; authors, issues and supersedes packets; reads its packet as a file; comments; transitions status | **Only what its grant says** |
| Public reader | Sees that the product exists and what it claims | None |

**A session is a first-class actor here, not an event source.** It authenticates with a
credential this product issued and writes to the product within the scopes its workload is
granted. That is what makes this product the identity product's first real consumer.

**No kind of session is privileged.** Any session puts work in the tracker, provided it is
authenticated with a credential this product issued and its workload holds the matching scope
([ADR-0059](decisions.md#adr-0059)). What kind of session it is — which product it owns,
whether it planned the work or is executing it — is not something the product checks.

**Nobody edits a packet body.** Not the Founder, not a session. A
packet whose scope was wrong is superseded, and the original stays as the record of what was
asked.

## Why the app is authoritative for a packet

**A packet lives in one place: this product.** A packet that exists in two places — a file and a
record — needs something to prove the copies agree. One home makes that whole class of drift
impossible rather than merely detectable.

A file in a repository did three jobs well, and the app must do all three:

| What a file in a repository gave | What the app must provide |
|---|---|
| A packet body **frozen** once work starts | Scope immutable after issue; status and comments mutable |
| An **append-only, attributable history** — git, for free | An event history that cannot be edited, only appended |
| A packet readable **with no network and no credential** | A published export, below |

The freeze is not ceremony. Scope moving under a session mid-flight is corrupting.

### Reads are files, writes are calls

If a session had to call this product to learn what work it has, every session would acquire a
synchronous dependency on one service, and this product being down would stop all work
everywhere. [ADR-0005](decisions.md#adr-0005) rejects exactly that.

So **the app owns the packet, and a session reads a published export**: a versioned file with
provenance, a digest and a freshness bound — the same pattern the
[identity product](architecture.md#the-identity-product) uses for authority. No session calls
the tracker to find its work, and the freeze survives as a property of the export.

**Writes go the other way, and may fail.** A session commenting or moving a status makes an
outbound call that can fail without stopping the work; the packet it holds stays valid. Reads
are files and writes are best-effort, and that asymmetry is what keeps the dependency safe.

### Packets still held in repositories

Some packets still exist as files in a repository's `packets/` directory, and the deploy
publishes them as `repository-packets.json`, reconciled into the one export. They are retired
**when a session has demonstrably executed a packet delivered as an export** — not when the
app is merely deployed. Until then both sources exist and one export reconciles them.

### The identity product's first real consumer

A session authenticating with a scoped, expiring, revocable grant to read and comment on
packets is exactly acceptance scenarios 2, 3 and 4 of the identity product: a grant honoured by
a product that never calls the one that issued it, and a revocation that stops it. Neither
product can demonstrate that without the other.

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

1. **Author and issue.** A session holding the authoring scopes composes a packet against an
   initiative and epic, names its target and its tenant, and issues it. Its scope freezes at that moment.
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

### Authoring

- Any authenticated, authorized session **creates packets in the app**, and the app writes them.
- Issue a packet to a target repository so a session can find it.
- Supersede a packet whose scope was wrong, preserving the original as the record of what was
  asked.

### What an agent session can do

- **Authenticate** with a credential this product issued, acting as a workload that holds a
  scoped, expiring grant.
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

A session resuming work receives a compact, bounded view of the selected initiative/task,
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
24 hours and is clearly labelled cached. Product unavailability does not stop a
session's work, its evidence, git work, or handoff between sessions.

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
- Not initially the canonical owner of initiative intent, attempt events, git history, or
  GitHub checks.
- No task scheduling, autonomous planning, or session-to-session messaging.
- No mutation of source Markdown or immutable attempt evidence.
- No raw prompts, credentials, secret-bearing logs, or private source copied for
  convenience.
- **No ingestion from external trackers.** This product is where packets live, not a mirror of somewhere else. Linear and Jira are admitted only on observed need, and importing work from them would recreate the two-homes problem this design removes.
- No mandatory dependency for a session continuing its work.

## Public and private behavior

Private views may expose redacted operational relationships needed by the human and by
sessions. Public export is a separate allowlisted projection containing only
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

1. An authorized session creates a packet in the app; it appears in the published export; a
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

Increments 1 and 2 need no cloud account. Increment 5 depends on the identity product's
grants, which it now publishes, and increment 6 is gated on demonstrated execution rather than
deployment.

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
