package surface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/martcoca/work-tracker/authoring"
	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packet"
	"github.com/martcoca/work-tracker/tenant"
)

type Route struct {
	Name    string
	Method  string
	Pattern string
}

var builtRoutes = []Route{
	{Name: "health", Method: http.MethodGet, Pattern: "/healthz"},
	{Name: "initiatives", Method: http.MethodGet, Pattern: "/api/initiatives"},
	{Name: "initiative", Method: http.MethodGet, Pattern: "/api/initiatives/{initiative}"},
	{Name: "epic", Method: http.MethodGet, Pattern: "/api/initiatives/{initiative}/epics/{epic}"},
	{Name: "packet", Method: http.MethodGet, Pattern: "/api/initiatives/{initiative}/epics/{epic}/packets/{packet}"},
	{Name: "draft", Method: http.MethodGet, Pattern: "/api/drafts/{draft}"},
	{Name: "authored-packet", Method: http.MethodGet, Pattern: "/api/authored/packets/{packet}"},
	{Name: "draft-create", Method: http.MethodPost, Pattern: "/api/initiatives/{initiative}/epics/{epic}/drafts"},
	{Name: "draft-update", Method: http.MethodPut, Pattern: "/api/drafts/{draft}"},
	{Name: "draft-issue", Method: http.MethodPost, Pattern: "/api/drafts/{draft}/issue"},
	{Name: "supersession-draft-create", Method: http.MethodPost, Pattern: "/api/initiatives/{initiative}/epics/{epic}/packets/{packet}/supersessions"},
}

var allowedMutationRoutes = map[string]string{
	http.MethodPost + " /api/initiatives/{initiative}/epics/{epic}/drafts":                         "draft-create",
	http.MethodPut + " /api/drafts/{draft}":                                                        "draft-update",
	http.MethodPost + " /api/drafts/{draft}/issue":                                                 "draft-issue",
	http.MethodPost + " /api/initiatives/{initiative}/epics/{epic}/packets/{packet}/supersessions": "supersession-draft-create",
}

var ErrSnapshotUnavailable = errors.New("verified export snapshot is unavailable")

func BuiltRoutes() []Route { return append([]Route(nil), builtRoutes...) }

// ValidateReadOnly mechanically rejects any method capable of changing server state.
func ValidateReadOnly(routes []Route) error {
	for _, route := range routes {
		if route.Method != http.MethodGet {
			return fmt.Errorf("route %q uses mutating method %s", route.Name, route.Method)
		}
	}
	return nil
}

// ValidateAuthoringRoutes permits only the four named draft lifecycle mutations. The
// exact allowlist makes adding an issued-packet body edit fail service construction.
func ValidateAuthoringRoutes(routes []Route) error {
	seenNames := make(map[string]struct{}, len(routes))
	seenMutations := make(map[string]struct{}, len(allowedMutationRoutes))
	for _, route := range routes {
		if _, duplicate := seenNames[route.Name]; duplicate {
			return fmt.Errorf("duplicate route name %q", route.Name)
		}
		seenNames[route.Name] = struct{}{}
		if route.Method == http.MethodGet {
			continue
		}
		key := route.Method + " " + route.Pattern
		expectedName, allowed := allowedMutationRoutes[key]
		if !allowed || expectedName != route.Name {
			return fmt.Errorf("route %q is not an allowed draft lifecycle mutation: %s", route.Name, key)
		}
		if _, duplicate := seenMutations[key]; duplicate {
			return fmt.Errorf("duplicate mutation route %s", key)
		}
		seenMutations[key] = struct{}{}
	}
	for key := range allowedMutationRoutes {
		if _, present := seenMutations[key]; !present {
			return fmt.Errorf("required authoring route is missing: %s", key)
		}
	}
	return nil
}

type Service struct {
	snapshots SnapshotSource
	verifier  identity.Verifier
	authors   *authoring.Workspace
	now       func() time.Time
}

// SnapshotSource supplies an already-verified immutable snapshot. Implementations may
// replace the pointer after a background refresh, but CurrentSnapshot must never fetch.
type SnapshotSource interface {
	CurrentSnapshot() *Snapshot
	ExportStatuses(time.Time) []HeldExportStatus
}

type staticSnapshotSource struct{ snapshot *Snapshot }

func (source staticSnapshotSource) CurrentSnapshot() *Snapshot { return source.snapshot }

