import assert from "node:assert/strict";
import test from "node:test";

import { RETAINED_PIN_TAGS, assertLiveAllocation, selectRetainedTraffic } from "./run-tags.mjs";

const LATEST = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST";
const REVISION = "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION";

function revisionName(ordinal) {
  return `tracker-reader-${String(ordinal).padStart(5, "0")}-abc`;
}

function pin(ordinal) {
  return { type: REVISION, revision: revisionName(ordinal), tag: `fh-${String(ordinal).repeat(4)}` };
}

// `count` pins, newest ordinal last, with the newest also the latest ready revision.
function service(count, overrides = {}) {
  const pins = Array.from({ length: count }, (unused, index) => pin(index + 1));
  return {
    latestReadyRevision: `projects/p/locations/r/services/tracker-reader/revisions/${revisionName(count)}`,
    traffic: [{ type: LATEST, percent: 100 }, ...pins],
    ...overrides,
  };
}

function revisions(count) {
  return Array.from({ length: count }, (unused, index) => ({
    name: `projects/p/locations/r/services/tracker-reader/revisions/${revisionName(index + 1)}`,
    createTime: `2026-09-${String(index + 1).padStart(2, "0")}T00:00:00Z`,
  }));
}

test("accepts one latest target at 100% alongside zero-percent pins", () => {
  const { latest, pins } = assertLiveAllocation(service(3));
  assert.equal(latest.percent, 100);
  assert.deepEqual(
    pins.map((target) => target.revision),
    [revisionName(1), revisionName(2), revisionName(3)],
  );
});

test("refuses a second latest-revision target", () => {
  const drifted = service(1);
  drifted.traffic.push({ type: LATEST, percent: 0 });
  assert.throws(() => assertLiveAllocation(drifted), /exactly one latest-revision target/);
});

test("refuses a pin that takes live traffic from the latest revision", () => {
  const drifted = service(2);
  drifted.traffic[0].percent = 40;
  drifted.traffic[1].percent = 60;
  assert.throws(() => assertLiveAllocation(drifted), /exactly one latest-revision target/);
});

test("refuses a split that leaves the latest target short of the whole allocation", () => {
  const drifted = service(1);
  drifted.traffic.push({ type: REVISION, revision: revisionName(9), tag: "fh-9999", percent: 25 });
  assert.throws(() => assertLiveAllocation(drifted), /takes live traffic away/);
});

test("refuses a revision target carrying no tag", () => {
  const drifted = service(1);
  drifted.traffic.push({ type: REVISION, revision: revisionName(9) });
  assert.throws(() => assertLiveAllocation(drifted), /untagged revision target/);
});

test("refuses a target that names no retained revision", () => {
  const drifted = service(1);
  drifted.traffic.push({ type: REVISION, revision: "Not A Revision", tag: "fh-0000" });
  assert.throws(() => assertLiveAllocation(drifted), /names no retained revision/);
});

test("reclaims nothing while the retained pins are within the bound", () => {
  const count = RETAINED_PIN_TAGS;
  assert.equal(selectRetainedTraffic(service(count), revisions(count)), null);
});

test("reclaims the oldest pins once the bound is exceeded", () => {
  const count = RETAINED_PIN_TAGS + 3;
  const selection = selectRetainedTraffic(service(count), revisions(count));
  assert.deepEqual(
    selection.dropped.map((target) => target.revision).sort(),
    [revisionName(1), revisionName(2), revisionName(3)],
  );
  assert.equal(selection.traffic.length, RETAINED_PIN_TAGS + 1);
  assert.equal(selection.traffic[0].percent, 100);
  assert.ok(selection.traffic.some((target) => target.revision === revisionName(count)));
});

test("never reclaims the pin on the latest ready revision", () => {
  const count = RETAINED_PIN_TAGS + 1;
  // The revision list omits the newest revision, so creation order alone would sort its
  // pin oldest and drop the pin the release just published.
  const selection = selectRetainedTraffic(service(count), revisions(count - 1));
  assert.deepEqual(
    selection.dropped.map((target) => target.revision),
    [revisionName(1)],
  );
  assert.ok(selection.traffic.some((target) => target.revision === revisionName(count)));
});

test("refuses to reclaim a pin no deploy created", () => {
  const count = RETAINED_PIN_TAGS + 1;
  const foreign = service(count);
  foreign.traffic[1].tag = "operator-hold";
  assert.throws(
    () => selectRetainedTraffic(foreign, revisions(count)),
    /no deploy created and this will not drop: operator-hold/,
  );
});

test("refuses a retention limit that is not a positive integer", () => {
  assert.throws(() => selectRetainedTraffic(service(1), revisions(1), 0), /positive integer/);
});
