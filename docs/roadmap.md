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
human, and the only human has said they will not. So **machine authoring (item 4) is on the
critical path**, not a later refinement — and it depends on the identity product publishing
`packet:author`, `packet:issue` and `packet:supersede`, which it does not yet. That request
leaves this repository and has lead time, so it is raised now rather than when item 4 starts.

## Now

### 1. The tracker keeps its own export alive, and current

Two defects with one cause: **only a deploy, or issuing a packet in the app, republishes
`packets.json`.**

- **The API stops starting 48 hours after the last deploy.** `packets.json` and
  `repository-packets.json` expire 48 hours after publication and nothing renews them on a
  schedule. An expired `packets.json` refuses startup — `TestPresentInvalidPacketRefusesStartup`,
  its `expired` case — and `main` exits on that error. Cloud Run scales to zero, so the first
  cold start after expiry takes the API down, and only a deploy brings it back.
- **The app shows a frozen packet list.** The signed-in app displays `packets.json`: 16
  packets with out-of-date statuses, while `repository-packets.json` holds 22. A deploy renews
  its envelope without rebuilding its payload, behind an envelope that looks current.

The design answer is the tracker's, not a workflow's: the runtime already holds the store, the
repository source and the Hosting grant, and a scheduled job would still leave a cold start
after expiry refusing to start.

- **Goal:** the service republishes its own export when it is expired, close to expiry, or
  different from what its sources reconcile to — at startup and when a source changes — and an
  expired copy of its own export never stops it starting.
- **Out of bounds:** the published envelope, schema, digest and lifetime; publishing on every
  write; any workflow or IAM change; the identity product's exports, which stay strict.
- **Check:** each rule has a test that fails when it is removed — an expired own export is
  republished rather than refusing startup; a stale union is republished and an identical one
  is not; an unreachable store still refuses to publish. Live, after deploy, `packets.json`
  holds every id in `repository-packets.json`, with no duplicates.
- **Decision it takes:** the tracker's own earlier outputs are merge inputs, verified for
  integrity but not freshness. Freshness binds what it publishes and the authority it
  consumes. Recorded as an ADR with the change.

This also resolves issue #44, which asks for a manual step that is no longer going to happen.

### 2. Make the documents agree with the decisions

- The technical specification (three places) and the root README still say an export is
  fresh for **one hour**. ADR-0053 made it **48 hours**; `contract.FreshnessBound` and the live
  exports carry 48.
- **Out of bounds:** `packets/` and `evidence/`, which record what was true when written.
- **Check:** searching `README.md` and `docs/` for "one hour" finds nothing but this item.

## Next

### 3. The session write-back API

Acceptance scenarios 2, 3 and 5. The packet model already supports comments, status
transitions and evidence-before-`done`, and credentials already authenticate — but **no route
exposes any of it**, to a human or an agent. This is the largest gap between the
specification and the product, and it builds the authorization path item 4 reuses:
credential → workload → scope in the grant export.

- **Goal:** an agent presenting a credential comments on and transitions the packet it is
  bound to, authorized by the scope its workload holds in the identity product's grant export,
  with a caller-supplied idempotency key.
- **Out of bounds:** defining scope names the identity product does not publish; any route
  that edits a body; measuring revocation timing (item 6).
- **Check:** each refusal — unknown, revoked, expired, no grant, wrong scope, wrong packet,
  other tenant, `done` without evidence — returns its own code, and each has a test that fails
  when its rule is removed; a repeated idempotency key produces one event.

To establish first: the grant export carries three grants today with `packet:comment` and
`packet:transition-status`. One names a GitHub Actions identity, which ADR-0057 retired; the
other two are unexamined. Confirm which workload a credential here acts as and whether a grant
names it — and find the identity product's published conformance vectors — before building on
either.

### 4. Sessions author with a credential

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

### 5. The Founder comments

The actor table says the Founder "signs in, navigates, reads packets and their history,
**comments**". Nothing lets them. A comment is how the Founder redirects work without authoring
it.

- **Check:** a signed-in human appends an attributed comment; a second submission with the
  same idempotency key is one comment; no route lets anyone edit or delete one.

### 6. Measure how long a revoked grant keeps working

Acceptance scenario 3, and ADR-0056's "revocation has two speeds". Needs item 3. **The
measured number is the deliverable**, whatever it is; the bound is read from
`contract.FreshnessBound`, never written down in a test.

## Later

### 7. A session client

Acceptance scenarios 1 and 6 assume something a session runs: it reads its packet from the
export, and reports comments as unsent rather than losing them when the product is down.
Nothing like it exists. It becomes worth building once items 3 and 4 give it something to
call.

### 8. A cold start with an expired identity export exits rather than failing closed

The tenant directory and agent grants are required at startup, so if the identity product
stops publishing, the tracker's next cold start after their expiry exits. Acceptance scenario
7 asks for it to keep running, fail closed on what those exports authorize, and say how old
they are. Held copies already behave that way while an instance is alive; a cold start does
not.

### 9. Retire `packets/`

Capability roadmap increment 6. Gated on the app being the only source of the export (item 1),
on sessions authoring in the app (item 4), and on a session having executed work delivered as
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
| 1 | Created in the app by an authorized session, appears in the export, executed without calling the product | **Partial.** Human authoring and publish-on-issue are built; publication has never run live; no session can author (item 4) or has executed from an export |
| 2 | A session with a grant comments and transitions | **Not built.** No route (item 3) |
| 3 | A revoked grant is refused within the bound, distinguishably | **Half.** Credential revocation is immediate and tested; grant refusal needs item 3 |
| 4 | Editing a body is refused because the route does not exist | **Built.** The route allowlist fails service construction if one is added |
| 5 | `done` without evidence is refused | **Model only.** Tested in `packet`; no API reaches it (item 3) |
| 6 | Product offline: sessions keep working, comments reported unsent | **Reads only.** Exports are static files; nothing reports unsent comments (item 7) |
| 7 | The identity product offline: serve from held exports and say how old | **Partial.** A running instance holds and ages its copies; a cold start after expiry exits (item 8) |
| 8 | Unknown tenant refused at issue; retired refused differently | **Built** and tested |
| 9 | The Founder sees every packet in an initiative, including blocked with what it needs | **Broken live.** Navigation works but shows the frozen export, and stops 48 hours after a deploy (item 1); nothing can set `blocked` (item 3) |
| 10 | Projection dropped and rebuilt identically | **Built** and tested at the model level |
