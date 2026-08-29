// Package packet models the immutable scope and append-only history of a work packet.
package packet

import "time"

// PacketID identifies a packet. Its contents are opaque to the model.
type PacketID string

// TenantID identifies the tenant that owns a packet. Directory validation belongs to
// the layer above this package.
type TenantID string

// EventID identifies one event in the append-only log.
type EventID string

// Actor identifies the human or agent responsible for an event. Authentication and
// authorization belong to the layer above this package.
type Actor string

// Version is the number of events in a packet's stream.
type Version uint64

// Status is the projected workflow status of a packet.
type Status string

const (
	StatusNotStarted Status = "not started"
	StatusInProgress Status = "in progress"
	StatusDone       Status = "done"
	StatusBlocked    Status = "blocked"
)

// CloseReason records why a packet stopped accepting lifecycle changes.
type CloseReason string

const (
	CloseReasonDone       CloseReason = "done"
	CloseReasonSuperseded CloseReason = "superseded"
)

// Body is the scope frozen when a packet is issued.
//
// Body values are copied into an issue event and returned by value. The model exposes no
// operation that can replace or edit one after issue.
type Body struct {
	Goal     string
	Boundary string
	DoneWhen string
	Check    string
	Context  string
}

// Evidence is an opaque reference proving that a packet's check passed.
type Evidence string

// Metadata is present on every event.
type Metadata struct {
	ID    EventID
	At    time.Time
	Actor Actor
}

// EventKind identifies a domain event without relying on its Go type name.
type EventKind string

const (
	EventPacketIssued           EventKind = "packet issued"
	EventPacketTaken            EventKind = "packet taken"
	EventPacketCommented        EventKind = "packet commented"
	EventPacketStatusTransition EventKind = "packet status transitioned"
	EventPacketSuperseded       EventKind = "packet superseded"
	EventPacketClosed           EventKind = "packet closed"
)

// Event is one immutable fact in a packet stream.
type Event interface {
	Kind() EventKind
	Metadata() Metadata
	packetEvent()
}

// PacketIssued freezes the packet body and tenant at issue time. ParentID is set only
// when this packet supersedes another packet.
type PacketIssued struct {
	Meta     Metadata
	PacketID PacketID
	TenantID TenantID
	Body     Body
	ParentID PacketID
}

func (e PacketIssued) Kind() EventKind    { return EventPacketIssued }
func (e PacketIssued) Metadata() Metadata { return e.Meta }
func (e PacketIssued) packetEvent()       {}

// PacketTaken attributes the act of taking work. The accompanying status transition
// moves the packet from not started to in progress in the same append.
type PacketTaken struct {
	Meta     Metadata
	PacketID PacketID
}

func (e PacketTaken) Kind() EventKind    { return EventPacketTaken }
func (e PacketTaken) Metadata() Metadata { return e.Meta }
func (e PacketTaken) packetEvent()       {}

// PacketCommented appends an attributed comment.
type PacketCommented struct {
	Meta     Metadata
	PacketID PacketID
	Text     string
}

func (e PacketCommented) Kind() EventKind    { return EventPacketCommented }
func (e PacketCommented) Metadata() Metadata { return e.Meta }
func (e PacketCommented) packetEvent()       {}

// PacketStatusTransitioned records a legal change in workflow status. Evidence is
// required when To is StatusDone and forbidden for every other target status.
type PacketStatusTransitioned struct {
	Meta     Metadata
	PacketID PacketID
	From     Status
	To       Status
	Evidence []Evidence
}

func (e PacketStatusTransitioned) Kind() EventKind    { return EventPacketStatusTransition }
func (e PacketStatusTransitioned) Metadata() Metadata { return e.Meta }
func (e PacketStatusTransitioned) packetEvent()       {}

// PacketSuperseded links an original packet to its replacement.
type PacketSuperseded struct {
	Meta          Metadata
	PacketID      PacketID
	ReplacementID PacketID
}

func (e PacketSuperseded) Kind() EventKind    { return EventPacketSuperseded }
func (e PacketSuperseded) Metadata() Metadata { return e.Meta }
func (e PacketSuperseded) packetEvent()       {}

// PacketClosed records why a packet was closed.
type PacketClosed struct {
	Meta     Metadata
	PacketID PacketID
	Reason   CloseReason
}

func (e PacketClosed) Kind() EventKind    { return EventPacketClosed }
func (e PacketClosed) Metadata() Metadata { return e.Meta }
func (e PacketClosed) packetEvent()       {}

// Comment is the projected, attributed form of a PacketCommented event.
type Comment struct {
	EventID EventID
	At      time.Time
	Actor   Actor
	Text    string
}

// Closure is the projected, attributed form of a PacketClosed event.
type Closure struct {
	EventID EventID
	At      time.Time
	Actor   Actor
	Reason  CloseReason
}

// Packet is an immutable snapshot derived from the event log. Its fields are private so
// callers cannot mutate the projection through a returned value.
type Packet struct {
	id           PacketID
	tenantID     TenantID
	body         Body
	status       Status
	version      Version
	takenBy      Actor
	comments     []Comment
	evidence     []Evidence
	parentID     PacketID
	supersededBy PacketID
	closure      *Closure
}

func (p Packet) ID() PacketID       { return p.id }
func (p Packet) TenantID() TenantID { return p.tenantID }
func (p Packet) Body() Body         { return p.body }
func (p Packet) Status() Status     { return p.status }
func (p Packet) Version() Version   { return p.version }

// TakenBy returns the actor that took the packet, if it has been taken.
func (p Packet) TakenBy() (Actor, bool) { return p.takenBy, p.takenBy != "" }

// Comments returns a copy so callers cannot edit or delete projected comments.
func (p Packet) Comments() []Comment {
	return append([]Comment(nil), p.comments...)
}

// Evidence returns a copy of the evidence attached to the done transition.
func (p Packet) Evidence() []Evidence {
	return append([]Evidence(nil), p.evidence...)
}

// ParentID returns the packet this packet supersedes, if any.
func (p Packet) ParentID() (PacketID, bool) { return p.parentID, p.parentID != "" }

// SupersededBy returns this packet's replacement, if any.
func (p Packet) SupersededBy() (PacketID, bool) {
	return p.supersededBy, p.supersededBy != ""
}

// Closure returns a copy of the packet's closure, if it is closed.
func (p Packet) Closure() (Closure, bool) {
	if p.closure == nil {
		return Closure{}, false
	}
	return *p.closure, true
}

// IssueCommand contains the facts needed to issue a packet.
type IssueCommand struct {
	PacketID PacketID
	TenantID TenantID
	Body     Body
	Actor    Actor
}

// TakeCommand takes a not-started packet and moves it to in progress.
type TakeCommand struct {
	PacketID        PacketID
	ExpectedVersion Version
	Actor           Actor
}

// CommentCommand appends a comment without changing status.
type CommentCommand struct {
	PacketID        PacketID
	ExpectedVersion Version
	Actor           Actor
	Text            string
}

// TransitionCommand changes status. Evidence is accepted only for a transition to done.
type TransitionCommand struct {
	PacketID        PacketID
	ExpectedVersion Version
	Actor           Actor
	To              Status
	Evidence        []Evidence
}

// SupersedeCommand atomically closes a packet and issues its replacement.
type SupersedeCommand struct {
	PacketID          PacketID
	ExpectedVersion   Version
	ReplacementID     PacketID
	ReplacementTenant TenantID
	ReplacementBody   Body
	Actor             Actor
}
