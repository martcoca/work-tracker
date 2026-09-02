package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/tenant"
)

func TestPublishBuildsVerifiedRepositoryExport(t *testing.T) {
	now := time.Date(2030, time.March, 4, 12, 0, 0, 0, time.UTC)
	repository := t.TempDir()
	writeRepositoryPacket(t, repository, "packet-done", "done", "")
	writeRepositoryPacket(t, repository, "packet-old", "superseded", "")
	writeRepositoryPacket(t, repository, "packet-new", "not started", "packet-old")
	if err := os.MkdirAll(filepath.Join(repository, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "evidence", "packet-done.md"), []byte("proof\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	initializeGitRepository(t, repository)

	directoryContents := directoryExport(t, now, []tenant.Record{{
		ID: "tenant-synthetic", Slug: "synthetic", DisplayName: "Synthetic Tenant",
		Status: tenant.StatusActive, CreatedAt: "2029-01-01T00:00:00Z", Version: 1,
	}})
	client := fixtureClient(directoryContents)

	result, err := publish(context.Background(), config{
		repositoryRoot: repository, outputDirectory: "dist", tenantDirectoryURL: "https://identity.example.invalid/tenant-directory.json",
	}, client, now)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if result.packetCount != 3 || result.schema != packetexport.Schema || result.repository != "synthetic/work-tracker" {
		t.Fatalf("publication = %+v", result)
	}
	verified, err := packetexport.VerifyFile(result.path, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(verified.Packets) != 3 {
		t.Fatalf("packet count = %d", len(verified.Packets))
	}
	var old packetexport.Record
	for _, record := range verified.Packets {
		if record.ID == "packet-old" {
			old = record
		}
	}
	if old.Closure == nil || old.Closure.Reason != "superseded" || old.SupersededBy == nil || *old.SupersededBy != "packet-new" {
		t.Fatalf("superseded record = %+v", old)
	}
	if _, err := os.Stat(filepath.Join(repository, "dist", packetexport.FileName)); err != nil {
		t.Fatalf("published file: %v", err)
	}
}

func TestPublishRefusesAmbiguousActiveTenant(t *testing.T) {
	now := time.Date(2030, time.March, 4, 12, 0, 0, 0, time.UTC)
	records := []tenant.Record{
		{ID: "tenant-a", Slug: "a", DisplayName: "Tenant A", Status: tenant.StatusActive, CreatedAt: "2029-01-01T00:00:00Z", Version: 1},
		{ID: "tenant-b", Slug: "b", DisplayName: "Tenant B", Status: tenant.StatusActive, CreatedAt: "2029-01-01T00:00:00Z", Version: 1},
	}
	output := filepath.Join(t.TempDir(), "must-not-exist")
	_, err := publish(context.Background(), config{
		repositoryRoot: t.TempDir(), outputDirectory: output, tenantDirectoryURL: "https://identity.example.invalid/tenant-directory.json",
	}, fixtureClient(directoryExport(t, now, records)), now)
	if err == nil || !strings.Contains(err.Error(), "exactly one active tenant; found 2") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("failed publication created output: %v", statErr)
	}
}

func TestTenantDirectoryEndpointPolicy(t *testing.T) {
	for _, invalid := range []string{
		"", "tenant-directory.json", "http://example.com/tenant-directory.json",
		"https://user@example.com/tenant-directory.json", "https://example.com/tenant-directory.json?token=no",
	} {
		if err := validateEndpoint(invalid); err == nil {
			t.Errorf("validateEndpoint(%q) unexpectedly passed", invalid)
		}
	}
	for _, valid := range []string{
		"https://identity.example.com/tenant-directory.json", "http://localhost:8080/tenant-directory.json", "http://127.0.0.1:8080/tenant-directory.json",
	} {
		if err := validateEndpoint(valid); err != nil {
			t.Errorf("validateEndpoint(%q) = %v", valid, err)
		}
	}
}

func directoryExport(t *testing.T, now time.Time, records []tenant.Record) []byte {
	t.Helper()
	envelope, err := contract.Build(tenant.Schema, records, contract.Publication{
		PublishedAt: now,
		Source:      contract.Source{Repository: "synthetic/identity", Commit: strings.Repeat("d", 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := contract.Serialize(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func fixtureClient(contents []byte) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(contents))),
			Request:    request,
		}, nil
	})}
}

func writeRepositoryPacket(t *testing.T, repository, id, status, supersedes string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repository, "packets"), 0o755); err != nil {
		t.Fatal(err)
	}
	extra := ""
	if supersedes != "" {
		extra = "- **Supersedes:** `" + supersedes + "`\n"
	}
	contents := "# Packet\n\n" +
		"- **Packet id:** `" + id + "`\n" +
		"- **Status:** " + status + "\n" + extra +
		"\n## Context\n\ncontext\n\n## Goal\n\ngoal\n\n## Boundary\n\nboundary\n" +
		"\n## Done when\n\ndone when\n\n## Check\n\ngo test ./...\n"
	if err := os.WriteFile(filepath.Join(repository, "packets", id+".md"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func initializeGitRepository(t *testing.T, repository string) {
	t.Helper()
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "-c", "user.name=Synthetic Author", "-c", "user.email=synthetic@example.invalid", "commit", "-qm", "initial")
	runGit(t, repository, "remote", "add", "origin", "https://github.com/synthetic/work-tracker.git")
}

func runGit(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
