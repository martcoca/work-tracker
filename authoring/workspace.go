// Package authoring manages editable drafts before handing immutable scope to the packet model.
package authoring

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packet"
)

var (
	ErrDraftNotFound = errors.New("draft not found")
	ErrDraftIssued   = errors.New("draft was already issued")
	ErrDraftConflict = errors.New("draft changed since it was read")
	ErrIncomplete    = errors.New("draft is incomplete")
	ErrInvalidScope  = errors.New("draft scope is invalid")
	packetIDPattern  = regexp.MustCompile(`^[0-9]{4}-E[0-9]{2}-T[0-9]{2}$`)
)

type State string

const (
	StateDraft  State = "draft"
	StateIssued State = "issued"
)

// ScopeValidator proves that initiative, epic, target, and tenant are real at issue time.
type ScopeValidator interface {
	ValidateScope(tenantID, initiativeID, epicID, target string, at time.Time) error
}

// Draft is editable application state. It is not a packet until Issue succeeds.
type Draft struct {
	ID            string      `json:"id"`
	PacketID      string      `json:"packet_id"`
	InitiativeID  string      `json:"initiative_id"`
	EpicID        string      `json:"epic_id"`
	Target        string      `json:"target"`
	TenantID      string      `json:"tenant_id"`
	AuthorSubject string      `json:"author_subject"`
	Body          packet.Body `json:"body"`
	ParentID      string      `json:"parent_id,omitempty"`
	State         State       `json:"state"`
	Version       uint64      `json:"version"`
}

type CreateCommand struct {
	PacketID     string
	InitiativeID string
	EpicID       string
	Target       string
	Body         packet.Body
}

type EditCommand struct {
	DraftID         string
	ExpectedVersion uint64
	PacketID        string
	Target          string
	Body            packet.Body
}

type SupersessionCommand struct {
	ParentID     string
	PacketID     string
	InitiativeID string
	EpicID       string
	Target       string
	Body         packet.Body
}

type IssueCommand struct {
	DraftID         string
	ExpectedVersion uint64
}

type IssueResult struct {
	Draft     Draft
	Packet    packet.Packet
	Parent    packet.Packet
	HasParent bool
}

// ConflictError reports optimistic-concurrency failure for a draft edit or issue.
type ConflictError struct {
	Expected uint64
	Actual   uint64
}

func (err *ConflictError) Error() string {
	return fmt.Sprintf("%s: expected version %d, actual version %d", ErrDraftConflict, err.Expected, err.Actual)
}

func (err *ConflictError) Unwrap() error { return ErrDraftConflict }

// Workspace owns only pre-issue drafts and delegates issued state to E01's Tracker.
type Workspace struct {
	mu         sync.Mutex
	drafts     map[string]Draft
	tracker    *packet.Tracker
	scope      ScopeValidator
	now        func() time.Time
	newDraftID func() (string, error)
}

func NewWorkspace(tracker *packet.Tracker, scope ScopeValidator) (*Workspace, error) {
	if tracker == nil || scope == nil {
		return nil, errors.New("packet tracker and scope validator are required")
	}
	return &Workspace{
		drafts: make(map[string]Draft), tracker: tracker, scope: scope,
		now: time.Now, newDraftID: randomDraftID,
	}, nil
}

func randomDraftID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate draft id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

// Create starts an editable draft. Body fields may be incomplete until issue.
func (workspace *Workspace) Create(principal identity.Principal, command CreateCommand) (Draft, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()

	if err := validatePrincipal(principal); err != nil {
		return Draft{}, err
	}
	if err := validateIdentity(command.PacketID, command.InitiativeID, command.EpicID, command.Target); err != nil {
		return Draft{}, err
	}
	id, err := workspace.newDraftID()
	if err != nil {
		return Draft{}, err
	}
	draft := Draft{
		ID: id, PacketID: command.PacketID, InitiativeID: command.InitiativeID, EpicID: command.EpicID,
		Target: command.Target, TenantID: principal.TenantID, AuthorSubject: principal.Subject,
		Body: command.Body, State: StateDraft, Version: 1,
	}
	workspace.drafts[id] = draft
	return draft, nil
}

// CreateSupersession starts a replacement draft without changing the parent.
func (workspace *Workspace) CreateSupersession(principal identity.Principal, command SupersessionCommand) (Draft, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()

	if err := validatePrincipal(principal); err != nil {
		return Draft{}, err
	}
	parent, err := workspace.tracker.Packet(packet.PacketID(command.ParentID))
	if err != nil || string(parent.TenantID()) != principal.TenantID {
		return Draft{}, ErrDraftNotFound
	}
	if !packetInScope(command.ParentID, command.InitiativeID, command.EpicID) {
		return Draft{}, fmt.Errorf("%w: parent does not belong to route scope", ErrInvalidScope)
	}
	if err := validateIdentity(command.PacketID, command.InitiativeID, command.EpicID, command.Target); err != nil {
		return Draft{}, err
	}
	if command.PacketID == command.ParentID {
		return Draft{}, fmt.Errorf("%w: replacement must have a new packet id", ErrInvalidScope)
	}
	id, err := workspace.newDraftID()
	if err != nil {
		return Draft{}, err
	}
	draft := Draft{
		ID: id, PacketID: command.PacketID, InitiativeID: command.InitiativeID, EpicID: command.EpicID,
		Target: command.Target, TenantID: principal.TenantID, AuthorSubject: principal.Subject,
		Body: command.Body, ParentID: command.ParentID, State: StateDraft, Version: 1,
	}
	workspace.drafts[id] = draft
	return draft, nil
}

