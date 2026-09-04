package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCostGuardAcceptsFoundationAllowlist(t *testing.T) {
	input := planJSON(`[
    {"address":"google_firebase_hosting_site.tracker","type":"google_firebase_hosting_site","values":{}},
    {"address":"google_identity_platform_config.tracker","type":"google_identity_platform_config","values":{"authorized_domains":["tracker.martcoca.com"]}},
    {"address":"google_identity_platform_default_supported_idp_config.google","type":"google_identity_platform_default_supported_idp_config","values":{"idp_id":"google.com","enabled":true}},
	{"address":"google_service_account.reader","type":"google_service_account","values":{"account_id":"reader-synthetic","project":"project-synthetic"}},
	{"address":"google_firestore_database.events","type":"google_firestore_database","values":{"name":"(default)","type":"FIRESTORE_NATIVE","database_edition":"STANDARD","point_in_time_recovery_enablement":"POINT_IN_TIME_RECOVERY_DISABLED","delete_protection_state":"DELETE_PROTECTION_ENABLED","deletion_policy":"ABANDON"}},
	{"address":"google_project_iam_member.runtime_firestore","type":"google_project_iam_member","values":{"role":"roles/datastore.user","member":"serviceAccount:reader-synthetic@project-synthetic.iam.gserviceaccount.com","condition":[{"expression":"resource.name == \"projects/project-synthetic/databases/(default)\""}]}},
	{"address":"google_project_iam_member.runtime_hosting","type":"google_project_iam_member","values":{"role":"roles/firebasehosting.admin","member":"serviceAccount:reader-synthetic@project-synthetic.iam.gserviceaccount.com","condition":[]}},
    {"address":"google_cloud_run_v2_service_iam_member.public","type":"google_cloud_run_v2_service_iam_member","values":{"role":"roles/run.invoker","member":"allUsers"}}
  ]`)
	var output bytes.Buffer
	if err := run(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "idle cost zero") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestCostGuardRejectsPaidOrBroadFirestore(t *testing.T) {
	base := `
	{"address":"google_firebase_hosting_site.tracker","type":"google_firebase_hosting_site","values":{}},
	{"address":"google_identity_platform_config.tracker","type":"google_identity_platform_config","values":{"authorized_domains":["tracker.martcoca.com"]}},
	{"address":"google_identity_platform_default_supported_idp_config.google","type":"google_identity_platform_default_supported_idp_config","values":{"idp_id":"google.com","enabled":true}},
	{"address":"google_service_account.reader","type":"google_service_account","values":{"account_id":"reader-synthetic","project":"project-synthetic"}},`
	database := `{"address":"google_firestore_database.events","type":"google_firestore_database","values":{"name":"(default)","type":"FIRESTORE_NATIVE","database_edition":"STANDARD","point_in_time_recovery_enablement":"POINT_IN_TIME_RECOVERY_DISABLED","delete_protection_state":"DELETE_PROTECTION_ENABLED","deletion_policy":"ABANDON"}}`
	member := `{"address":"google_project_iam_member.runtime_firestore","type":"google_project_iam_member","values":{"role":"roles/datastore.user","member":"serviceAccount:reader-synthetic@project-synthetic.iam.gserviceaccount.com","condition":[{"expression":"resource.name == \"projects/project-synthetic/databases/(default)\""}]}}`
	hosting := `{"address":"google_project_iam_member.runtime_hosting","type":"google_project_iam_member","values":{"role":"roles/firebasehosting.admin","member":"serviceAccount:reader-synthetic@project-synthetic.iam.gserviceaccount.com","condition":[]}}`
	for name, resources := range map[string]string{
		"paid recovery":         base + strings.Replace(database, "POINT_IN_TIME_RECOVERY_DISABLED", "POINT_IN_TIME_RECOVERY_ENABLED", 1) + "," + member + "," + hosting,
		"broad firestore grant": base + database + "," + strings.Replace(member, `resource.name == \"projects/project-synthetic/databases/(default)\"`, "true", 1) + "," + hosting,
		"broad hosting grant":   base + database + "," + member + "," + strings.Replace(hosting, "roles/firebasehosting.admin", "roles/editor", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(strings.NewReader(planJSON("["+resources+"]")), &bytes.Buffer{}); err == nil {
				t.Fatal("unsafe Firestore plan passed")
			}
		})
	}
}

func TestCostGuardRejectsIdleInstanceAndNonAllowlistedResource(t *testing.T) {
	for name, resources := range map[string]string{
		"idle instance": `[{"address":"google_cloud_run_v2_service.reader","type":"google_cloud_run_v2_service","values":{"deletion_protection":true,"template":[{"scaling":[{"min_instance_count":1}]}]}}]`,
		"database":      `[{"address":"google_sql_database_instance.write","type":"google_sql_database_instance","values":{}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(strings.NewReader(planJSON(resources)), &bytes.Buffer{}); err == nil {
				t.Fatal("unsafe plan passed")
			}
		})
	}
}

func planJSON(resources string) string {
	return `{"planned_values":{"root_module":{"resources":` + resources + `}}}`
}
