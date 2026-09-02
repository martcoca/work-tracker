// Command verify-runtime-exports proves the public authority inputs are reachable,
// authentic, and fresh before a deployment can change the running revision.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/martcoca/work-tracker/runtimeexport"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "runtime export verification: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := runtimeexport.ConfigFromEnvironment()
	if err != nil {
		return err
	}
	reader, err := runtimeexport.New(config, nil, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := reader.Refresh(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := reader.Ready(now); err != nil {
		return err
	}
	statuses := reader.ExportStatuses(now)
	for _, required := range []runtimeexport.ExportName{runtimeexport.TenantDirectory, runtimeexport.AgentGrants} {
		found := false
		for _, status := range statuses {
			if status.Name != string(required) {
				continue
			}
			found = true
			if !status.Available || status.Stale || !status.Required || status.ServiceOwned {
				return fmt.Errorf("%s did not verify as fresh required authority", required)
			}
			fmt.Printf("verified %s expires_at=%s\n", required, status.ExpiresAt)
		}
		if !found {
			return fmt.Errorf("%s status is missing", required)
		}
	}
	fmt.Println("PASS: required authority exports are reachable, authentic, and fresh")
	return nil
}
