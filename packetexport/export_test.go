package packetexport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packet"
	"github.com/martcoca/work-tracker/tenant"
)

type allowTenants struct{}

func (allowTenants) ValidateTenantID(string, time.Time) error { return nil }

var exportPublication = contract.Publication{
	PublishedAt: time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC),
	Source: contract.Source{
		Repository: "synthetic/tracker",
		Commit:     "dddddddddddddddddddddddddddddddddddddddd",
	},
}

func trackerForExport(t *testing.T) *packet.Tracker {
	t.Helper()
	tracker, err := packet.NewTracker(allowTenants{})
	if err != nil {
		t.Fatalf("new tracker: %v", err)
	}
	return tracker
}

func bodyForExport(label string) packet.Body {
	return packet.Body{
		Goal: "goal " + label, Boundary: "boundary " + label, DoneWhen: "done " + label,
		Check: "check " + label, Context: "context " + label,
	}
}

func issueForExport(t *testing.T, tracker *packet.Tracker, id packet.PacketID) packet.Packet {
	t.Helper()
	projected, err := tracker.Issue(packet.IssueCommand{
		PacketID: id, TenantID: "tenant-synthetic", Body: bodyForExport(string(id)), Actor: "actor-author",
	})
	if err != nil {
		t.Fatalf("issue %s: %v", id, err)
	}
	return projected
}

