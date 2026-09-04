package packet

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tracker owns an append-only event log and its disposable projection.
//
// Every mutating command requires the version observed by its caller. Commands are
// serialized under one lock; a stale version is rejected before any domain rule is
// evaluated or any event is appended.
type Tracker struct {
	mu              sync.RWMutex
	streams         map[PacketID][]Event
	projection      map[PacketID]Packet
	eventIDs        map[EventID]struct{}
	projectionReady bool
	store           EventStore
	tenantValidator TenantValidator
	now             func() time.Time
	newEventID      func() (EventID, error)
}

// NewTracker returns an empty tracker backed by an in-memory append-only log. Issuing is
// unavailable without a supplied, verified tenant directory.
func NewTracker(tenantValidator TenantValidator) (*Tracker, error) {
	return NewTrackerWithStore(tenantValidator, NewMemoryEventStore())
}

// NewTrackerWithStore rebuilds the disposable projection from an append-only store.
func NewTrackerWithStore(tenantValidator TenantValidator, store EventStore) (*Tracker, error) {
	if tenantValidator == nil {
		return nil, ErrTenantValidatorRequired
	}
	if store == nil {
		return nil, fmt.Errorf("event store is required")
	}
	tracker := newTrackerWithStore(time.Now, randomEventID, tenantValidator, store)
	if err := tracker.RebuildProjection(); err != nil {
		return nil, err
	}
	return tracker, nil
}

func newTracker(now func() time.Time, newEventID func() (EventID, error), tenantValidator TenantValidator) *Tracker {
	return newTrackerWithStore(now, newEventID, tenantValidator, NewMemoryEventStore())
}

func newTrackerWithStore(now func() time.Time, newEventID func() (EventID, error), tenantValidator TenantValidator, store EventStore) *Tracker {
	return &Tracker{
		streams:         make(map[PacketID][]Event),
		projection:      make(map[PacketID]Packet),
		eventIDs:        make(map[EventID]struct{}),
		projectionReady: true,
		store:           store,
		tenantValidator: tenantValidator,
		now:             now,
		newEventID:      newEventID,
	}
}

func randomEventID() (EventID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate event id: %w", err)
	}
	return EventID(hex.EncodeToString(raw[:])), nil
}

// Issue appends the event that freezes a new packet's body.
func (t *Tracker) Issue(command IssueCommand) (Packet, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.prepareLocked(command.PacketID); err != nil {
		return Packet{}, err
	}
	if _, exists := t.streams[command.PacketID]; exists {
		return Packet{}, fmt.Errorf("%w: %q", ErrAlreadyExists, command.PacketID)
	}
	if err := t.tenantValidator.ValidateTenantID(string(command.TenantID), t.now().UTC()); err != nil {
		return Packet{}, err
	}
	meta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, err
	}
	event := PacketIssued{
		Meta:     meta,
		PacketID: command.PacketID,
		TenantID: command.TenantID,
		Body:     command.Body,
	}
	if err := t.appendLocked(event); err != nil {
		return Packet{}, err
	}
	return clonePacket(t.projection[command.PacketID]), nil
}

// Take appends an attributed take and the not-started to in-progress transition as one
// atomic mutation.
func (t *Tracker) Take(command TakeCommand) (Packet, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.prepareLocked(command.PacketID); err != nil {
		return Packet{}, err
	}
	current, err := t.currentLocked(command.PacketID, command.ExpectedVersion)
	if err != nil {
		return Packet{}, err
	}
	takenMeta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, err
	}
	transitionMeta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, err
	}
	events := []Event{
		PacketTaken{Meta: takenMeta, PacketID: command.PacketID},
		PacketStatusTransitioned{
			Meta:     transitionMeta,
			PacketID: command.PacketID,
			From:     current.status,
			To:       StatusInProgress,
		},
	}
	if err := t.appendLocked(events...); err != nil {
		return Packet{}, err
	}
	return clonePacket(t.projection[command.PacketID]), nil
}

