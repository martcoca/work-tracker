package main

import (
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/runtimeexport"
	"github.com/martcoca/work-tracker/tenant"
)

func TestFixtureBuildsEveryFreshDependency(t *testing.T) {
	now := time.Date(2035, time.May, 6, 12, 0, 0, 0, time.UTC)
	documents, err := buildDocuments(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packetexport.Verify(documents["/packets.json"], now); err != nil {
		t.Fatal(err)
	}
	if _, err := tenant.Parse(documents["/tenant-directory.json"], now); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.Verify(documents["/agent-grants.json"], runtimeexport.AgentGrantsSchema, now); err != nil {
		t.Fatal(err)
	}
}