func TestBuildSerializesCompletePacketsInStableOrder(t *testing.T) {
	tracker := trackerForExport(t)
	issueForExport(t, tracker, "packet-z")
	active := issueForExport(t, tracker, "packet-a")
	var err error
	active, err = tracker.Take(packet.TakeCommand{
		PacketID: active.ID(), ExpectedVersion: active.Version(), Actor: "actor-worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err = tracker.Comment(packet.CommentCommand{
		PacketID: active.ID(), ExpectedVersion: active.Version(), Actor: "actor-reviewer", Text: "synthetic comment",
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err = tracker.Transition(packet.TransitionCommand{
		PacketID: active.ID(), ExpectedVersion: active.Version(), Actor: "actor-worker",
		To: packet.StatusDone, Evidence: []packet.Evidence{"evidence/synthetic.md"},
	})
	if err != nil {
		t.Fatal(err)
	}

	envelope, err := Build(tracker, exportPublication)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if envelope.Schema != "martcoca.tracker.packets/1" {
		t.Fatalf("schema = %q", envelope.Schema)
	}
	var records []Record
	if err := json.Unmarshal(envelope.Payload, &records); err != nil {
		t.Fatal(err)
	}
	if got := []string{records[0].ID, records[1].ID}; !reflect.DeepEqual(got, []string{"packet-a", "packet-z"}) {
		t.Fatalf("packet order = %v", got)
	}
	completed := records[0]
	if completed.TenantID != "tenant-synthetic" || completed.Goal != "goal packet-a" || completed.Status != "done" {
		t.Fatalf("completed packet = %+v", completed)
	}
	if completed.Version != uint64(len(completed.History)) || completed.Version != 6 {
		t.Fatalf("version=%d history=%d", completed.Version, len(completed.History))
	}
	if completed.Closure == nil || completed.Closure.Reason != "done" {
		t.Fatalf("closure = %+v", completed.Closure)
	}
	if got := completed.Evidence; !reflect.DeepEqual(got, []string{"evidence/synthetic.md"}) {
		t.Fatalf("evidence = %v", got)
	}
	if len(completed.Comments) != 1 || completed.Comments[0].Actor != "actor-reviewer" {
		t.Fatalf("comments = %+v", completed.Comments)
	}
	issued := completed.History[0]
	if issued.Body == nil || issued.Body.Goal != completed.Goal || issued.TenantID == nil || *issued.TenantID != completed.TenantID {
		t.Fatalf("issued history = %+v", issued)
	}

	serialized, _, err := Serialize(tracker, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(serialized, exportPublication.PublishedAt.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !reflect.DeepEqual(verified.Packets, records) {
		t.Fatalf("verified packets differ")
	}
}

func TestPacketDigestIgnoresRecordInsertionOrder(t *testing.T) {
	recordA := deterministicRecord("packet-a", "event-a")
	recordB := deterministicRecord("packet-b", "event-b")
	forward, err := BuildRecords([]Record{recordA, recordB}, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := BuildRecords([]Record{recordB, recordA}, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	if forward.Digest != reverse.Digest {
		t.Fatalf("digests differ: %s != %s", forward.Digest, reverse.Digest)
	}
	if !bytes.Equal(forward.Payload, reverse.Payload) {
		t.Fatalf("payloads differ:\n%s\n%s", forward.Payload, reverse.Payload)
	}
	if strings.Contains(string(forward.Payload), `"comments":null`) || strings.Contains(string(forward.Payload), `"evidence":null`) {
		t.Fatalf("empty collections serialized as null: %s", forward.Payload)
	}
}

func TestPacketVerifierDistinguishesTamperedStaleAndMissing(t *testing.T) {
	envelope, err := BuildRecords([]Record{deterministicRecord("packet-alpha", "event-alpha")}, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := contract.Serialize(envelope)
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte(strings.Replace(string(serialized), "goal packet-alpha", "goal packet-bravo", 1))
	if _, err := Verify(tampered, exportPublication.PublishedAt); !errors.Is(err, contract.ErrDigestMismatch) {
		t.Fatalf("tampered error = %v, want ErrDigestMismatch", err)
	}
	if _, err := Verify(tampered, exportPublication.PublishedAt.Add(contract.FreshnessBound)); !errors.Is(err, contract.ErrStaleExport) {
		t.Fatalf("stale error = %v, want ErrStaleExport", err)
	}
	if _, err := VerifyFile(filepath.Join(t.TempDir(), "missing.json"), exportPublication.PublishedAt); !errors.Is(err, contract.ErrExportNotFound) {
		t.Fatalf("missing error = %v, want ErrExportNotFound", err)
	}
}

func TestPublishWritesOnlyLocallyWithResolvedGitProvenance(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "-q")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("synthetic repository\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "-c", "user.name=Synthetic Author", "-c", "user.email=synthetic@example.invalid", "commit", "-qm", "initial")
	runGit(t, repository, "remote", "add", "origin", "https://github.com/synthetic/repository.git")

	tracker := trackerForExport(t)
	issueForExport(t, tracker, "packet-publish")
	output := filepath.Join(t.TempDir(), "exports")
	path, envelope, err := Publish(context.Background(), tracker, output, repository, exportPublication.PublishedAt)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if path != filepath.Join(output, FileName) {
		t.Fatalf("path = %q", path)
	}
	if envelope.Source.Repository != "synthetic/repository" || len(envelope.Source.Commit) != 40 {
		t.Fatalf("source = %+v", envelope.Source)
	}
	if _, err := VerifyFile(path, exportPublication.PublishedAt); err != nil {
		t.Fatalf("verify published file: %v", err)
	}

	failedOutput := filepath.Join(t.TempDir(), "must-not-exist")
	if _, _, err := Publish(context.Background(), tracker, failedOutput, t.TempDir(), exportPublication.PublishedAt); !errors.Is(err, contract.ErrInvalidProvenance) {
		t.Fatalf("missing provenance error = %v, want ErrInvalidProvenance", err)
	}
	if _, err := os.Stat(failedOutput); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed publisher created output: %v", err)
	}
}

func TestExportContractDemonstration(t *testing.T) {
	first, err := BuildRecords([]Record{
		deterministicRecord("packet-z", "event-z"),
		deterministicRecord("packet-a", "event-a"),
	}, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := contract.Serialize(first)
	if err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, serialized, "", "  "); err != nil {
		t.Fatal(err)
	}
	t.Logf("1. envelope:\n%s", pretty.String())
	publishedAt, _ := time.Parse(time.RFC3339, first.PublishedAt)
	expiresAt, _ := time.Parse(time.RFC3339, first.ExpiresAt)
	t.Logf("2. expires_at - published_at = %s", expiresAt.Sub(publishedAt))

	second, err := BuildRecords([]Record{
		deterministicRecord("packet-a", "event-a"),
		deterministicRecord("packet-z", "event-z"),
	}, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("3. forward digest=%s reverse digest=%s identical=%v", first.Digest, second.Digest, first.Digest == second.Digest)
	tampered := []byte(strings.Replace(string(serialized), "goal packet-a", "goal packet-x", 1))
	_, tamperedErr := Verify(tampered, exportPublication.PublishedAt)
	_, staleErr := Verify(serialized, exportPublication.PublishedAt.Add(contract.FreshnessBound))
	t.Logf("4. changed payload byte refused on digest=%v error=%v", errors.Is(tamperedErr, contract.ErrDigestMismatch), tamperedErr)
	t.Logf("5. export at expires_at refused as stale=%v distinct=%v error=%v", errors.Is(staleErr, contract.ErrStaleExport), !errors.Is(staleErr, contract.ErrDigestMismatch), staleErr)

	directoryEnvelope, err := contract.Build(tenant.Schema, []tenant.Record{
		{ID: "tenant-active", Slug: "active", DisplayName: "Synthetic Active", Status: tenant.StatusActive, CreatedAt: "2030-01-01T00:00:00Z", Version: 1},
		{ID: "tenant-retired", Slug: "retired", DisplayName: "Synthetic Retired", Status: tenant.StatusRetired, CreatedAt: "2030-01-01T00:00:00Z", Version: 2},
	}, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	directoryBytes, err := contract.Serialize(directoryEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := tenant.Parse(directoryBytes, exportPublication.PublishedAt)
	if err != nil {
		t.Fatal(err)
	}
	tracker, err := packet.NewTracker(directory)
	if err != nil {
		t.Fatal(err)
	}
	_, unknownErr := tracker.Issue(packet.IssueCommand{
		PacketID: "packet-unknown", TenantID: "tenant-unknown", Body: bodyForExport("unknown"), Actor: "actor-author",
	})
	_, retiredErr := tracker.Issue(packet.IssueCommand{
		PacketID: "packet-retired", TenantID: "tenant-retired", Body: bodyForExport("retired"), Actor: "actor-author",
	})
	t.Logf("6. unknown tenant refused=%v message=%v", errors.Is(unknownErr, tenant.ErrUnknownTenant), unknownErr)
	t.Logf("   retired tenant refused differently=%v message=%v", errors.Is(retiredErr, tenant.ErrRetiredTenant), retiredErr)
}

func TestReconcileKeepsRepositoryOnlyAndLetsAppWinDuplicate(t *testing.T) {
	repositoryOnly := deterministicRecord("packet-repository", "event-repository")
	repositoryDuplicate := deterministicRecord("packet-shared", "event-old")
	appDuplicate := deterministicRecord("packet-shared", "event-new")
	appDuplicate.Goal = "durable app projection"
	appOnly := deterministicRecord("packet-app", "event-app")

	reconciled := Reconcile(
		[]Record{repositoryDuplicate, repositoryOnly},
		[]Record{appOnly, appDuplicate},
	)
	envelope, err := BuildRecords(reconciled, exportPublication)
	if err != nil {
		t.Fatal(err)
	}
	var records []Record
	if err := json.Unmarshal(envelope.Payload, &records); err != nil {
		t.Fatal(err)
	}
	if got := []string{records[0].ID, records[1].ID, records[2].ID}; !reflect.DeepEqual(got, []string{"packet-app", "packet-repository", "packet-shared"}) {
		t.Fatalf("ids = %v", got)
	}
	if records[2].Goal != "durable app projection" || records[2].History[0].EventID != "event-new" {
		t.Fatalf("duplicate did not use app projection: %+v", records[2])
	}
}

func deterministicRecord(id, eventID string) Record {
	return Record{
		ID: id, TenantID: "tenant-synthetic", Goal: "goal " + id, Boundary: "logic only",
		DoneWhen: "tests pass", Check: "go test ./...", Context: "synthetic", Status: "not started", Version: 1,
		Comments: []Comment{}, Evidence: []string{}, History: []HistoryEvent{{
			Kind: "packet issued", EventID: eventID, Timestamp: "2035-03-04T09:00:00Z", Actor: "actor-author",
			TenantID: stringPointer("tenant-synthetic"),
			Body:     &Body{Goal: "goal " + id, Boundary: "logic only", DoneWhen: "tests pass", Check: "go test ./...", Context: "synthetic"},
		}},
	}
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2030-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2030-01-01T00:00:00Z",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
