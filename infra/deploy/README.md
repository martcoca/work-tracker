# Merge delivery stack

This root owns only the existing `tracker-reader` Cloud Run service. Its GCS backend prefix
is `work-tracker/delivery`; the bucket is supplied at initialization and is not committed.
Every apply requires an immutable image digest and a full merge commit. The commit is set
on the Cloud Run revision as the `source-commit` annotation.

The revision explicitly receives `FIRESTORE_DATABASE_ID=(default)`. Its existing runtime
identity is granted the conditioned data-plane role by `infra/identity`; delivery cannot
grant or widen that role.

The Firebase CLI configuration is an output of the same applied inputs. It deliberately
uses the CLI schema (`source`, header lists, and `pinTag`); no REST `ServingConfig` is
manufactured from it. The deploy workflow writes this output to a temporary `firebase.json`
and supplies the merge commit as the Hosting release message and as a file in the released
version.

`google_cloud_run_v2_service.reader` has the same address it had in `infra/identity`, so
the migration moves that address between state files without importing, recreating, or
renaming the live service. `scripts/cloud/gcp/migrate-state.sh` performs the guarded move.

After migration, a second checkout supplies the currently deployed digest and the intended
full source commit through `TF_VAR_container_image` and `TF_VAR_source_commit`, along with
the non-secret deployment coordinates, then runs:

```sh
scripts/cloud/gcp/check-remote-state.sh
```

The check refuses an unversioned bucket, initializes the committed
`work-tracker/delivery` prefix, acquires its lock while planning, and passes the saved plan
through the same deployment guard as CI. It never applies. The state object is
`gs://$GCP_STATE_BUCKET/work-tracker/delivery/default.tfstate`; the bucket name remains a
protected deployment coordinate rather than tracked configuration.

## Rollback

The `Rollback` GitHub Actions workflow takes one input: the full commit of a version that
the `Deploy` workflow previously released. Dispatch it on `main`. To roll forward, dispatch
the same workflow again with the commit that was live before the rollback.

The workflow finds the retained live-channel Hosting release whose message is exactly that
commit. Exact equality is deliberate: the deployment's earlier `packet-authority-preflight`
release carries new static files while it is still pinned to the previous API. Before
changing live traffic, the workflow verifies that the selected finalized Hosting version
has exactly one `/api/**` rewrite to `tracker-reader`, resolves its Cloud Run tag to a
retained revision, and requires that revision's `source-commit` annotation to equal the
same input commit. A missing version, missing pin, reconciling service, unknown revision,
or commit mismatch refuses the rollback.

The only write is then one Firebase Hosting live release of that already-finalized version.
Because the Hosting version contains the pinned Cloud Run tag, its frontend and API move
together; there is no independent Cloud Run traffic command that could leave an old
frontend talking to a newer API. The workflow finishes by fetching that version's unique
commit marker from `https://tracker.martcoca.com` over TLS.

A pin that sits on the **latest ready** revision is reported differently: Cloud Run keeps
it as a named revision target in the service spec but folds it into the latest-revision
entry of `trafficStatuses`, which carries the tag and no revision at all. Rolling *forward*
to the commit currently deployed is exactly that shape, so the policy resolves such a pin
through the service's own `latestReadyRevision` and still requires the resolved revision's
`source-commit` annotation to equal the requested commit.

### How far back a rollback reaches

That mechanism depends entirely on the pin outliving the deploy that created it, and for a
long time it did not. `traffic` was declared in this stack and not ignored, so every apply
sent `[{LATEST, 100}]` and dropped the `fh-` tag Firebase had added; seventeen retained
revisions carried exactly one tag, on the revision already serving, and the only rollback
the design permitted was a no-op. The apply now ignores `traffic` — routing is
`LATEST`, so it follows the newest ready revision without being re-asserted — and the two
jobs the apply was doing implicitly moved to `scripts/deploy/prune-run-tags.mjs`, which
runs after the Hosting refresh:

- it refuses any allocation other than one latest-revision target at 100% with
  zero-percent named pins, which is the drift the apply used to correct; and
- it keeps the newest `RETAINED_PIN_TAGS` pins and reclaims the rest.

The bound is **ten**, and the Firebase CLI reuses the pin already on the latest ready
revision rather than adding a second, so ten pins are ten deploys of rollback reach. A pin
is a zero-percent traffic target on a revision that already exists and scales to zero, so
it configures no compute and costs nothing; the bound is there because unbounded growth is
a leak. Rolling back past it is refused rather than approximated — `selectPinnedRevision`
finds no retained revision for the recorded tag and stops.
