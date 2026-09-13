# Architecture the product inherits

This product was decomposed inside a larger portfolio. Those constraints still bind it, and
they are reproduced here so this repository is self-sufficient — nothing below requires the
doctrine repository to read.

## Layer 1 — cross-portfolio

**No speculative shared spine.** Products own capabilities, not technologies. There is no
shared service, no shared database, and no synchronous dependency between products. Two
products that need the same fact do not call each other for it.

**Interfaces are evidence contracts.** Where products must agree, they agree through a
published, versioned file with provenance and a digest — not an API call. A consumer reads
it, verifies it, and keeps serving from the last good copy when the producer is unavailable.

**Canonical sources stay canonical.** One place authors a fact. Everywhere else holds a
read-only replica, and the replica never becomes a second author.

**Public evidence is an explicit projection.** What is public is an allowlist, chosen
deliberately, never a filtered view of everything.

## Layer 2 — system

- **One deployable unit** per product.
- **Deny by default**, with authorization enforced server-side on every route.
- **Explicit version on every envelope.**
- **Caller-supplied idempotency** on every state change.
- **Evidence is immutable**; corrections are appended, never overwritten.
- **No last-write-wins.** A conflicting write is refused, not silently resolved.
- **Idle cost zero.** Nothing bills while nobody is using it.
- **WCAG 2.2 AA** on any human surface.

## The stack

Fixed by ADR-0028, and not this product's to revisit:

| Layer | Choice |
|---|---|
| Cloud | GCP |
| API | Go on Cloud Run, minimum instances zero |
| Frontend | Vue on Firebase Hosting |
| Store | Firestore, Standard Native, `(default)` database |
| Human identity | Identity Platform |
| Machine identity | Credentials this product issues (ADR-0056) |
| Deploy identity | Workload identity federation, keyless |
| Infrastructure | OpenTofu |

## The identity product

A sibling product, `identity-and-tenancy`, is the canonical author of two facts this product
consumes and never writes:

- **The tenant directory** — which tenants exist and their status.
- **Agent grants** — which workload may use which scope, in which tenant, until when.

Both arrive as published files at `https://identity.martcoca.com`, read without calling that
product, verified offline before use. Its scope vocabulary is the only place a scope name is
defined; a scope this product recognises and that product does not is drift.

This product never calls it synchronously, never stores a credential belonging to it, and
keeps serving from the last good copy when it is unavailable.

## What was left behind

The operating model that produced this product — packet briefs copied between repositories,
per-packet evidence essays, dispatch envelopes, and the checks enforcing all of it — is **not
part of this product** and is not reproduced here. The product records packets; it is not
governed by the convention that produced them.

The one inherited discipline worth keeping is narrow and has repeatedly earned its place:
**a check is not evidence until you have made it fail.** Removing a rule must break a test,
and a verification that cannot fail proves nothing. Four checks in this repository's history
passed vacuously because a pipeline masked a failing exit status.
