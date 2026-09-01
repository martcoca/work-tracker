# Packets

Work in this repository is defined by packets. A packet states a Goal, a Boundary, a
Check, and enough context to execute without reading another repository.

## Working order

| # | Packet | Status | Is |
|---|---|---|---|
| 1 | [`0004-E01-T01.md`](0004-E01-T01.md) | done | The packet model: append-only events, the projection, the freeze, transition rules. No cloud |
| 2 | [`0004-E01-T02.md`](0004-E01-T02.md) | done | The export contract: shared envelope, stable digest, one-hour bound, tenant validated against 0000's directory |
| 3 | [`0004-E02-T01.md`](0004-E02-T01.md) | done | Sign in, navigate initiative to epic to packet, read-only. **Authored and planned; the Founder applies** |
| — | [`0004-E02-T04.md`](0004-E02-T04.md) | done | **Take this before E02-T03.** How an export actually reaches the reader: fetch at runtime, hold the last good copy, fail closed when it expires |
| — | [`0004-E02-T03.md`](0004-E02-T03.md) | done | The deployable image: `container_image` is required and nothing produced one, so the whole cloud path is blocked on it |
| 4 | [`0004-E02-T02.md`](0004-E02-T02.md) | done | Author, issue and supersede a packet in the app. Issue is where scope freezes |
| 5 | [`0004-E03-T01.md`](0004-E03-T01.md) | not started | The session API: authenticate with a grant from 0000, comment, transition. **Needs 0000 publishing grants** |
| 6 | [`0004-E03-T02.md`](0004-E03-T02.md) | not started | Revoke a grant and measure how long it takes to stop working. The number is the deliverable |
| — | [`0004-E02-T05.md`](0004-E02-T05.md) | done | The first deployment refused to start: it cannot fetch its own packet export before it exists. Authority exports stay strict |

Take the packet the Founder names. Otherwise take the next one in this table whose
`Status:` is not `done`. The table is the order; the numbers are only identity.

**E01 needs no cloud account.** E02-T01 stops at a clean plan — applying cloud identity is
the Founder's. **E03 cannot start until initiative 0000 publishes agent grants**; if its
grants export does not exist, stop and say so rather than stubbing the contract.

E04 — removing `packets/` from every repository, including this one — is deliberately
unwritten. Its shape depends on what E01 through E03 actually build, and it is gated on a
session having demonstrably executed a packet delivered as an export.

## Rules

- **One packet in flight at a time.**
- **Never edit a packet body.** If it asks for the wrong thing, say so and stop. If a *step*
  is impossible but its intent is clear, do the nearest valid thing and say what you changed
  — stopping is for authority, not for difficulty.
- **You may set `Status:`** and nothing else in the file.
- **Run the Check yourself** before opening a pull request.
- **Write `evidence/<packet-id>.md`** in the same pull request: the Check output, what you
  verified, what you could not, and any decision the packet left to you. CI enforces it.
- **If you are blocked, open an issue labelled `blocked`.** A blocker mentioned only in
  conversation does not survive the conversation.
- **Branch, commit, open a pull request.** Never commit to `main`, never merge your own work.
- **Stop at anything irreversible or cost-incurring** — cloud apply, provisioning, deletion,
  publishing, spend. You hold no such authority.

## If no packet applies

Stop and ask. The absence of a packet is information, not an invitation.
