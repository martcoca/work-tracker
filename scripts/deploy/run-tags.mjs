// Traffic policy for the Cloud Run pins that make a rollback reachable.
//
// Firebase Hosting's `pinTag` rewrite records a zero-percent `fh-` traffic tag against the
// revision a Hosting version serves. That tag is the whole of the rollback mechanism: a
// released Hosting version routes `/api/**` through its recorded tag, so a version whose
// tag no longer exists cannot be rolled back to. Bounding how many pins survive is
// therefore bounding how far back a rollback can reach.

// Ten deploys of reach. The Firebase CLI reuses the pin already on the latest ready
// revision, so a deploy adds exactly one. A pin is a zero-percent traffic target on a
// revision that already exists and scales to zero: it configures no compute and costs
// nothing to keep, so the bound exists because unbounded growth is a leak, not because the
// eleventh pin is expensive.
export const RETAINED_PIN_TAGS = 10;

const LATEST = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST";
const REVISION = "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION";

// Only Hosting's own generated pins are ever dropped. A tag in any other shape was put
// there by something this script does not model, so it refuses rather than deleting it.
const hostingPinTag = /^fh-[0-9a-z][0-9a-z-]{0,60}$/;
const revisionID = /^[a-z0-9][a-z0-9-]{0,62}$/;

function percentOf(target) {
  const percent = target?.percent ?? 0;
  if (!Number.isInteger(percent) || percent < 0 || percent > 100) {
    throw new Error("Cloud Run traffic target has a non-integer percent");
  }
  return percent;
}

// The declared allocation used to be re-asserted by every apply. It is not any more — the
// apply ignores traffic so it stops erasing the pins — so it is asserted here instead:
// exactly one latest-revision target taking all traffic, and every other target a
// zero-percent named pin.
export function assertLiveAllocation(service) {
  const traffic = service?.traffic;
  if (!Array.isArray(traffic) || traffic.length === 0) {
    throw new Error("Cloud Run service declares no traffic allocation");
  }

  const latest = traffic.filter((target) => target?.type === LATEST);
  if (latest.length !== 1 || percentOf(latest[0]) !== 100) {
    throw new Error("Cloud Run traffic is not exactly one latest-revision target at 100%");
  }

  const pins = [];
  for (const target of traffic) {
    if (target === latest[0]) continue;
    if (target?.type !== REVISION || !revisionID.test(target?.revision ?? "")) {
      throw new Error("Cloud Run traffic contains a target that names no retained revision");
    }
    if (typeof target?.tag !== "string" || target.tag.length === 0) {
      throw new Error("Cloud Run traffic contains an untagged revision target");
    }
    if (percentOf(target) !== 0) {
      throw new Error(`Cloud Run pin ${target.tag} takes live traffic away from the latest revision`);
    }
    pins.push(target);
  }

  if (traffic.reduce((total, target) => total + percentOf(target), 0) !== 100) {
    throw new Error("Cloud Run traffic does not total 100%");
  }
  return { latest: latest[0], pins };
}

function shortName(name) {
  const value = String(name ?? "");
  return value.slice(value.lastIndexOf("/") + 1);
}

// A pin whose revision the API did not list sorts oldest: it resolves to nothing, so it is
// the first thing worth reclaiming.
function creationOrder(revisions) {
  const order = new Map();
  for (const revision of revisions ?? []) {
    const created = Date.parse(revision?.createTime ?? "");
    const short = shortName(revision?.name);
    if (short) order.set(short, Number.isNaN(created) ? -Infinity : created);
  }
  return (target) => order.get(target.revision) ?? -Infinity;
}

// Returns the traffic array to write, or null when the service is already within the
// bound. The pin on the latest ready revision is what the release just published, so it is
// never a candidate for dropping regardless of what the revision list says.
export function selectRetainedTraffic(service, revisions, limit = RETAINED_PIN_TAGS) {
  if (!Number.isInteger(limit) || limit < 1) {
    throw new Error("pin retention limit must be a positive integer");
  }
  const { latest, pins } = assertLiveAllocation(service);
  if (pins.length <= limit) return null;

  const live = shortName(service?.latestReadyRevision);
  const createdAt = creationOrder(revisions);
  const newestFirst = [...pins].sort((left, right) => {
    if (left.revision === live) return -1;
    if (right.revision === live) return 1;
    return createdAt(right) - createdAt(left);
  });

  const dropped = newestFirst.slice(limit);
  const foreign = dropped.filter((target) => !hostingPinTag.test(target.tag));
  if (foreign.length > 0) {
    throw new Error(
      `Cloud Run carries ${foreign.length} pin(s) no deploy created and this will not drop: ` +
        foreign.map((target) => target.tag).join(", "),
    );
  }

  return { traffic: [latest, ...newestFirst.slice(0, limit)], dropped };
}
