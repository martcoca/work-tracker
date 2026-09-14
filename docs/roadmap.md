# Roadmap

What to do next, in order, and why. Owned and kept current by the session working on this
product. It is derived from the gap between the
[product specification](product-specification.md) and
[what is actually true](state-of-the-product.md), not from `packets/`.

Take the top item unless the Founder names another. Each item states why it matters and the
check that proves it done. Last derived **2026-09-13**.

## A decision the Founder owns, before the authoring work

**The specification's authoring workflow has no author any more.** It was written for an
operating model in which a chief-of-staff session authored packets and worker sessions took
them. That model is retired: one session now owns each product end to end, and the Founder
does not author packets. Every requirement below that names the chief-of-staff — authoring
and issuing in the app, dispatch by export, the Resume Capsule, ADR-0058's machine authoring —
is waiting on one question: **who creates work in the tracker now?**

**Recommendation:** the session that owns a product records its own work in the tracker
through a credential this product issued. The Founder signs in to see what every owning
session is doing, what it proved, and what is waiting on them, and comments to redirect. That
keeps the product's category — intent-to-evidence traceability for agent-run engineering —
and every invariant it already enforces, and it makes the Founder a reader rather than an
author, which ADR-0058 already says they are.

What it would change, and would need the specification amended to say:

- The **chief-of-staff actor** becomes "the owning session", with the same authoring scopes.
- **"What is waiting on the Founder"** becomes the primary human view, ahead of navigation.
- **The Resume Capsule** — a handoff view between sessions — loses its main consumer and
  should be dropped from the first slice rather than built.
- **Dispatch by export** stays as a property (no session needs the tracker up to work) but
  stops being the reason for the design.

Items that depend on this answer are marked **(direction)**. Nothing in *Now* or *Next* does.

## Now

### 1. Show the Founder the packets that exist

The signed-in app displays `packets.json`, which has been frozen since the app publisher
shipped: 16 packets with out-of-date statuses, while `repository-packets.json` holds 22. A
deploy renews the envelope without rebuilding the payload, and the only thing that rebuilds it
is issuing a packet in the app, which nobody is going to do. This is the product showing its
one user the wrong state, behind an envelope that looks current.

- **Goal:** the published `packets.json` is rebuilt from its sources whenever they differ from
  what is live, without anyone issuing a packet.
- **Out of bounds:** the envelope, schema, digest and lifetime; publishing on every write; any
  new IAM grant — the runtime already holds the Hosting grant. If it turns out to need one,
  stop and ask.
- **Check:** a test in which a stale published union is detected and republished, which fails
  when the rebuild is removed; the store-unreachable path still refuses to publish an empty
  export; and after deploy, live `packets.json` contains every id in
  `repository-packets.json`, with no duplicates.

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
specification and the product, and it is needed under any answer to the decision above.

- **Goal:** an agent presenting a credential comments on and transitions the packet it is
  bound to, authorized by the scope its workload holds in 0000's grant export, with a
  caller-supplied idempotency key.
- **Out of bounds:** defining scope names 0000 does not publish; any route that edits a body;
  measuring revocation timing (item 5).
- **Check:** each refusal — unknown, revoked, expired, no grant, wrong scope, wrong packet,
  other tenant, `done` without evidence — returns its own code, and each has a test that fails
  when its rule is removed; a repeated idempotency key produces one event.

To establish first: 0000 publishes three grants today carrying `packet:comment` and
`packet:transition-status`. One names a GitHub Actions identity, which ADR-0057 retired; the
other two are unexamined. Confirm which workload a credential here acts as and whether a grant
names it — and find 0000's published conformance vectors — before building on either.

### 4. The Founder comments

The actor table says the Founder "signs in, navigates, reads packets and their history,
**comments**". Nothing lets them. A comment is how the Founder redirects work without
authoring it, so it matters more under the recommended direction, not less.

- **Check:** a signed-in human appends an attributed comment; a second submission with the
  same idempotency key is one comment; no route lets anyone edit or delete one.

### 5. Measure how long a revoked grant keeps working

Acceptance scenario 3, and ADR-0056's "revocation has two speeds". Needs item 3. **The
measured number is the deliverable**, whatever it is; the bound is read from
`contract.FreshnessBound`, never written down in a test.

## Depends on the direction decision

### 6. Machine authoring (direction)

ADR-0058: author, issue and supersede with a credential holding `packet:author`,
`packet:issue` or `packet:supersede`. Also blocked outside this repository — 0000's export
publishes none of those three scopes today.

### 7. A session client (direction)

Acceptance scenarios 1 and 6 assume something a session runs: it reads its packet from the
export, and reports comments as unsent rather than losing them when the product is down.
Nothing like it exists.

### 8. Retire `packets/` (direction)

Capability roadmap increment 6. Gated on the app being the only source of the export (item 1)
and on the direction decision, because `packets/` is still the live product's data.

## Parked — not required by the specification

- **Cloud spend observation and alerting.** A portfolio concern, not a product requirement,
  and it needs the Founder to link billing. The plan-time cost guard already enforces idle
  cost zero.
- **Recording what running the organization costs.** Same.

## Acceptance scenarios, as of 2026-09-13

| # | Scenario | State |
|---|---|---|
| 1 | Created in the app, appears in the export, executed without calling the product | **Partial.** Human authoring and publish-on-issue are built; publication has never run live; no session has executed from an export |
| 2 | A session with a grant comments and transitions | **Not built.** No route (item 3) |
| 3 | A revoked grant is refused within the bound, distinguishably | **Half.** Credential revocation is immediate and tested; grant refusal needs item 3 |
| 4 | Editing a body is refused because the route does not exist | **Built.** The route allowlist fails service construction if one is added |
| 5 | `done` without evidence is refused | **Model only.** Tested in `packet`; no API reaches it (item 3) |
| 6 | Product offline: sessions keep working, comments reported unsent | **Reads only.** Exports are static files; nothing reports unsent comments (item 7) |
| 7 | 0000 offline: serve from held exports and say how old | **Built** for the tenant directory, including its age in the app |
| 8 | Unknown tenant refused at issue; retired refused differently | **Built** and tested |
| 9 | The Founder sees every packet in an initiative, including blocked with what it needs | **Broken live.** Navigation works but shows the frozen export (item 1); nothing can set `blocked` (item 3) |
| 10 | Projection dropped and rebuilt identically | **Built** and tested at the model level |
