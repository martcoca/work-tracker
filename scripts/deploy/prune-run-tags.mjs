// Re-assert the live traffic allocation after a deploy and bound how many Hosting pins
// survive. The apply no longer manages traffic — it was erasing every pin but the newest,
// which left exactly one rollback target and that target already serving — so the two
// things the apply used to do implicitly are done here explicitly and can fail.
import { pathToFileURL } from "node:url";

import { GoogleAuth } from "google-auth-library";

import { RETAINED_PIN_TAGS, assertLiveAllocation, selectRetainedTraffic } from "./run-tags.mjs";

const runAPI = "https://run.googleapis.com/v2";
const projectID = /^[a-z][a-z0-9-]{4,61}[a-z0-9]$/;
const regionID = /^[a-z][a-z0-9-]{0,62}$/;

function quotePath(value) {
  return value.split("/").map(encodeURIComponent).join("/");
}

async function listRevisions(client, serviceName) {
  const revisions = [];
  const seenTokens = new Set();
  let pageToken = "";
  do {
    const query = new URLSearchParams({ pageSize: "100" });
    if (pageToken) query.set("pageToken", pageToken);
    const { data } = await client.request({
      url: `${runAPI}/${quotePath(serviceName)}/revisions?${query}`,
    });
    revisions.push(...(data?.revisions ?? []));
    pageToken = data?.nextPageToken ?? "";
    if (pageToken && seenTokens.has(pageToken)) {
      throw new Error("Cloud Run repeated a revision page token");
    }
    seenTokens.add(pageToken);
  } while (pageToken);
  return revisions;
}

export async function pruneRunTags(env = process.env) {
  const projectId = env.PROJECT_ID ?? "";
  const region = env.REGION ?? "";
  if (!projectID.test(projectId) || !regionID.test(region)) {
    throw new Error("project id or region is absent or malformed");
  }

  const auth = new GoogleAuth({ scopes: ["https://www.googleapis.com/auth/cloud-platform"] });
  const client = await auth.getClient();
  const serviceName = `projects/${projectId}/locations/${region}/services/tracker-reader`;
  const { data: service } = await client.request({ url: `${runAPI}/${quotePath(serviceName)}` });

  // Fails the deploy on any allocation the apply would once have corrected: a second
  // latest target, a pin taking live traffic, a percentage that does not total 100.
  const { pins } = assertLiveAllocation(service);

  const selection = selectRetainedTraffic(service, await listRevisions(client, serviceName));
  if (selection === null) {
    console.log(`PASS: ${pins.length}/${RETAINED_PIN_TAGS} rollback pins retained, none to reclaim`);
    return;
  }

  const query = new URLSearchParams({ updateMask: "traffic" });
  const { data: operation } = await client.request({
    url: `${runAPI}/${quotePath(serviceName)}?${query}`,
    method: "PATCH",
    data: { traffic: selection.traffic },
  });
  if (operation?.error) {
    throw new Error(`Cloud Run refused the pin reclamation: ${operation.error.message ?? "unknown"}`);
  }

  console.log(`reclaimed ${selection.dropped.map((target) => target.tag).join(", ")}`);
  console.log(`PASS: ${RETAINED_PIN_TAGS}/${RETAINED_PIN_TAGS} rollback pins retained`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  pruneRunTags().catch((error) => {
    console.error(`pin retention refused: ${error.message}`);
    process.exitCode = 1;
  });
}
