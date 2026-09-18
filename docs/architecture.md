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

Set by [ADR-0061](decisions.md#adr-0061). The target is AWS; the product runs on GCP until the
AWS deployment reaches parity.

| Layer | Target | Today |
|---|---|---|
| Cloud | AWS, `us-east-1` | GCP |
| API | Go on AWS Lambda behind an API Gateway HTTP API | Go on Cloud Run |
| Frontend | Angular, on a private S3 bucket behind CloudFront | Vue on Firebase Hosting |
| Store | **Open — pending research** | Firestore |
| Human identity | This product's own Cognito user pool | Identity Platform |
| Machine identity | Credentials this product issues (ADR-0056) | the same |
| Deploy identity | GitHub Actions OIDC into product-scoped IAM roles | GCP workload identity federation |
| Infrastructure | OpenTofu | OpenTofu |

The AWS platform this lands on already defines its constraints: on-demand Lambda with no
provisioned concurrency and no resident compute, private S3 origins behind CloudFront with
Origin Access Control, Cloudflare for DNS, Cognito on the Lite plan with PKCE and no
self-registration, and keyless deployment through GitHub OIDC. Its one rule this product
cannot follow as written — S3 is the only store it permits — is the open datastore question.

## The identity product

A sibling product, `identity-and-tenancy` — initiative 0000, and "0000" wherever the
specifications name it — is the canonical author of two facts this product
consumes and never writes:

- **The tenant directory** — which tenants exist and their status.
- **Agent grants** — which workload may use which scope, in which tenant, until when.

Both arrive as published files at `https://identity.martcoca.com`, read without calling that
product, verified offline before use. Its scope vocabulary is the only place a scope name is
defined; a scope this product recognises and that product does not is drift.

This product never calls it synchronously, never stores a credential belonging to it, and
keeps serving from the last good copy when it is unavailable.
