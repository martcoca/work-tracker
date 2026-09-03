import assert from "node:assert/strict";
import test from "node:test";

import {
  selectDeployedVersion,
  selectPinnedRevision,
  validateRollbackInputs,
  verifyRevisionProvenance,
} from "./rollback-policy.mjs";

const targetCommit = "a".repeat(40);
const input = {
  targetCommit,
  projectId: "project-synthetic",
  region: "region-synthetic",
  siteId: "hosting-synthetic",
};
const versionName = "sites/hosting-synthetic/versions/version-final";

function fixtures() {
  return {
    releases: [
      {
        message: `${targetCommit} packet-authority-preflight`,
        releaseTime: "2026-09-03T10:00:00Z",
        version: { name: "sites/hosting-synthetic/versions/version-preflight" },
      },
      {
        message: targetCommit,
        releaseTime: "2026-09-03T10:01:00Z",
        version: { name: versionName },
      },
    ],
    version: {
      name: versionName,
      status: "FINALIZED",
      config: {
        rewrites: [
          { glob: "/api/**", run: { serviceId: "tracker-reader", region: input.region, tag: "fh-final" } },
          { glob: "**", path: "/index.html" },
        ],
      },
    },
    service: {
      reconciling: false,
      trafficStatuses: [
        { revision: "tracker-reader-00002-new", percent: 100 },
        { revision: "tracker-reader-00001-old", percent: 0, tag: "fh-final" },
      ],
    },
    revision: {
      name: `projects/${input.projectId}/locations/${input.region}/services/tracker-reader/revisions/tracker-reader-00001-old`,
      annotations: { "source-commit": targetCommit },
    },
  };
}

test("accepts only a final Hosting release pinned to the same Cloud Run commit", () => {
  const values = fixtures();
  const validated = validateRollbackInputs(input);
  const selected = selectDeployedVersion(values.releases, validated.siteId, validated.targetCommit);
  assert.equal(selected, versionName);
  const pin = selectPinnedRevision(values.version, values.service, {
    versionName: selected,
    region: validated.region,
  });
  assert.deepEqual(pin, { tag: "fh-final", revision: "tracker-reader-00001-old" });
  assert.doesNotThrow(() => verifyRevisionProvenance(values.revision, { ...validated, revision: pin.revision }));
});

test("refuses a preflight release even though its static files carry the commit", () => {
  const values = fixtures();
  values.releases = values.releases.slice(0, 1);
  assert.throws(
    () => selectDeployedVersion(values.releases, input.siteId, targetCommit),
    /no final live Hosting release/,
  );
});

test("refuses an unpinned or ambiguous API rewrite", () => {
  const values = fixtures();
  delete values.version.config.rewrites[0].run.tag;
  assert.throws(
    () => selectPinnedRevision(values.version, values.service, { versionName, region: input.region }),
    /exactly one pinned tracker API rewrite/,
  );

  const duplicate = fixtures();
  duplicate.version.config.rewrites.push(duplicate.version.config.rewrites[0]);
  assert.throws(
    () => selectPinnedRevision(duplicate.version, duplicate.service, { versionName, region: input.region }),
    /exactly one pinned tracker API rewrite/,
  );
});

test("refuses a Hosting version pinned to a newer API commit", () => {
  const values = fixtures();
  values.revision.annotations["source-commit"] = "b".repeat(40);
  assert.throws(
    () => verifyRevisionProvenance(values.revision, {
      ...input,
      revision: "tracker-reader-00001-old",
    }),
    /different commit/,
  );
});

test("refuses malformed operator input before an API request", () => {
  assert.throws(() => validateRollbackInputs({ ...input, targetCommit: "main" }), /target commit/);
  assert.throws(() => validateRollbackInputs({ ...input, siteId: "bad/site" }), /Hosting site id/);
});

