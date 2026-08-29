package surface

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/tenant"
)

var surfaceClock = time.Date(2035, time.May, 6, 12, 30, 0, 0, time.UTC)

func TestSyntheticHumanNavigatesOnlyTheirTenant(t *testing.T) {
	snapshot := testSnapshot(t, surfaceClock.Add(-30*time.Minute))
	service := testService(t, snapshot, surfaceClock)

	initiatives := get(t, service, "/api/initiatives", "human-a")
	if initiatives.Code != http.StatusOK || !strings.Contains(initiatives.Body.String(), `"id":"0004"`) {
		t.Fatalf("initiatives response: %d %s", initiatives.Code, initiatives.Body.String())
	}
	t.Logf("1. signed synthetic human initiative list: %s", strings.TrimSpace(initiatives.Body.String()))
	epics := get(t, service, "/api/initiatives/0004", "human-a")
	if epics.Code != http.StatusOK || !strings.Contains(epics.Body.String(), `"id":"E02"`) {
		t.Fatalf("initiative response: %d %s", epics.Code, epics.Body.String())
	}
	packets := get(t, service, "/api/initiatives/0004/epics/E02", "human-a")
	if packets.Code != http.StatusOK || !strings.Contains(packets.Body.String(), `"unclaimed":true`) {
		t.Fatalf("epic response: %d %s", packets.Code, packets.Body.String())
	}
	detail := get(t, service, "/api/initiatives/0004/epics/E02/packets/0004-E02-T01", "human-a")
	for _, expected := range []string{`"goal":"Synthetic readable goal"`, `"status":"not started"`, `"history"`, `"comments"`} {
		if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), expected) {
			t.Fatalf("packet detail missing %s: %d %s", expected, detail.Code, detail.Body.String())
		}
	}
	t.Logf("2. packet body, status, history, and comments: %s", strings.TrimSpace(detail.Body.String()))

	isolation := get(t, service, "/api/initiatives/0005/epics/E01/packets/0005-E01-T01", "human-a")
	if isolation.Code != http.StatusNotFound || strings.Contains(isolation.Body.String(), "Tenant B") {
		t.Fatalf("cross-tenant response = %d %s", isolation.Code, isolation.Body.String())
	}
	t.Logf("3. tenant B packet requested as tenant A: HTTP %d %s", isolation.Code, strings.TrimSpace(isolation.Body.String()))
	if _, err := snapshot.Packet(identity.Principal{TenantID: "tenant-a"}, "0005", "E01", "0005-E01-T01", surfaceClock); !errors.Is(err, ErrTenantIsolation) {
		t.Fatalf("internal isolation error = %v", err)
	}
}

func TestUnknownAndRetiredTenantAreRefusedDifferently(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	unknown := get(t, service, "/api/initiatives", "human-unknown")
	retired := get(t, service, "/api/initiatives", "human-retired")
	if unknown.Code != http.StatusForbidden || !strings.Contains(unknown.Body.String(), `"code":"unknown_tenant"`) {
		t.Fatalf("unknown = %d %s", unknown.Code, unknown.Body.String())
	}
	if retired.Code != http.StatusForbidden || !strings.Contains(retired.Body.String(), `"code":"retired_tenant"`) {
		t.Fatalf("retired = %d %s", retired.Code, retired.Body.String())
	}
}

