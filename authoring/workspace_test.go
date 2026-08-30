package authoring

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packet"
)

var author = identity.Principal{Subject: "human-author", TenantID: "tenant-synthetic"}

type validator struct {
	rejectTenant string
	scopeCalls   int
	tenantCalls  int
}

func (value *validator) ValidateTenantID(id string, _ time.Time) error {
	value.tenantCalls++
	if id == value.rejectTenant {
		return errors.New("unknown tenant")
	}
	return nil
}

func (value *validator) ValidateScope(tenantID, initiativeID, epicID, target string, _ time.Time) error {
	value.scopeCalls++
	if tenantID == value.rejectTenant {
		return errors.New("unknown tenant")
	}
	if initiativeID != "0004" || epicID != "E02" || target != "work-tracker" {
		return ErrInvalidScope
	}
	return nil
}

func testWorkspace(t *testing.T, validation *validator) *Workspace {
	t.Helper()
	tracker, err := packet.NewTracker(validation)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(tracker, validation)
	if err != nil {
		t.Fatal(err)
	}
	next := 0
	workspace.newDraftID = func() (string, error) {
		next++
		return fmt.Sprintf("draft-%d", next), nil
	}
	workspace.now = func() time.Time { return time.Date(2035, time.May, 6, 12, 30, 0, 0, time.UTC) }
	return workspace
}

func completeBody(label string) packet.Body {
	return packet.Body{
		Goal: "Goal " + label, Boundary: "Boundary " + label, DoneWhen: "Done " + label,
		Check: "Check " + label, Context: "Context " + label,
	}
}