// Comment appends an attributed comment. It never replaces or removes an earlier one.
func (t *Tracker) Comment(command CommentCommand) (Packet, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.prepareLocked(command.PacketID); err != nil {
		return Packet{}, err
	}
	if _, err := t.currentLocked(command.PacketID, command.ExpectedVersion); err != nil {
		return Packet{}, err
	}
	meta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, err
	}
	if err := t.appendLocked(PacketCommented{
		Meta:     meta,
		PacketID: command.PacketID,
		Text:     command.Text,
	}); err != nil {
		return Packet{}, err
	}
	return clonePacket(t.projection[command.PacketID]), nil
}

// Transition appends a legal status transition. Moving to done also appends an explicit
// close event in the same atomic mutation.
func (t *Tracker) Transition(command TransitionCommand) (Packet, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.prepareLocked(command.PacketID); err != nil {
		return Packet{}, err
	}
	current, err := t.currentLocked(command.PacketID, command.ExpectedVersion)
	if err != nil {
		return Packet{}, err
	}
	transitionMeta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, err
	}
	events := []Event{PacketStatusTransitioned{
		Meta:     transitionMeta,
		PacketID: command.PacketID,
		From:     current.status,
		To:       command.To,
		Evidence: append([]Evidence(nil), command.Evidence...),
	}}
	if command.To == StatusDone {
		closedMeta, err := t.nextMetadataLocked(command.Actor)
		if err != nil {
			return Packet{}, err
		}
		events = append(events, PacketClosed{
			Meta:     closedMeta,
			PacketID: command.PacketID,
			Reason:   CloseReasonDone,
		})
	}
	if err := t.appendLocked(events...); err != nil {
		return Packet{}, err
	}
	return clonePacket(t.projection[command.PacketID]), nil
}

// Supersede atomically issues a replacement that names its parent, links the parent to
// the replacement, and closes the parent as superseded.
func (t *Tracker) Supersede(command SupersedeCommand) (Packet, Packet, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.prepareLocked(command.PacketID, command.ReplacementID); err != nil {
		return Packet{}, Packet{}, err
	}
	if _, err := t.currentLocked(command.PacketID, command.ExpectedVersion); err != nil {
		return Packet{}, Packet{}, err
	}
	if _, exists := t.streams[command.ReplacementID]; exists {
		return Packet{}, Packet{}, fmt.Errorf("%w: %q", ErrAlreadyExists, command.ReplacementID)
	}
	if err := t.tenantValidator.ValidateTenantID(string(command.ReplacementTenant), t.now().UTC()); err != nil {
		return Packet{}, Packet{}, err
	}
	issuedMeta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, Packet{}, err
	}
	supersededMeta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, Packet{}, err
	}
	closedMeta, err := t.nextMetadataLocked(command.Actor)
	if err != nil {
		return Packet{}, Packet{}, err
	}
	events := []Event{
		PacketIssued{
			Meta:     issuedMeta,
			PacketID: command.ReplacementID,
			TenantID: command.ReplacementTenant,
			Body:     command.ReplacementBody,
			ParentID: command.PacketID,
		},
		PacketSuperseded{
			Meta:          supersededMeta,
			PacketID:      command.PacketID,
			ReplacementID: command.ReplacementID,
		},
		PacketClosed{
			Meta:     closedMeta,
			PacketID: command.PacketID,
			Reason:   CloseReasonSuperseded,
		},
	}
	if err := t.appendLocked(events...); err != nil {
		return Packet{}, Packet{}, err
	}
	return clonePacket(t.projection[command.PacketID]), clonePacket(t.projection[command.ReplacementID]), nil
}

