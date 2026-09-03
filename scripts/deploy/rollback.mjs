import { execFileSync } from "node:child_process";
import { pathToFileURL } from "node:url";

import { GoogleAuth } from "google-auth-library";

import {
  selectDeployedVersion,
  selectPinnedRevision,
  validateRollbackInputs,
  verifyRevisionProvenance,
} from "./rollback-policy.mjs";

const hostingAPI = "https://firebasehosting.googleapis.com/v1beta1";
const runAPI = "https://run.googleapis.com/v2";
const publicOrigin = "https://tracker.martcoca.com";

function quotePath(value) {
  return value.split("/").map(encodeURIComponent).join("/");
}

async function request(client, options) {
  const response = await client.request(options);
  return response.data;
}

async function listLiveReleases(client, siteId) {
  const releases = [];
  const seenTokens = new Set();
  let pageToken = "";
  do {
    const query = new URLSearchParams({ pageSize: "100" });
    if (pageToken) query.set("pageToken", pageToken);
    const page = await request(client, {
      url: `${hostingAPI}/sites/${encodeURIComponent(siteId)}/channels/live/releases?${query}`,
    });
    if (page?.releases !== undefined && !Array.isArray(page.releases)) {
      throw new Error("Hosting returned malformed release history");
    }
    releases.push(...(page?.releases ?? []));
    pageToken = page?.nextPageToken ?? "";
    if (pageToken && seenTokens.has(pageToken)) {
      throw new Error("Hosting repeated a release-history page token");
    }
    seenTokens.add(pageToken);
  } while (pageToken);
  return releases;
}

function assertAncestor(targetCommit) {
  try {
    execFileSync("git", ["merge-base", "--is-ancestor", targetCommit, "HEAD"], { stdio: "ignore" });
  } catch {
    throw new Error("target commit is not an ancestor of the checked-out main branch");
  }
}

async function waitForLiveCommit(targetCommit, runId) {
  const url = `${publicOrigin}/source-commit-${targetCommit}.txt?rollback-run=${encodeURIComponent(runId)}`;
  for (let attempt = 1; attempt <= 12; attempt += 1) {
    try {
      const response = await fetch(url, { cache: "no-store" });
      if (response.ok && (await response.text()).trim() === targetCommit) return;
    } catch {
      // The bounded retry below handles propagation and transient network failure alike.
    }
    if (attempt < 12) await new Promise((resolve) => setTimeout(resolve, 5000));
  }
  throw new Error("live TLS endpoint did not serve the selected Hosting commit");
}

export async function rollback(env = process.env) {
  const input = validateRollbackInputs({
    targetCommit: env.TARGET_COMMIT,
    projectId: env.PROJECT_ID,
    region: env.REGION,
    siteId: env.HOSTING_SITE_ID,
  });
  assertAncestor(input.targetCommit);

  const auth = new GoogleAuth({ scopes: ["https://www.googleapis.com/auth/cloud-platform"] });
  const client = await auth.getClient();
  const releases = await listLiveReleases(client, input.siteId);
  const versionName = selectDeployedVersion(releases, input.siteId, input.targetCommit);
  const version = await request(client, { url: `${hostingAPI}/${quotePath(versionName)}` });

  const serviceName = `projects/${input.projectId}/locations/${input.region}/services/tracker-reader`;
  const service = await request(client, { url: `${runAPI}/${quotePath(serviceName)}` });
  const pin = selectPinnedRevision(version, service, { versionName, region: input.region });
  const revisionName = `${serviceName}/revisions/${pin.revision}`;
  const revision = await request(client, { url: `${runAPI}/${quotePath(revisionName)}` });
  verifyRevisionProvenance(revision, { ...input, revision: pin.revision });

  console.log(`verified target commit ${input.targetCommit}`);
  console.log(`Hosting version ${versionName} pins Cloud Run revision ${pin.revision}`);

  // This is the only state-changing request. Releasing the already-finalized Hosting
  // version changes the static frontend and its embedded Cloud Run tag together.
  const runId = /^[0-9]+$/.test(env.GITHUB_RUN_ID ?? "") ? env.GITHUB_RUN_ID : "local";
  const message = `rollback ${input.targetCommit} via GitHub Actions run ${runId}`;
  const query = new URLSearchParams({ versionName });
  const release = await request(client, {
    url: `${hostingAPI}/sites/${encodeURIComponent(input.siteId)}/channels/live/releases?${query}`,
    method: "POST",
    data: { message },
  });
  if (release?.version?.name !== versionName) {
    throw new Error("Hosting created a live release for a different version");
  }

  await waitForLiveCommit(input.targetCommit, runId);
  console.log(`released ${release.name} (${release.type ?? "release"})`);
  console.log(`PASS: live frontend and pinned API now resolve to ${input.targetCommit}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  rollback().catch((error) => {
    console.error(`rollback refused: ${error.message}`);
    process.exitCode = 1;
  });
}
