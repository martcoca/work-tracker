// Command verify-runtime-exports proves the public authority inputs are reachable,
// authentic, and fresh before a deployment can change the running revision.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/martcoca/work-tracker/runtimeexport"
	"github.com/martcoca/work-tracker/surface"
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
	if err := verifyStatuses(statuses); err != nil {
		return err
	}
	for _, status := range statuses {
		fmt.Printf("verified %s expires_at=%s\n", status.Name, status.ExpiresAt)
	}
	fmt.Println("PASS: packet and required authority exports are reachable, authentic, and fresh")
	return nil
}

func verifyStatuses(statuses []surface.HeldExportStatus) error {
	byName := make(map[string]surface.HeldExportStatus, len(statuses))
	for _, status := range statuses {
		byName[status.Name] = status
	}
	for _, name := range []runtimeexport.ExportName{runtimeexport.Packets, runtimeexport.TenantDirectory, runtimeexport.AgentGrants} {
		status, found := byName[string(name)]
		if !found {
			return fmt.Errorf("%s status is missing", name)
		}
		if !status.Available || status.Stale {
			return fmt.Errorf("%s did not verify as a fresh live export", name)
		}
		if name == runtimeexport.Packets {
			if status.Required || !status.ServiceOwned {
				return fmt.Errorf("%s export policy changed unexpectedly", name)
			}
			continue
		}
		if !status.Required || status.ServiceOwned {
			return fmt.Errorf("%s did not verify as required external authority", name)
		}
	}
	return nil
}
