package runtimeexport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/surface"
	"github.com/martcoca/work-tracker/tenant"
)

type fixtureSource struct {
	mu        sync.RWMutex
	documents map[string][]byte
	hang      bool
	started   chan struct{}
	release   chan struct{}
	calls     int
}

type pathGateTransport struct {
	mu          sync.RWMutex
	base        http.RoundTripper
	unavailable map[string]bool
}

func (transport *pathGateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.RLock()
	unavailable := transport.unavailable[request.URL.Path]
	transport.mu.RUnlock()
	if unavailable {
		return nil, errors.New("synthetic endpoint unreachable")
	}
	return transport.base.RoundTrip(request)
}

func (transport *pathGateTransport) setUnavailable(path string, unavailable bool) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.unavailable[path] = unavailable
}

func newFixtureSource(documents map[string][]byte) *fixtureSource {
	return &fixtureSource{
		documents: documents, started: make(chan struct{}, 1), release: make(chan struct{}),
	}
}

func (source *fixtureSource) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	source.mu.Lock()
	source.calls++
	hang := source.hang
	contents := append([]byte(nil), source.documents[request.URL.Path]...)
	source.mu.Unlock()
	if hang {
		select {
		case source.started <- struct{}{}:
		default:
		}
		select {
		case <-source.release:
		case <-request.Context().Done():
			return
		}
	}
	if len(contents) == 0 {
		http.Error(response, "fixture missing", http.StatusNotFound)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write(contents)
}

func (source *fixtureSource) setDocument(path string, contents []byte) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.documents[path] = append([]byte(nil), contents...)
}

func (source *fixtureSource) setHang(value bool) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.hang = value
}

func (source *fixtureSource) callCount() int {
	source.mu.RLock()
	defer source.mu.RUnlock()
	return source.calls
}

