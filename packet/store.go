package packet

import (
	"fmt"
	"sort"
	"sync"
)

// EventRecord is one immutable event at an exact position in a packet stream.
// StreamVersion starts at one and must be contiguous within each packet.
type EventRecord struct {
	PacketID      PacketID
	StreamVersion Version
	Event         Event
}

// EventStore preserves packet events and atomically compares every touched stream's
// current version before appending. Implementations must never update or delete events.
type EventStore interface {
	// Load returns every stream when packetIDs is empty, or only the named streams.
	Load(packetIDs ...PacketID) ([]EventRecord, error)
	Append(expected map[PacketID]Version, records []EventRecord) error
}

// MemoryEventStore provides the same append and compare contract without external I/O.
// It is the default for callers that do not opt into a durable store and is useful for
// exercising multiple Tracker instances against one shared log.
type MemoryEventStore struct {
	mu       sync.Mutex
	records  []EventRecord
	versions map[PacketID]Version
	eventIDs map[EventID]struct{}
}

func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		versions: make(map[PacketID]Version),
		eventIDs: make(map[EventID]struct{}),
	}
}

func (store *MemoryEventStore) Load(packetIDs ...PacketID) ([]EventRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if len(packetIDs) == 0 {
		return cloneRecords(store.records), nil
	}
	wanted := make(map[PacketID]struct{}, len(packetIDs))
	for _, packetID := range packetIDs {
		wanted[packetID] = struct{}{}
	}
	result := make([]EventRecord, 0)
	for _, record := range store.records {
		if _, ok := wanted[record.PacketID]; ok {
			result = append(result, EventRecord{
				PacketID: record.PacketID, StreamVersion: record.StreamVersion, Event: cloneEvent(record.Event),
			})
		}
	}
	return result, nil
}

func (store *MemoryEventStore) Append(expected map[PacketID]Version, records []EventRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	for packetID, version := range expected {
		actual := store.versions[packetID]
		if actual != version {
			return &ConflictError{PacketID: packetID, Expected: version, Actual: actual}
		}
	}
	if err := validateAppend(expected, records); err != nil {
		return err
	}
	batchIDs := make(map[EventID]struct{}, len(records))
	for _, record := range records {
		id := record.Event.Metadata().ID
		if _, exists := store.eventIDs[id]; exists {
			return fmt.Errorf("%w: duplicate event id %q", ErrInvalidEvent, id)
		}
		if _, exists := batchIDs[id]; exists {
			return fmt.Errorf("%w: duplicate event id %q", ErrInvalidEvent, id)
		}
		batchIDs[id] = struct{}{}
	}

	store.records = append(store.records, cloneRecords(records)...)
	for _, record := range records {
		store.versions[record.PacketID] = record.StreamVersion
		store.eventIDs[record.Event.Metadata().ID] = struct{}{}
	}
	return nil
}

func validateAppend(expected map[PacketID]Version, records []EventRecord) error {
	if len(records) == 0 {
		return fmt.Errorf("%w: append has no events", ErrInvalidEvent)
	}
	next := make(map[PacketID]Version, len(expected))
	for packetID, version := range expected {
		if packetID == "" {
			return fmt.Errorf("%w: append has an empty packet id", ErrInvalidEvent)
		}
		next[packetID] = version + 1
	}
	for _, record := range records {
		want, touched := next[record.PacketID]
		if !touched {
			return fmt.Errorf("%w: packet %q has no expected version", ErrInvalidEvent, record.PacketID)
		}
		if record.Event == nil || eventPacketID(record.Event) != record.PacketID {
			return fmt.Errorf("%w: event does not belong to packet %q", ErrInvalidEvent, record.PacketID)
		}
		if record.StreamVersion != want {
			return fmt.Errorf("%w: packet %q stream version is %d, want %d", ErrInvalidEvent, record.PacketID, record.StreamVersion, want)
		}
		next[record.PacketID]++
	}
	return nil
}

func cloneRecords(records []EventRecord) []EventRecord {
	result := make([]EventRecord, len(records))
	for index, record := range records {
		result[index] = EventRecord{
			PacketID:      record.PacketID,
			StreamVersion: record.StreamVersion,
			Event:         cloneEvent(record.Event),
		}
	}
	return result
}

func sortRecords(records []EventRecord) {
	sort.Slice(records, func(left, right int) bool {
		if records[left].PacketID != records[right].PacketID {
			return records[left].PacketID < records[right].PacketID
		}
		return records[left].StreamVersion < records[right].StreamVersion
	})
}
