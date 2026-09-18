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

From [ADR-0061](decisions.md#adr-0061): AWS — Go on Lambda behind an API Gateway HTTP API,
Angular on S3 and CloudFront, Cognito, GitHub OIDC for deployment, OpenTofu. **The datastore
is open, pending research.**

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

The target is AWS ([ADR-0061](decisions.md#adr-0061)). The product runs on GCP today and stays
there until the AWS deployment reaches parity.

| Concern | Target on AWS | Today, on GCP |
|---|---|---|
| API | Go on AWS Lambda, on-demand, no provisioned concurrency, behind an API Gateway HTTP API | Go on Cloud Run, minimum instances zero |
| Frontend | Angular, built static, in a private S3 bucket served by CloudFront with Origin Access Control | Vue on Firebase Hosting |
| Human identity | This product's own Cognito user pool, Lite plan, hosted sign-in, authorization code with PKCE, no self-registration | Identity Platform |
| Human routes at the edge | API Gateway JWT authorizer; application authorization stays server-side and deny-by-default | in the service |
| Machine identity | Credentials this product issues, authenticated in the service ([ADR-0056](decisions.md#adr-0056)) | the same |
| Published exports | Static objects in S3 behind the same distribution, short-cached | Firebase Hosting |
| Datastore | **Open — see below** | Firestore |
| Deploy identity | GitHub Actions OIDC assuming product-scoped IAM roles, bound to this repository and `refs/heads/main` | GCP workload identity federation |
| Infrastructure | OpenTofu, guarded plan before apply | OpenTofu |

Excluded: provisioned concurrency and any resident compute, a load balancer, Route 53, a search
service, a broker, GraphQL, WebSockets, and any general model API. A projection rebuild job is
deferred until measured need.

**Lambda fits the design rather than merely tolerating it.** The service already assumes no
warm process: it verifies every export it holds on arrival, fails closed on expiry, and renews
its own export ([ADR-0060](decisions.md#adr-0060)). A Lambda environment is that lifecycle made
explicit.

**Two things change shape, and must be designed rather than ported:**

- **Export renewal.** ADR-0060 renews the export on an interval while an instance is alive.
  Lambda keeps nothing alive between requests, so renewal becomes a finite function invoked on
  a schedule — EventBridge Scheduler invoking it directly, which the AWS platform permits when
  time is the real trigger. That also closes the gap where an export expires after two days
  with no visitor.
- **Machine credentials at the edge.** The JWT authorizer validates Cognito tokens for humans.
  A product-issued credential is not a Cognito token, so agent routes authenticate in the
  service, and no route may be reachable by a credential it was not written to accept.

### The datastore — open, pending research

The AWS platform baseline makes S3 the only durable store and forbids every database,
DynamoDB included. That rule was written for products with no transactional state. This one
has it, so the choice is made by research rather than by default.

**What the store must do:**

- Append to a packet's event log with **one-winner concurrency**: two writers at the same
  version produce exactly one accepted event and one explicit conflict, never a silent merge.
- List packets by initiative and epic, and rebuild the projection from the log.
- Look up a credential by id and record its use in the same transaction as the revocation and
  expiry check, so a concurrent revocation is observed rather than raced.
- Honour caller-supplied idempotency keys.
- Cost nothing at rest, and run locally in tests without a cloud account.

**The candidates:**

| | Access patterns | Platform rules | At rest |
|---|---|---|---|
| DynamoDB, on-demand | Native: conditional writes for concurrency, queries by key | Requires amending the baseline | No hourly charge; storage and requests |
| S3 with conditional writes | Concurrency through `If-None-Match` and `If-Match`; listing needs maintained index objects | Allowed today | No hourly charge; storage and requests |

The research establishes, for each: the concurrency guarantee under real contention, the cost
of listing and of a full projection rebuild, the credential-authentication transaction, local
testability, and the actual idle and per-request cost at this product's volume, checked against
current first-party pricing. **Nothing migrates until it is decided**, and the decision is
recorded in `decisions.md`.

### Hostname

**`tracker.martcoca.com`** — the human surface and the API behind it, with the published
packet exports under a stable path on the same host. Cloudflare stays authoritative for DNS and
points the name at CloudFront, with an ACM certificate in `us-east-1`.

One subdomain per product, named for what the product is rather than for the cloud it sits on,
parallel to `identity.martcoca.com`. A reader should not have to know which cloud serves
either.

Cognito, like any OIDC provider, needs exact callback and sign-out URLs when the app client is
configured, so the hostname is a prerequisite of the identity integration, not a detail of
deployment.

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
| Who this human is | **this product's own Cognito user pool** |
| Which tenant this human belongs to | a claim on their token, checked against the directory |

### Why the human is not shared, and that is deliberate

[ADR-0046](decisions.md#adr-0046)
is explicit: human identity is per-cloud and there is **no SSO**, because the federated tiers
collapse to 50 MAU on AWS and GCP and the cost analysis rejected it. 0000 signs a member into
*itself* with its own Cognito pool; this product signs a human into *itself* with a separate one.

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

"Reads a file" says nothing about how the file gets there. An early deployable image pointed
at local paths with no volume, no mount and no fetch: a build without the exports would start
and fail, and one with the exports baked in would ship authority frozen at build time, under a
48-hour freshness bound. Both are wrong.

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

**Nothing is baked into the artifact.** The artifact carries the binary. Data arrives at runtime and
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

Human identity is this product's Cognito user pool. Agent identity is a **machine credential this product
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

**Today:** one Cloud Run service plus Firebase Hosting, deployed keyless on merge to `main`
through GitHub Actions and GCP workload identity federation, with a guarded plan before apply
and a rollback that moves the frontend and the API together.

**Target:** Lambda functions behind an API Gateway HTTP API, and the Angular build in a private
S3 bucket behind CloudFront, deployed keyless on merge through GitHub Actions OIDC into
product-scoped IAM roles bound to this repository and `refs/heads/main`. Each function is an
immutable versioned archive promoted through an alias, so a rollback moves the alias and the
frontend release together. The AWS account boundary, the OIDC provider and the cost guard
belong to the platform; this product owns its own stack.

Idle cost stays zero: on-demand Lambda with no provisioned concurrency, no load balancer, and
nothing resident.

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
- Every check has a negative case, because a check is not evidence until it has been made to fail.

## Reliability, performance, and cost

No availability target is claimed. The design tolerates the product being unavailable, which
is what makes that acceptable — but only for reads. Writes queue in the session's own report
and are retried or reported as unsent.

Cost is zero at rest. The one real performance requirement is export freshness, and it is a
correctness requirement: a session working from a stale export is doing the wrong work.

## Technical sequence

The product is live on GCP and its domain is built. What remains is the move to AWS, and the
gaps [`roadmap.md`](roadmap.md) tracks.

1. **Research and decide the datastore.** Only step 4 depends on the answer.
2. **Keep building provider-neutral product work** on the current deployment — the session
   write-back API, machine authoring, Founder comments — in Go packages that carry over
   unchanged.
3. **The AWS stack for this product:** its Cognito pool, the S3 and CloudFront frontend, the
   API Gateway HTTP API and Lambda, and product-scoped deploy roles, each planned and passed
   through the cost guard.
4. **Store adapters** for the chosen datastore, behind the store interfaces, so the domain code
   does not change.
5. **The Angular frontend**, rebuilt against the same API.
6. **Parity, then cutover.** Every acceptance scenario that passes on GCP passes on AWS; then
   `tracker.martcoca.com` moves and the GCP deployment is retired.

Step 1 needs no cloud account. Every step from 3 on changes the cloud, and each is the
Founder's to approve before it is applied.

## Architecture decisions

All stated in full in [`decisions.md`](decisions.md):

- ADR-0061 — AWS, Angular and Go, with the datastore open
- ADR-0005 — no shared spine, the constraint that shapes the read path
- ADR-0051 — the packet as the unit this product records
- ADR-0053 — the 48-hour export lifetime
- ADR-0056 and ADR-0057 — machine access, and grants that name workloads
- ADR-0058 and ADR-0059 — authoring as a machine operation, open to any authorized session
- ADR-0060 — the product renews its own export

## Assumptions and open technical decisions

Resolved:

- ~~What does this product track?~~ **Packets**: a unit of work with a frozen scope, its
  evidence, and the pull request it produced.
- ~~Where do packets live?~~ **Here.** A repository's `packets/` directory is a migration
  source, reconciled into the export until it is retired.
- ~~Does a session call this product for its work?~~ **No.** It reads an export. Writes are the
  only synchronous direction and may fail.
- ~~The scope vocabulary.~~ The identity product publishes `packet:comment` and
  `packet:transition-status`. The authoring scopes of ADR-0058 are requested and not yet
  published.

Still open, and honestly so:

- **The datastore on AWS.** See *Technology allocation*.
- **How a human's tenant claim is established** at account creation. The Founder creates
  members in both products separately (ADR-0046), so the claim is set by whoever creates the
  account — and nothing yet checks the two products agree about who belongs where.
- **Whether comments need threading.** One flat append-only list is the smaller claim and
  probably right. It earns threading when a real conversation needs it.
- **What happens to a packet whose target repository is archived.** The record outlives the
  repository, which is an argument for this product, and nothing yet says how it renders.
