package surface

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/martcoca/work-tracker/authoring"
	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packet"
	"github.com/martcoca/work-tracker/packetexport"
)

var errInvalidRequest = errors.New("invalid request")

const maximumAuthoringRequest = 64 << 10

type authoringRequest struct {
	PacketID string `json:"packet_id"`
	Target   string `json:"target"`
	Goal     string `json:"goal"`
	Boundary string `json:"boundary"`
	DoneWhen string `json:"done_when"`
	Check    string `json:"check"`
	Context  string `json:"context"`
}

type editDraftRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	PacketID        string `json:"packet_id"`
	Target          string `json:"target"`
	Goal            string `json:"goal"`
	Boundary        string `json:"boundary"`
	DoneWhen        string `json:"done_when"`
	Check           string `json:"check"`
	Context         string `json:"context"`
}

type issueDraftRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
}

type draftView struct {
	ID           string `json:"id"`
	PacketID     string `json:"packet_id"`
	InitiativeID string `json:"initiative_id"`
	EpicID       string `json:"epic_id"`
	Target       string `json:"target"`
	TenantID     string `json:"tenant_id"`
	ParentID     string `json:"parent_id,omitempty"`
	State        string `json:"state"`
	Version      uint64 `json:"version"`
	Goal         string `json:"goal"`
	Boundary     string `json:"boundary"`
	DoneWhen     string `json:"done_when"`
	Check        string `json:"check"`
	Context      string `json:"context"`
}

type draftResponse struct {
	Draft draftView `json:"draft"`
}

type issuedResponse struct {
	Draft  draftView            `json:"draft"`
	Packet packetexport.Record  `json:"packet"`
	Parent *packetexport.Record `json:"parent,omitempty"`
}

func (service *Service) createDraft(principal identity.Principal, request *http.Request) (any, error) {
	var body authoringRequest
	if err := decodeAuthoringRequest(request, &body); err != nil {
		return nil, err
	}
	draft, err := service.authors.Create(principal, authoring.CreateCommand{
		PacketID: body.PacketID, InitiativeID: request.PathValue("initiative"), EpicID: request.PathValue("epic"),
		Target: body.Target, Body: body.packetBody(),
	})
	if err != nil {
		return nil, err
	}
	return draftResponse{Draft: makeDraftView(draft)}, nil
}

func (service *Service) getDraft(principal identity.Principal, request *http.Request) (any, error) {
	draft, err := service.authors.Draft(principal, request.PathValue("draft"))
	if err != nil {
		return nil, err
	}
	return draftResponse{Draft: makeDraftView(draft)}, nil
}

func (service *Service) updateDraft(principal identity.Principal, request *http.Request) (any, error) {
	var body editDraftRequest
	if err := decodeAuthoringRequest(request, &body); err != nil {
		return nil, err
	}
	draft, err := service.authors.Edit(principal, authoring.EditCommand{
		DraftID: request.PathValue("draft"), ExpectedVersion: body.ExpectedVersion,
		PacketID: body.PacketID, Target: body.Target, Body: body.packetBody(),
	})
	if err != nil {
		return nil, err
	}
	return draftResponse{Draft: makeDraftView(draft)}, nil
}

func (service *Service) issueDraft(principal identity.Principal, request *http.Request) (any, error) {
	var body issueDraftRequest
	if err := decodeAuthoringRequest(request, &body); err != nil {
		return nil, err
	}
	result, err := service.authors.Issue(principal, authoring.IssueCommand{
		DraftID: request.PathValue("draft"), ExpectedVersion: body.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	record, err := packetexport.SnapshotRecord(service.authors.Tracker(), result.Packet.ID())
	if err != nil {
		return nil, err
	}
	response := issuedResponse{Draft: makeDraftView(result.Draft), Packet: record}
	if result.HasParent {
		parent, err := packetexport.SnapshotRecord(service.authors.Tracker(), result.Parent.ID())
		if err != nil {
			return nil, err
		}
		response.Parent = &parent
	}
	return response, nil
}

func (service *Service) createSupersessionDraft(principal identity.Principal, request *http.Request) (any, error) {
	var body authoringRequest
	if err := decodeAuthoringRequest(request, &body); err != nil {
		return nil, err
	}
	draft, err := service.authors.CreateSupersession(principal, authoring.SupersessionCommand{
		ParentID: request.PathValue("packet"), PacketID: body.PacketID,
		InitiativeID: request.PathValue("initiative"), EpicID: request.PathValue("epic"),
		Target: body.Target, Body: body.packetBody(),
	})
	if err != nil {
		return nil, err
	}
	return draftResponse{Draft: makeDraftView(draft)}, nil
}

func (service *Service) getAuthoredPacket(principal identity.Principal, request *http.Request) (any, error) {
	now := service.now().UTC()
	status := service.snapshot.directoryStatus(now)
	if status.Stale {
		return PacketView{Directory: status}, ErrDirectoryStale
	}
	if err := service.snapshot.directory.ValidateTenantID(principal.TenantID, now); err != nil {
		return PacketView{Directory: status}, err
	}
	projected, err := service.authors.Packet(principal, request.PathValue("packet"))
	if err != nil {
		return PacketView{Directory: status}, err
	}
	record, err := packetexport.SnapshotRecord(service.authors.Tracker(), projected.ID())
	if err != nil {
		return PacketView{Directory: status}, err
	}
	return PacketView{Directory: status, Packet: record}, nil
}

func (request authoringRequest) packetBody() packet.Body {
	return packet.Body{
		Goal: request.Goal, Boundary: request.Boundary, DoneWhen: request.DoneWhen,
		Check: request.Check, Context: request.Context,
	}
}

func (request editDraftRequest) packetBody() packet.Body {
	return packet.Body{
		Goal: request.Goal, Boundary: request.Boundary, DoneWhen: request.DoneWhen,
		Check: request.Check, Context: request.Context,
	}
}

func makeDraftView(draft authoring.Draft) draftView {
	return draftView{
		ID: draft.ID, PacketID: draft.PacketID, InitiativeID: draft.InitiativeID, EpicID: draft.EpicID,
		Target: draft.Target, TenantID: draft.TenantID, ParentID: draft.ParentID,
		State: string(draft.State), Version: draft.Version,
		Goal: draft.Body.Goal, Boundary: draft.Body.Boundary, DoneWhen: draft.Body.DoneWhen,
		Check: draft.Body.Check, Context: draft.Body.Context,
	}
}

func decodeAuthoringRequest(request *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("%w: content type must be application/json", errInvalidRequest)
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximumAuthoringRequest+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: decode JSON", errInvalidRequest)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: exactly one JSON value is required", errInvalidRequest)
	}
	return nil
}

type snapshotAuthoringScope struct {
	snapshot *Snapshot
	targets  map[string]struct{}
}

func (scope snapshotAuthoringScope) ValidateScope(tenantID, initiativeID, epicID, target string, at time.Time) error {
	if _, available := scope.targets[target]; !available {
		return authoring.ErrInvalidScope
	}
	packets, _, err := scope.snapshot.tenantPackets(identity.Principal{TenantID: tenantID}, at)
	if err != nil {
		return err
	}
	for _, indexed := range packets {
		if indexed.initiative == initiativeID && indexed.epic == epicID {
			return nil
		}
	}
	return authoring.ErrInvalidScope
}

func authoringRouteMethods() []string {
	methods := make([]string, 0, len(builtRoutes))
	for _, route := range builtRoutes {
		methods = append(methods, strings.Join([]string{route.Method, route.Pattern}, " "))
	}
	return methods
}
