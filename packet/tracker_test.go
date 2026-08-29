package packet

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type deterministicSources struct {
	next int
}

func (s *deterministicSources) eventID() (EventID, error) {
	s.next++
	return EventID(fmt.Sprintf("event-%03d", s.next)), nil
}

func (s *deterministicSources) time() time.Time {
	return time.Date(2026, time.August, 28, 12, 0, s.next, 0, time.UTC)
}

func testTracker() *Tracker {
	sources := &deterministicSources{}
	return newTracker(sources.time, sources.eventID, allowAllTenants{})
}

type allowAllTenants struct{}

func (allowAllTenants) ValidateTenantID(string, time.Time) error { return nil }

func testBody(label string) Body {
	return Body{
		Goal:     "goal " + label,
		Boundary: "boundary " + label,
		DoneWhen: "done when " + label,
		Check:    "check " + label,
		Context:  "context " + label,
	}
}

func issueForTest(t *testing.T, tracker *Tracker, id PacketID) Packet {
	t.Helper()
	packet, err := tracker.Issue(IssueCommand{
		PacketID: id,
		TenantID: "tenant-synthetic",
		Body:     testBody(string(id)),
		Actor:    "actor-author",
	})
	if err != nil {
		t.Fatalf("issue %s: %v", id, err)
	}
	return packet
}

func takeForTest(t *testing.T, tracker *Tracker, packet Packet) Packet {
	t.Helper()
	taken, err := tracker.Take(TakeCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-worker",
	})
	if err != nil {
		t.Fatalf("take %s: %v", packet.ID(), err)
	}
	return taken
}

func doneForTest(t *testing.T, tracker *Tracker, packet Packet) Packet {
	t.Helper()
	done, err := tracker.Transition(TransitionCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-worker",
		To:              StatusDone,
		Evidence:        []Evidence{"evidence/synthetic.md"},
	})
	if err != nil {
		t.Fatalf("complete %s: %v", packet.ID(), err)
	}
	return done
}

func TestLifecycleEventTypesCarryMetadata(t *testing.T) {
	tracker := testTracker()
	packet := issueForTest(t, tracker, "packet-lifecycle")
	packet = takeForTest(t, tracker, packet)
	packet, err := tracker.Comment(CommentCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-reviewer",
		Text:            "verified the check",
	})
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	doneForTest(t, tracker, packet)

	original := issueForTest(t, tracker, "packet-original")
	if _, _, err := tracker.Supersede(SupersedeCommand{
		PacketID:          original.ID(),
		ExpectedVersion:   original.Version(),
		ReplacementID:     "packet-replacement",
		ReplacementTenant: "tenant-synthetic",
		ReplacementBody:   testBody("replacement"),
		Actor:             "actor-author",
	}); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	seen := make(map[EventKind]bool)
	for _, id := range []PacketID{"packet-lifecycle", "packet-original", "packet-replacement"} {
		history, err := tracker.History(id)
		if err != nil {
			t.Fatalf("history %s: %v", id, err)
		}
		for _, event := range history {
			seen[event.Kind()] = true
			meta := event.Metadata()
			if meta.ID == "" || meta.At.IsZero() || meta.Actor == "" {
				t.Errorf("%s has incomplete metadata: %+v", event.Kind(), meta)
			}
		}
	}

	for _, kind := range []EventKind{
		EventPacketIssued,
		EventPacketTaken,
		EventPacketCommented,
		EventPacketStatusTransition,
		EventPacketSuperseded,
		EventPacketClosed,
	} {
		if !seen[kind] {
			t.Errorf("event kind %q was not recorded", kind)
		}
	}
}