func (source staticSnapshotSource) ExportStatuses(now time.Time) []HeldExportStatus {
	return source.snapshot.heldStatuses(now)
}

func NewService(snapshot *Snapshot, verifier identity.Verifier) (*Service, error) {
	if snapshot == nil {
		return nil, errors.New("snapshot and identity verifier are required")
	}
	return NewServiceFromSource(staticSnapshotSource{snapshot: snapshot}, verifier)
}

// NewServiceFromSource keeps outbound refresh work outside handlers while allowing every
// request and issue-time tenant check to see the latest verified snapshot.
func NewServiceFromSource(snapshots SnapshotSource, verifier identity.Verifier) (*Service, error) {
	if snapshots == nil || snapshots.CurrentSnapshot() == nil || verifier == nil {
		return nil, errors.New("snapshot and identity verifier are required")
	}
	if err := ValidateAuthoringRoutes(builtRoutes); err != nil {
		return nil, err
	}
	tracker, err := packet.NewTracker(dynamicTenantValidator{snapshots: snapshots})
	if err != nil {
		return nil, err
	}
	authors, err := authoring.NewWorkspace(tracker, snapshotAuthoringScope{
		snapshots: snapshots, targets: map[string]struct{}{"work-tracker": {}},
	})
	if err != nil {
		return nil, err
	}
	return &Service{snapshots: snapshots, verifier: verifier, authors: authors, now: time.Now}, nil
}

func (service *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, route := range builtRoutes {
		var handler http.HandlerFunc
		switch route.Name {
		case "health":
			handler = func(response http.ResponseWriter, _ *http.Request) {
				now := service.now().UTC()
				exports := service.snapshots.ExportStatuses(now)
				statusCode := http.StatusOK
				statusText := "ok"
				if service.snapshots.CurrentSnapshot() == nil || exportsUnavailable(exports) {
					statusCode = http.StatusServiceUnavailable
					statusText = "exports unavailable"
				}
				writeJSON(response, statusCode, struct {
					Status  string             `json:"status"`
					Exports []HeldExportStatus `json:"exports"`
				}{Status: statusText, Exports: exports})
			}
		case "initiatives":
			handler = service.authenticated(http.StatusOK, func(principal identity.Principal, _ *http.Request) (any, error) {
				snapshot, err := service.currentSnapshot()
				if err != nil {
					return nil, err
				}
				return snapshot.Initiatives(principal, service.now().UTC())
			})
		case "initiative":
			handler = service.authenticated(http.StatusOK, func(principal identity.Principal, request *http.Request) (any, error) {
				snapshot, err := service.currentSnapshot()
				if err != nil {
					return nil, err
				}
				return snapshot.Initiative(principal, request.PathValue("initiative"), service.now().UTC())
			})
		case "epic":
			handler = service.authenticated(http.StatusOK, func(principal identity.Principal, request *http.Request) (any, error) {
				snapshot, err := service.currentSnapshot()
				if err != nil {
					return nil, err
				}
				return snapshot.Epic(principal, request.PathValue("initiative"), request.PathValue("epic"), service.now().UTC())
			})
		case "packet":
			handler = service.authenticated(http.StatusOK, func(principal identity.Principal, request *http.Request) (any, error) {
				snapshot, err := service.currentSnapshot()
				if err != nil {
					return nil, err
				}
				return snapshot.Packet(principal, request.PathValue("initiative"), request.PathValue("epic"), request.PathValue("packet"), service.now().UTC())
			})
		case "draft":
			handler = service.authenticated(http.StatusOK, service.getDraft)
		case "authored-packet":
			handler = service.authenticated(http.StatusOK, service.getAuthoredPacket)
		case "draft-create":
			handler = service.authenticated(http.StatusCreated, service.createDraft)
		case "draft-update":
			handler = service.authenticated(http.StatusOK, service.updateDraft)
		case "draft-issue":
			handler = service.authenticated(http.StatusCreated, service.issueDraft)
		case "supersession-draft-create":
			handler = service.authenticated(http.StatusCreated, service.createSupersessionDraft)
		default:
			panic("unimplemented built route: " + route.Name)
		}
		mux.HandleFunc(route.Method+" "+route.Pattern, handler)
	}
	return securityHeaders(mux)
}

func exportsUnavailable(exports []HeldExportStatus) bool {
	if len(exports) == 0 {
		return true
	}
	for _, export := range exports {
		if !export.Available || export.Stale {
			return true
		}
	}
	return false
}