// Edit replaces every editable value on a draft and increments its version.
func (workspace *Workspace) Edit(principal identity.Principal, command EditCommand) (Draft, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()

	draft, err := workspace.ownedDraft(principal, command.DraftID)
	if err != nil {
		return Draft{}, err
	}
	if err := requireEditable(draft); err != nil {
		return Draft{}, err
	}
	if command.ExpectedVersion != draft.Version {
		return Draft{}, &ConflictError{Expected: command.ExpectedVersion, Actual: draft.Version}
	}
	if err := validateIdentity(command.PacketID, draft.InitiativeID, draft.EpicID, command.Target); err != nil {
		return Draft{}, err
	}
	if command.PacketID == draft.ParentID {
		return Draft{}, fmt.Errorf("%w: replacement must have a new packet id", ErrInvalidScope)
	}
	draft.PacketID = command.PacketID
	draft.Target = command.Target
	draft.Body = command.Body
	draft.Version++
	workspace.drafts[draft.ID] = draft
	return draft, nil
}

// Issue validates the complete draft and passes its body once to E01's immutable model.
func (workspace *Workspace) Issue(principal identity.Principal, command IssueCommand) (IssueResult, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()

	draft, err := workspace.ownedDraft(principal, command.DraftID)
	if err != nil {
		return IssueResult{}, err
	}
	if err := requireEditable(draft); err != nil {
		return IssueResult{}, err
	}
	if command.ExpectedVersion != draft.Version {
		return IssueResult{}, &ConflictError{Expected: command.ExpectedVersion, Actual: draft.Version}
	}
	if err := validateComplete(draft); err != nil {
		return IssueResult{}, err
	}
	if err := workspace.scope.ValidateScope(draft.TenantID, draft.InitiativeID, draft.EpicID, draft.Target, workspace.now().UTC()); err != nil {
		return IssueResult{}, err
	}

	actor := packet.Actor(principal.Subject)
	result := IssueResult{}
	if draft.ParentID == "" {
		result.Packet, err = workspace.tracker.Issue(packet.IssueCommand{
			PacketID: packet.PacketID(draft.PacketID), TenantID: packet.TenantID(draft.TenantID),
			Body: draft.Body, Actor: actor,
		})
	} else {
		result.Parent, result.Packet, err = workspace.tracker.Supersede(packet.SupersedeCommand{
			PacketID: packet.PacketID(draft.ParentID), ReplacementID: packet.PacketID(draft.PacketID),
			ExpectedVersion:   currentVersion(workspace.tracker, draft.ParentID),
			ReplacementTenant: packet.TenantID(draft.TenantID), ReplacementBody: draft.Body, Actor: actor,
		})
		result.HasParent = err == nil
	}
	if err != nil {
		return IssueResult{}, err
	}
	draft.State = StateIssued
	draft.Version++
	workspace.drafts[draft.ID] = draft
	result.Draft = draft
	return result, nil
}

// Draft returns an owned draft snapshot for page reloads and tests.
func (workspace *Workspace) Draft(principal identity.Principal, id string) (Draft, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	return workspace.ownedDraft(principal, id)
}

// Packet returns an authored packet only to its tenant.
func (workspace *Workspace) Packet(principal identity.Principal, id string) (packet.Packet, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	projected, err := workspace.tracker.Packet(packet.PacketID(id))
	if err != nil || string(projected.TenantID()) != principal.TenantID {
		return packet.Packet{}, packet.ErrNotFound
	}
	return projected, nil
}

func (workspace *Workspace) Tracker() *packet.Tracker { return workspace.tracker }

func (workspace *Workspace) ownedDraft(principal identity.Principal, id string) (Draft, error) {
	draft, exists := workspace.drafts[id]
	if !exists || draft.AuthorSubject != principal.Subject || draft.TenantID != principal.TenantID {
		return Draft{}, ErrDraftNotFound
	}
	return draft, nil
}

func validatePrincipal(principal identity.Principal) error {
	if strings.TrimSpace(principal.Subject) == "" || strings.TrimSpace(principal.TenantID) == "" {
		return errors.New("authenticated subject and tenant are required")
	}
	return nil
}

func validateIdentity(packetID, initiativeID, epicID, target string) error {
	if strings.TrimSpace(target) == "" || !packetIDPattern.MatchString(packetID) || !packetInScope(packetID, initiativeID, epicID) {
		return fmt.Errorf("%w: packet id, initiative, epic, and target must name one scope", ErrInvalidScope)
	}
	return nil
}

func packetInScope(packetID, initiativeID, epicID string) bool {
	return strings.HasPrefix(packetID, initiativeID+"-"+epicID+"-")
}

func validateComplete(draft Draft) error {
	fields := []string{
		draft.PacketID, draft.InitiativeID, draft.EpicID, draft.Target,
		draft.Body.Goal, draft.Body.Boundary, draft.Body.DoneWhen, draft.Body.Check, draft.Body.Context,
	}
	for _, value := range fields {
		if strings.TrimSpace(value) == "" {
			return ErrIncomplete
		}
	}
	return nil
}

func requireEditable(draft Draft) error {
	if draft.State == StateIssued {
		return ErrDraftIssued
	}
	return nil
}

func currentVersion(tracker *packet.Tracker, id string) packet.Version {
	projected, err := tracker.Packet(packet.PacketID(id))
	if err != nil {
		return 0
	}
	return projected.Version()
}
