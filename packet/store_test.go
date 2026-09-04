package packet

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestTrackerRebuildsProjectionFromSharedEventStore(t *testing.T) {
	store := NewMemoryEventStore()
	first := persistentTestTracker(t, store, "first")
	original := issueForTest(t, first, "packet-persisted")
	original = takeForTest(t, first, original)
	original, err := first.Comment(CommentCommand{
		PacketID:        original.ID(),
		ExpectedVersion: original.Version(),
		Actor:           "actor-reviewer",
		Text:            "stored, not projected",
	})
	if err != nil {
		t.Fatal(err)
	}

	second := persistentTestTracker(t, store, "second")
	reloaded, err := second.Packet(original.ID())
	if err != nil {
		t.Fatalf("load from a second tracker: %v", err)
	}
	if !reflect.DeepEqual(reloaded, original) {
		t.Fatalf("cold projection differs\n got: %#v\nwant: %#v", reloaded, original)
	}

	second.DropProjection()
	if _, err := second.Packet(original.ID()); !errors.Is(err, ErrProjectionUnavailable) {
		t.Fatalf("packet after drop error = %v, want ErrProjectionUnavailable", err)
	}
	if err := second.RebuildProjection(); err != nil {
		t.Fatalf("rebuild from shared store: %v", err)
	}
	rebuilt, err := second.Packet(original.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rebuilt, original) {
		t.Fatalf("store rebuild differs\n got: %#v\nwant: %#v", rebuilt, original)
	}
}

func TestMemoryEventStoreRefusesOneOfTwoConcurrentWriters(t *testing.T) {
	store := NewMemoryEventStore()
	issued := PacketIssued{
		Meta:     testMetadata("event-issued", "actor-author"),
		PacketID: "packet-concurrent-store",
		TenantID: "tenant-synthetic",
		Body:     testBody("concurrent store"),
	}
	if err := store.Append(map[PacketID]Version{issued.PacketID: 0}, []EventRecord{{
		PacketID: issued.PacketID, StreamVersion: 1, Event: issued,
	}}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for index := 1; index <= 2; index++ {
		writers.Add(1)
		go func(index int) {
			defer writers.Done()
			<-start
			event := PacketCommented{
				Meta:     testMetadata(EventID(fmt.Sprintf("event-writer-%d", index)), Actor(fmt.Sprintf("actor-%d", index))),
				PacketID: issued.PacketID,
				Text:     fmt.Sprintf("writer %d", index),
			}
			results <- store.Append(map[PacketID]Version{issued.PacketID: 1}, []EventRecord{{
				PacketID: issued.PacketID, StreamVersion: 2, Event: event,
			}})
		}(index)
	}
	close(start)
	writers.Wait()
	close(results)

	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrConflict):
			conflicted++
		default:
			t.Fatalf("unexpected append result: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("append results: succeeded=%d conflicted=%d, want 1 and 1", succeeded, conflicted)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("stored events = %d, want issued plus exactly one comment", len(records))
	}
}

func TestEventStoreRejectsNonContiguousAppend(t *testing.T) {
	store := NewMemoryEventStore()
	event := PacketIssued{
		Meta:     testMetadata("event-gap", "actor-author"),
		PacketID: "packet-gap",
		TenantID: "tenant-synthetic",
		Body:     testBody("gap"),
	}
	err := store.Append(map[PacketID]Version{event.PacketID: 0}, []EventRecord{{
		PacketID: event.PacketID, StreamVersion: 2, Event: event,
	}})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v, want ErrInvalidEvent", err)
	}
}

func persistentTestTracker(t *testing.T, store EventStore, prefix string) *Tracker {
	t.Helper()
	sources := &deterministicSources{}
	tracker := newTrackerWithStore(
		sources.time,
		func() (EventID, error) {
			id, err := sources.eventID()
			return EventID(prefix + "-" + string(id)), err
		},
		allowAllTenants{},
		store,
	)
	if err := tracker.RebuildProjection(); err != nil {
		t.Fatalf("new persistent tracker: %v", err)
	}
	return tracker
}

func testMetadata(id EventID, actor Actor) Metadata {
	return Metadata{ID: id, At: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC), Actor: actor}
}