func TestBodyIsFrozenAndTheAPIOffersNoEditOperation(t *testing.T) {
	tracker := testTracker()
	body := testBody("immutable")
	issued, err := tracker.Issue(IssueCommand{
		PacketID: "packet-immutable",
		TenantID: "tenant-synthetic",
		Body:     body,
		Actor:    "actor-author",
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	body.Goal = "edited input"
	returned := issued.Body()
	returned.Boundary = "edited snapshot"
	history, err := tracker.History(issued.ID())
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	issuedEvent := history[0].(PacketIssued)
	issuedEvent.Body.Context = "edited event copy"

	stored, err := tracker.Packet(issued.ID())
	if err != nil {
		t.Fatalf("packet: %v", err)
	}
	if stored.Body() != testBody("immutable") {
		t.Fatalf("stored body changed: %+v", stored.Body())
	}

	wantMethods := []string{
		"Comment", "DropProjection", "History", "Issue", "Packet", "Packets",
		"RebuildProjection", "Supersede", "Take", "Transition",
	}
	if got := exportedTrackerMethods(); !reflect.DeepEqual(got, wantMethods) {
		t.Fatalf("Tracker API changed; reassess frozen-body invariant\n got: %v\nwant: %v", got, wantMethods)
	}
}

func TestSupersessionLinksBothPacketsAndPreservesOriginalBody(t *testing.T) {
	tracker := testTracker()
	original := issueForTest(t, tracker, "packet-old")
	originalBody := original.Body()

	parent, replacement, err := tracker.Supersede(SupersedeCommand{
		PacketID:          original.ID(),
		ExpectedVersion:   original.Version(),
		ReplacementID:     "packet-new",
		ReplacementTenant: "tenant-other-opaque",
		ReplacementBody:   testBody("corrected"),
		Actor:             "actor-author",
	})
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}

	if got := parent.Body(); got != originalBody {
		t.Fatalf("original body changed\n got: %+v\nwant: %+v", got, originalBody)
	}
	if got, ok := parent.SupersededBy(); !ok || got != replacement.ID() {
		t.Fatalf("parent replacement link = %q, %v", got, ok)
	}
	if got, ok := replacement.ParentID(); !ok || got != parent.ID() {
		t.Fatalf("replacement parent link = %q, %v", got, ok)
	}
	closure, ok := parent.Closure()
	if !ok || closure.Reason != CloseReasonSuperseded {
		t.Fatalf("parent closure = %+v, %v", closure, ok)
	}
	if replacement.Body() != testBody("corrected") {
		t.Fatalf("replacement body = %+v", replacement.Body())
	}

	_, err = tracker.Take(TakeCommand{
		PacketID:        parent.ID(),
		ExpectedVersion: parent.Version(),
		Actor:           "actor-worker",
	})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("take superseded packet error = %v, want ErrClosed", err)
	}
}

func TestOnlyLegalTransitionsAreAccepted(t *testing.T) {
	t.Run("not started to in progress to done", func(t *testing.T) {
		tracker := testTracker()
		packet := takeForTest(t, tracker, issueForTest(t, tracker, "packet-happy"))
		packet = doneForTest(t, tracker, packet)
		if packet.Status() != StatusDone {
			t.Fatalf("status = %q", packet.Status())
		}
		closure, ok := packet.Closure()
		if !ok || closure.Reason != CloseReasonDone {
			t.Fatalf("done closure = %+v, %v", closure, ok)
		}
	})

	t.Run("not started to done is refused atomically", func(t *testing.T) {
		tracker := testTracker()
		packet := issueForTest(t, tracker, "packet-skip")
		_, err := tracker.Transition(TransitionCommand{
			PacketID:        packet.ID(),
			ExpectedVersion: packet.Version(),
			Actor:           "actor-worker",
			To:              StatusDone,
			Evidence:        []Evidence{"evidence/synthetic.md"},
		})
		if !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("error = %v, want ErrIllegalTransition", err)
		}
		history, historyErr := tracker.History(packet.ID())
		if historyErr != nil {
			t.Fatalf("history: %v", historyErr)
		}
		if len(history) != 1 {
			t.Fatalf("failed transition appended events: %d", len(history))
		}
	})

	t.Run("not started and in progress can become blocked", func(t *testing.T) {
		for _, start := range []Status{StatusNotStarted, StatusInProgress} {
			tracker := testTracker()
			packet := issueForTest(t, tracker, PacketID("packet-block-"+string(start)))
			if start == StatusInProgress {
				packet = takeForTest(t, tracker, packet)
			}
			blocked, err := tracker.Transition(TransitionCommand{
				PacketID:        packet.ID(),
				ExpectedVersion: packet.Version(),
				Actor:           "actor-worker",
				To:              StatusBlocked,
			})
			if err != nil {
				t.Fatalf("%s to blocked: %v", start, err)
			}
			if blocked.Status() != StatusBlocked {
				t.Fatalf("status = %q", blocked.Status())
			}
		}
	})

	t.Run("done is terminal", func(t *testing.T) {
		tracker := testTracker()
		packet := doneForTest(t, tracker, takeForTest(t, tracker, issueForTest(t, tracker, "packet-terminal")))
		for _, target := range []Status{StatusInProgress, StatusBlocked} {
			_, err := tracker.Transition(TransitionCommand{
				PacketID:        packet.ID(),
				ExpectedVersion: packet.Version(),
				Actor:           "actor-worker",
				To:              target,
			})
			if !errors.Is(err, ErrClosed) {
				t.Errorf("done to %s error = %v, want ErrClosed", target, err)
			}
		}
	})

	t.Run("blocked has no invented exit", func(t *testing.T) {
		tracker := testTracker()
		packet := issueForTest(t, tracker, "packet-blocked")
		packet, err := tracker.Transition(TransitionCommand{
			PacketID:        packet.ID(),
			ExpectedVersion: packet.Version(),
			Actor:           "actor-worker",
			To:              StatusBlocked,
		})
		if err != nil {
			t.Fatalf("block: %v", err)
		}
		_, err = tracker.Transition(TransitionCommand{
			PacketID:        packet.ID(),
			ExpectedVersion: packet.Version(),
			Actor:           "actor-worker",
			To:              StatusDone,
			Evidence:        []Evidence{"evidence/synthetic.md"},
		})
		if !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("blocked to done error = %v, want ErrIllegalTransition", err)
		}
	})
}

