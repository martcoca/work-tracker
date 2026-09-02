package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

const (
	testImage  = "registry.invalid/tracker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestDeployGuardAcceptsOnlyTheExactZeroIdleRevision(t *testing.T) {
	var output bytes.Buffer
	if err := run(strings.NewReader(planJSON(`
      "actions":["update"],
      "after":{
        "deletion_protection":true,
        "template":[{
          "annotations":{"source-commit":"`+testCommit+`"},
          "scaling":[{"min_instance_count":0}],
          "containers":[{"image":"`+testImage+`","resources":[{"cpu_idle":true}]}]
        }]
      }`)), &output, testImage, testCommit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "idle cost zero") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestDeployGuardRejectsDestroyCreateCostAndUnprovenProvenance(t *testing.T) {
	validAfter := `"deletion_protection":true,"template":[{"annotations":{"source-commit":"` + testCommit + `"},"scaling":[{"min_instance_count":0}],"containers":[{"image":"` + testImage + `","resources":[{"cpu_idle":true}]}]}]`
	tests := map[string]string{
		"destroy":       `"actions":["delete"],"after":null`,
		"replace":       `"actions":["delete","create"],"after":{` + validAfter + `}`,
		"create":        `"actions":["create"],"after":{` + validAfter + `}`,
		"idle instance": `"actions":["update"],"after":{` + strings.Replace(validAfter, `"min_instance_count":0`, `"min_instance_count":1`, 1) + `}`,
		"cpu always on": `"actions":["update"],"after":{` + strings.Replace(validAfter, `"cpu_idle":true`, `"cpu_idle":false`, 1) + `}`,
		"wrong digest":  `"actions":["update"],"after":{` + strings.Replace(validAfter, testImage, "registry.invalid/tracker:latest", 1) + `}`,
		"wrong commit":  `"actions":["update"],"after":{` + strings.Replace(validAfter, testCommit, strings.Repeat("c", 40), 1) + `}`,
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(strings.NewReader(planJSON(change)), &bytes.Buffer{}, testImage, testCommit); err == nil {
				t.Fatal("unsafe deploy plan passed")
			}
		})
	}
}

func TestDeployGuardRejectsASecondResource(t *testing.T) {
	input := fmt.Sprintf(`{"resource_changes":[
      {"address":"%s","type":"google_cloud_run_v2_service","change":{"actions":["no-op"],"after":{}}},
      {"address":"google_storage_bucket.extra","type":"google_storage_bucket","change":{"actions":["create"],"after":{}}}
    ]}`, readerAddress)
	if err := run(strings.NewReader(input), &bytes.Buffer{}, testImage, testCommit); err == nil {
		t.Fatal("additional infrastructure passed")
	}
}

func planJSON(change string) string {
	return `{"resource_changes":[{"address":"` + readerAddress + `","type":"google_cloud_run_v2_service","change":{` + change + `}}]}`
}
