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

The stack is fixed: GCP, Go on Cloud Run, Vue on Firebase Hosting, Firestore, Identity
Platform, workload identity federation, OpenTofu. Not this product's to revisit.

## ADR-0045 — the chief-of-staff initializes rather than dispatches

Retired the dispatcher's attempt-envelope model. It is why this product tracks **packets**
rather than attempt envelopes: the envelope's producer no longer exists.

## ADR-0046 — identity is per-cloud, with a replicated tenant directory

Human identity is per product and never shared — this product's humans are Identity Platform
and nobody else's. Only tenant and account facts replicate, one way, from a single author.
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

## ADR-0058 — the chief-of-staff authors through the product, not through a person

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