func TestDoneRequiresEvidenceAndOtherTransitionsRejectIt(t *testing.T) {
	tracker := testTracker()
	packet := takeForTest(t, tracker, issueForTest(t, tracker, "packet-evidence"))

	_, err := tracker.Transition(TransitionCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-worker",
		To:              StatusDone,
	})
	if !errors.Is(err, ErrEvidenceRequired) {
		t.Fatalf("missing evidence error = %v, want ErrEvidenceRequired", err)
	}
	_, err = tracker.Transition(TransitionCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-worker",
		To:              StatusDone,
		Evidence:        []Evidence{"  "},
	})
	if !errors.Is(err, ErrEvidenceRequired) {
		t.Fatalf("blank evidence error = %v, want ErrEvidenceRequired", err)
	}

	blockedTracker := testTracker()
	blockedPacket := issueForTest(t, blockedTracker, "packet-evidence-on-blocked")
	_, err = blockedTracker.Transition(TransitionCommand{
		PacketID:        blockedPacket.ID(),
		ExpectedVersion: blockedPacket.Version(),
		Actor:           "actor-worker",
		To:              StatusBlocked,
		Evidence:        []Evidence{"not-allowed"},
	})
	if !errors.Is(err, ErrUnexpectedEvidence) {
		t.Fatalf("unexpected evidence error = %v, want ErrUnexpectedEvidence", err)
	}

	evidence := []Evidence{"evidence/synthetic.md"}
	done, err := tracker.Transition(TransitionCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-worker",
		To:              StatusDone,
		Evidence:        evidence,
	})
	if err != nil {
		t.Fatalf("done with evidence: %v", err)
	}
	evidence[0] = "edited input"
	returned := done.Evidence()
	returned[0] = "edited output"
	stored, err := tracker.Packet(packet.ID())
	if err != nil {
		t.Fatalf("packet: %v", err)
	}
	if got := stored.Evidence(); !reflect.DeepEqual(got, []Evidence{"evidence/synthetic.md"}) {
		t.Fatalf("stored evidence changed: %v", got)
	}
}

