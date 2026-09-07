# work-tracker

The Agentic Work Tracker: where task packets live, who is doing them, and what happened.

## What it is

A packet is a unit of work — a Goal, a Boundary, what "done" means, and the Check that proves
it. This product records packets as they are authored, issued, taken, commented on,
transitioned and closed.

Its users are a human who wants to see what is happening, and **agent sessions that
authenticate and write back**. Sessions are first-class actors here, not an event source.

## The two properties that decide the design

**A packet's scope is frozen at issue.** Goal, Boundary, Done-when, Check and Context are
written once and never edited — by anyone. A packet whose scope moved mid-flight means a
session executed something nobody can afterwards reconstruct. A wrong packet is *superseded*
by a new one naming its parent; the original stays as the record of what was asked.

That is deliberately more awkward than editing, and the awkwardness is doing work.

**Reads are files; writes are calls.** A session never calls this product to learn what work
it has — it reads a published export, versioned and digest-verified, with a one-hour freshness
bound. Comments and status transitions are outbound calls that may fail without stopping the
work.

That asymmetry is why a work tracker does not become a shared spine. This product being
unavailable degrades reporting; it does not halt the organization.

## What it depends on

Identity comes from the Identity and Tenancy product, consumed as **files**: agent grants say
whether a session may act, and the tenant directory says whose work a packet is. This product
never calls that one either, and serves from its last exports when it is unavailable.

## Agent credentials

A signed-in human creates a packet-bound machine credential with
`POST /api/agent-credentials`, then receives its bearer value once. Later list and detail
responses expose only identity metadata: tenant, packet, attempt, issue/expiry, creator,
revocation, and last use. `POST /api/agent-credentials/{credential}/revoke` takes effect on
the next authentication transaction.

The bearer value is presented as `Authorization: Bearer <one-time value>`. The service stores
only its SHA-256 digest in the existing Firestore database. A successful match puts the
session principal and tenant on the request; it contributes no scope. Every comment or
status operation must still find its named permission in 0000's current agent-grants export.