func (service *Service) currentSnapshot() (*Snapshot, error) {
	snapshot := service.snapshots.CurrentSnapshot()
	if snapshot == nil {
		return nil, ErrSnapshotUnavailable
	}
	return snapshot, nil
}

type readOperation func(identity.Principal, *http.Request) (any, error)

func (service *Service) authenticated(successStatus int, operation readOperation) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		token, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			writeAPIError(response, http.StatusUnauthorized, "authentication_required", "Sign in is required.", ExportStatus{})
			return
		}
		principal, err := service.verifier.Verify(request.Context(), token)
		if err != nil {
			writeAPIError(response, http.StatusUnauthorized, "invalid_identity", "The sign-in token was refused.", ExportStatus{})
			return
		}
		result, err := operation(principal, request)
		if err != nil {
			service.writeOperationError(response, err)
			return
		}
		writeJSON(response, successStatus, result)
	}
}

func (service *Service) writeOperationError(response http.ResponseWriter, err error) {
	var status ExportStatus
	if snapshot := service.snapshots.CurrentSnapshot(); snapshot != nil {
		status = snapshot.directoryStatus(service.now().UTC())
	}
	switch {
	case errors.Is(err, ErrSnapshotUnavailable):
		writeAPIError(response, http.StatusServiceUnavailable, "exports_unavailable", "No verified export snapshot is available.", status)
	case errors.Is(err, ErrDirectoryStale):
		writeAPIError(response, http.StatusServiceUnavailable, "directory_stale", "The tenant directory is stale; no tenant data is being shown.", status)
	case errors.Is(err, ErrPacketExportStale), errors.Is(err, contract.ErrStaleExport):
		writeAPIError(response, http.StatusServiceUnavailable, "packets_stale", "The packet export is stale; no packet data is being shown.", status)
	case errors.Is(err, tenant.ErrUnknownTenant):
		writeAPIError(response, http.StatusForbidden, "unknown_tenant", "The signed-in tenant is absent from the directory.", status)
	case errors.Is(err, tenant.ErrRetiredTenant):
		writeAPIError(response, http.StatusForbidden, "retired_tenant", "The signed-in tenant is retired.", status)
	case errors.Is(err, ErrTenantIsolation), errors.Is(err, ErrViewNotFound):
		writeAPIError(response, http.StatusNotFound, "not_found", "That item is not available to this tenant.", status)
	case errors.Is(err, authoring.ErrDraftNotFound), errors.Is(err, packet.ErrNotFound):
		writeAPIError(response, http.StatusNotFound, "not_found", "That authoring item is not available.", status)
	case errors.Is(err, authoring.ErrDraftIssued):
		writeAPIError(response, http.StatusConflict, "draft_issued", "Issued scope is frozen; create a supersession instead.", status)
	case errors.Is(err, authoring.ErrDraftConflict), errors.Is(err, packet.ErrConflict), errors.Is(err, packet.ErrAlreadyExists), errors.Is(err, packet.ErrClosed):
		writeAPIError(response, http.StatusConflict, "write_conflict", "The authoring state changed; reload before continuing.", status)
	case errors.Is(err, authoring.ErrIncomplete):
		writeAPIError(response, http.StatusUnprocessableEntity, "draft_incomplete", "Every packet field is required before issue.", status)
	case errors.Is(err, authoring.ErrInvalidScope):
		writeAPIError(response, http.StatusUnprocessableEntity, "invalid_scope", "Initiative, epic, packet id, or target is not available.", status)
	case errors.Is(err, errInvalidRequest):
		writeAPIError(response, http.StatusBadRequest, "invalid_request", "The request body is invalid.", status)
	default:
		writeAPIError(response, http.StatusInternalServerError, "operation_failed", "The operation could not be completed.", status)
	}
}

func writeAPIError(response http.ResponseWriter, status int, code, message string, directory ExportStatus) {
	writeJSON(response, status, struct {
		Code      string       `json:"code"`
		Message   string       `json:"message"`
		Directory ExportStatus `json:"directory,omitempty"`
	}{Code: code, Message: message, Directory: directory})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}

// StaticVerifier is intentionally absent: synthetic identities are injected only through
// the Verifier interface in tests and cannot be enabled by runtime configuration.
var _ identity.Verifier = verifierFunc(nil)

type verifierFunc func(context.Context, string) (identity.Principal, error)

func (function verifierFunc) Verify(ctx context.Context, token string) (identity.Principal, error) {
	return function(ctx, token)
}
