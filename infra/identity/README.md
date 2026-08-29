# Identity plan handoff

This directory authors the human sign-in and read-only hosting configuration for
`tracker.martcoca.com`. It deliberately stops at a plan. A worker must not run `tofu apply`,
deploy Firebase Hosting, bind DNS, provision a certificate, create users, or set claims.

The plan expects an existing billing-enabled project and protected values for every variable.
Real identifiers and the Google OAuth client secret stay outside the repository. The Google
provider stores the OAuth secret in raw state, so the Founder must select an encrypted,
access-controlled state backend before apply.

The callback is `https://tracker.martcoca.com/__/auth/handler`; the logout return is
`https://tracker.martcoca.com/signed-out`. DNS and certificate readiness for that hostname,
pre-provisioned Identity Platform users with signed `tenant_id` custom claims, and an
immutable reader image containing the last verified exports are apply prerequisites.

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

The `firebase_hosting_configuration` output is the configuration the Founder deploys after
the prerequisites exist. It serves `dist/`, rewrites same-origin `/api/**` reads to the
Cloud Run service, and leaves Firebase's reserved `/__/auth/handler` on the settled host.
