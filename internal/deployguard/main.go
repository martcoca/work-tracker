// Command deployguard refuses a delivery plan that creates or destroys infrastructure,
// configures idle compute, or separates the deployed revision from the inspected digest
// and merge commit carried by its workflow.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const readerAddress = "google_cloud_run_v2_service.reader"

type plan struct {
	ResourceChanges []resourceChange `json:"resource_changes"`
}

type resourceChange struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Change  struct {
		Actions []string       `json:"actions"`
		After   map[string]any `json:"after"`
	} `json:"change"`
}

func main() {
	expectedImage := os.Getenv("EXPECTED_IMAGE")
	expectedCommit := os.Getenv("EXPECTED_COMMIT")
	if expectedImage == "" || expectedCommit == "" {
		fmt.Fprintln(os.Stderr, "deploy guard: EXPECTED_IMAGE and EXPECTED_COMMIT are required")
		os.Exit(1)
	}
	if err := run(os.Stdin, os.Stdout, expectedImage, expectedCommit); err != nil {
		fmt.Fprintf(os.Stderr, "deploy guard: %v\n", err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer, expectedImage, expectedCommit string) error {
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	var candidate plan
	if err := decoder.Decode(&candidate); err != nil {
		return fmt.Errorf("decode plan: %w", err)
	}
	if len(candidate.ResourceChanges) == 0 {
		return errors.New("plan has no resource changes; revision provenance is not demonstrated")
	}
	seen := false
	for _, planned := range candidate.ResourceChanges {
		if planned.Address != readerAddress || planned.Type != "google_cloud_run_v2_service" {
			return fmt.Errorf("%s has non-delivery type %s", planned.Address, planned.Type)
		}
		if seen {
			return errors.New("plan contains the reader more than once")
		}
		seen = true
		if !allowedActions(planned.Change.Actions) {
			return fmt.Errorf("%s has forbidden actions %v", planned.Address, planned.Change.Actions)
		}
		if err := verifyReader(planned.Change.After, expectedImage, expectedCommit); err != nil {
			return err
		}
	}
	if !seen {
		return errors.New("plan is missing the reader revision")
	}
	fmt.Fprintln(output, "PASS: deploy guard (no create/destroy, idle cost zero, exact digest and commit)")
	return nil
}

func allowedActions(actions []string) bool {
	if len(actions) != 1 {
		return false
	}
	return actions[0] == "update" || actions[0] == "no-op"
}

func verifyReader(after map[string]any, expectedImage, expectedCommit string) error {
	if after["deletion_protection"] != true {
		return errors.New("reader must retain deletion protection")
	}
	template, err := oneBlock(after["template"], "template")
	if err != nil {
		return err
	}
	annotations, ok := template["annotations"].(map[string]any)
	if !ok || annotations["source-commit"] != expectedCommit {
		return errors.New("reader revision does not name the expected merge commit")
	}
	scaling, err := oneBlock(template["scaling"], "template.scaling")
	if err != nil {
		return err
	}
	minimum, ok := scaling["min_instance_count"].(json.Number)
	if !ok || minimum.String() != "0" {
		return errors.New("reader configures non-zero idle instances")
	}
	container, err := oneBlock(template["containers"], "template.containers")
	if err != nil {
		return err
	}
	if container["image"] != expectedImage {
		return errors.New("reader image is not the exact inspected registry digest")
	}
	resources, err := oneBlock(container["resources"], "template.containers.resources")
	if err != nil {
		return err
	}
	if resources["cpu_idle"] != true {
		return errors.New("reader disables CPU idling")
	}
	return nil
}

func oneBlock(value any, name string) (map[string]any, error) {
	blocks, ok := value.([]any)
	if !ok || len(blocks) != 1 {
		return nil, fmt.Errorf("%s must contain exactly one block", name)
	}
	block, ok := blocks[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an object", name)
	}
	return block, nil
}
