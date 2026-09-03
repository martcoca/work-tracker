const fullCommit = /^[0-9a-f]{40}$/;
const projectID = /^[a-z][a-z0-9-]{4,61}[a-z0-9]$/;
const regionID = /^[a-z][a-z0-9-]{0,62}$/;
const siteID = /^[a-z0-9][a-z0-9-]{0,62}$/;
const revisionID = /^[a-z0-9][a-z0-9-]{0,62}$/;

function requireMatch(value, pattern, name) {
  if (typeof value !== "string" || !pattern.test(value)) {
    throw new Error(`${name} is absent or malformed`);
  }
  return value;
}

export function validateRollbackInputs(input) {
  return {
    targetCommit: requireMatch(input.targetCommit, fullCommit, "target commit"),
    projectId: requireMatch(input.projectId, projectID, "project id"),
    region: requireMatch(input.region, regionID, "region"),
    siteId: requireMatch(input.siteId, siteID, "Hosting site id"),
  };
}

export function selectDeployedVersion(releases, siteId, targetCommit) {
  if (!Array.isArray(releases)) {
    throw new Error("Hosting release history is not an array");
  }

  const prefix = `sites/${siteId}/versions/`;
  const candidates = releases
    .filter((release) => release?.message === targetCommit)
    .filter((release) => typeof release?.version?.name === "string" && release.version.name.startsWith(prefix))
    .sort((left, right) => String(right.releaseTime).localeCompare(String(left.releaseTime)));

  if (candidates.length === 0) {
    throw new Error("no final live Hosting release carries the target commit exactly");
  }

  // Deploy publishes a preflight version with a suffixed message and a final version with
  // the bare commit. Requiring exact equality prevents selecting the preflight version,
  // whose static files are new while its Cloud Run pin is intentionally still old.
  return candidates[0].version.name;
}

export function selectPinnedRevision(version, service, expected) {
  if (version?.name !== expected.versionName) {
    throw new Error("Hosting returned a different version than the selected release");
  }
  if (version?.status !== "FINALIZED") {
    throw new Error("selected Hosting version is not finalized");
  }

  const rewrites = Array.isArray(version?.config?.rewrites) ? version.config.rewrites : [];
  const apiPins = rewrites.filter((rewrite) =>
    rewrite?.glob === "/api/**" &&
    rewrite?.run?.serviceId === "tracker-reader" &&
    rewrite?.run?.region === expected.region &&
    typeof rewrite?.run?.tag === "string" &&
    rewrite.run.tag.length > 0,
  );
  if (apiPins.length !== 1) {
    throw new Error("Hosting version does not contain exactly one pinned tracker API rewrite");
  }

  if (service?.reconciling !== false) {
    throw new Error("Cloud Run service is reconciling or its state is unknown");
  }
  const traffic = Array.isArray(service?.trafficStatuses) ? service.trafficStatuses : [];
  const matches = traffic.filter((target) => target?.tag === apiPins[0].run.tag);
  if (matches.length !== 1) {
    throw new Error("Hosting pin does not resolve to exactly one retained Cloud Run revision");
  }

  const revision = requireMatch(matches[0].revision, revisionID, "pinned Cloud Run revision");
  return { tag: apiPins[0].run.tag, revision };
}

export function verifyRevisionProvenance(revision, expected) {
  const expectedName = `projects/${expected.projectId}/locations/${expected.region}/services/tracker-reader/revisions/${expected.revision}`;
  if (revision?.name !== expectedName) {
    throw new Error("Cloud Run returned a different revision than the Hosting pin");
  }
  if (revision?.annotations?.["source-commit"] !== expected.targetCommit) {
    throw new Error("pinned Cloud Run revision was built from a different commit");
  }
}