// Packet returns a defensive copy of the current projection for one packet.
func (t *Tracker) Packet(id PacketID) (Packet, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.prepareLocked(id); err != nil {
		return Packet{}, err
	}
	packet, ok := t.projection[id]
	if !ok {
		return Packet{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return clonePacket(packet), nil
}

// Packets returns defensive snapshots sorted by packet id.
func (t *Tracker) Packets() ([]Packet, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.prepareLocked(); err != nil {
		return nil, err
	}
	packets := make([]Packet, 0, len(t.projection))
	for _, projected := range t.projection {
		packets = append(packets, clonePacket(projected))
	}
	sort.Slice(packets, func(left, right int) bool {
		return packets[left].ID() < packets[right].ID()
	})
	return packets, nil
}

// History returns defensive copies of the events for one packet in append order.
func (t *Tracker) History(id PacketID) ([]Event, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.refreshLocked(t.projectionReady, id); err != nil {
		return nil, err
	}

	history, ok := t.streams[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	result := make([]Event, len(history))
	for i, event := range history {
		result[i] = cloneEvent(event)
	}
	return result, nil
}

// DropProjection discards all derived state while retaining the event log.
func (t *Tracker) DropProjection() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.projection = nil
	t.eventIDs = nil
	t.projectionReady = false
}

// RebuildProjection recreates all current state by replaying the append-only event log.
func (t *Tracker) RebuildProjection() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.refreshLocked(true)
}

func (t *Tracker) refreshLocked(rebuildProjection bool, packetIDs ...PacketID) error {
	records, err := t.store.Load(packetIDs...)
	if err != nil {
		return fmt.Errorf("load event store: %w", err)
	}
	sortRecords(records)

	fullReload := len(packetIDs) == 0
	targets := make(map[PacketID]struct{}, len(packetIDs))
	for _, packetID := range packetIDs {
		if packetID == "" {
			return fmt.Errorf("%w: cannot load an empty packet id", ErrInvalidEvent)
		}
		targets[packetID] = struct{}{}
	}
	streams := make(map[PacketID][]Event)
	rebuilt := make(map[PacketID]Packet)
	eventIDs := make(map[EventID]struct{}, len(t.eventIDs))
	if !fullReload && rebuildProjection {
		for id := range t.eventIDs {
			eventIDs[id] = struct{}{}
		}
		for packetID := range targets {
			for _, event := range t.streams[packetID] {
				delete(eventIDs, event.Metadata().ID)
			}
		}
	}
	for _, record := range records {
		if !fullReload {
			if _, requested := targets[record.PacketID]; !requested {
				return fmt.Errorf("%w: store returned unrequested packet %q", ErrInvalidEvent, record.PacketID)
			}
		}
		want := Version(len(streams[record.PacketID]) + 1)
		if record.StreamVersion != want {
			return fmt.Errorf("%w: packet %q stream version is %d, want %d", ErrInvalidEvent, record.PacketID, record.StreamVersion, want)
		}
		if record.Event == nil || eventPacketID(record.Event) != record.PacketID {
			return fmt.Errorf("%w: event does not belong to packet %q", ErrInvalidEvent, record.PacketID)
		}
		meta := record.Event.Metadata()
		if _, duplicate := eventIDs[meta.ID]; duplicate {
			return fmt.Errorf("%w: duplicate event id %q", ErrInvalidEvent, meta.ID)
		}
		if rebuildProjection {
			packet := rebuilt[record.PacketID]
			if err := applyEvent(&packet, record.Event); err != nil {
				return fmt.Errorf("rebuild %q at version %d: %w", record.PacketID, packet.version+1, err)
			}
			rebuilt[record.PacketID] = packet
		}
		streams[record.PacketID] = append(streams[record.PacketID], cloneEvent(record.Event))
		eventIDs[meta.ID] = struct{}{}
	}
	if fullReload {
		t.streams = streams
		if rebuildProjection {
			t.projection = rebuilt
			t.eventIDs = eventIDs
			t.projectionReady = true
		}
		return nil
	}
	for packetID := range targets {
		delete(t.streams, packetID)
		if history := streams[packetID]; len(history) > 0 {
			t.streams[packetID] = history
		}
		if rebuildProjection {
			delete(t.projection, packetID)
			if projected, exists := rebuilt[packetID]; exists {
				t.projection[packetID] = projected
			}
		}
	}
	if rebuildProjection {
		t.eventIDs = eventIDs
	}
	return nil
}

func (t *Tracker) prepareLocked(packetIDs ...PacketID) error {
	if err := t.requireProjectionLocked(); err != nil {
		return err
	}
	return t.refreshLocked(true, packetIDs...)
}

func (t *Tracker) requireProjectionLocked() error {
	if !t.projectionReady {
		return ErrProjectionUnavailable
	}
	return nil
}

func (t *Tracker) currentLocked(id PacketID, expected Version) (Packet, error) {
	if err := t.requireProjectionLocked(); err != nil {
		return Packet{}, err
	}
	actual := Version(len(t.streams[id]))
	if actual != expected {
		return Packet{}, &ConflictError{PacketID: id, Expected: expected, Actual: actual}
	}
	packet, ok := t.projection[id]
	if !ok {
		return Packet{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return packet, nil
}

func (t *Tracker) nextMetadataLocked(actor Actor) (Metadata, error) {
	id, err := t.newEventID()
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{ID: id, At: t.now().UTC(), Actor: actor}, nil
}

func (t *Tracker) appendLocked(events ...Event) error {
	working := make(map[PacketID]Packet)
	batchIDs := make(map[EventID]struct{})
	expected := make(map[PacketID]Version)
	nextVersion := make(map[PacketID]Version)
	records := make([]EventRecord, 0, len(events))

	for _, event := range events {
		packetID := eventPacketID(event)
		if packetID == "" {
			return fmt.Errorf("%w: event has no packet id", ErrInvalidEvent)
		}
		meta := event.Metadata()
		if _, exists := t.eventIDs[meta.ID]; exists {
			return fmt.Errorf("%w: duplicate event id %q", ErrInvalidEvent, meta.ID)
		}
		if _, exists := batchIDs[meta.ID]; exists {
			return fmt.Errorf("%w: duplicate event id %q", ErrInvalidEvent, meta.ID)
		}
		packet, touched := working[packetID]
		if !touched {
			packet = clonePacket(t.projection[packetID])
			expected[packetID] = Version(len(t.streams[packetID]))
			nextVersion[packetID] = expected[packetID] + 1
		}
		if err := applyEvent(&packet, event); err != nil {
			return err
		}
		working[packetID] = packet
		batchIDs[meta.ID] = struct{}{}
		records = append(records, EventRecord{
			PacketID:      packetID,
			StreamVersion: nextVersion[packetID],
			Event:         event,
		})
		nextVersion[packetID]++
	}
	if err := t.store.Append(expected, records); err != nil {
		packetIDs := make([]PacketID, 0, len(expected))
		for packetID := range expected {
			packetIDs = append(packetIDs, packetID)
		}
		_ = t.refreshLocked(true, packetIDs...)
		return err
	}

	for _, record := range records {
		copied := cloneEvent(record.Event)
		packetID := record.PacketID
		t.streams[packetID] = append(t.streams[packetID], copied)
		t.eventIDs[record.Event.Metadata().ID] = struct{}{}
	}
	for packetID, packet := range working {
		t.projection[packetID] = packet
	}
	return nil
}

func applyEvent(packet *Packet, event Event) error {
	if err := validateMetadata(event.Metadata()); err != nil {
		return err
	}

	switch event := event.(type) {
	case PacketIssued:
		if packet.version != 0 {
			return fmt.Errorf("%w: packet %q was already issued", ErrInvalidEvent, event.PacketID)
		}
		if event.PacketID == "" {
			return fmt.Errorf("%w: packet id is required", ErrInvalidEvent)
		}
		if event.TenantID == "" {
			return fmt.Errorf("%w: tenant id is required", ErrInvalidEvent)
		}
		if event.ParentID == event.PacketID {
			return fmt.Errorf("%w: packet cannot supersede itself", ErrInvalidEvent)
		}
		packet.id = event.PacketID
		packet.tenantID = event.TenantID
		packet.body = event.Body
		packet.status = StatusNotStarted
		packet.parentID = event.ParentID

	case PacketTaken:
		if err := requireIssuedPacket(*packet, event.PacketID); err != nil {
			return err
		}
		if packet.closure != nil {
			return ErrClosed
		}
		if packet.takenBy != "" {
			return fmt.Errorf("%w: packet was already taken", ErrInvalidEvent)
		}
		packet.takenBy = event.Meta.Actor

	case PacketCommented:
		if err := requireIssuedPacket(*packet, event.PacketID); err != nil {
			return err
		}
		if strings.TrimSpace(event.Text) == "" {
			return fmt.Errorf("%w: comment text is required", ErrInvalidEvent)
		}
		packet.comments = append(packet.comments, Comment{
			EventID: event.Meta.ID,
			At:      event.Meta.At,
			Actor:   event.Meta.Actor,
			Text:    event.Text,
		})

	case PacketStatusTransitioned:
		if err := requireIssuedPacket(*packet, event.PacketID); err != nil {
			return err
		}
		if packet.closure != nil {
			return ErrClosed
		}
		if event.From != packet.status {
			return fmt.Errorf("%w: event says %q, projection is %q", ErrInvalidEvent, event.From, packet.status)
		}
		if !legalTransition(packet.status, event.To) {
			return fmt.Errorf("%w: %q to %q", ErrIllegalTransition, packet.status, event.To)
		}
		if event.To == StatusDone {
			if len(event.Evidence) == 0 {
				return ErrEvidenceRequired
			}
			for _, evidence := range event.Evidence {
				if strings.TrimSpace(string(evidence)) == "" {
					return ErrEvidenceRequired
				}
			}
			packet.evidence = append([]Evidence(nil), event.Evidence...)
		} else if len(event.Evidence) != 0 {
			return ErrUnexpectedEvidence
		}
		packet.status = event.To

	case PacketSuperseded:
		if err := requireIssuedPacket(*packet, event.PacketID); err != nil {
			return err
		}
		if packet.closure != nil {
			return ErrClosed
		}
		if event.ReplacementID == "" || event.ReplacementID == event.PacketID {
			return fmt.Errorf("%w: valid replacement id is required", ErrInvalidEvent)
		}
		if packet.supersededBy != "" {
			return fmt.Errorf("%w: packet was already superseded", ErrInvalidEvent)
		}
		packet.supersededBy = event.ReplacementID

	case PacketClosed:
		if err := requireIssuedPacket(*packet, event.PacketID); err != nil {
			return err
		}
		if packet.closure != nil {
			return ErrClosed
		}
		switch event.Reason {
		case CloseReasonDone:
			if packet.status != StatusDone {
				return fmt.Errorf("%w: completion close requires done status", ErrInvalidEvent)
			}
		case CloseReasonSuperseded:
			if packet.supersededBy == "" {
				return fmt.Errorf("%w: supersession close requires replacement link", ErrInvalidEvent)
			}
		default:
			return fmt.Errorf("%w: unknown close reason %q", ErrInvalidEvent, event.Reason)
		}
		packet.closure = &Closure{
			EventID: event.Meta.ID,
			At:      event.Meta.At,
			Actor:   event.Meta.Actor,
			Reason:  event.Reason,
		}

	default:
		return fmt.Errorf("%w: unknown event type %T", ErrInvalidEvent, event)
	}

	packet.version++
	return nil
}

func validateMetadata(meta Metadata) error {
	if meta.ID == "" {
		return fmt.Errorf("%w: event id is required", ErrInvalidEvent)
	}
	if meta.At.IsZero() {
		return fmt.Errorf("%w: event timestamp is required", ErrInvalidEvent)
	}
	if meta.Actor == "" {
		return fmt.Errorf("%w: event actor is required", ErrInvalidEvent)
	}
	return nil
}

func requireIssuedPacket(packet Packet, eventPacketID PacketID) error {
	if packet.version == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, eventPacketID)
	}
	if packet.id != eventPacketID {
		return fmt.Errorf("%w: event packet id %q does not match %q", ErrInvalidEvent, eventPacketID, packet.id)
	}
	return nil
}

func legalTransition(from, to Status) bool {
	switch from {
	case StatusNotStarted:
		return to == StatusInProgress || to == StatusBlocked
	case StatusInProgress:
		return to == StatusDone || to == StatusBlocked
	case StatusDone, StatusBlocked:
		return false
	default:
		return false
	}
}

func eventPacketID(event Event) PacketID {
	switch event := event.(type) {
	case PacketIssued:
		return event.PacketID
	case PacketTaken:
		return event.PacketID
	case PacketCommented:
		return event.PacketID
	case PacketStatusTransitioned:
		return event.PacketID
	case PacketSuperseded:
		return event.PacketID
	case PacketClosed:
		return event.PacketID
	default:
		return ""
	}
}

func clonePacket(packet Packet) Packet {
	packet.comments = append([]Comment(nil), packet.comments...)
	packet.evidence = append([]Evidence(nil), packet.evidence...)
	if packet.closure != nil {
		closure := *packet.closure
		packet.closure = &closure
	}
	return packet
}

func cloneEvent(event Event) Event {
	if transitioned, ok := event.(PacketStatusTransitioned); ok {
		transitioned.Evidence = append([]Evidence(nil), transitioned.Evidence...)
		return transitioned
	}
	return event
}
