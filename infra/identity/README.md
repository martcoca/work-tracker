# Identity plan handoff

This directory owns the slowly-changing sign-in, Firebase project/site, runtime identity,
and public invocation policy for `tracker.martcoca.com`. Cloud Run revision delivery moved
to `infra/deploy`; this foundation deliberately does not receive the merge deploy identity.

The plan expects an existing billing-enabled project and protected values for every variable.
Real identifiers and the Google OAuth client secret stay outside the repository. The Google
provider stores the OAuth secret in raw state, so the Founder must select an encrypted,
access-controlled state backend before apply.

The callback is `https://tracker.martcoca.com/__/auth/handler`; the logout return is
`https://tracker.martcoca.com/signed-out`. DNS and certificate readiness for that hostname
and pre-provisioned Identity Platform users with signed `tenant_id` custom claims remain
apply prerequisites. No merge workflow receives the Google OAuth client secret or
authority to change Identity Platform.

The checked synthetic plan is produced without contacting a project:

```sh
tofu -chdir=infra/identity init -backend=false
tofu -chdir=infra/identity validate
tofu -chdir=infra/identity test -no-color
GOOGLE_OAUTH_ACCESS_TOKEN=synthetic-plan-not-a-credential \
  tofu -chdir=infra/identity plan -refresh=false -input=false -lock=false \
  -var-file=../../config/example/identity.tfvars -out=/tmp/work-tracker-identity.tfplan
scripts/cloud/gcp/cost-guard.sh /tmp/work-tracker-identity.tfplan
```

The GCS backend prefix is `work-tracker/foundation`; the versioned bucket is supplied at
initialization and never committed. The existing local state must be migrated before this
configuration is planned after the Cloud Run resource split. Follow
`scripts/cloud/gcp/migrate-state.sh`; planning first would incorrectly propose removal.
