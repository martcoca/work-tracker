# Roadmap

What to do next, in order, and why. Owned and kept current by the session working on this
product. It is derived from the gap between the
[product specification](product-specification.md) and
[what is actually true](state-of-the-product.md), not from `packets/`.

Take the top item unless the Founder names another. Each item states why it matters and the
check that proves it done. Last derived **2026-09-14**.

## Decided: any authorized session puts work in the tracker

[ADR-0059](decisions.md#adr-0059), Founder decision 2026-09-14. There is no chief-of-staff
role. Any session authors, issues, supersedes, comments and transitions if it is authenticated
with a credential this product issued and its workload holds the named scope. The human path
stays; the Founder reads, navigates and comments.

**What that does to the order:** today the only way to put work in the tracker is a signed-in
human, and the only human has said they will not. So **machine authoring (item 3) is on the
critical path**, not a later refinement — and it depends on the identity product publishing
`packet:author`, `packet:issue` and `packet:supersede`, which it does not yet. That request
leaves this repository and has lead time, so it is raised now rather than when item 3 starts.

## Blocked

### The export renewal does not reach production

ADR-0060's renewal is deployed and has not been seen to publish anything. It is correct against
the real live files locally, so the cause is in production, and finding it needs the Cloud Run
logs — issue #69, waiting on the Founder. Until it works, the frozen list and the 48-hour expiry
both stand, and item 1 below is moot.

## Now

### 1. Deploys stop writing a stale union over the renewed export

[ADR-0060](decisions.md#adr-0060) made the service renew `packets.json` itself, which ends the
48-hour outage and the frozen list. One conflict remains, in the deploy: it still renews the
union it fetched *without rebuilding it*, and uploads the site twice. The new revision's first
instance starts between the two uploads, renews the export, and the second upload writes the
stale union back over it. The service's next check notices and corrects it, but every deploy
shows the frozen list for a while and costs an extra Hosting version.

- **Goal:** the deploy writes the union the service would: the live union reconciled with this
  commit's repository export, instead of a renewal of the live payload.
- **Out of bounds:** giving the deploy identity access to the store; the envelope.
- **Check:** after a deploy, live `packets.json` already holds every id in
  `repository-packets.json`, published by the deploy itself, before any renewal by the service;
  and a test that fails if the renewal-without-rebuild is restored.
- **Draft pull request:** it changes `.github/workflows/deploy.yml`.

## Next

### 2. The session write-back API

Acceptance scenarios 2, 3 and 5. The packet model already supports comments, status
transitions and evidence-before-`done`, and credentials already authenticate — but **no route
exposes any of it**, to a human or an agent. This is the largest gap between the
specification and the product, and it builds the authorization path item 3 reuses:
credential → workload → scope in the grant export.

- **Goal:** an agent presenting a credential comments on and transitions the packet it is
  bound to, authorized by the scope its workload holds in the identity product's grant export,
  with a caller-supplied idempotency key.
- **Out of bounds:** defining scope names the identity product does not publish; any route
  that edits a body; measuring revocation timing (item 5).
- **Check:** each refusal — unknown, revoked, expired, no grant, wrong scope, wrong packet,
  other tenant, `done` without evidence — returns its own code, and each has a test that fails
  when its rule is removed; a repeated idempotency key produces one event.

What the grant export says, read 2026-09-14: three grants in the tenant. Two are **revoked** —
the GitHub Actions identity ADR-0057 retired, and a `packet:comment`-only Google workload. One
is **active**: a Google-issued workload holding `packet:comment` and
`packet:transition-status`, expiring 2026-12-12. So a credential acting as that workload has
something to be authorized by, and a credential acting as either revoked one is the live
refusal case.

Still to establish: where the identity product publishes its **conformance vectors** — the
agreed cases for evaluating a grant, so this product honours its rules without copying them
from its source. Nothing in this repository has them. Ask before building the evaluator.

### 3. Sessions author with a credential

ADR-0058 and ADR-0059. Each authoring route — draft create and update, issue, supersession —
accepts a signed-in human **or** a credential whose workload holds the matching scope.

- **Goal:** a session creates, issues and supersedes a packet with no person signed in, and
  the packet's history records the workload that authored it.
- **Out of bounds:** removing or changing the human path; inventing scopes; any body edit.
- **Check:** `packet:author` permits draft create and update only, `packet:issue` issue only,
  `packet:supersede` supersession only, and a credential holding the wrong one is refused
  naming the scope it lacked — each rule with a test that fails when removed; the existing
  human authoring tests pass untouched; the workload attribution survives into the export.
- **Needs from the identity product:** the three scopes in its vocabulary, and a grant naming
  the workload that will author. Outside this repository — ask the Founder.

### 4. The Founder comments

The actor table says the Founder "signs in, navigates, reads packets and their history,
**comments**". Nothing lets them. A comment is how the Founder redirects work without authoring
it.

- **Check:** a signed-in human appends an attributed comment; a second submission with the
  same idempotency key is one comment; no route lets anyone edit or delete one.

### 5. Measure how long a revoked grant keeps working

Acceptance scenario 3, and ADR-0056's "revocation has two speeds". Needs item 2. **The
measured number is the deliverable**, whatever it is; the bound is read from
`contract.FreshnessBound`, never written down in a test.

## Later

### 6. A session client

Acceptance scenarios 1 and 6 assume something a session runs: it reads its packet from the
export, and reports comments as unsent rather than losing them when the product is down.
Nothing like it exists. It becomes worth building once items 2 and 3 give it something to
call.

### 7. A cold start with an expired identity export exits rather than failing closed

The tenant directory and agent grants are required at startup, so if the identity product
stops publishing, the tracker's next cold start after their expiry exits. Acceptance scenario
7 asks for it to keep running, fail closed on what those exports authorize, and say how old
they are. Held copies already behave that way while an instance is alive; a cold start does
not.

### 8. Keep the export fresh for readers outside the app

The service renews its export only while an instance is running, and Cloud Run runs one only
when someone uses the app. After two days with no visitor, `packets.json` expires for anything
reading it directly. Nothing does today. Once a session reads its work from the export, a
scheduled request that wakes the service once a day closes the gap — free on a public
repository, and a workflow change, so a draft.

### 9. Retire `packets/`

Capability roadmap increment 6. Gated on the app being the only source of the export,
on sessions authoring in the app (item 3), and on a session having executed work delivered as
an export — because `packets/` is still the live product's data.

## Parked — not scheduled

- **The Resume Capsule.** A first-slice experience in the specification whose original
  consumer, the chief-of-staff, is gone. Not scheduled until a session resuming work needs it.
- **Cloud spend observation and alerting.** A portfolio concern rather than a product
  requirement, and it needs the Founder to link billing. The plan-time cost guard already
  enforces idle cost zero.
- **Recording what running the organization costs.** Same.

## Acceptance scenarios, as of 2026-09-14

| # | Scenario | State |
|---|---|---|
| 1 | Created in the app by an authorized session, appears in the export, executed without calling the product | **Partial.** Human authoring and publish-on-issue are built; publication has never run live; no session can author (item 3) or has executed from an export |
| 2 | A session with a grant comments and transitions | **Not built.** No route (item 2) |
| 3 | A revoked grant is refused within the bound, distinguishably | **Half.** Credential revocation is immediate and tested; grant refusal needs item 2 |
| 4 | Editing a body is refused because the route does not exist | **Built.** The route allowlist fails service construction if one is added |
| 5 | `done` without evidence is refused | **Model only.** Tested in `packet`; no API reaches it (item 2) |
| 6 | Product offline: sessions keep working, comments reported unsent | **Reads only.** Exports are static files; nothing reports unsent comments (item 6) |
| 7 | The identity product offline: serve from held exports and say how old | **Partial.** A running instance holds and ages its copies; a cold start after expiry exits (item 7) |
| 8 | Unknown tenant refused at issue; retired refused differently | **Built** and tested |
| 9 | The Founder sees every packet in an initiative, including blocked with what it needs | **Broken live.** Navigation works but shows the frozen export; its renewal is deployed and not yet working (#69); nothing can set `blocked` (item 2) |
| 10 | Projection dropped and rebuilt identically | **Built** and tested at the model level |
