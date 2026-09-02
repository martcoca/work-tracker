package repositorypacket

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/packet"
)

type allowTenant struct{}

func (allowTenant) ValidateTenantID(string, time.Time) error { return nil }

func TestLoadReplaysRepositoryStatusesAndSupersession(t *testing.T) {
	repository := t.TempDir()
	writePacket(t, repository, "packet-done", "done", "", "completed goal")
	writePacket(t, repository, "packet-old", "superseded", "", "original goal")
	writePacket(t, repository, "packet-new", "not started", "packet-old", "replacement goal")
	writePacket(t, repository, "packet-active", "in progress", "", "active goal")
	writePacket(t, repository, "packet-blocked", "blocked", "", "blocked goal")
	if err := os.MkdirAll(filepath.Join(repository, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "evidence", "packet-done.md"), []byte("proof\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tracker, err := Load(repository, allowTenant{}, "tenant-synthetic")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	packets, err := tracker.Packets()
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 5 {
		t.Fatalf("packet count = %d", len(packets))
	}

	done := packetByID(t, tracker, "packet-done")
	if done.Status() != packet.StatusDone || !reflect.DeepEqual(done.Evidence(), []packet.Evidence{"evidence/packet-done.md"}) {
		t.Fatalf("done packet status=%q evidence=%v", done.Status(), done.Evidence())
	}
	if done.Body().Goal != "completed goal" || !strings.Contains(done.Body().Check, "go test ./...") {
		t.Fatalf("done body = %+v", done.Body())
	}
	active := packetByID(t, tracker, "packet-active")
	if active.Status() != packet.StatusInProgress {
		t.Fatalf("active status = %q", active.Status())
	}
	blocked := packetByID(t, tracker, "packet-blocked")
	if blocked.Status() != packet.StatusBlocked {
		t.Fatalf("blocked status = %q", blocked.Status())
	}
	old := packetByID(t, tracker, "packet-old")
	closure, ok := old.Closure()
	if !ok || closure.Reason != packet.CloseReasonSuperseded || old.Status() != packet.StatusNotStarted {
		t.Fatalf("superseded packet status=%q closure=%+v present=%v", old.Status(), closure, ok)
	}
	if replacement, ok := old.SupersededBy(); !ok || replacement != "packet-new" {
		t.Fatalf("superseded_by=%q present=%v", replacement, ok)
	}
	newPacket := packetByID(t, tracker, "packet-new")
	if parent, ok := newPacket.ParentID(); !ok || parent != "packet-old" {
		t.Fatalf("parent_id=%q present=%v", parent, ok)
	}
}

func TestLoadRejectsRepositoryStateThatCannotBeReplayed(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
		want    string
	}{
		{
			name: "unknown status",
			prepare: func(t *testing.T, root string) {
				writePacket(t, root, "packet-a", "waiting", "", "goal")
			},
			want: "unsupported Status",
		},
		{
			name: "superseded without replacement",
			prepare: func(t *testing.T, root string) {
				writePacket(t, root, "packet-a", "superseded", "", "goal")
			},
			want: "has no replacement",
		},
		{
			name: "replacement does not match parent status",
			prepare: func(t *testing.T, root string) {
				writePacket(t, root, "packet-a", "not started", "", "goal")
				writePacket(t, root, "packet-b", "not started", "packet-a", "goal")
			},
			want: "not superseded",
		},
		{
			name: "done without evidence",
			prepare: func(t *testing.T, root string) {
				writePacket(t, root, "packet-a", "done", "", "goal")
			},
			want: "evidence",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := t.TempDir()
			test.prepare(t, repository)
			_, err := Load(repository, allowTenant{}, "tenant-synthetic")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadCurrentRepository(t *testing.T) {
	repository, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	tracker, err := Load(repository, allowTenant{}, "tenant-synthetic")
	if err != nil {
		t.Fatalf("load current repository: %v", err)
	}
	packets, err := tracker.Packets()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(repository, "packets", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != len(entries)-1 {
		t.Fatalf("loaded %d packets from %d specifications", len(packets), len(entries)-1)
	}
}

func packetByID(t *testing.T, tracker *packet.Tracker, id packet.PacketID) packet.Packet {
	t.Helper()
	result, err := tracker.Packet(id)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func writePacket(t *testing.T, repository, id, status, supersedes, goal string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repository, "packets"), 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := ""
	if supersedes != "" {
		metadata = "- **Supersedes:** `" + supersedes + "`\n"
	}
	contents := "# Packet\n\n" +
		"- **Packet id:** `" + id + "`\n" +
		"- **Status:** " + status + "\n" + metadata +
		"\n## Context\n\ncontext text\n" +
		"\n## Goal\n\n" + goal + "\n" +
		"\n## Boundary\n\nboundary text\n" +
		"\n## Done when\n\n- result exists\n" +
		"\n## Check\n\n```bash\ngo test ./...\n```\n"
	if err := os.WriteFile(filepath.Join(repository, "packets", id+".md"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