func TestStartFetchesVerifiesServesAndSchedulesRefresh(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	documents := fixtureDocuments(t, now.Add(-10*time.Minute), now.Add(-10*time.Minute), now.Add(-10*time.Minute), "Runtime fixture goal")
	fixture := newFixtureSource(documents)
	server := httptest.NewServer(fixture)
	defer server.Close()

	config := fixtureConfig(server.URL)
	config.RefreshInterval = 10 * time.Millisecond
	reader := newFixtureReader(t, config, server.Client(), func() time.Time { return now })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reader.Start(ctx); err != nil {
		t.Fatal(err)
	}
	service := fixtureService(t, reader)

	response := requestInitiatives(service)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"0004"`) {
		t.Fatalf("initial fixture was not served: %d %s", response.Code, response.Body.String())
	}
	status := reader.ExportStatuses(now)
	for _, export := range status {
		if !export.Available || export.Stale || export.AgeSeconds != 600 || export.LastSuccess == "" {
			t.Fatalf("unexpected verified status: %+v", export)
		}
	}
	t.Logf("fetched, verified, and served fixture; statuses: %+v", status)

	updated := fixtureDocuments(t, now.Add(-5*time.Minute), now.Add(-5*time.Minute), now.Add(-5*time.Minute), "Scheduled refresh goal")
	for path, contents := range updated {
		fixture.setDocument(path, contents)
	}
	deadline := time.Now().Add(time.Second)
	for {
		detail := requestPacket(service)
		if detail.Code == http.StatusOK && strings.Contains(detail.Body.String(), "Scheduled refresh goal") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled refresh did not replace the snapshot: %d %s", detail.Code, detail.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fixture.callCount() < 6 {
		t.Fatalf("fixture calls = %d, want startup and scheduled fetches", fixture.callCount())
	}
}

func TestEveryDependencyRefusesTamperingAndRetainsPreviousCopy(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	paths := map[ExportName]string{
		Packets: "/packets.json", TenantDirectory: "/tenant-directory.json", AgentGrants: "/agent-grants.json",
	}
	for _, name := range allExports {
		t.Run(string(name), func(t *testing.T) {
			documents := fixtureDocuments(t, now.Add(-10*time.Minute), now.Add(-10*time.Minute), now.Add(-10*time.Minute), "Last good goal")
			fixture := newFixtureSource(documents)
			server := httptest.NewServer(fixture)
			defer server.Close()
			reader := newFixtureReader(t, fixtureConfig(server.URL), server.Client(), func() time.Time { return now })
			if err := reader.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}

			path := paths[name]
			fixture.setDocument(path, tamperPayload(t, documents[path]))
			err := reader.Refresh(context.Background())
			if !errors.Is(err, contract.ErrDigestMismatch) {
				t.Fatalf("tampered %s error = %v", name, err)
			}
			for _, status := range reader.ExportStatuses(now) {
				if status.Name == string(name) && (!status.Available || !strings.Contains(status.RefreshError, "digest")) {
					t.Fatalf("tampered status = %+v", status)
				}
			}
			if detail := requestPacket(fixtureService(t, reader)); detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "Last good goal") {
				t.Fatalf("last good snapshot was not retained: %d %s", detail.Code, detail.Body.String())
			}
			t.Logf("%s tamper refused: %v", name, err)
		})
	}
}

func TestFailedFetchRetainsCopyAndMakesItsAgeVisible(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newFixtureSource(fixtureDocuments(t, now.Add(-10*time.Minute), now.Add(-10*time.Minute), now.Add(-10*time.Minute), "Available offline"))
	server := httptest.NewServer(fixture)
	reader := newFixtureReader(t, fixtureConfig(server.URL), server.Client(), func() time.Time { return now })
	if err := reader.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.Close()
	now = now.Add(20 * time.Minute)
	refreshErr := reader.Refresh(context.Background())
	if refreshErr == nil {
		t.Fatal("unreachable fixture refresh succeeded")
	}

	response := requestPacket(fixtureService(t, reader))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Available offline") {
		t.Fatalf("held copy not served after source failure: %d %s", response.Code, response.Body.String())
	}
	for _, status := range reader.ExportStatuses(now) {
		if status.AgeSeconds != 1800 || status.RefreshError != "fetch request failed" {
			t.Fatalf("source failure status = %+v", status)
		}
	}
	t.Logf("source killed (%v); held copies still served at age_seconds=1800", refreshErr)
}

func TestHeldCopiesFailClosedAtOriginalExpiry(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name      ExportName
		published map[ExportName]time.Time
		want      error
	}{
		{name: Packets, published: map[ExportName]time.Time{Packets: base.Add(-contract.FreshnessBound + time.Minute), TenantDirectory: base.Add(-5 * time.Minute), AgentGrants: base.Add(-5 * time.Minute)}, want: surface.ErrPacketExportStale},
		{name: TenantDirectory, published: map[ExportName]time.Time{Packets: base.Add(-5 * time.Minute), TenantDirectory: base.Add(-contract.FreshnessBound + time.Minute), AgentGrants: base.Add(-5 * time.Minute)}, want: surface.ErrDirectoryStale},
		{name: AgentGrants, published: map[ExportName]time.Time{Packets: base.Add(-5 * time.Minute), TenantDirectory: base.Add(-5 * time.Minute), AgentGrants: base.Add(-contract.FreshnessBound + time.Minute)}, want: contract.ErrStaleExport},
	}
	for _, test := range tests {
		t.Run(string(test.name), func(t *testing.T) {
			now := base
			fixture := newFixtureSource(fixtureDocuments(t, test.published[Packets], test.published[TenantDirectory], test.published[AgentGrants], "Expires without extension"))
			server := httptest.NewServer(fixture)
			defer server.Close()
			reader := newFixtureReader(t, fixtureConfig(server.URL), server.Client(), func() time.Time { return now })
			if err := reader.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			now = now.Add(2 * time.Minute)
			if test.name == AgentGrants {
				if _, err := reader.VerifiedCopy(test.name, now); !errors.Is(err, test.want) || !strings.Contains(err.Error(), string(test.name)) {
					t.Fatalf("agent grant expiry = %v", err)
				}
			} else {
				_, err := reader.CurrentSnapshot().Initiatives(identity.Principal{TenantID: "tenant-a"}, now)
				if !errors.Is(err, test.want) {
					t.Fatalf("%s render error = %v", test.name, err)
				}
			}
			status := statusFor(t, reader.ExportStatuses(now), test.name)
			if !status.Stale || status.ExpiredBy != 60 {
				t.Fatalf("expired status = %+v", status)
			}
			t.Logf("%s failed closed at expires_at: %+v", test.name, status)
		})
	}
}

func TestStartWithoutReachablePacketServesEmptyThenRefreshesWithoutRestart(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	documents := fixtureDocuments(t, now.Add(-5*time.Minute), now.Add(-5*time.Minute), now.Add(-5*time.Minute), "Appeared after startup")
	packetDocument := documents["/packets.json"]
	delete(documents, "/packets.json")
	fixture := newFixtureSource(documents)
	server := httptest.NewServer(fixture)
	defer server.Close()
	gate := &pathGateTransport{base: server.Client().Transport, unavailable: map[string]bool{"/packets.json": true}}
	reader := newFixtureReader(t, fixtureConfig(server.URL), &http.Client{Transport: gate}, func() time.Time { return now })
	if err := reader.Refresh(context.Background()); err != nil {
		t.Fatalf("expected packet absence was reported as a refresh error: %v", err)
	}
	if err := reader.Ready(now); err != nil {
		t.Fatalf("empty tracker was not ready after verified authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reader.Start(ctx); err != nil {
		t.Fatalf("empty tracker start = %v", err)
	}
	service := fixtureService(t, reader)
	initiatives := requestInitiatives(service)
	if initiatives.Code != http.StatusOK || !strings.Contains(initiatives.Body.String(), `"initiatives":[]`) {
		t.Fatalf("empty tracker response = %d %s", initiatives.Code, initiatives.Body.String())
	}
	health := requestHealth(service)
	packetStatus := statusFor(t, reader.ExportStatuses(now), Packets)
	if health.Code != http.StatusOK || packetStatus.Available || packetStatus.Required || !packetStatus.ServiceOwned || !packetStatus.Absent || packetStatus.RefreshError != "" {
		t.Fatalf("empty tracker status = health %d %s; packet %+v", health.Code, health.Body.String(), packetStatus)
	}
	for _, authority := range []ExportName{TenantDirectory, AgentGrants} {
		status := statusFor(t, reader.ExportStatuses(now), authority)
		if !status.Available || !status.Required || status.ServiceOwned || status.Absent {
			t.Fatalf("authority status %s = %+v", authority, status)
		}
	}
	t.Logf("started with unreachable packet export and valid authority: %s", strings.TrimSpace(initiatives.Body.String()))

	fixture.setDocument("/packets.json", packetDocument)
	gate.setUnavailable("/packets.json", false)
	if err := reader.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh after packet publication = %v", err)
	}
	refreshed := requestInitiatives(service)
	if refreshed.Code != http.StatusOK || !strings.Contains(refreshed.Body.String(), `"id":"0004"`) {
		t.Fatalf("running service did not pick up packet = %d %s", refreshed.Code, refreshed.Body.String())
	}
	t.Logf("same service picked up packet on refresh: %s", strings.TrimSpace(refreshed.Body.String()))
}

func TestPresentInvalidPacketRefusesStartup(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name    string
		packet  func(*testing.T, []byte) []byte
		wantErr error
	}{
		{name: "malformed", packet: func(_ *testing.T, _ []byte) []byte { return []byte("{") }, wantErr: contract.ErrInvalidExport},
		{name: "wrong digest", packet: tamperPayload, wantErr: contract.ErrDigestMismatch},
		{name: "expired", packet: func(t *testing.T, _ []byte) []byte {
			return fixtureDocuments(t, now.Add(-2*contract.FreshnessBound), now.Add(-5*time.Minute), now.Add(-5*time.Minute), "Expired packet")["/packets.json"]
		}, wantErr: contract.ErrStaleExport},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			documents := fixtureDocuments(t, now.Add(-5*time.Minute), now.Add(-5*time.Minute), now.Add(-5*time.Minute), "Invalid startup")
			documents["/packets.json"] = test.packet(t, documents["/packets.json"])
			fixture := newFixtureSource(documents)
			server := httptest.NewServer(fixture)
			defer server.Close()
			reader := newFixtureReader(t, fixtureConfig(server.URL), server.Client(), func() time.Time { return now })
			err := reader.Start(context.Background())
			if !errors.Is(err, test.wantErr) || !strings.Contains(err.Error(), "packets") {
				t.Fatalf("%s packet startup error = %v", test.name, err)
			}
			if reader.CurrentSnapshot() != nil {
				t.Fatalf("%s packet produced a snapshot", test.name)
			}
			if status := statusFor(t, reader.ExportStatuses(now), Packets); status.Absent {
				t.Fatalf("%s packet was reported absent: %+v", test.name, status)
			}
			t.Logf("present %s packet refused: %v", test.name, err)
		})
	}
}

func TestAuthorityExportsRemainStrictAtStartup(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	paths := map[ExportName]string{TenantDirectory: "/tenant-directory.json", AgentGrants: "/agent-grants.json"}
	for _, authority := range []ExportName{TenantDirectory, AgentGrants} {
		for _, mode := range []string{"missing", "stale", "tampered", "unreachable"} {
			t.Run(string(authority)+"/"+mode, func(t *testing.T) {
				documents := fixtureDocuments(t, now.Add(-5*time.Minute), now.Add(-5*time.Minute), now.Add(-5*time.Minute), "Authority gate")
				path := paths[authority]
				switch mode {
				case "missing":
					delete(documents, path)
				case "stale":
					stale := fixtureDocuments(t, now.Add(-5*time.Minute), now.Add(-2*contract.FreshnessBound), now.Add(-2*contract.FreshnessBound), "Authority gate")
					documents[path] = stale[path]
				case "tampered":
					documents[path] = tamperPayload(t, documents[path])
				}
				fixture := newFixtureSource(documents)
				server := httptest.NewServer(fixture)
				defer server.Close()
				gate := &pathGateTransport{base: server.Client().Transport, unavailable: map[string]bool{path: mode == "unreachable"}}
				reader := newFixtureReader(t, fixtureConfig(server.URL), &http.Client{Transport: gate}, func() time.Time { return now })
				err := reader.Start(context.Background())
				if err == nil || !strings.Contains(err.Error(), string(authority)) {
					t.Fatalf("%s %s startup error = %v", authority, mode, err)
				}
				switch mode {
				case "stale":
					if !strings.Contains(err.Error(), contract.ErrStaleExport.Error()) || !strings.Contains(err.Error(), "expired") {
						t.Fatalf("stale authority error = %v", err)
					}
				case "tampered":
					if !strings.Contains(err.Error(), "digest mismatch") {
						t.Fatalf("tampered authority error = %v", err)
					}
				default:
					if !errors.Is(err, ErrNoUsableExport) {
						t.Fatalf("unavailable authority error = %v", err)
					}
				}
				t.Logf("%s %s refused: %v", authority, mode, err)
			})
		}
	}
}

func TestExportOwnershipAndStartupPolicyAreExplicit(t *testing.T) {
	reader := &Reader{config: DefaultConfig()}
	want := map[ExportName]struct {
		serviceOwned bool
		required     bool
	}{
		Packets:         {serviceOwned: true, required: false},
		TenantDirectory: {serviceOwned: false, required: true},
		AgentGrants:     {serviceOwned: false, required: true},
	}
	for _, configured := range reader.sources() {
		expected := want[configured.name]
		if configured.serviceOwned != expected.serviceOwned || configured.requiredAtStartup != expected.required {
			t.Fatalf("%s policy = owned:%t required:%t", configured.name, configured.serviceOwned, configured.requiredAtStartup)
		}
		delete(want, configured.name)
	}
	if len(want) != 0 {
		t.Fatalf("sources omitted from policy: %+v", want)
	}
}

func TestPageDoesNotWaitForHangingRefresh(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newFixtureSource(fixtureDocuments(t, now.Add(-10*time.Minute), now.Add(-10*time.Minute), now.Add(-10*time.Minute), "Nonblocking page"))
	server := httptest.NewServer(fixture)
	defer server.Close()
	config := fixtureConfig(server.URL)
	config.FetchTimeout = 2 * time.Second
	reader := newFixtureReader(t, config, server.Client(), func() time.Time { return now })
	if err := reader.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.setHang(true)
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- reader.Refresh(context.Background()) }()
	select {
	case <-fixture.started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach hanging fixture")
	}

	started := time.Now()
	response := requestPacket(fixtureService(t, reader))
	elapsed := time.Since(started)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Nonblocking page") {
		t.Fatalf("page during refresh = %d %s", response.Code, response.Body.String())
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("page waited %s for hanging fetch", elapsed)
	}
	close(fixture.release)
	if err := <-refreshDone; err != nil {
		t.Fatalf("released refresh failed: %v", err)
	}
	t.Logf("page rendered in %s while all three fetches were hanging", elapsed)
}

func TestConfigUsesSaneDefaultsAndRejectsCredentialShapedEndpoints(t *testing.T) {
	for _, name := range []string{"PACKET_EXPORT_URL", "TENANT_DIRECTORY_URL", "AGENT_GRANTS_URL", "EXPORT_REFRESH_INTERVAL", "EXPORT_FETCH_TIMEOUT"} {
		t.Setenv(name, "")
	}
	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config != DefaultConfig() {
		t.Fatalf("defaults = %+v", config)
	}
	t.Setenv("EXPORT_REFRESH_INTERVAL", "45s")
	t.Setenv("EXPORT_FETCH_TIMEOUT", "3s")
	t.Setenv("PACKET_EXPORT_URL", "https://static.example.test/packets.json")
	configured, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if configured.RefreshInterval != 45*time.Second || configured.FetchTimeout != 3*time.Second || configured.PacketURL != "https://static.example.test/packets.json" {
		t.Fatalf("environment overrides = %+v", configured)
	}
	bad := DefaultConfig()
	bad.PacketURL = "https://static.example.test/packets.json?token=not-allowed"
	if _, err := New(bad, nil, nil); err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("credential-shaped endpoint error = %v", err)
	}
	bad = DefaultConfig()
	bad.PacketURL = "http://static.example.test/packets.json"
	if _, err := New(bad, nil, nil); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("plain HTTP endpoint error = %v", err)
	}
}

func fixtureConfig(baseURL string) Config {
	return Config{
		PacketURL: baseURL + "/packets.json", TenantDirectoryURL: baseURL + "/tenant-directory.json",
		AgentGrantsURL: baseURL + "/agent-grants.json", RefreshInterval: time.Hour, FetchTimeout: time.Second,
	}
}

func newFixtureReader(t *testing.T, config Config, client *http.Client, now func() time.Time) *Reader {
	t.Helper()
	reader, err := New(config, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader.now = now
	return reader
}

func fixtureDocuments(t *testing.T, packetAt, directoryAt, grantsAt time.Time, goal string) map[string][]byte {
	t.Helper()
	body := packetexport.Body{Goal: goal, Boundary: "Fixture boundary", DoneWhen: "Fixture done", Check: "go test ./...", Context: "Fixture context"}
	tenantID := "tenant-a"
	record := packetexport.Record{
		ID: "0004-E02-T04", TenantID: tenantID, Goal: body.Goal, Boundary: body.Boundary,
		DoneWhen: body.DoneWhen, Check: body.Check, Context: body.Context, Status: "not started", Version: 1,
		Comments: []packetexport.Comment{}, Evidence: []string{}, History: []packetexport.HistoryEvent{{
			Kind: "packet issued", EventID: "event-fixture", Timestamp: packetAt.Format(time.RFC3339),
			Actor: "human-fixture", TenantID: &tenantID, Body: &body,
		}},
	}
	directory := []tenant.Record{{
		ID: tenantID, Slug: "tenant-a", DisplayName: "Tenant A", Status: tenant.StatusActive,
		CreatedAt: "2025-01-01T00:00:00Z", Version: 1,
	}}
	grants := []map[string]any{{"subject": "agent-fixture", "tenant_id": tenantID, "version": 1}}
	return map[string][]byte{
		"/packets.json":          serializedEnvelope(t, packetexport.Schema, []packetexport.Record{record}, packetAt),
		"/tenant-directory.json": serializedEnvelope(t, tenant.Schema, directory, directoryAt),
		"/agent-grants.json":     serializedEnvelope(t, AgentGrantsSchema, grants, grantsAt),
	}
}

func serializedEnvelope(t *testing.T, schema string, payload any, publishedAt time.Time) []byte {
	t.Helper()
	envelope, err := contract.Build(schema, payload, contract.Publication{
		PublishedAt: publishedAt, Source: contract.Source{Repository: "synthetic/source", Commit: strings.Repeat("a", 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := contract.Serialize(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func tamperPayload(t *testing.T, contents []byte) []byte {
	t.Helper()
	marker := []byte(`"payload":`)
	index := bytes.Index(contents, marker)
	if index < 0 {
		t.Fatal("fixture envelope has no payload")
	}
	tampered := append([]byte(nil), contents...)
	for offset := index + len(marker); offset < len(tampered); offset++ {
		if tampered[offset] == 'a' {
			tampered[offset] = 'b'
			return tampered
		}
	}
	t.Fatal("fixture payload has no mutable byte")
	return nil
}

type fixtureVerifier struct{}

func (fixtureVerifier) Verify(_ context.Context, token string) (identity.Principal, error) {
	if token != "human-a" {
		return identity.Principal{}, identity.ErrInvalidToken
	}
	return identity.Principal{Subject: "human-a", TenantID: "tenant-a"}, nil
}

func fixtureService(t *testing.T, reader *Reader) *surface.Service {
	t.Helper()
	service, err := surface.NewServiceFromSource(reader, fixtureVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func requestInitiatives(service *surface.Service) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/initiatives", nil)
	request.Header.Set("Authorization", "Bearer human-a")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	return response
}

func requestPacket(service *surface.Service) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/initiatives/0004/epics/E02/packets/0004-E02-T04", nil)
	request.Header.Set("Authorization", "Bearer human-a")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	return response
}

func requestHealth(service *surface.Service) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	return response
}

func statusFor(t *testing.T, statuses []surface.HeldExportStatus, name ExportName) surface.HeldExportStatus {
	t.Helper()
	for _, status := range statuses {
		if status.Name == string(name) {
			return status
		}
	}
	t.Fatalf("status %s not found in %+v", name, statuses)
	return surface.HeldExportStatus{}
}

func ExampleReader() {
	config := DefaultConfig()
	fmt.Println(config.RefreshInterval, config.FetchTimeout)
	// Output: 5m0s 5s
}

// spaHost answers every path it does not hold with its index document and HTTP 200, the
// way Firebase Hosting does behind a `"**" -> /index.html` rewrite. This is not a
// hypothetical: the first deployment served on its real domain refused to start with
//
//	startup refused: packets: invalid export: decode envelope:
//	invalid character '<' looking for beginning of value
//
// because its own packets export is published by nothing yet, and the host answered the
// absence with a web page instead of a 404.
type spaHost struct{ documents map[string][]byte }

func (host spaHost) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	contents, ok := host.documents[request.URL.Path]
	if !ok {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("<!doctype html>\n<html><head><title>Work Tracker</title></head></html>"))
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write(contents)
}

func TestSinglePageHostAnswersAbsenceWithMarkup(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("a service-owned export may be answered with markup", func(t *testing.T) {
		documents := fixtureDocuments(t, now.Add(-5*time.Minute), now.Add(-5*time.Minute), now.Add(-5*time.Minute), "Markup absence")
		delete(documents, "/packets.json")
		server := httptest.NewServer(spaHost{documents: documents})
		defer server.Close()
		reader := newFixtureReader(t, fixtureConfig(server.URL), server.Client(), func() time.Time { return now })
		if err := reader.Start(context.Background()); err != nil {
			t.Fatalf("startup refused an absent service-owned export: %v", err)
		}
	})

	// The same leniency must never reach authority. A host that answers with markup where a
	// tenant directory belongs is telling us nothing about who is authorised.
	for _, authority := range []string{"/tenant-directory.json", "/agent-grants.json"} {
		t.Run("authority may not be: "+authority, func(t *testing.T) {
			documents := fixtureDocuments(t, now.Add(-5*time.Minute), now.Add(-5*time.Minute), now.Add(-5*time.Minute), "Markup absence")
			delete(documents, authority)
			server := httptest.NewServer(spaHost{documents: documents})
			defer server.Close()
			reader := newFixtureReader(t, fixtureConfig(server.URL), server.Client(), func() time.Time { return now })
			if err := reader.Start(context.Background()); err == nil {
				t.Fatalf("startup accepted markup in place of %s", authority)
			}
		})
	}
}
