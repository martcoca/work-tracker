# What is actually true, 2026-09-14

Verified against the live product and this repository on the date above, not recalled. Where
something is broken or unbuilt it says so, because a state document that only lists successes
is the thing it is supposed to replace.

**Direction, decided 2026-09-18:** the product moves to AWS, with an Angular frontend and a Go
backend ([ADR-0061](decisions.md#adr-0061)). Nothing has moved yet, and the datastore on AWS is
open pending research. Everything under *Live* below describes the GCP deployment that is
serving today, and stays true until the cutover.

## Live

**https://tracker.martcoca.com** — serving `973ac6da`, confirmed by fetching that commit's
own marker file and comparing its content to the sha.

| | |
|---|---|
| API | Go on Cloud Run, revision `tracker-reader-00038-5t6`, minimum instances zero |
| Frontend | Vue on Firebase Hosting |
| Store | Firestore, Standard Native, `(default)` |
| Human sign-in | Identity Platform, working |
| Routes | 15 |
| Go packages | 16 |
| Deploy | keyless on merge to `main`, ~3 minutes |
| Rollback | works, both surfaces move together, ten deploys of reach |

## What a person can do today

- Sign in, navigate initiatives → epics → packets, read history.
- Author, issue and supersede a packet through the app.
- Create, list and revoke an **agent credential**, with the secret shown exactly once.

## What a machine can do today

- Be issued a credential bound to a packet, an attempt, and the **workload** it acts as.
- Present it and be resolved to a principal, with last-use recorded.
- Be refused distinguishably: unknown, revoked, expired, malformed header, no live grant.

It **cannot** yet comment, transition status, or author — those routes either do not exist or
do not accept a credential. That is the next work.

## What is broken, and known

**`packets.json` is frozen, and the fix for it has not been seen working.** The app displays
`packets.json`, the union of repository and app-authored packets, and it still holds 16 packets
with out-of-date statuses while `repository-packets.json` holds 22. A deploy renews its envelope
without rebuilding its payload, so **no freshness check can catch it.**

[ADR-0060](decisions.md#adr-0060) (#67) makes the service renew the export itself, and it is
deployed. But in the 25 minutes after `8edeefb` deployed — including eight minutes of
sustained traffic to rule out background work starved of CPU — no app-published export
appeared. Run locally against the real live files, the same renewal releases all 22 packets
correctly, so the failure is in production: read-only startup, a failing Hosting API step, or a
store read. Telling which needs the Cloud Run logs: issue #69.

**48 hours after the last deploy, the app stops showing packets.** Nothing that works renews
`packets.json` except a deploy. Since #67, an expired copy no longer stops the API starting —
tested, not yet observed live — but the app refuses to render packets from it, so the packet
views fail until someone deploys. On 2026-09-14 the live copy expires at
`2026-09-16T11:41:38Z`.

**Nothing lets anyone comment or transition a status.** The packet model supports both, with
evidence required for `done`, but no route exposes either — to a human or to an agent.

## How work is managed

**By [`docs/roadmap.md`](roadmap.md), derived from the specification — not by `packets/`.**
One session owns this product end to end. `packets/` and `evidence/` are a historical record
of how it was first built, and still the live product's data — the deploy publishes
`packets/` as `repository-packets.json` — but they no longer decide what is worked on. Their statuses are
frozen as they stood on 2026-09-13: 15 done, 5 not started, 1 in progress, 1 superseded.

## Cost

Idle cost is zero and is enforced rather than intended:

- Cloud Run scales to zero, CPU idles, maximum two instances.
- A plan-time guard refuses any resource type that bills while unused, and refuses a Cloud
  Run service without deletion protection or with non-zero minimum instances.
- Firestore Standard bills only stored data and operations, both inside a free tier at this
  volume.
- Ten retained traffic tags cost nothing — a tag is a zero-percent target on a revision that
  already exists.

## Operational facts worth not rediscovering

- **The SPA catch-all rewrite returns `index.html` with HTTP 200 for any missing path.** A
  status code proves nothing about whether a file exists; compare content.
- **`curl -sf <url> | head` exits 0 on an HTTP error**, because a pipeline reports the last
  command's status. Fetch to a file and inspect it separately.
- **Cloud Run v2 omits `reconciling` when settled.** `observedGeneration == generation` is the
  positive signal; requiring `reconciling === false` refuses every healthy service.
- **A pin on the latest ready revision is folded** into the latest-revision entry of
  `trafficStatuses`, which reports no `revision`. Resolve it through `latestReadyRevision`.
- **The container build context is a deny-all allowlist** in both `.dockerignore` and the
  `Dockerfile`. A new top-level Go package must be added to both or `go test` passes while the
  image fails to build.
- **The tracker cannot cold-start without unexpired identity exports.** The tenant directory
  and agent grants are required at startup. The identity product publishes them daily; if it
  stops, the tracker's next cold start after their expiry exits.
- **A retired packet publishes as `status: "not started"` with `superseded_by` set.** The
  model has four statuses and `superseded` is not one. Selecting work by status alone picks up
  retired packets.
