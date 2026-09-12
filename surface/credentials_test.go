package surface

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/agentcredential"
	"github.com/martcoca/work-tracker/contract"
)

func TestSignedInHumanCreatesCredentialOnceAndRetrievesOnlyMetadata(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	issued := createAgentCredential(t, service, "human-a", surfaceClock.Add(45*time.Minute))
	if issued.Credential == "" || issued.Metadata.Workload.Kind != agentcredential.WorkloadKind ||
		issued.Metadata.TenantID != "tenant-a" {
		t.Fatalf("issued credential metadata = %#v", issued.Metadata)
	}

	detail := get(t, service, "/api/agent-credentials/"+issued.Metadata.ID, "human-a")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), issued.Credential) || strings.Contains(detail.Body.String(), `"credential":`) {
		t.Fatalf("credential detail exposed one-time value: %d %s", detail.Code, detail.Body.String())
	}
	listed := get(t, service, "/api/agent-credentials", "human-a")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), issued.Credential) ||
		!strings.Contains(listed.Body.String(), issued.Metadata.ID) ||
		!strings.Contains(listed.Body.String(), issued.Metadata.Workload.Issuer) ||
		!strings.Contains(listed.Body.String(), issued.Metadata.Workload.Subject) {
		t.Fatalf("credential list = %d %s", listed.Code, listed.Body.String())
	}
	otherTenant := get(t, service, "/api/agent-credentials/"+issued.Metadata.ID, "human-b")
	if otherTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant detail = %d %s", otherTenant.Code, otherTenant.Body.String())
	}

	// A machine bearer cannot enter the human-authenticated creation route.
	bootstrap := writeJSONRequest(t, service, http.MethodPost, "/api/agent-credentials", issued.Credential, map[string]any{
		"packet_id": "0004-E02-T01", "attempt_id": "attempt-bootstrap",
		"expires_at": surfaceClock.Add(45 * time.Minute).Format(time.RFC3339),
		"workload": map[string]any{
			"tenant_id": "tenant-a", "issuer": "https://identity.invalid", "subject": "workload-a",
		},
	})
	if bootstrap.Code != http.StatusUnauthorized || !strings.Contains(bootstrap.Body.String(), `"code":"invalid_identity"`) {
		t.Fatalf("machine bootstrap = %d %s", bootstrap.Code, bootstrap.Body.String())
	}

	packetResponse := get(t, service, "/api/initiatives/0004/epics/E02/packets/0004-E02-T01", "human-a")
	if strings.Contains(packetResponse.Body.String(), issued.Credential) || strings.Contains(packetResponse.Body.String(), issued.Metadata.ID) {
		t.Fatal("credential metadata reached the public packet representation")
	}
	t.Logf("created credential %s; detail and list contain metadata only; machine bootstrap refused", issued.Metadata.ID)
}

