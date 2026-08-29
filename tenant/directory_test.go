package tenant

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packet"
)

var directoryPublication = contract.Publication{
	PublishedAt: time.Date(2035, time.January, 1, 0, 0, 0, 0, time.UTC),
	Source: contract.Source{
		Repository: "synthetic/identity",
		Commit:     "cccccccccccccccccccccccccccccccccccccccc",
	},
}

func tenantRecord(id string, status Status) Record {
	return Record{
		ID:          id,
		Slug:        "slug-" + id,
		DisplayName: "Synthetic " + id,
		Status:      status,
		CreatedAt:   "2030-01-01T00:00:00Z",
		Version:     1,
	}
}

func directoryBytes(t *testing.T, payload any) []byte {
	t.Helper()
	envelope, err := contract.Build(Schema, payload, directoryPublication)
	if err != nil {
		t.Fatalf("build directory: %v", err)
	}
	serialized, err := contract.Serialize(envelope)
	if err != nil {
		t.Fatalf("serialize directory: %v", err)
	}
	return serialized
}

func TestDirectoryMatchesThePublishedSixFieldContract(t *testing.T) {
	contents := directoryBytes(t, []Record{
		tenantRecord("tenant-retired", StatusRetired),
		tenantRecord("tenant-active", StatusActive),
		tenantRecord("tenant-suspended", StatusSuspended),
	})
	directory, err := Parse(contents, directoryPublication.PublishedAt.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []Record{
		tenantRecord("tenant-active", StatusActive),
		tenantRecord("tenant-retired", StatusRetired),
		tenantRecord("tenant-suspended", StatusSuspended),
	}
	if got := directory.Records(); !reflect.DeepEqual(got, want) {
		t.Fatalf("records\n got: %+v\nwant: %+v", got, want)
	}

	wrongField := []map[string]any{{
		"id": "tenant-active", "slug": "active", "display_name": "Synthetic active", "display_label": "wrong field",
		"status": "active", "created_at": "2030-01-01T00:00:00Z", "version": 1,
	}}
	if _, err := Parse(directoryBytes(t, wrongField), directoryPublication.PublishedAt); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatalf("renamed field error = %v, want ErrInvalidDirectory", err)
	}

	invalidStatus := []Record{tenantRecord("tenant-invalid", Status("deleted"))}
	if _, err := Parse(directoryBytes(t, invalidStatus), directoryPublication.PublishedAt); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatalf("invalid status error = %v, want ErrInvalidDirectory", err)
	}
}

func TestDirectoryDistinguishesUnknownRetiredAndStale(t *testing.T) {
	directory, err := Parse(directoryBytes(t, []Record{
		tenantRecord("tenant-active", StatusActive),
		tenantRecord("tenant-suspended", StatusSuspended),
		tenantRecord("tenant-retired", StatusRetired),
	}), directoryPublication.PublishedAt)
	if err != nil {
		t.Fatal(err)
	}

	if err := directory.ValidateTenantID("tenant-active", directoryPublication.PublishedAt); err != nil {
		t.Fatalf("active tenant: %v", err)
	}
	if err := directory.ValidateTenantID("tenant-suspended", directoryPublication.PublishedAt); err != nil {
		t.Fatalf("suspended tenant is not denied by this packet: %v", err)
	}
	if err := directory.ValidateTenantID("tenant-unknown", directoryPublication.PublishedAt); !errors.Is(err, ErrUnknownTenant) {
		t.Fatalf("unknown error = %v, want ErrUnknownTenant", err)
	}
	if err := directory.ValidateTenantID("tenant-retired", directoryPublication.PublishedAt); !errors.Is(err, ErrRetiredTenant) {
		t.Fatalf("retired error = %v, want ErrRetiredTenant", err)
	}
	if err := directory.ValidateTenantID("tenant-active", directoryPublication.PublishedAt.Add(time.Hour)); !errors.Is(err, contract.ErrStaleExport) {
		t.Fatalf("stale error = %v, want ErrStaleExport", err)
	}
}

func TestDirectoryLoadVerifiesFileIntegrityAndAbsence(t *testing.T) {
	contents := directoryBytes(t, []Record{tenantRecord("tenant-active", StatusActive)})
	directoryPath := filepath.Join(t.TempDir(), "tenant-directory.json")
	if err := os.WriteFile(directoryPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directoryPath, directoryPublication.PublishedAt); err != nil {
		t.Fatalf("load: %v", err)
	}

	tampered := []byte(strings.Replace(string(contents), "tenant-active", "tenant-edited", 1))
	if err := os.WriteFile(directoryPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directoryPath, directoryPublication.PublishedAt); !errors.Is(err, contract.ErrDigestMismatch) {
		t.Fatalf("tampered error = %v, want ErrDigestMismatch", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json"), directoryPublication.PublishedAt); !errors.Is(err, contract.ErrExportNotFound) {
		t.Fatalf("missing error = %v, want ErrExportNotFound", err)
	}
}

func TestTrackerRefusesUnknownAndRetiredTenantsAtIssue(t *testing.T) {
	directory, err := Parse(directoryBytes(t, []Record{
		tenantRecord("tenant-active", StatusActive),
		tenantRecord("tenant-retired", StatusRetired),
	}), time.Now().UTC())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tracker, err := packet.NewTracker(directory)
	if err != nil {
		t.Fatalf("new tracker: %v", err)
	}
	body := packet.Body{Goal: "synthetic goal", Boundary: "logic", DoneWhen: "tested", Check: "go test", Context: "synthetic"}
	active, err := tracker.Issue(packet.IssueCommand{
		PacketID: "packet-active", TenantID: "tenant-active", Body: body, Actor: "actor-author",
	})
	if err != nil {
		t.Fatalf("active issue: %v", err)
	}
	if _, err := tracker.Issue(packet.IssueCommand{
		PacketID: "packet-unknown", TenantID: "tenant-unknown", Body: body, Actor: "actor-author",
	}); !errors.Is(err, ErrUnknownTenant) {
		t.Fatalf("unknown issue error = %v, want ErrUnknownTenant", err)
	}
	if _, err := tracker.Issue(packet.IssueCommand{
		PacketID: "packet-retired", TenantID: "tenant-retired", Body: body, Actor: "actor-author",
	}); !errors.Is(err, ErrRetiredTenant) {
		t.Fatalf("retired issue error = %v, want ErrRetiredTenant", err)
	}

	_, _, err = tracker.Supersede(packet.SupersedeCommand{
		PacketID: active.ID(), ExpectedVersion: active.Version(), ReplacementID: "packet-retired-replacement",
		ReplacementTenant: "tenant-retired", ReplacementBody: body, Actor: "actor-author",
	})
	if !errors.Is(err, ErrRetiredTenant) {
		t.Fatalf("retired supersession error = %v, want ErrRetiredTenant", err)
	}
	history, err := tracker.History(active.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("failed supersession appended %d events", len(history)-1)
	}
}

func TestTrackerRequiresATenantValidator(t *testing.T) {
	if _, err := packet.NewTracker(nil); !errors.Is(err, packet.ErrTenantValidatorRequired) {
		t.Fatalf("error = %v, want ErrTenantValidatorRequired", err)
	}
}
