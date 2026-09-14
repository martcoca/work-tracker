# Agentic Work Tracker — Technical Specification

- **Accountable function:** CTO
- **Satisfies:** [Product Specification](product-specification.md)

## Technical outcome

A packet is authored, issued, executed and closed as a record in one product. A human signs
in and navigates initiatives to packets. An agent session authenticates with a credential
**this product issued**, reads the packet assigned to it **as a published file**, and writes
back comments and status transitions.

The tracker owns the work. **Nothing depends on it being reachable to do work.**

## Inherited portfolio constraints

From [layer 1](architecture.md#layer-1--cross-portfolio): no shared spine; interfaces are
evidence contracts rather than synchronous calls; canonical sources stay canonical; public
evidence is an explicit projection.

From [layer 2](architecture.md#layer-2--system): one deployable unit; deny by default
with authorization server-side; explicit version on every envelope; caller-supplied
idempotency on state change; evidence immutable and corrections appended; no
last-write-wins; idle cost zero; WCAG 2.2 AA on any human surface.

From [ADR-0028](decisions.md#adr-0028): GCP, Go,
Vue, Cloud Run, Firebase Hosting, Firestore, Identity Platform, workload identity, OpenTofu.

From [ADR-0005](decisions.md#adr-0005): **no shared spine.**
This is the binding constraint on this product and is treated as such below.

## System context and boundaries

```
Founder ──sign in──▶ [ Work Tracker ]  ◀──authenticate── agent session
                          │  ▲                                │
                    packets, status,                    comments, status
                    comments, history                   (best effort, may fail)
                          │
                          ▼
                 published packet exports
                          │
              ┌───────────┴───────────┐
              ▼                       ▼
        agent session            agent session
       (reads a file)           (reads a file)
```

**A session never calls this product to learn what work it has.** It reads an export. The
write path — comments and status — is a synchronous call in the other direction, and it may
fail without stopping the work.

That asymmetry is the whole architecture. It is what stops a work tracker becoming the
shared spine [ADR-0005](decisions.md#adr-0005) rejects, and
it means this product being unavailable degrades reporting rather than halting the
organization.

## Components and responsibilities

| Component | Responsibility |
|---|---|
| Packet authoring | An authorized session, or a signed-in human, composes a packet; the app writes it |
| Packet store | Append-only events: issued, taken, commented, status changed, closed |
| Projection | Current packet state per initiative and epic; droppable and rebuildable |
| Export publisher | Emits the versioned packet exports sessions read |
| Human surface | Sign in, navigate initiative → epic → packet, read history, comment |
| Session API | Authenticated read, comment, and status transition for agent principals |
| Grant validator | **Consumed from [0000](architecture.md#the-identity-product)**, not reimplemented |
| Tenant reader | Reads 0000's published tenant directory; refuses a packet naming an unknown or retired tenant |

The validator is a dependency, not a component. Reimplementing it here would create the
second divergent copy that initiative 0010 exists to prevent.

## Technology allocation

Unchanged from the prior draft and from the matrix: **GCP**, **Go** on **Cloud Run** with
minimum instances zero, **Vue** on **Firebase Hosting**, **Firestore**, **Identity Platform**
for the human, **product-issued machine credentials** for agent sessions
([ADR-0056](decisions.md#adr-0056)),
**workload identity federation** for deployment, **OpenTofu**.

Excluded from the first boundary: search service, broker, GraphQL, WebSockets, and any
general model API. A projection rebuild job is deferred until measured need.

### Hostname

**`tracker.martcoca.com`** — the human surface and the API behind it.

Parallel to `identity.martcoca.com`, which serves 0000's exports. One subdomain per product,
named for what the product is rather than for the cloud it happens to sit on: this one is on
GCP and that one on AWS, and a reader should not have to know or care.

The published packet exports are served from the same host under a stable path rather than a
second subdomain. They are the same product's output, and a consumer already has to know one
name.

**This hostname is needed before `0004-E02-T01` can be applied**, because Identity Platform
requires exact callback and logout URLs at configuration time — not at deploy time. The
DNS record and certificate are prerequisites of that packet, not of this specification.

Every component below names the packet that delivers it. A component with no packet is a
decomposition gap, and this table exists because one went unnoticed for weeks: Firestore was
specified here throughout and delivered by nothing, while the product was reported live.

```stack
Go on Cloud Run: 0004-E02-T03 0004-E05-T01
Vue on Firebase Hosting: 0004-E02-T01 0004-E05-T01
Firestore: 0004-E07-T01
Identity Platform: 0004-E02-T01
workload identity federation: 0004-E05-T01
OpenTofu: 0004-E05-T01
packet export: 0004-E05-T03 0004-E07-T02
machine access: 0004-E03-T04 0004-E03-T05 0004-E02-T06
session API: 0004-E03-T01 0004-E03-T03 0004-E03-T06
```

## Interfaces and data

### The packet record

A packet has a **frozen part** and a **mutable part**, and the split is the product's central
invariant.

| Field | Mutability |
|---|---|
| `id`, `initiative`, `epic`, `target`, `executor`, `approval`, `shape` | frozen at issue |
| `goal`, `boundary`, `done_when`, `check`, `context` | **frozen at issue** |
| `status` | mutable — `not started`, `in progress`, `done`, `blocked` |
| `comments` | append-only |
| `history` | append-only, never edited |

**Scope is frozen once a packet is issued.** Scope moving under a running session is
corrupting, and the file-based convention enforced this in CI where it has fired in anger. A
packet whose Goal changes mid-flight is a different packet; the app **supersedes** it with a
new one that names its parent, rather than editing it.

### The packet export

The contract a session reads. Same envelope as
[0000's exports](architecture.md#the-identity-product), deliberately — one envelope
shape across the portfolio is worth more than a bespoke one here.

```json
{
  "schema": "martcoca.tracker.packets/1",
  "published_at": "...",
  "expires_at": "...",
  "source":  { "repository": "...", "commit": "..." },
  "digest":  "sha256:...",
  "payload": [ { "id": "...", "status": "...", "goal": "...", "...": "..." } ]
}
```

A consumer refuses an expired export and works from nothing rather than from stale work
assignment. Freshness bound: **48 hours** ([ADR-0053](decisions.md#adr-0053)), matching 0000,
because two different bounds in one organization is a footgun. The bound is
`contract.FreshnessBound`; nothing else states it.

### Tenancy: the second export, and the one this product nearly forgot

[0000](architecture.md#the-identity-product) publishes **two** contracts. Agent
grants answer *may this principal do this*. The **tenant directory**
(`martcoca.identity.tenant-directory/1`) answers *whose work is this*, and this product needs
both.

**Every packet carries a `tenant_id`.** It is not derived from the repository, the initiative,
or the signed-in user — it is the tenant record's stable opaque id, read from the published
directory. A packet whose `tenant_id` is not in the current directory is refused at issue.

**The tracker reads the tenant directory as a file**, on the same terms as the grant export:
versioned, digest-verified, refused when stale. It never calls 0000.

| Fact | Source |
|---|---|
| Which tenants exist, their status | 0000's tenant directory export |
| Whether this session may act | 0000's agent grants export |
| Who this human is | **this product's own Identity Platform** |
| Which tenant this human belongs to | a claim on their token, checked against the directory |

### Why the human is not shared, and that is deliberate

[ADR-0046](decisions.md#adr-0046)
is explicit: human identity is per-cloud and there is **no SSO**, because the federated tiers
collapse to 50 MAU on AWS and GCP and the cost analysis rejected it. 0000 signs a member into
*itself* with Cognito; this product signs a human into *itself* with Identity Platform.

**The tenant is the only shared fact**, which is exactly what the directory is for. The same
person is two accounts, and that is the design rather than a gap in it.

So a human's token carries a tenant claim, and the tracker checks that claim against the
published directory. A token naming a tenant that does not exist, or one whose status is
`retired`, is refused — **absence and denial are distinguishable**, as 0000 requires, and a
retired tenant is published as retired rather than omitted.

### What multi-tenancy is *not* in the first boundary

There is one tenant today, and 0000's own specification says a tenant is currently "a
container with one occupant" that "earns its keep when a second one exists."

**The contract is wired; the features are not.** Every packet carries a tenant id, isolation
is enforced in the query path, and a test proves tenant A cannot reach tenant B's packets.
Tenant administration, invitations, per-tenant configuration and billing are **excluded** —
building them for one occupant is the fabrication the portfolio definition forbids.

The cost of wiring the contract now is small. The cost of retrofitting a tenant id onto an
append-only event log later is not.

### How a reader actually obtains an export

The specification said "reads a file" throughout and never said how the file gets there.
That gap surfaced when a session was asked to build the deployable image: the Cloud Run
configuration points at `/data/packets.json` and `/data/tenant-directory.json`, with no
volume, no mount and no fetch. A binary-only image would start and fail; an image with the
exports baked in would ship authority frozen at build time, under a 48-hour freshness bound.
Both are wrong, and the packet did not say which was meant because the specification had not
decided.

**Decision: a reader fetches the export over HTTPS and holds the last good copy.**

- **On start, and on a schedule thereafter**, the service fetches each export it depends on —
  this product's packet export, and 0000's tenant directory and agent grants.
- **The fetched copy is held in memory**, verified on arrival: digest checked, `expires_at`
  checked. A copy that fails either is refused and the previous good one is kept.
- **When the held copy passes its `expires_at`, the reader fails closed** on whatever that
  export authorises — it does not serve stale authority and does not silently extend it.
- **A failed fetch is not an outage.** The last good copy continues to serve until it expires,
  and the age is visible.

**This is still not a synchronous dependency.** An export is a static object on a CDN, not a
call into the product that published it. 0000 can be entirely down and this product keeps
serving from what it holds; the coupling is to a file's availability, not a service's, and it
degrades on a timer rather than instantly.

**Nothing is baked into the image.** The image carries the binary. Data arrives at runtime and
expires on schedule, which is the only arrangement consistent with a freshness bound: an export
compiled into an artifact is stale the moment the artifact is built.

### The session API

Three operations, all requiring a valid grant from 0000:

| Operation | Method | Notes |
|---|---|---|
| Read a packet | `GET` | Also available as an export; the API is the convenience, not the contract |
| Comment | `POST` | Append-only. Caller-supplied idempotency key |
| Transition status | `POST` | Legal transitions only; caller-supplied idempotency key |

Authoring — create and update a draft, issue it, supersede a packet — is open to a session on
the same terms, under `packet:author`, `packet:issue` and `packet:supersede`
([ADR-0058](decisions.md#adr-0058), [ADR-0059](decisions.md#adr-0059)).

**Deny by default.** A session presents a credential this product issued. The product
resolves it to a principal, refuses it outright if it is unknown, revoked, expired or
malformed, and records the use. Only then does the grant validator check, offline against
the published export, that the **workload** this credential acts as holds the named scope in
this tenant — `0000` is asked *may this workload do this*, never *which attempt is this*
([ADR-0057](decisions.md#adr-0057)). An
unrecognized scope is refused. **No session may transition a packet its credential is not
bound to** — that binding is this product's to enforce, because it is this product that
minted it — and **no session may edit a packet body**, a route that does not exist.

### Status transitions

`not started → in progress → done`, and any state → `blocked`. `done` is terminal; reopening
supersedes with a new packet. **A transition to `done` requires evidence to be attached** —
the field is not optional, because a status nobody substantiated is the thing this product
exists to replace.

## Security, privacy, and AI governance

Human identity is Identity Platform. Agent identity is a **machine credential this product
issues**, carrying a scope grant from `0000` for the workload it acts as
([ADR-0056](decisions.md#adr-0056),
[ADR-0057](decisions.md#adr-0057)). The
credential is high-entropy, returned exactly once at creation, and stored only as an
irreversible digest; it can be listed, revoked immediately, and seen to have been used. A
stored secret is a real cost the previous design avoided by having no working mechanism, and
it is this product's to protect.

**This product replicates no credential and holds none belonging to anything else.** It
never stores a credential for a sibling product, never a cloud key, and Claude Code and
Codex subscription credentials stay outside it entirely.

Deny by default, server-side, on every session route. Holding a grant is not authority; the
named scope is. Packet content is private to the tenant; the public projection is an explicit
allowlist and carries no packet body, comment, or principal identifier.

## Deployment and delivery

One Cloud Run service plus static hosting, deployed keyless from GitHub Actions through the
federation `platform-gcp` provides. Guarded plan before apply, per the cost guard. Idle cost
zero: minimum instances zero, Firestore free tier, no load balancer.

## Observability and operations

Correlation id spanning author → issue → take → transition → close. Structured logs with no
credential, token, packet body, or principal identifier.

The operationally interesting signal is **export freshness**, exactly as in 0000 — a session
working from a stale export is working from stale assignment, and that must be visible.

## Verification

- A packet is issued in the app, appears in an export, and a session executes it **without
  calling this product**.
- A packet naming a tenant absent from the published directory is **refused at issue**.
- A packet whose tenant is `retired` is refused, distinguishably from one whose tenant is
  unknown.
- Tenant A cannot reach tenant B's packets by any route the product exposes.
- Both of 0000's exports are consumed as **files**; the product starts and serves with 0000
  entirely unavailable.
- A session with a valid grant comments and transitions status; one with a revoked grant is
  refused within the freshness bound.
- **No route can edit a packet body**, asserted mechanically against the built routes.
- Dropping the projection and rebuilding from the event log produces an identical result.
- The service is taken entirely offline and **sessions holding a current export keep working**.
- A `done` transition without evidence is refused.
- Negative cases exist for every check, per [OM-0018](architecture.md#what-was-left-behind).

## Reliability, performance, and cost

No availability target is claimed. The design tolerates the product being unavailable, which
is what makes that acceptable — but only for reads. Writes queue in the session's own report
and are retried or reported as unsent.

Cost is zero at rest. The one real performance requirement is export freshness, and it is a
correctness requirement: a session working from a stale export is doing the wrong work.

## Technical sequence

1. Packet event model, projection, and the export contract — **local, no cloud**.
2. The export publisher, and a session reading a packet from an export.
3. The human surface: sign in, navigate initiative → epic → packet, read history.
4. Packet authoring in the app, writing to the store.
5. The session API: authenticated read, comment, status transition, consuming 0000's validator.
6. Comment and status write-back from a real session.
7. Migration: `packets/` removed from the target repositories.

**Steps 1 and 2 need no cloud account**, exactly as 0000's first steps did. **Step 5 cannot
start until 0000 publishes grants**, which is a real cross-initiative dependency and the
first one this portfolio has had.

**Step 7 is gated on evidence, not deployment** — it happens when a session has demonstrably
executed a packet delivered as an export, not when the app is live.

## Architecture decisions

- [ADR-0028](decisions.md#adr-0028) — the stack
- [ADR-0005](decisions.md#adr-0005) — no shared spine, the
  constraint that shapes the read path
- [ADR-0045](decisions.md#adr-0045)
  — why the attempt-envelope model this spec replaced no longer has a producer
- [ADR-0051](decisions.md#adr-0051) — the packet
  as the unit this product records

## Assumptions and open technical decisions

Resolved:

- ~~What does this product track?~~ **Packets**, not dispatcher attempt envelopes. The
  previous model's producer was retired by ADR-0045; the packet, its evidence and its pull
  request are what an agent attempt now produces, because that is what the organization has
  been producing all week.
- ~~Where do packets live?~~ **Here**, once live. Repository `packets/` is a stop-gap.
- ~~Does a session call this product for its work?~~ **No.** It reads an export. Writes are
  the only synchronous direction and may fail.

Still open, and honestly so:

- **How a human's tenant claim is established** at account creation. The Founder creates
  members in both products separately (ADR-0046), so the claim is set by whoever creates the
  account — and nothing yet checks the two products agree about who belongs where.
- **The scope vocabulary for session grants.** 0000 has deliberately not defined one, waiting
  for two consumers. This is the second consumer, so the vocabulary can finally be named —
  but it should be named by the two products together, not invented here.
- **Whether comments need threading.** One flat append-only list is the smaller claim and
  probably right. It earns threading when a real conversation needs it.
- **What happens to a packet whose target repository is archived.** The record outlives the
  repository, which is an argument for this product, and nothing yet says how it renders.