func TestCommentsAppendInOrderWithAttributionAndCannotBeEdited(t *testing.T) {
	tracker := testTracker()
	packet := issueForTest(t, tracker, "packet-comments")

	var err error
	packet, err = tracker.Comment(CommentCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-first",
		Text:            "first comment",
	})
	if err != nil {
		t.Fatalf("first comment: %v", err)
	}
	packet, err = tracker.Comment(CommentCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-second",
		Text:            "second comment",
	})
	if err != nil {
		t.Fatalf("second comment: %v", err)
	}

	want := []Comment{
		{EventID: "event-002", At: time.Date(2026, time.August, 28, 12, 0, 2, 0, time.UTC), Actor: "actor-first", Text: "first comment"},
		{EventID: "event-003", At: time.Date(2026, time.August, 28, 12, 0, 3, 0, time.UTC), Actor: "actor-second", Text: "second comment"},
	}
	if got := packet.Comments(); !reflect.DeepEqual(got, want) {
		t.Fatalf("comments\n got: %+v\nwant: %+v", got, want)
	}

	comments := packet.Comments()
	comments[0].Text = "edited"
	comments = comments[1:]
	stored, err := tracker.Packet(packet.ID())
	if err != nil {
		t.Fatalf("packet: %v", err)
	}
	if got := stored.Comments(); !reflect.DeepEqual(got, want) {
		t.Fatalf("stored comments changed: %+v", got)
	}
}

func TestEveryPacketRequiresAnOpaqueTenantID(t *testing.T) {
	tracker := testTracker()
	_, err := tracker.Issue(IssueCommand{
		PacketID: "packet-no-tenant",
		Body:     testBody("no tenant"),
		Actor:    "actor-author",
	})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("empty tenant error = %v, want ErrInvalidEvent", err)
	}

	packet, err := tracker.Issue(IssueCommand{
		PacketID: "packet-opaque-tenant",
		TenantID: "opaque::tenant/value?uninterpreted",
		Body:     testBody("opaque tenant"),
		Actor:    "actor-author",
	})
	if err != nil {
		t.Fatalf("opaque tenant: %v", err)
	}
	if packet.TenantID() != "opaque::tenant/value?uninterpreted" {
		t.Fatalf("tenant = %q", packet.TenantID())
	}
}

func TestConcurrentTransitionsFromOneVersionRejectTheLoser(t *testing.T) {
	tracker := testTracker()
	packet := issueForTest(t, tracker, "packet-concurrent")

	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, actor := range []Actor{"actor-one", "actor-two"} {
		workers.Add(1)
		go func(actor Actor) {
			defer workers.Done()
			<-start
			_, err := tracker.Transition(TransitionCommand{
				PacketID:        packet.ID(),
				ExpectedVersion: packet.Version(),
				Actor:           actor,
				To:              StatusBlocked,
			})
			results <- err
		}(actor)
	}
	close(start)
	workers.Wait()
	close(results)

	var succeeded, conflicted int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrConflict):
			conflicted++
		default:
			t.Fatalf("transition error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	history, err := tracker.History(packet.ID())
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history contains %d events, want issue plus one transition", len(history))
	}
}

func TestProjectionCanBeDroppedAndRebuiltIdentically(t *testing.T) {
	tracker := testTracker()
	parent := takeForTest(t, tracker, issueForTest(t, tracker, "packet-rebuild-parent"))
	parent, err := tracker.Comment(CommentCommand{
		PacketID:        parent.ID(),
		ExpectedVersion: parent.Version(),
		Actor:           "actor-reviewer",
		Text:            "preserved across replay",
	})
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	parent, replacement, err := tracker.Supersede(SupersedeCommand{
		PacketID:          parent.ID(),
		ExpectedVersion:   parent.Version(),
		ReplacementID:     "packet-rebuild-child",
		ReplacementTenant: "tenant-synthetic",
		ReplacementBody:   testBody("rebuild child"),
		Actor:             "actor-author",
	})
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	replacement, err = tracker.Comment(CommentCommand{
		PacketID:        replacement.ID(),
		ExpectedVersion: replacement.Version(),
		Actor:           "actor-reviewer",
		Text:            "replacement comment",
	})
	if err != nil {
		t.Fatalf("replacement comment: %v", err)
	}
	wantParent := parent
	wantReplacement := replacement

	tracker.DropProjection()
	if _, err := tracker.Packet(parent.ID()); !errors.Is(err, ErrProjectionUnavailable) {
		t.Fatalf("packet after drop error = %v, want ErrProjectionUnavailable", err)
	}
	if err := tracker.RebuildProjection(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	gotParent, err := tracker.Packet(parent.ID())
	if err != nil {
		t.Fatalf("parent after rebuild: %v", err)
	}
	gotReplacement, err := tracker.Packet(replacement.ID())
	if err != nil {
		t.Fatalf("replacement after rebuild: %v", err)
	}
	if !reflect.DeepEqual(gotParent, wantParent) {
		t.Fatalf("rebuilt parent differs\n got: %#v\nwant: %#v", gotParent, wantParent)
	}
	if !reflect.DeepEqual(gotReplacement, wantReplacement) {
		t.Fatalf("rebuilt replacement differs\n got: %#v\nwant: %#v", gotReplacement, wantReplacement)
	}
}

func TestProjectionRejectsEventsWithoutCompleteMetadata(t *testing.T) {
	valid := PacketIssued{
		Meta: Metadata{
			ID:    "event-valid",
			At:    time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC),
			Actor: "actor-author",
		},
		PacketID: "packet-metadata",
		TenantID: "tenant-synthetic",
		Body:     testBody("metadata"),
	}

	for name, mutate := range map[string]func(*PacketIssued){
		"id":        func(event *PacketIssued) { event.Meta.ID = "" },
		"timestamp": func(event *PacketIssued) { event.Meta.At = time.Time{} },
		"actor":     func(event *PacketIssued) { event.Meta.Actor = "" },
	} {
		t.Run(name, func(t *testing.T) {
			event := valid
			mutate(&event)
			var projection Packet
			if err := applyEvent(&projection, event); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("error = %v, want ErrInvalidEvent", err)
			}
		})
	}
}

