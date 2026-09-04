// Command costguard proves that a saved OpenTofu plan contains only the packet's
// allowlisted serverless identity/hosting resources and no configured idle compute.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var allowedTypes = map[string]bool{
	"google_project_service":                                true,
	"google_firebase_project":                               true,
	"google_firebase_web_app":                               true,
	"google_firebase_hosting_site":                          true,
	"google_identity_platform_config":                       true,
	"google_identity_platform_default_supported_idp_config": true,
	"google_service_account":                                true,
	"google_cloud_run_v2_service":                           true,
	"google_cloud_run_v2_service_iam_member":                true,
	// Firestore Standard has no provisioned capacity or warm replica and bills only stored
	// data and operations, both with a documented free tier. Its conditioned IAM member is
	// additive and is checked below for the one data-plane role and one exact database.
	"google_firestore_database": true,
	"google_project_iam_member": true,
}

type plan struct {
	PlannedValues struct {
		RootModule module `json:"root_module"`
	} `json:"planned_values"`
}

type module struct {
	Resources    []resource `json:"resources"`
	ChildModules []module   `json:"child_modules"`
}

type resource struct {
	Address string         `json:"address"`
	Type    string         `json:"type"`
	Values  map[string]any `json:"values"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "cost guard: %v\n", err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	var candidate plan
	if err := decoder.Decode(&candidate); err != nil {
		return fmt.Errorf("decode plan: %w", err)
	}
	resources := flatten(candidate.PlannedValues.RootModule)
	if len(resources) == 0 {
		return errors.New("plan contains no resources")
	}
	runtimeMember, err := expectedRuntimeMember(resources)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, planned := range resources {
		if !allowedTypes[planned.Type] {
			return fmt.Errorf("%s has non-allowlisted type %s", planned.Address, planned.Type)
		}
		seen[planned.Type] = true
		if err := rejectIdleSettings(planned.Address, planned.Values); err != nil {
			return err
		}
		switch planned.Type {
		case "google_cloud_run_v2_service":
			if planned.Values["deletion_protection"] != true {
				return fmt.Errorf("%s must enable deletion protection", planned.Address)
			}
		case "google_cloud_run_v2_service_iam_member":
			if planned.Values["role"] != "roles/run.invoker" || planned.Values["member"] != "allUsers" {
				return fmt.Errorf("%s grants authority beyond the public Cloud Run entrypoint", planned.Address)
			}
		case "google_identity_platform_config":
			domains, _ := planned.Values["authorized_domains"].([]any)
			if len(domains) != 1 || domains[0] != "tracker.martcoca.com" {
				return fmt.Errorf("%s does not authorize exactly tracker.martcoca.com", planned.Address)
			}
		case "google_identity_platform_default_supported_idp_config":
			if planned.Values["idp_id"] != "google.com" || planned.Values["enabled"] != true {
				return fmt.Errorf("%s is not the enabled tier-1 Google provider", planned.Address)
			}
		case "google_firestore_database":
			if err := validateFirestoreDatabase(planned); err != nil {
				return err
			}
		case "google_project_iam_member":
			if err := validateFirestoreMember(planned, runtimeMember); err != nil {
				return err
			}
		}
	}
	for _, required := range []string{
		"google_firebase_hosting_site", "google_identity_platform_config",
		"google_identity_platform_default_supported_idp_config", "google_service_account",
		"google_firestore_database", "google_project_iam_member",
	} {
		if !seen[required] {
			return fmt.Errorf("plan is missing %s", required)
		}
	}
	fmt.Fprintf(output, "PASS: cost guard (%d planned resources, idle cost zero)\n", len(resources))
	return nil
}

func expectedRuntimeMember(resources []resource) (string, error) {
	for _, planned := range resources {
		if planned.Type != "google_service_account" {
			continue
		}
		accountID, accountOK := planned.Values["account_id"].(string)
		project, projectOK := planned.Values["project"].(string)
		if !accountOK || !projectOK || accountID == "" || project == "" {
			return "", fmt.Errorf("%s does not expose its account_id and project", planned.Address)
		}
		return fmt.Sprintf("serviceAccount:%s@%s.iam.gserviceaccount.com", accountID, project), nil
	}
	return "", errors.New("plan is missing google_service_account")
}

func validateFirestoreDatabase(planned resource) error {
	required := map[string]any{
		"name":                              "(default)",
		"type":                              "FIRESTORE_NATIVE",
		"database_edition":                  "STANDARD",
		"point_in_time_recovery_enablement": "POINT_IN_TIME_RECOVERY_DISABLED",
		"delete_protection_state":           "DELETE_PROTECTION_ENABLED",
		"deletion_policy":                   "ABANDON",
	}
	for field, want := range required {
		if planned.Values[field] != want {
			return fmt.Errorf("%s has %s=%v, want %v", planned.Address, field, planned.Values[field], want)
		}
	}
	return nil
}

func validateFirestoreMember(planned resource, runtimeMember string) error {
	if planned.Values["role"] != "roles/datastore.user" || planned.Values["member"] != runtimeMember {
		return fmt.Errorf("%s grants authority beyond the runtime Firestore data role", planned.Address)
	}
	conditions, _ := planned.Values["condition"].([]any)
	if len(conditions) != 1 {
		return fmt.Errorf("%s must have exactly one database condition", planned.Address)
	}
	condition, _ := conditions[0].(map[string]any)
	want := "resource.name == \"projects/" + firestoreProject(runtimeMember) + "/databases/(default)\""
	if condition["expression"] != want {
		return fmt.Errorf("%s is not confined to the default Firestore database", planned.Address)
	}
	return nil
}

func firestoreProject(member string) string {
	const suffix = ".iam.gserviceaccount.com"
	at := strings.LastIndex(member, "@")
	if at == -1 || !strings.HasSuffix(member, suffix) {
		return ""
	}
	return strings.TrimSuffix(member[at+1:], suffix)
}

func flatten(current module) []resource {
	result := append([]resource(nil), current.Resources...)
	for _, child := range current.ChildModules {
		result = append(result, flatten(child)...)
	}
	return result
}

func rejectIdleSettings(address string, value any) error {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "min_instance_count" {
				if number, ok := child.(json.Number); ok && number.String() != "0" {
					return fmt.Errorf("%s configures min_instance_count=%s", address, number)
				}
			}
			if key == "cpu_idle" && child == false {
				return fmt.Errorf("%s disables CPU idling", address)
			}
			if err := rejectIdleSettings(address, child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := rejectIdleSettings(address, child); err != nil {
				return err
			}
		}
	}
	return nil
}