func createDraft(t *testing.T, workspace *Workspace, packetID string, body packet.Body) Draft {
	t.Helper()
	draft, err := workspace.Create(author, CreateCommand{
		PacketID: packetID, InitiativeID: "0004", EpicID: "E02", Target: "work-tracker", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestDraftIsFreelyEditedSeveralTimesThenIssuedWithAuthor(t *testing.T) {
	validation := &validator{}
	workspace := testWorkspace(t, validation)
	draft := createDraft(t, workspace, "0004-E02-T90", completeBody("first"))
	for index, label := range []string{"second", "third", "final"} {
		updated, err := workspace.Edit(author, EditCommand{
			DraftID: draft.ID, ExpectedVersion: draft.Version, PacketID: draft.PacketID,
			Target: draft.Target, Body: completeBody(label),
		})
		if err != nil {
			t.Fatalf("edit %d: %v", index+1, err)
		}
		draft = updated
	}
	result, err := workspace.Issue(author, IssueCommand{DraftID: draft.ID, ExpectedVersion: draft.Version})
	if err != nil {
		t.Fatal(err)
	}
	if result.Packet.Body() != completeBody("final") || result.Draft.State != StateIssued {
		t.Fatalf("issued result = %+v %+v", result.Packet.Body(), result.Draft)
	}
	history, err := workspace.Tracker().History(result.Packet.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Metadata().Actor != "human-author" {
		t.Fatalf("issue history = %+v", history)
	}
	if validation.scopeCalls != 1 || validation.tenantCalls != 1 {
		t.Fatalf("scope calls=%d tenant calls=%d", validation.scopeCalls, validation.tenantCalls)
	}
}

func TestIssuedDraftCannotBeEditedOrIssuedAgain(t *testing.T) {
	workspace := testWorkspace(t, &validator{})
	draft := createDraft(t, workspace, "0004-E02-T90", completeBody("frozen"))
	issued, err := workspace.Issue(author, IssueCommand{DraftID: draft.ID, ExpectedVersion: draft.Version})
	if err != nil {
		t.Fatal(err)
	}
	_, editErr := workspace.Edit(author, EditCommand{
		DraftID: draft.ID, ExpectedVersion: issued.Draft.Version, PacketID: draft.PacketID,
		Target: draft.Target, Body: completeBody("changed"),
	})
	if !errors.Is(editErr, ErrDraftIssued) {
		t.Fatalf("edit error = %v", editErr)
	}
	_, issueErr := workspace.Issue(author, IssueCommand{DraftID: draft.ID, ExpectedVersion: issued.Draft.Version})
	if !errors.Is(issueErr, ErrDraftIssued) {
		t.Fatalf("second issue error = %v", issueErr)
	}
	stored, err := workspace.Tracker().Packet(issued.Packet.ID())
	if err != nil || stored.Body() != completeBody("frozen") {
		t.Fatalf("stored body = %+v, error=%v", stored.Body(), err)
	}
}

func TestSupersessionPreservesOriginalAndLinksBothWays(t *testing.T) {
	workspace := testWorkspace(t, &validator{})
	originalDraft := createDraft(t, workspace, "0004-E02-T90", completeBody("original"))
	original, err := workspace.Issue(author, IssueCommand{DraftID: originalDraft.ID, ExpectedVersion: originalDraft.Version})
	if err != nil {
		t.Fatal(err)
	}
	replacementDraft, err := workspace.CreateSupersession(author, SupersessionCommand{
		ParentID: string(original.Packet.ID()), PacketID: "0004-E02-T91", InitiativeID: "0004", EpicID: "E02",
		Target: "work-tracker", Body: completeBody("replacement"),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workspace.Issue(author, IssueCommand{DraftID: replacementDraft.ID, ExpectedVersion: replacementDraft.Version})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasParent || result.Parent.Body() != completeBody("original") {
		t.Fatalf("parent = %+v", result.Parent)
	}
	if child, ok := result.Parent.SupersededBy(); !ok || child != result.Packet.ID() {
		t.Fatalf("replacement link = %q, %v", child, ok)
	}
	if parent, ok := result.Packet.ParentID(); !ok || parent != result.Parent.ID() {
		t.Fatalf("parent link = %q, %v", parent, ok)
	}
	if closure, ok := result.Parent.Closure(); !ok || closure.Reason != packet.CloseReasonSuperseded {
		t.Fatalf("closure = %+v, %v", closure, ok)
	}
}

func TestIncompleteDraftAndUnknownTenantAreRefusedAtIssue(t *testing.T) {
	validation := &validator{rejectTenant: "tenant-unknown"}
	workspace := testWorkspace(t, validation)
	incomplete := completeBody("incomplete")
	incomplete.Check = ""
	draft := createDraft(t, workspace, "0004-E02-T90", incomplete)
	if _, err := workspace.Issue(author, IssueCommand{DraftID: draft.ID, ExpectedVersion: draft.Version}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("incomplete error = %v", err)
	}

	unknown := identity.Principal{Subject: "human-unknown", TenantID: "tenant-unknown"}
	unknownDraft, err := workspace.Create(unknown, CreateCommand{
		PacketID: "0004-E02-T91", InitiativeID: "0004", EpicID: "E02", Target: "work-tracker", Body: completeBody("unknown"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Issue(unknown, IssueCommand{DraftID: unknownDraft.ID, ExpectedVersion: unknownDraft.Version}); err == nil || err.Error() != "unknown tenant" {
		t.Fatalf("unknown tenant error = %v", err)
	}
	if _, err := workspace.Tracker().Packet("0004-E02-T91"); !errors.Is(err, packet.ErrNotFound) {
		t.Fatalf("unknown tenant packet error = %v", err)
	}
}

func TestDraftOwnershipAndScopeAreEnforced(t *testing.T) {
	workspace := testWorkspace(t, &validator{})
	draft := createDraft(t, workspace, "0004-E02-T90", completeBody("private"))
	other := identity.Principal{Subject: "human-other", TenantID: author.TenantID}
	if _, err := workspace.Draft(other, draft.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("other author error = %v", err)
	}
	wrongScope := createDraft(t, workspace, "0004-E02-T91", completeBody("scope"))
	wrongScope.Target = "not-real"
	workspace.drafts[wrongScope.ID] = wrongScope
	if _, err := workspace.Issue(author, IssueCommand{DraftID: wrongScope.ID, ExpectedVersion: wrongScope.Version}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("scope error = %v", err)
	}
}
