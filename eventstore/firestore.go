// Package eventstore contains durable adapters for the packet event log.
package eventstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	cloudfirestore "cloud.google.com/go/firestore"
	"github.com/martcoca/work-tracker/packet"
)

const (
	DefaultDatabaseID = "(default)"
	DefaultNamespace  = "authoring"
	defaultTimeout    = 10 * time.Second
	eventSchema       = int64(1)
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type Config struct {
	ProjectID        string
	DatabaseID       string
	Namespace        string
	OperationTimeout time.Duration
}

// Firestore preserves immutable event documents and per-packet version counters. The
// counters are compare-and-set in the same transaction that creates each event document.
type Firestore struct {
	client  *cloudfirestore.Client
	root    *cloudfirestore.DocumentRef
	timeout time.Duration
	events  *cloudfirestore.CollectionRef
	streams *cloudfirestore.CollectionRef
}

type eventDocument struct {
	SchemaVersion int64  `firestore:"schema_version"`
	PacketID      string `firestore:"packet_id"`
	StreamVersion int64  `firestore:"stream_version"`
	Kind          string `firestore:"kind"`
	Payload       []byte `firestore:"payload"`
}

type streamDocument struct {
	Version int64 `firestore:"version"`
}

func NewFirestore(ctx context.Context, config Config) (*Firestore, error) {
	config.ProjectID = strings.TrimSpace(config.ProjectID)
	config.DatabaseID = strings.TrimSpace(config.DatabaseID)
	config.Namespace = strings.TrimSpace(config.Namespace)
	if config.ProjectID == "" {
		return nil, errors.New("Firestore project id is required")
	}
	if config.DatabaseID == "" {
		config.DatabaseID = DefaultDatabaseID
	}
	if config.Namespace == "" {
		config.Namespace = DefaultNamespace
	}
	if !namespacePattern.MatchString(config.Namespace) {
		return nil, errors.New("Firestore namespace must be a lowercase path-safe name")
	}
	if config.OperationTimeout <= 0 {
		config.OperationTimeout = defaultTimeout
	}

	client, err := cloudfirestore.NewClientWithDatabase(ctx, config.ProjectID, config.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("create Firestore client: %w", err)
	}
	root := client.Collection("work_tracker_event_stores").Doc(config.Namespace)
	return &Firestore{
		client: client, root: root, timeout: config.OperationTimeout,
		events: root.Collection("events"), streams: root.Collection("streams"),
	}, nil
}

func (store *Firestore) Close() error { return store.client.Close() }

func (store *Firestore) Load(packetIDs ...packet.PacketID) ([]packet.EventRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), store.timeout)
	defer cancel()

	documents := make([]*cloudfirestore.DocumentSnapshot, 0)
	if len(packetIDs) == 0 {
		loaded, err := store.events.Documents(ctx).GetAll()
		if err != nil {
			return nil, fmt.Errorf("read Firestore events: %w", err)
		}
		documents = loaded
	} else {
		seen := make(map[packet.PacketID]struct{}, len(packetIDs))
		for _, packetID := range packetIDs {
			if packetID == "" {
				return nil, errors.New("cannot load an empty packet id")
			}
			if _, duplicate := seen[packetID]; duplicate {
				continue
			}
			loaded, err := store.events.Where("packet_id", "==", string(packetID)).Documents(ctx).GetAll()
			if err != nil {
				return nil, fmt.Errorf("read Firestore events for packet: %w", err)
			}
			documents = append(documents, loaded...)
			seen[packetID] = struct{}{}
		}
	}
	records := make([]packet.EventRecord, 0, len(documents))
	for _, snapshot := range documents {
		var document eventDocument
		if err := snapshot.DataTo(&document); err != nil {
			return nil, fmt.Errorf("decode Firestore event %q: %w", snapshot.Ref.ID, err)
		}
		record, err := decodeRecord(document)
		if err != nil {
			return nil, fmt.Errorf("decode Firestore event %q: %w", snapshot.Ref.ID, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func (store *Firestore) Append(expected map[packet.PacketID]packet.Version, records []packet.EventRecord) error {
	encoded, finalVersions, err := encodeAppend(expected, records)
	if err != nil {
		return err
	}
	packetIDs := make([]packet.PacketID, 0, len(expected))
	for packetID := range expected {
		packetIDs = append(packetIDs, packetID)
	}
	sort.Slice(packetIDs, func(left, right int) bool { return packetIDs[left] < packetIDs[right] })
	streamRefs := make([]*cloudfirestore.DocumentRef, len(packetIDs))
	for index, packetID := range packetIDs {
		streamRefs[index] = store.streams.Doc(pathID(string(packetID)))
	}

	ctx, cancel := context.WithTimeout(context.Background(), store.timeout)
	defer cancel()
	return store.client.RunTransaction(ctx, func(_ context.Context, transaction *cloudfirestore.Transaction) error {
		snapshots, err := transaction.GetAll(streamRefs)
		if err != nil {
			return fmt.Errorf("read Firestore stream versions: %w", err)
		}
		for index, snapshot := range snapshots {
			packetID := packetIDs[index]
			actual, err := decodeStreamVersion(snapshot)
			if err != nil {
				return err
			}
			if actual != expected[packetID] {
				return &packet.ConflictError{PacketID: packetID, Expected: expected[packetID], Actual: actual}
			}
		}

		for index, record := range records {
			if err := transaction.Create(store.events.Doc(pathID(string(record.Event.Metadata().ID))), encoded[index]); err != nil {
				return fmt.Errorf("create immutable Firestore event: %w", err)
			}
		}
		for index, packetID := range packetIDs {
			version := int64(finalVersions[packetID])
			if snapshots[index].Exists() {
				if err := transaction.Update(streamRefs[index], []cloudfirestore.Update{{Path: "version", Value: version}}); err != nil {
					return fmt.Errorf("advance Firestore stream version: %w", err)
				}
			} else if err := transaction.Create(streamRefs[index], streamDocument{Version: version}); err != nil {
				return fmt.Errorf("create Firestore stream version: %w", err)
			}
		}
		return nil
	})
}

func encodeAppend(expected map[packet.PacketID]packet.Version, records []packet.EventRecord) ([]eventDocument, map[packet.PacketID]packet.Version, error) {
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("%w: append has no events", packet.ErrInvalidEvent)
	}
	next := make(map[packet.PacketID]packet.Version, len(expected))
	for packetID, version := range expected {
		if packetID == "" {
			return nil, nil, fmt.Errorf("%w: append has an empty packet id", packet.ErrInvalidEvent)
		}
		next[packetID] = version + 1
	}
	encoded := make([]eventDocument, 0, len(records))
	seenIDs := make(map[packet.EventID]struct{}, len(records))
	for _, record := range records {
		want, touched := next[record.PacketID]
		if !touched || record.Event == nil || eventPacketID(record.Event) != record.PacketID || record.StreamVersion != want {
			return nil, nil, fmt.Errorf("%w: invalid event position for packet %q", packet.ErrInvalidEvent, record.PacketID)
		}
		meta := record.Event.Metadata()
		if meta.ID == "" || meta.At.IsZero() || meta.Actor == "" {
			return nil, nil, fmt.Errorf("%w: event metadata is incomplete", packet.ErrInvalidEvent)
		}
		if _, duplicate := seenIDs[meta.ID]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate event id %q", packet.ErrInvalidEvent, meta.ID)
		}
		payload, err := json.Marshal(record.Event)
		if err != nil {
			return nil, nil, fmt.Errorf("encode event %q: %w", meta.ID, err)
		}
		encoded = append(encoded, eventDocument{
			SchemaVersion: eventSchema,
			PacketID:      string(record.PacketID),
			StreamVersion: int64(record.StreamVersion),
			Kind:          string(record.Event.Kind()),
			Payload:       payload,
		})
		seenIDs[meta.ID] = struct{}{}
		next[record.PacketID]++
	}
	final := make(map[packet.PacketID]packet.Version, len(next))
	for packetID, version := range next {
		final[packetID] = version - 1
	}
	return encoded, final, nil
}

func decodeRecord(document eventDocument) (packet.EventRecord, error) {
	if document.SchemaVersion != eventSchema {
		return packet.EventRecord{}, fmt.Errorf("unsupported event schema %d", document.SchemaVersion)
	}
	if document.PacketID == "" || document.StreamVersion < 1 {
		return packet.EventRecord{}, errors.New("event position is incomplete")
	}
	event, err := decodeEvent(packet.EventKind(document.Kind), document.Payload)
	if err != nil {
		return packet.EventRecord{}, err
	}
	if eventPacketID(event) != packet.PacketID(document.PacketID) {
		return packet.EventRecord{}, errors.New("payload packet id does not match its event position")
	}
	return packet.EventRecord{
		PacketID:      packet.PacketID(document.PacketID),
		StreamVersion: packet.Version(document.StreamVersion),
		Event:         event,
	}, nil
}

func decodeEvent(kind packet.EventKind, payload []byte) (packet.Event, error) {
	var target packet.Event
	switch kind {
	case packet.EventPacketIssued:
		target = &packet.PacketIssued{}
	case packet.EventPacketTaken:
		target = &packet.PacketTaken{}
	case packet.EventPacketCommented:
		target = &packet.PacketCommented{}
	case packet.EventPacketStatusTransition:
		target = &packet.PacketStatusTransitioned{}
	case packet.EventPacketSuperseded:
		target = &packet.PacketSuperseded{}
	case packet.EventPacketClosed:
		target = &packet.PacketClosed{}
	default:
		return nil, fmt.Errorf("unknown event kind %q", kind)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return nil, err
	}
	switch event := target.(type) {
	case *packet.PacketIssued:
		return *event, nil
	case *packet.PacketTaken:
		return *event, nil
	case *packet.PacketCommented:
		return *event, nil
	case *packet.PacketStatusTransitioned:
		return *event, nil
	case *packet.PacketSuperseded:
		return *event, nil
	case *packet.PacketClosed:
		return *event, nil
	default:
		panic("unreachable packet event type")
	}
}

func decodeStreamVersion(snapshot *cloudfirestore.DocumentSnapshot) (packet.Version, error) {
	if !snapshot.Exists() {
		return 0, nil
	}
	var document streamDocument
	if err := snapshot.DataTo(&document); err != nil {
		return 0, fmt.Errorf("decode Firestore stream %q: %w", snapshot.Ref.ID, err)
	}
	if document.Version < 1 {
		return 0, fmt.Errorf("Firestore stream %q has invalid version %d", snapshot.Ref.ID, document.Version)
	}
	return packet.Version(document.Version), nil
}

func pathID(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func eventPacketID(event packet.Event) packet.PacketID {
	switch event := event.(type) {
	case packet.PacketIssued:
		return event.PacketID
	case packet.PacketTaken:
		return event.PacketID
	case packet.PacketCommented:
		return event.PacketID
	case packet.PacketStatusTransitioned:
		return event.PacketID
	case packet.PacketSuperseded:
		return event.PacketID
	case packet.PacketClosed:
		return event.PacketID
	default:
		return ""
	}
}
