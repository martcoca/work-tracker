package surface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/identity"
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
}

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

type Service struct {
	snapshot *Snapshot
	verifier identity.Verifier
	now      func() time.Time
}

func NewService(snapshot *Snapshot, verifier identity.Verifier) (*Service, error) {
	if snapshot == nil || verifier == nil {
		return nil, errors.New("snapshot and identity verifier are required")
	}
	if err := ValidateReadOnly(builtRoutes); err != nil {
		return nil, err
	}
	return &Service{snapshot: snapshot, verifier: verifier, now: time.Now}, nil
}

func (service *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, route := range builtRoutes {
		var handler http.HandlerFunc
		switch route.Name {
		case "health":
			handler = func(response http.ResponseWriter, _ *http.Request) {
				writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
			}
		case "initiatives":
			handler = service.authenticated(func(principal identity.Principal, _ *http.Request) (any, error) {
				return service.snapshot.Initiatives(principal, service.now().UTC())
			})
		case "initiative":
			handler = service.authenticated(func(principal identity.Principal, request *http.Request) (any, error) {
				return service.snapshot.Initiative(principal, request.PathValue("initiative"), service.now().UTC())
			})
		case "epic":
			handler = service.authenticated(func(principal identity.Principal, request *http.Request) (any, error) {
				return service.snapshot.Epic(principal, request.PathValue("initiative"), request.PathValue("epic"), service.now().UTC())
			})
		case "packet":
			handler = service.authenticated(func(principal identity.Principal, request *http.Request) (any, error) {
				return service.snapshot.Packet(principal, request.PathValue("initiative"), request.PathValue("epic"), request.PathValue("packet"), service.now().UTC())
			})
		default:
			panic("unimplemented built route: " + route.Name)
		}
		mux.HandleFunc(route.Method+" "+route.Pattern, handler)
	}
	return securityHeaders(mux)
}

type readOperation func(identity.Principal, *http.Request) (any, error)

func (service *Service) authenticated(operation readOperation) http.HandlerFunc {
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
		writeJSON(response, http.StatusOK, result)
	}
}

func (service *Service) writeOperationError(response http.ResponseWriter, err error) {
	status := service.snapshot.directoryStatus(service.now().UTC())
	switch {
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
	default:
		writeAPIError(response, http.StatusInternalServerError, "read_failed", "The read could not be completed.", status)
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