func TestHistoryIsDefensive(t *testing.T) {
	tracker := testTracker()
	packet := doneForTest(t, tracker, takeForTest(t, tracker, issueForTest(t, tracker, "packet-history-copy")))
	history, err := tracker.History(packet.ID())
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	transitioned := history[len(history)-2].(PacketStatusTransitioned)
	transitioned.Evidence[0] = "edited history"
	history[len(history)-2] = transitioned

	again, err := tracker.History(packet.ID())
	if err != nil {
		t.Fatalf("history again: %v", err)
	}
	got := again[len(again)-2].(PacketStatusTransitioned).Evidence
	if !reflect.DeepEqual(got, []Evidence{"evidence/synthetic.md"}) {
		t.Fatalf("stored event changed: %v", got)
	}
}

func TestPacketsReturnsSortedDefensiveSnapshots(t *testing.T) {
	tracker := testTracker()
	issueForTest(t, tracker, "packet-z")
	issueForTest(t, tracker, "packet-a")

	packets, err := tracker.Packets()
	if err != nil {
		t.Fatalf("packets: %v", err)
	}
	if got := []PacketID{packets[0].ID(), packets[1].ID()}; !reflect.DeepEqual(got, []PacketID{"packet-a", "packet-z"}) {
		t.Fatalf("packet order = %v", got)
	}
	body := packets[0].Body()
	body.Goal = "edited"
	stored, err := tracker.Packet("packet-a")
	if err != nil {
		t.Fatalf("packet: %v", err)
	}
	if stored.Body().Goal == "edited" {
		t.Fatal("Packets exposed mutable projected body")
	}
}

