package eventstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/eventstore"
	"github.com/martcoca/work-tracker/packet"
)

// TestFirestorePersistsAndSerializesPacketEvents is intentionally skipped unless an
// authorized operator supplies an existing Firestore project. It creates a unique,
// append-only test namespace and leaves its few documents in place as durable evidence.
func TestFirestorePersistsAndSerializesPacketEvents(t *testing.T) {
	projectID := os.Getenv("FIRESTORE_INTEGRATION_PROJECT_ID")
	if projectID == "" {
		t.Skip("FIRESTORE_INTEGRATION_PROJECT_ID is not set; real-store proof was not run")
	}
	databaseID := os.Getenv("FIRESTORE_INTEGRATION_DATABASE_ID")
	if databaseID == "" {
		databaseID = eventstore.DefaultDatabaseID
	}
	namespace := fmt.Sprintf("e07-%d", time.Now().UTC().UnixNano())
	config := eventstore.Config{ProjectID: projectID, DatabaseID: databaseID, Namespace: namespace}

	firstStore := openStore(t, config)
	first := openTracker(t, firstStore)
	want, err := first.Issue(packet.IssueCommand{
		PacketID: "0004-E07-T99", TenantID: "tenant-integration", Actor: "actor-integration",
		Body: packet.Body{Goal: "persist", Boundary: "integration", DoneWhen: "survives", Check: "replay", Context: namespace},
	})
	if err != nil {
		t.Fatalf("issue against Firestore: %v", err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatalf("close first client: %v", err)
	}

	coldStore := openStore(t, config)
	cold := openTracker(t, coldStore)
	got, err := cold.Packet(want.ID())
	if err != nil {
		t.Fatalf("read after new client and tracker: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cold projection differs\n got: %#v\nwant: %#v", got, want)
	}
	cold.DropProjection()
	if err := cold.RebuildProjection(); err != nil {
		t.Fatalf("rebuild projection from Firestore events: %v", err)
	}
	rebuilt, err := cold.Packet(want.ID())
	if err != nil || !reflect.DeepEqual(rebuilt, want) {
		t.Fatalf("rebuilt packet differs: packet=%#v error=%v", rebuilt, err)
	}
	if err := coldStore.Close(); err != nil {
		t.Fatalf("close cold client: %v", err)
	}

	leftStore := openStore(t, config)
	defer leftStore.Close()
	rightStore := openStore(t, config)
	defer rightStore.Close()
	left := openTracker(t, leftStore)
	right := openTracker(t, rightStore)
	leftPrior, err := left.Packet(want.ID())
	if err != nil {
		t.Fatal(err)
	}
	rightPrior, err := right.Packet(want.ID())
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for index, candidate := range []*packet.Tracker{left, right} {
		writers.Add(1)
		go func(index int, candidate *packet.Tracker) {
			defer writers.Done()
			<-start
			_, err := candidate.Comment(packet.CommentCommand{
				PacketID:        want.ID(),
				ExpectedVersion: []packet.Version{leftPrior.Version(), rightPrior.Version()}[index],
				Actor:           packet.Actor(fmt.Sprintf("writer-%d", index+1)),
				Text:            fmt.Sprintf("concurrent writer %d", index+1),
			})
			results <- err
		}(index, candidate)
	}
	close(start)
	writers.Wait()
	close(results)

	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, packet.ErrConflict):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent writer result: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent writers: succeeded=%d conflicted=%d, want 1 and 1", succeeded, conflicted)
	}

	finalStore := openStore(t, config)
	defer finalStore.Close()
	final := openTracker(t, finalStore)
	projected, err := final.Packet(want.ID())
	if err != nil {
		t.Fatal(err)
	}
	if projected.Version() != 2 || len(projected.Comments()) != 1 {
		t.Fatalf("accepted event was lost: version=%d comments=%d", projected.Version(), len(projected.Comments()))
	}
	t.Logf("real Firestore namespace %q survived a new client, replayed identically, and retained the accepted concurrent event while refusing the stale writer", namespace)
}

func openStore(t *testing.T, config eventstore.Config) *eventstore.Firestore {
	t.Helper()
	store, err := eventstore.NewFirestore(context.Background(), config)
	if err != nil {
		t.Fatalf("open Firestore: %v", err)
	}
	return store
}

func openTracker(t *testing.T, store packet.EventStore) *packet.Tracker {
	t.Helper()
	tracker, err := packet.NewTrackerWithStore(integrationTenants{}, store)
	if err != nil {
		t.Fatalf("open tracker: %v", err)
	}
	return tracker
}

type integrationTenants struct{}

func (integrationTenants) ValidateTenantID(string, time.Time) error { return nil }
