# Founder-applied deploy trust

This root module is the only Founder-applied part of E05-T01. It does not create a
workload-identity pool or provider: `platform-gcp` already created a pool dedicated to
`work-tracker`, whose provider admits only the immutable repository identity. This module
creates a separate deploy service account and binds one complete subject in that pool:

```text
repo:<owner-id>/<repository-id>:ref:refs/heads/main
```

The validation rejects mutable `owner/name`, every ref except `refs/heads/main`, wildcards,
and pull-request subjects.

The Founder supplies the existing project, project number, region, state bucket, image
repository, runtime account, workload pool, and immutable repository identity as protected
inputs. After reviewing a saved plan, the Founder applies it. That apply grants the deployer:

- Cloud Run Developer on `tracker-reader` only;
- Artifact Registry Reader on the one image repository;
- Service Account User on the runtime identity only; that identity's own data and export
  publication grants are declared in the Founder-applied foundation root;
- Storage Object Admin on the existing versioned state bucket, conditioned so object
  reads/writes cover only `objects/work-tracker/delivery/**`. Foundation state holds the
  Identity Platform OAuth secret and remains unreadable. Cloud Storage evaluates object
  listing on the bucket itself and cannot prefix-restrict that operation, so the condition
  also permits listing object names in this one bucket;
- Firebase Hosting Admin in the dedicated tracker project. Firebase explicitly does not
  support custom roles for Hosting, so this is its narrowest supported write role;
- API Keys Viewer in that project, the read-only companion role Firebase documents as
  required for CLI deploys.

It grants no image upload or deletion, service creation, project IAM administration,
Identity Platform administration, service-account key creation, state-bucket administration,
or access to another project. The existing publisher identity remains the only identity
that can push an image.

Initialize with the already-versioned platform bucket and the committed prefix:

```sh
tofu -chdir=infra/trust init -backend-config="bucket=$GCP_STATE_BUCKET"
tofu -chdir=infra/trust plan -input=false -out="$RUNNER_TEMP/work-tracker-trust.tfplan"
# Founder only, after reviewing the saved plan:
tofu -chdir=infra/trust apply "$RUNNER_TEMP/work-tracker-trust.tfplan"
```

No worker runs the final command.
