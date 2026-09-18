# Decisions that bind this product

Every architecture decision this product is held to, stated in full so this repository needs
nothing else. The ADR numbers are their original identifiers in the portfolio's decision log
and are kept for provenance.

Decisions taken from here on belong in this repository.

## ADR-0005 — range is the deliverable, and there is no shared spine

No shared service, no shared database, no synchronous dependency between products. This is
the binding constraint on the read path: a session obtains its packet from a **published
file**, not by calling this product. It means the product can be entirely unavailable and
work continues from the last good export.

## ADR-0028 — portfolio stacks follow workloads

The original allocation placed this product on GCP: Go on Cloud Run, Vue on Firebase Hosting,
Firestore, Identity Platform, workload identity federation, OpenTofu. **Superseded for this
product by [ADR-0061](#adr-0061)**, which moves it to AWS with an Angular frontend and a Go
backend.


## ADR-0046 — identity is per-cloud, with a replicated tenant directory

Human identity is per product and never shared — this product's humans live in its own
Cognito user pool and nobody else's. Only tenant and account facts replicate, one way, from a single author.
**Credentials never replicate.**

## ADR-0051 — the packet is the unit of isolation

The packet is the unit this product records: one packet, one unit of work, one frozen scope.

## ADR-0053 — export lifetime matches the publishing budget

A published export carries a **48-hour** freshness bound. A consumer refuses an export whose
`expires_at` has passed rather than serving stale authority. This is the outer bound on how
long a revoked grant can still be honoured somewhere.

## ADR-0056 — machine access is a product feature, not cloud plumbing

**This product issues its own machine credentials.** It stores only a hash, returns the
secret once, and resolves a presented credential to a principal. Listing, revocation and
last-use are ordinary product surface. No cloud mints anything.

Two consequences that are easy to lose:

- **Revocation has two speeds.** Revoking a credential here is immediate. Revoking the grant
  in `identity-and-tenancy` is bounded by the 48-hour export lifetime. Both are real and they
  answer different questions.
- **The first credential is created by a signed-in human.** No agent mints its own authority.
  This is a one-time bootstrap, not a recurring human step.

## ADR-0057 — the session is not an authority record

A grant names a **workload**, never a per-attempt session. The identity product answers *may
this workload do this, in this tenant, until when*. The binding of a credential to a packet
and an attempt lives **here**, because this product mints it and the identity product never
sees it.

An earlier design put a session principal in the grant, requiring a merged pull request per
attempt inside the hour it lived. It was unbuildable, and its removal is why the grant
lookup uses issuer and subject and never an attempt id.

The cost, accepted deliberately: the identity product cannot say which attempt acted. That
attribution lives in this product's credential records — packet, attempt, last use.

## ADR-0058 — authoring is a machine operation, with three scopes

Authoring is a **machine** operation. Three scopes, deliberately not one:

| Scope | Permits |
|---|---|
| `packet:author` | Create and update a draft. Reversible; nothing a session sees changes. |
| `packet:issue` | Issue a draft. Scope freezes; every session can take it. |
| `packet:supersede` | Retire a packet by replacing it, original preserved. |

A single `packet:write` would make the reversible act and the irreversible one
indistinguishable in a grant.

The human path is **not** removed — a signed-in human keeps every route. What changes is that
authoring stops *requiring* a person. The Founder reads, navigates and comments; they do not
author packets.

*Refined by ADR-0059: any authorized session may hold these scopes.*

## ADR-0059 — any authorized session puts work in the tracker

*Founder decision, 2026-09-14.* **No kind of session is privileged.** Any session may author, issue and supersede packets, comment on them and transition
their status, provided it is **authenticated** with a credential this product issued and
**authorized** because the workload that credential acts as holds the named scope in the
published grant export.

- **Authority is the scope, never the kind of session.** The product does not know or check
  whether a session owns a product, planned the work, or is executing it. ADR-0058's three
  authoring scopes are the whole of authoring authority.
- **The human path stays.** A signed-in human keeps every route. The Founder reads, navigates
  and comments, and does not author.
- **The consequence that orders the work:** until some workload holds `packet:author` and
  `packet:issue`, nothing puts new work in the tracker at all, because the only human who
  could has said they will not. Machine authoring is therefore on the critical path, and it
  needs the identity product to publish those scopes.

## ADR-0060 — the tracker renews its own export, and its own expired output never stops it

*2026-09-14.* Only a deploy, or issuing a packet in the app, republished `packets.json`. An
export nobody renewed within its 48-hour lifetime expired; the reader refused to start on an
expired copy of it; so the one process able to renew it could not start, and nothing else would.

- **The service renews its own export.** It checks at startup and then on every refresh
  interval, and republishes when the live export is older than half its lifetime or differs
  from what its sources reconcile to. The check runs in the background, because a Hosting
  release can outlast a cold start's startup probe.
- **Its own earlier outputs are merge inputs, verified for integrity but not freshness.** The
  last public `packets.json` and the repository migration export are checked for schema, exact
  lifetime, digest and provenance, and accepted after they expire. Freshness binds what the
  tracker **publishes**, which is verified in full before release, and every export it
  **honours as authority**, which stays strict.
- **An expired copy of its own export is held, not refused.** It fails closed on every use —
  the app renders no packets from it and `VerifiedCopy` refuses it — until a renewal replaces
  it. The identity product's exports are unaffected: an expired tenant directory or grant
  export still refuses startup.

## ADR-0061 — the product moves to AWS, with an Angular frontend and a Go backend

*Founder decision, 2026-09-18.* This product and the architecture builder standardise on one
stack, so a session can move between them without relearning a platform.

| Layer | Target | Replaces |
|---|---|---|
| Cloud | AWS, `us-east-1` | GCP |
| API | Go on AWS Lambda, on-demand, behind an API Gateway HTTP API | Go on Cloud Run |
| Frontend | Angular, built static, served from a private S3 bucket through CloudFront with Origin Access Control | Vue on Firebase Hosting |
| Human identity | This product's own Amazon Cognito user pool, Lite plan, hosted sign-in with PKCE | Identity Platform |
| Machine identity | Credentials this product issues ([ADR-0056](#adr-0056)) | unchanged |
| Deploy identity | GitHub Actions OIDC assuming product-scoped IAM roles | GCP workload identity federation |
| Infrastructure | OpenTofu, planned and passed through the cost guard before apply | unchanged |
| DNS | Cloudflare, `tracker.martcoca.com` | unchanged |
| Datastore | **Open — pending research** | Firestore |

Carried over unchanged: idle cost zero, deny by default, no shared spine, the published export
contract and its 48-hour bound, and the identity product as the shared authority for tenants
and grants.

**The datastore is deliberately undecided.** The AWS platform baseline allows S3 as the only
durable store and forbids every database, while this product needs an append-only event log
with one-winner concurrency, listing by initiative and epic, credential lookup by id, and
idempotency keys. That choice needs research before it is made; the technical specification
states the question. Nothing moves until it is answered.

**GCP stays live until the AWS deployment reaches parity**, then traffic moves and the GCP
deployment is retired. The domain code — the packet model, the export contract, credentials
and authorization — is provider-neutral Go and carries over. What is rebuilt is the frontend,
the hosting, the identity integration and the store adapters.

Supersedes [ADR-0028](#adr-0028)'s allocation for this product.
