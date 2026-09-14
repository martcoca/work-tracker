# What is actually true, 2026-09-14

Verified against the live product and this repository on the date above, not recalled. Where
something is broken or unbuilt it says so, because a state document that only lists successes
is the thing it is supposed to replace.

## Live

**https://tracker.martcoca.com** — serving `fe3afbe0`, confirmed by fetching that commit's
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

**`packets.json` is frozen.** It is the union of repository and app-authored packets, and a
deploy *renews* it rather than rebuilding it: fresh `published_at`, fresh `source.commit`, and
an assertion that the payload digest did not change. Measured today, both published in the
same second:

```
packets.json             n=16   published 17:14:00
repository-packets.json  n=22   published 17:13:59
```

The union is six packets behind while its envelope looks current, so **no freshness check can
catch it.** And it is **what the signed-in app displays**: the runtime reader fetches
`packets.json`, never `repository-packets.json`, so the Founder sees 16 packets with
out-of-date statuses. It is only rebuilt when someone issues a packet in the app, which has
never happened. Fixing that is the top of the [roadmap](roadmap.md).

**The API stops starting 48 hours after the last deploy.** Nothing renews `packets.json` or
`repository-packets.json` except a deploy. An expired `packets.json` refuses startup and
`main` exits, so with Cloud Run at zero instances the first cold start after expiry takes the
API down until someone deploys. On 2026-09-14 the live copies expire at
`2026-09-16T11:18:27Z`. The static frontend keeps loading; everything behind `/api` does not.

**Nothing lets anyone comment or transition a status.** The packet model supports both, with
evidence required for `done`, but no route exposes either — to a human or to an agent.

## How work is managed

**By [`docs/roadmap.md`](roadmap.md), derived from the specification — not by `packets/`.**
The operating model in which one session authored packets and another executed them is
retired; one session owns this product end to end. `packets/` and `evidence/` remain as the
record of that model and as the live product's data (the deploy publishes `packets/` as
`repository-packets.json`), and no longer decide what is worked on. Their statuses are
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
