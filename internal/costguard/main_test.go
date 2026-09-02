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
    {"address":"google_service_account.reader","type":"google_service_account","values":{}},
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
