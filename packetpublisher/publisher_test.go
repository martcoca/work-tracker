package packetpublisher

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packet"
	"github.com/martcoca/work-tracker/packetexport"
)

type allowTenant struct{}

func (allowTenant) ValidateTenantID(string, time.Time) error { return nil }

type captureDestination struct {
	calls    int
	contents []byte
	message  string
}

func (destination *captureDestination) Publish(_ context.Context, contents []byte, message string) error {
	destination.calls++
	destination.contents = append([]byte(nil), contents...)
	destination.message = message
	return nil
}

type switchStore struct {
	store *packet.MemoryEventStore
	fail  bool
}

func (store *switchStore) Load(ids ...packet.PacketID) ([]packet.EventRecord, error) {
	if store.fail {
		return nil, errors.New("synthetic store unavailable")
	}
	return store.store.Load(ids...)
}

func (store *switchStore) Append(expected map[packet.PacketID]packet.Version, records []packet.EventRecord) error {
	if store.fail {
		return errors.New("synthetic store unavailable")
	}
	return store.store.Append(expected, records)
}

var appSource = contract.Source{
	Repository: "tracker.martcoca.com/app",
	Commit:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
}

func TestPublisherReconcilesVerifiedBaselineWithDurableStore(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	baseline := exportRecords(t, now, contract.Source{
		Repository: "martcoca/work-tracker",
		Commit:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}, []packetexport.Record{
		record("0004-E01-T01", "repository only", "event-repository"),
		record("0004-E07-T99", "old repository projection", "event-old"),
	})
	tracker, err := packet.NewTracker(allowTenant{})
	if err != nil {
		t.Fatal(err)
	}
	issue(t, tracker, "0004-E07-T02", "app only")
	issue(t, tracker, "0004-E07-T99", "durable app projection")
	destination := &captureDestination{}
	repository := exportRecords(t, now, contract.Source{
		Repository: "martcoca/work-tracker", Commit: "cccccccccccccccccccccccccccccccccccccccc",
	}, []packetexport.Record{
		record("0004-E01-T01", "repository only refreshed", "event-repository-new"),
		record("0004-E07-T03", "new repository packet", "event-repository-new-packet"),
	})
	publisher, err := New(
		tracker,
		func(time.Time) ([]byte, error) { return baseline, nil },
		func(time.Time) ([]byte, error) { return repository, nil },
		destination,
		appSource,
	)
	if err != nil {
		t.Fatal(err)
	}
	publisher.now = func() time.Time { return now.Add(time.Hour) }

	result, err := publisher.Publish(context.Background())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if destination.calls != 1 || result.PacketCount != 4 {
		t.Fatalf("calls=%d result=%+v", destination.calls, result)
	}
	verified, err := packetexport.Verify(destination.contents, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("shipped verifier refused app export: %v", err)
	}
	if verified.Envelope.Source != appSource {
		t.Fatalf("source = %+v", verified.Envelope.Source)
	}
	gotIDs := []string{verified.Packets[0].ID, verified.Packets[1].ID, verified.Packets[2].ID, verified.Packets[3].ID}
	if !reflect.DeepEqual(gotIDs, []string{"0004-E01-T01", "0004-E07-T02", "0004-E07-T03", "0004-E07-T99"}) {
		t.Fatalf("ids = %v", gotIDs)
	}
	if verified.Packets[0].Goal != "repository only refreshed" {
		t.Fatalf("repository refresh did not replace retained projection: %+v", verified.Packets[0])
	}
	if verified.Packets[3].Goal != "durable app projection" {
		t.Fatalf("duplicate did not use durable projection: %+v", verified.Packets[3])
	}
}

func TestPublisherRefusesStoreFailureWithoutTouchingLastGoodExport(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	store := &switchStore{store: packet.NewMemoryEventStore()}
	tracker, err := packet.NewTrackerWithStore(allowTenant{}, store)
	if err != nil {
		t.Fatal(err)
	}
	issue(t, tracker, "0004-E07-T02", "durable")
	baseline := exportRecords(t, now, appSource, []packetexport.Record{record("0004-E01-T01", "last good", "event-good")})
	destination := &captureDestination{}
	publisher, err := New(
		tracker,
		func(time.Time) ([]byte, error) { return baseline, nil },
		func(time.Time) ([]byte, error) { return baseline, nil },
		destination,
		appSource,
	)
	if err != nil {
		t.Fatal(err)
	}
	publisher.now = func() time.Time { return now.Add(time.Hour) }
	store.fail = true

	_, err = publisher.Publish(context.Background())
	if err == nil {
		t.Fatalf("publish error=%v calls=%d", err, destination.calls)
	}
	if !stringsContains(err.Error(), "refuse publication while durable store is unavailable") {
		t.Fatalf("error = %v", err)
	}
	if destination.calls != 0 || len(destination.contents) != 0 {
		t.Fatalf("destination was touched: calls=%d bytes=%d", destination.calls, len(destination.contents))
	}
	verified, verifyErr := packetexport.Verify(baseline, now.Add(time.Hour))
	if verifyErr != nil || verified.Packets[0].Goal != "last good" {
		t.Fatalf("last good export changed: verify=%v packets=%+v", verifyErr, verified.Packets)
	}
}

func issue(t *testing.T, tracker *packet.Tracker, id packet.PacketID, goal string) {
	t.Helper()
	_, err := tracker.Issue(packet.IssueCommand{
		PacketID: id,
		TenantID: "tenant-synthetic",
		Actor:    "actor-author",
		Body: packet.Body{
			Goal: goal, Boundary: "boundary", DoneWhen: "done", Check: "check", Context: "context",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func exportRecords(t *testing.T, at time.Time, source contract.Source, records []packetexport.Record) []byte {
	t.Helper()
	contents, _, err := packetexport.SerializeRecords(records, contract.Publication{PublishedAt: at, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func record(id, goal, eventID string) packetexport.Record {
	tenantID := "tenant-synthetic"
	return packetexport.Record{
		ID: id, TenantID: tenantID, Goal: goal, Boundary: "boundary", DoneWhen: "done", Check: "check",
		Context: "context", Status: "not started", Version: 1,
		Comments: []packetexport.Comment{}, Evidence: []string{},
		History: []packetexport.HistoryEvent{{
			Kind: "packet issued", EventID: eventID, Timestamp: "2035-03-04T09:00:00Z", Actor: "actor-author",
			TenantID: &tenantID,
			Body:     &packetexport.Body{Goal: goal, Boundary: "boundary", DoneWhen: "done", Check: "check", Context: "context"},
		}},
	}
}

func stringsContains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
