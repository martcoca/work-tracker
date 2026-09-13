# What is actually true, 2026-09-13

Verified against the live product and this repository on the date above, not recalled. Where
something is broken or unbuilt it says so, because a state document that only lists successes
is the thing it is supposed to replace.

## Live

**https://tracker.martcoca.com** — serving `82712f63`, confirmed by fetching that commit's
own marker file.

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
catch it.** It becomes correct the first time the app itself publishes — which happens on the
first packet issued *in the app*, and has never happened. Anything reading packet state should
read `repository-packets.json` until then.

## Packets

15 done, 5 not started, 1 in progress. The five open ones:

| Packet | Is |
|---|---|
| `0004-E03-T01` | The session API: read, comment, transition, deny by default |
| `0004-E03-T03` | Revoke a grant and measure how long it takes to stop working |
| `0004-E03-T06` | Authoring accepts a credential, not only a person |
| `0004-E06-T01` | Observe cloud spend and alert before the bill |
| `0004-E06-T02` | Record what the organization spends to run |
| `0004-E07-T02` | *(in progress)* Publish the export from the app, so the app becomes the system of record |

`0004-E07-T02` is the one that unfreezes `packets.json`, and everything about the app being a
system of record rather than a viewer depends on it.

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
- **A retired packet publishes as `status: "not started"` with `superseded_by` set.** The
  model has four statuses and `superseded` is not one. Selecting work by status alone picks up
  retired packets.