func TestPacketModelDemonstration(t *testing.T) {
	tracker := testTracker()
	packet := issueForTest(t, tracker, "demo-lifecycle")
	packet = takeForTest(t, tracker, packet)
	packet, err := tracker.Comment(CommentCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-reviewer",
		Text:            "check reviewed",
	})
	if err != nil {
		t.Fatal(err)
	}
	packet = doneForTest(t, tracker, packet)
	history, err := tracker.History(packet.ID())
	if err != nil {
		t.Fatal(err)
	}
	t.Log("1. issued, taken, commented, and completed history:")
	for index, event := range history {
		t.Logf("   %d. %s", index+1, describeEvent(event))
	}

	bodyCopy := packet.Body()
	bodyCopy.Goal = "attempted edit"
	stored, err := tracker.Packet(packet.ID())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("2. exported Tracker methods=%v; edited copy goal=%q; stored goal=%q", exportedTrackerMethods(), bodyCopy.Goal, stored.Body().Goal)

	original := issueForTest(t, tracker, "demo-original")
	originalBody := original.Body()
	parent, replacement, err := tracker.Supersede(SupersedeCommand{
		PacketID:          original.ID(),
		ExpectedVersion:   original.Version(),
		ReplacementID:     "demo-replacement",
		ReplacementTenant: "tenant-synthetic",
		ReplacementBody:   testBody("demo corrected"),
		Actor:             "actor-author",
	})
	if err != nil {
		t.Fatal(err)
	}
	parentLink, _ := replacement.ParentID()
	replacementLink, _ := parent.SupersededBy()
	closure, _ := parent.Closure()
	t.Logf("3. supersession: original body unchanged=%v parent=%q replacement=%q closed=%q", parent.Body() == originalBody, parentLink, replacementLink, closure.Reason)

	noEvidence := takeForTest(t, tracker, issueForTest(t, tracker, "demo-no-evidence"))
	_, missingEvidenceErr := tracker.Transition(TransitionCommand{
		PacketID:        noEvidence.ID(),
		ExpectedVersion: noEvidence.Version(),
		Actor:           "actor-worker",
		To:              StatusDone,
	})
	t.Logf("4. done without evidence refused=%v error=%v", errors.Is(missingEvidenceErr, ErrEvidenceRequired), missingEvidenceErr)

	_, terminalErr := tracker.Transition(TransitionCommand{
		PacketID:        packet.ID(),
		ExpectedVersion: packet.Version(),
		Actor:           "actor-worker",
		To:              StatusInProgress,
	})
	t.Logf("5. done to in progress refused=%v error=%v", errors.Is(terminalErr, ErrClosed), terminalErr)

	beforeRebuild := parent
	tracker.DropProjection()
	if err := tracker.RebuildProjection(); err != nil {
		t.Fatal(err)
	}
	afterRebuild, err := tracker.Packet(parent.ID())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("6. projection identical after drop and replay=%v", reflect.DeepEqual(beforeRebuild, afterRebuild))

	concurrent := issueForTest(t, tracker, "demo-concurrent")
	priorVersion := concurrent.Version()
	first, firstErr := tracker.Transition(TransitionCommand{
		PacketID:        concurrent.ID(),
		ExpectedVersion: priorVersion,
		Actor:           "actor-one",
		To:              StatusBlocked,
	})
	_, secondErr := tracker.Transition(TransitionCommand{
		PacketID:        concurrent.ID(),
		ExpectedVersion: priorVersion,
		Actor:           "actor-two",
		To:              StatusBlocked,
	})
	t.Logf("7. same-prior transitions: first error=%v version=%d; second conflict=%v error=%v", firstErr, first.Version(), errors.Is(secondErr, ErrConflict), secondErr)
}

func exportedTrackerMethods() []string {
	typeOfTracker := reflect.TypeOf((*Tracker)(nil))
	methods := make([]string, 0, typeOfTracker.NumMethod())
	for i := 0; i < typeOfTracker.NumMethod(); i++ {
		methods = append(methods, typeOfTracker.Method(i).Name)
	}
	sort.Strings(methods)
	return methods
}

func describeEvent(event Event) string {
	meta := event.Metadata()
	details := ""
	switch event := event.(type) {
	case PacketIssued:
		details = fmt.Sprintf(" packet=%q tenant=%q", event.PacketID, event.TenantID)
	case PacketTaken:
		details = fmt.Sprintf(" packet=%q", event.PacketID)
	case PacketCommented:
		details = fmt.Sprintf(" packet=%q text=%q", event.PacketID, event.Text)
	case PacketStatusTransitioned:
		evidence := make([]string, len(event.Evidence))
		for i, item := range event.Evidence {
			evidence[i] = string(item)
		}
		details = fmt.Sprintf(" packet=%q from=%q to=%q evidence=[%s]", event.PacketID, event.From, event.To, strings.Join(evidence, ","))
	case PacketSuperseded:
		details = fmt.Sprintf(" packet=%q replacement=%q", event.PacketID, event.ReplacementID)
	case PacketClosed:
		details = fmt.Sprintf(" packet=%q reason=%q", event.PacketID, event.Reason)
	}
	return fmt.Sprintf("%s id=%q at=%s actor=%q%s", event.Kind(), meta.ID, meta.At.Format(time.RFC3339), meta.Actor, details)
}