func TestExpiredHeldDirectoryRendersAgeWithoutTenantData(t *testing.T) {
	published := surfaceClock.Add(-2 * time.Hour)
	service := testService(t, testSnapshot(t, published), surfaceClock)
	response := get(t, service, "/api/initiatives", "human-a")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"code":"directory_stale"`, `"age_seconds":7200`, `"stale":true`, `"expired_by_seconds":3600`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("stale response missing %s: %s", expected, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), "Synthetic readable goal") {
		t.Fatalf("stale response exposed packet data: %s", response.Body.String())
	}
	t.Logf("5. expired held directory: HTTP %d %s", response.Code, strings.TrimSpace(response.Body.String()))
}

func TestBuiltRouteListRejectsEveryMutatingMethod(t *testing.T) {
	routes := BuiltRoutes()
	if err := ValidateReadOnly(routes); err != nil {
		t.Fatal(err)
	}
	methods := make([]string, len(routes))
	for index, route := range routes {
		methods[index] = route.Method + " " + route.Pattern
	}
	t.Logf("built routes: %v", methods)
	mutated := append(routes, Route{Name: "synthetic mutation", Method: http.MethodPost, Pattern: "/api/packets"})
	if err := ValidateReadOnly(mutated); err == nil {
		t.Fatal("synthetic POST route was accepted")
	} else {
		t.Logf("synthetic POST refused: %v", err)
	}
}

func TestBuiltHandlerReturnsMethodNotAllowedForPost(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	request := httptest.NewRequest(http.MethodPost, "/api/initiatives", nil)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", response.Code)
	}
}

func testService(t *testing.T, snapshot *Snapshot, now time.Time) *Service {
	t.Helper()
	principals := map[string]identity.Principal{
		"human-a":       {Subject: "human-a", TenantID: "tenant-a"},
		"human-unknown": {Subject: "human-unknown", TenantID: "tenant-unknown"},
		"human-retired": {Subject: "human-retired", TenantID: "tenant-retired"},
	}
	verifier := verifierFunc(func(_ context.Context, token string) (identity.Principal, error) {
		principal, ok := principals[token]
		if !ok {
			return identity.Principal{}, identity.ErrInvalidToken
		}
		return principal, nil
	})
	service, err := NewService(snapshot, verifier)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	return service
}

func get(t *testing.T, service *Service, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	return response
}

func testSnapshot(t *testing.T, publishedAt time.Time) *Snapshot {
	t.Helper()
	publication := contract.Publication{
		PublishedAt: publishedAt,
		Source:      contract.Source{Repository: "synthetic/source", Commit: strings.Repeat("a", 40)},
	}
	directory := []tenant.Record{
		{ID: "tenant-a", Slug: "tenant-a", DisplayName: "Tenant A", Status: tenant.StatusActive, CreatedAt: "2030-01-01T00:00:00Z", Version: 1},
		{ID: "tenant-b", Slug: "tenant-b", DisplayName: "Tenant B", Status: tenant.StatusActive, CreatedAt: "2030-01-01T00:00:00Z", Version: 1},
		{ID: "tenant-retired", Slug: "retired", DisplayName: "Retired", Status: tenant.StatusRetired, CreatedAt: "2030-01-01T00:00:00Z", Version: 2},
	}
	packetA := testRecord("0004-E02-T01", "tenant-a", "Synthetic readable goal")
	packetB := testRecord("0005-E01-T01", "tenant-b", "Tenant B private goal")
	packetB.Status = "blocked"
	return snapshotFromPayloads(t, publication, []packetexport.Record{packetA, packetB}, directory)
}

func snapshotFromPayloads(t *testing.T, publication contract.Publication, packets []packetexport.Record, tenants []tenant.Record) *Snapshot {
	t.Helper()
	packetEnvelope, err := contract.Build(packetexport.Schema, packets, publication)
	if err != nil {
		t.Fatal(err)
	}
	directoryEnvelope, err := contract.Build(tenant.Schema, tenants, publication)
	if err != nil {
		t.Fatal(err)
	}
	packetBytes, _ := contract.Serialize(packetEnvelope)
	directoryBytes, _ := contract.Serialize(directoryEnvelope)
	snapshot, err := NewSnapshot(packetBytes, directoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func testRecord(id, tenantID, goal string) packetexport.Record {
	body := packetexport.Body{Goal: goal, Boundary: "Synthetic boundary", DoneWhen: "Synthetic check passes", Check: "go test ./...", Context: "Synthetic context"}
	tenantCopy := tenantID
	commentText := "Synthetic comment"
	return packetexport.Record{
		ID: id, TenantID: tenantID, Goal: body.Goal, Boundary: body.Boundary, DoneWhen: body.DoneWhen,
		Check: body.Check, Context: body.Context, Status: "not started", Version: 2,
		Comments: []packetexport.Comment{{
			EventID: "comment-" + id, Timestamp: "2035-05-06T12:10:00Z", Actor: "human-synthetic", Text: commentText,
		}}, Evidence: []string{}, History: []packetexport.HistoryEvent{
			{Kind: "packet issued", EventID: "event-" + id, Timestamp: "2035-05-06T12:00:00Z", Actor: "human-synthetic", TenantID: &tenantCopy, Body: &body},
			{Kind: "packet commented", EventID: "comment-" + id, Timestamp: "2035-05-06T12:10:00Z", Actor: "human-synthetic", Text: &commentText},
		},
	}
}

func TestViewsAreDeterministicallyOrdered(t *testing.T) {
	view, err := testSnapshot(t, surfaceClock.Add(-30*time.Minute)).Initiatives(identity.Principal{TenantID: "tenant-a"}, surfaceClock)
	if err != nil {
		t.Fatal(err)
	}
	want := []InitiativeSummary{{ID: "0004", EpicCount: 1, PacketCount: 1, UnclaimedCount: 1}}
	if !reflect.DeepEqual(view.Initiatives, want) {
		encoded, _ := json.Marshal(view.Initiatives)
		t.Fatalf("initiatives = %s", encoded)
	}
}
