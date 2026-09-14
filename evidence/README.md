# Evidence

> **Historical record.** As of 2026-09-13 packets no longer manage work here, and no new
> evidence files are written. The account of a change is its pull request description, and
> what it made true goes into [`docs/state-of-the-product.md`](../docs/state-of-the-product.md).

One file per packet, named for its packet id, written by the session that executed it and
committed in the same pull request.

Pull requests here merge automatically when their checks pass, so **nobody reads the diff
before it lands**. That makes this directory the only durable account of what was actually
done — a pull request description is read once, if at all, and a chat message is gone when
the window closes.

An evidence file records:

- the Check that was run and its real output;
- what was verified, and what could not be;
- any decision the packet deliberately left to the session, and the reasoning;
- anything noticed that falls outside the packet.

**The honest gaps are the valuable part.** A file that reports only success is indistinguishable
from one written without looking. `scripts/collect-delivery.sh` in the doctrine repository
reads this directory to report what has been delivered, so a missing file shows up as
missing rather than as done.
