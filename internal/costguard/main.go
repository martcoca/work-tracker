// Command costguard proves that a saved OpenTofu plan contains only the packet's
// allowlisted serverless identity/hosting resources and no configured idle compute.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
		}
	}
	for _, required := range []string{
		"google_firebase_hosting_site", "google_identity_platform_config",
		"google_identity_platform_default_supported_idp_config", "google_service_account",
	} {
		if !seen[required] {
			return fmt.Errorf("plan is missing %s", required)
		}
	}
	fmt.Fprintf(output, "PASS: cost guard (%d planned resources, idle cost zero)\n", len(resources))
	return nil
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
