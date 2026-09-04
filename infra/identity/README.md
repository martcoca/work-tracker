# Identity plan handoff

This directory owns the slowly-changing sign-in, Firebase project/site, durable event
database, runtime identity, and public invocation policy for `tracker.martcoca.com`. Cloud
Run revision delivery moved to `infra/deploy`; this foundation deliberately does not
receive the merge deploy identity.

The plan expects an existing billing-enabled project and protected values for every variable.
Real identifiers and the Google OAuth client secret stay outside the repository. The Google
provider stores the OAuth secret in raw state, so the Founder must select an encrypted,
access-controlled state backend before apply.

The callback is `https://tracker.martcoca.com/__/auth/handler`; the logout return is
`https://tracker.martcoca.com/signed-out`. DNS and certificate readiness for that hostname
remain explicitly human-owned: the registrar zone stays where the Founder controls it, and
this repository receives no DNS credential. `apply_prerequisites` prints the exact CNAME
target derived from `hosting_site_id` and the exact `_acme-challenge` TXT value supplied as
`custom_domain_acme_challenge`. The human step is to register the custom domain in Firebase
Hosting, publish both output records at the registrar, and wait for Firebase to report the
certificate connected. The public challenge value is an input rather than a committed live
identifier, so the output stays accurate if Firebase rotates it.

Pre-provisioned Identity Platform users with signed `tenant_id` custom claims also remain
apply prerequisites. No merge workflow receives the Google OAuth client secret or authority
to change Identity Platform.

The packet event log uses the `(default)` Firestore Native database in Standard edition,
located with Cloud Run. It has no provisioned capacity or warm replica. Paid point-in-time
recovery is disabled, database deletion protection is enabled, and the OpenTofu deletion
policy abandons rather than deletes it. The runtime receives `roles/datastore.user`,
conditioned on the exact default database resource. It receives no database administration,
backup, import/export, or IAM-management role.

The runtime also publishes the reconciled packet export by cloning the current Firebase
Hosting version, replacing only `packets.json`, and releasing the clone. Firebase Hosting
does not support custom roles or a file/site-scoped writer, so `roles/firebasehosting.admin`
in this dedicated tracker project is the narrowest supported publication grant. The runtime
uses its attached service-account identity and short-lived metadata token; no service-account
key or other long-lived credential is configured. Its publisher implements no site create,
update, disable, or delete call.

Firestore Standard provides one free database per project with 1 GiB storage, 50,000
document reads/day, 20,000 writes/day, 20,000 deletes/day, and 10 GiB outbound transfer per
month. This plan provisions no paid-at-idle feature. In `us-central1` beyond the free tier,
published list prices are $0.03/100,000 reads, $0.09/100,000 writes, $0.01/100,000 deletes,
and about $0.15/GiB-month of stored data. See the
[Firestore pricing page](https://cloud.google.com/firestore/pricing).

After the Founder applies this foundation change, an authorized real-store check can use
an existing project without printing its identifier:

```sh
FIRESTORE_INTEGRATION_PROJECT_ID=... scripts/cloud/gcp/check-firestore-store.sh
```

The check leaves a uniquely named, small append-only namespace in place as evidence; it
does not delete or rewrite events.

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
