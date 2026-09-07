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
)

func TestSignedInHumanCreatesCredentialOnceAndRetrievesOnlyMetadata(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	issued := createAgentCredential(t, service, "human-a", surfaceClock.Add(45*time.Minute))
	if issued.Credential == "" || issued.Metadata.Principal.Kind != "session" || issued.Metadata.TenantID != "tenant-a" {
		t.Fatalf("issued credential metadata = %#v", issued.Metadata)
	}

	detail := get(t, service, "/api/agent-credentials/"+issued.Metadata.ID, "human-a")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), issued.Credential) || strings.Contains(detail.Body.String(), `"credential":`) {
		t.Fatalf("credential detail exposed one-time value: %d %s", detail.Code, detail.Body.String())
	}
	listed := get(t, service, "/api/agent-credentials", "human-a")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), issued.Credential) || !strings.Contains(listed.Body.String(), issued.Metadata.ID) {
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
		return nil, ErrAgentGrantRequired
	})
	request := httptest.NewRequest(http.MethodPost, "/synthetic-grant-protected-operation", nil)
	request.Header.Set("Authorization", "Bearer "+issued.Credential)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"grant_required"`) {
		t.Fatalf("no-grant response = %d %s", response.Code, response.Body.String())
	}
	if authenticated.TenantID != "tenant-a" || authenticated.Principal != issued.Metadata.Principal {
		t.Fatalf("authenticated identity = %#v", authenticated)
	}
	detail := get(t, service, "/api/agent-credentials/"+issued.Metadata.ID, "human-a")
	metadata := decodeBody[credentialMetadataResponse](t, detail).Metadata
	if metadata.LastUsedAt == nil || !metadata.LastUsedAt.Equal(surfaceClock) {
		t.Fatalf("last used = %v", metadata.LastUsedAt)
	}
	t.Logf("authenticated as packet=%s attempt=%s; empty published-grant decision refused with HTTP %d; last use recorded",
		authenticated.Principal.PacketID, authenticated.Principal.AttemptID, response.Code)
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
	for name, body := range map[string]map[string]any{
		"other tenant packet": {
			"packet_id": "0005-E01-T01", "attempt_id": "attempt-a",
			"expires_at": surfaceClock.Add(time.Hour).Format(time.RFC3339),
		},
		"overlong": {
			"packet_id": "0004-E02-T01", "attempt_id": "attempt-a",
			"expires_at": surfaceClock.Add(time.Hour + time.Second).Format(time.RFC3339),
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := writeJSONRequest(t, service, http.MethodPost, "/api/agent-credentials", "human-a", body)
			if response.Code != http.StatusNotFound && response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func createAgentCredential(t *testing.T, service *Service, human string, expiresAt time.Time) agentcredential.Issued {
	t.Helper()
	response := writeJSONRequest(t, service, http.MethodPost, "/api/agent-credentials", human, map[string]any{
		"packet_id": "0004-E02-T01", "attempt_id": "attempt-e03-t04",
		"expires_at": expiresAt.Format(time.RFC3339Nano),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create credential = %d %s", response.Code, response.Body.String())
	}
	return decodeBody[agentcredential.Issued](t, response)
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