func TestMachineAuthenticationCarriesIdentityRecordsUseAndGrantsNothing(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	issued := createAgentCredential(t, service, "human-a", surfaceClock.Add(45*time.Minute))
	var authenticated agentcredential.Identity
	handler := service.agentAuthenticated(http.StatusOK, func(identity agentcredential.Identity, _ *http.Request) (any, error) {
		authenticated = identity
		return nil, emptyPublishedGrantRefusal(t, identity)
	})
	request := httptest.NewRequest(http.MethodPost, "/synthetic-grant-protected-operation", nil)
	request.Header.Set("Authorization", "Bearer "+issued.Credential)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"grant_required"`) {
		t.Fatalf("no-grant response = %d %s", response.Code, response.Body.String())
	}
	if authenticated.TenantID != "tenant-a" || authenticated.Workload != issued.Metadata.Workload ||
		authenticated.Binding != issued.Metadata.Binding {
		t.Fatalf("authenticated identity = %#v", authenticated)
	}
	detail := get(t, service, "/api/agent-credentials/"+issued.Metadata.ID, "human-a")
	metadata := decodeBody[credentialMetadataResponse](t, detail).Metadata
	if metadata.LastUsedAt == nil || !metadata.LastUsedAt.Equal(surfaceClock) {
		t.Fatalf("last used = %v", metadata.LastUsedAt)
	}
	t.Logf("authenticated workload=%s/%s beside packet=%s attempt=%s; verified empty agent-grants export refused with HTTP %d; last use recorded",
		authenticated.Workload.Issuer, authenticated.Workload.Subject,
		authenticated.Binding.PacketID, authenticated.Binding.AttemptID, response.Code)
}

func TestMachineCredentialRefusalsAreDistinctAndRevocationIsImmediate(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	issued := createAgentCredential(t, service, "human-a", surfaceClock.Add(20*time.Minute))
	operation := service.agentAuthenticated(http.StatusOK, func(identity agentcredential.Identity, _ *http.Request) (any, error) {
		return identity, nil
	})

	assertAgentAuthentication(t, operation, issued.Credential, http.StatusOK, "")
	assertAgentAuthentication(t, operation, mutateAgentCredential(issued.Credential), http.StatusUnauthorized, "unknown_credential")
	malformed := httptest.NewRequest(http.MethodGet, "/synthetic", nil)
	malformed.Header.Set("Authorization", issued.Credential)
	malformedResponse := httptest.NewRecorder()
	operation.ServeHTTP(malformedResponse, malformed)
	if malformedResponse.Code != http.StatusUnauthorized || !strings.Contains(malformedResponse.Body.String(), `"code":"malformed_authorization"`) {
		t.Fatalf("malformed = %d %s", malformedResponse.Code, malformedResponse.Body.String())
	}

	revoked := writeJSONRequest(t, service, http.MethodPost, "/api/agent-credentials/"+issued.Metadata.ID+"/revoke", "human-a", nil)
	if revoked.Code != http.StatusOK || !strings.Contains(revoked.Body.String(), `"revoked_by":"human-a"`) {
		t.Fatalf("revoke = %d %s", revoked.Code, revoked.Body.String())
	}
	assertAgentAuthentication(t, operation, issued.Credential, http.StatusUnauthorized, "revoked_credential")

	expiring := createAgentCredential(t, service, "human-a", surfaceClock.Add(time.Minute))
	service.now = func() time.Time { return surfaceClock.Add(time.Minute) }
	assertAgentAuthentication(t, operation, expiring.Credential, http.StatusUnauthorized, "expired_credential")
	t.Log("unknown, malformed, revoked, and expired credentials returned distinct codes; revocation affected the next request")
}

func TestCredentialRequestRequiresOwnedPacketAndAtMostOneHour(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	for name, test := range map[string]struct {
		body map[string]any
		want int
	}{
		"other tenant packet": {body: map[string]any{
			"packet_id": "0005-E01-T01", "attempt_id": "attempt-a",
			"expires_at": surfaceClock.Add(time.Hour).Format(time.RFC3339),
			"workload":   workloadRequestBody("tenant-a", "https://identity.invalid", "workload-a"),
		}, want: http.StatusNotFound},
		"overlong": {body: map[string]any{
			"packet_id": "0004-E02-T01", "attempt_id": "attempt-a",
			"expires_at": surfaceClock.Add(time.Hour + time.Second).Format(time.RFC3339),
			"workload":   workloadRequestBody("tenant-a", "https://identity.invalid", "workload-a"),
		}, want: http.StatusUnprocessableEntity},
	} {
		t.Run(name, func(t *testing.T) {
			response := writeJSONRequest(t, service, http.MethodPost, "/api/agent-credentials", "human-a", test.body)
			if response.Code != test.want {
				t.Fatalf("response = %d %s, want %d", response.Code, response.Body.String(), test.want)
			}
		})
	}
}

func TestCredentialRequestRefusesInvalidAndCrossTenantWorkloadsDistinctly(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	tests := []struct {
		name     string
		workload map[string]any
		status   int
		code     string
	}{
		{name: "workload absent", status: http.StatusUnprocessableEntity, code: "invalid_workload"},
		{name: "issuer malformed", workload: workloadRequestBody("tenant-a", "http://identity.invalid", "workload-a"), status: http.StatusUnprocessableEntity, code: "invalid_workload"},
		{name: "subject absent", workload: workloadRequestBody("tenant-a", "https://identity.invalid", ""), status: http.StatusUnprocessableEntity, code: "invalid_workload"},
		{name: "other tenant", workload: workloadRequestBody("tenant-b", "https://identity.invalid", "workload-b"), status: http.StatusForbidden, code: "workload_tenant_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := map[string]any{
				"packet_id": "0004-E02-T01", "attempt_id": "attempt-e03-t05",
				"expires_at": surfaceClock.Add(45 * time.Minute).Format(time.RFC3339Nano),
			}
			if test.workload != nil {
				body["workload"] = test.workload
			}
			response := writeJSONRequest(t, service, http.MethodPost, "/api/agent-credentials", "human-a", body)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("response = %d %s, want status=%d code=%q", response.Code, response.Body.String(), test.status, test.code)
			}
		})
	}
}

func createAgentCredential(t *testing.T, service *Service, human string, expiresAt time.Time) agentcredential.Issued {
	t.Helper()
	response := writeJSONRequest(t, service, http.MethodPost, "/api/agent-credentials", human, map[string]any{
		"packet_id": "0004-E02-T01", "attempt_id": "attempt-e03-t05",
		"expires_at": expiresAt.Format(time.RFC3339Nano),
		"workload":   workloadRequestBody("tenant-a", "https://identity.invalid", "workload-a"),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create credential = %d %s", response.Code, response.Body.String())
	}
	return decodeBody[agentcredential.Issued](t, response)
}

func workloadRequestBody(tenantID, issuer, subject string) map[string]any {
	return map[string]any{"tenant_id": tenantID, "issuer": issuer, "subject": subject}
}

func writeJSONRequest(t *testing.T, service *Service, method, path, bearer string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	return response
}

func assertAgentAuthentication(t *testing.T, handler http.Handler, credential string, status int, code string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/synthetic", nil)
	request.Header.Set("Authorization", "Bearer "+credential)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != status || code != "" && !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("credential response = %d %s, want status=%d code=%q", response.Code, response.Body.String(), status, code)
	}
}

func mutateAgentCredential(value string) string {
	last := value[len(value)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	return value[:len(value)-1] + string(replacement)
}

func emptyPublishedGrantRefusal(t *testing.T, identity agentcredential.Identity) error {
	t.Helper()
	publication := contract.Publication{
		PublishedAt: surfaceClock.Add(-time.Minute),
		Source: contract.Source{
			Repository: "synthetic/identity-and-tenancy",
			Commit:     strings.Repeat("a", 40),
		},
	}
	const agentGrantsSchema = "martcoca.identity.agent-grants/1"
	envelope, err := contract.Build(agentGrantsSchema, []any{}, publication)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := contract.Serialize(envelope)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := contract.Verify(contents, agentGrantsSchema, surfaceClock)
	if err != nil {
		t.Fatal(err)
	}
	var grants []json.RawMessage
	if err := json.Unmarshal(verified.Payload, &grants); err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 || identity.Workload.Kind != agentcredential.WorkloadKind ||
		identity.Binding.PacketID == "" || identity.Binding.AttemptID == "" {
		t.Fatal("synthetic published export was not the intended no-grant case")
	}
	return ErrAgentGrantRequired
}
