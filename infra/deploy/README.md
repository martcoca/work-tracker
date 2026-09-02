# Merge delivery stack

This root owns only the existing `tracker-reader` Cloud Run service. Its GCS backend prefix
is `work-tracker/delivery`; the bucket is supplied at initialization and is not committed.
Every apply requires an immutable image digest and a full merge commit. The commit is set
on the Cloud Run revision as the `source-commit` annotation.

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
