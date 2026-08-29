package surface

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHumanComposesEditsIssuesAndSupersedesEntirelyInApp(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)

	created := writeAuthoring(t, service, http.MethodPost, "/api/initiatives/0004/epics/E02/drafts", "human-a", authoringBody("0004-E02-T90", "first"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	draft := decodeBody[draftResponse](t, created).Draft
	for _, label := range []string{"second", "third", "final"} {
		body := authoringBody("0004-E02-T90", label)
		body["expected_version"] = draft.Version
		updated := writeAuthoring(t, service, http.MethodPut, "/api/drafts/"+draft.ID, "human-a", body)
		if updated.Code != http.StatusOK {
			t.Fatalf("edit %s = %d %s", label, updated.Code, updated.Body.String())
		}
		draft = decodeBody[draftResponse](t, updated).Draft
	}
	t.Logf("1. draft %s edited three times to version %d with goal %q", draft.ID, draft.Version, draft.Goal)

	issuedHTTP := writeAuthoring(t, service, http.MethodPost, "/api/drafts/"+draft.ID+"/issue", "human-a", map[string]any{"expected_version": draft.Version})
	if issuedHTTP.Code != http.StatusCreated {
		t.Fatalf("issue = %d %s", issuedHTTP.Code, issuedHTTP.Body.String())
	}
	issued := decodeBody[issuedResponse](t, issuedHTTP)
	if issued.Packet.Goal != "Goal final" || issued.Packet.History[0].Actor != "human-a" || issued.Draft.State != "issued" {
		t.Fatalf("issued = %+v", issued)
	}
	t.Logf("1. issued packet %s goal=%q author=%q state=%q", issued.Packet.ID, issued.Packet.Goal, issued.Packet.History[0].Actor, issued.Draft.State)

	changed := authoringBody("0004-E02-T90", "forbidden")
	changed["expected_version"] = issued.Draft.Version
	editIssued := writeAuthoring(t, service, http.MethodPut, "/api/drafts/"+draft.ID, "human-a", changed)
	if editIssued.Code != http.StatusConflict || !strings.Contains(editIssued.Body.String(), `"code":"draft_issued"`) {
		t.Fatalf("post-issue edit = %d %s", editIssued.Code, editIssued.Body.String())
	}
	issueAgain := writeAuthoring(t, service, http.MethodPost, "/api/drafts/"+draft.ID+"/issue", "human-a", map[string]any{"expected_version": issued.Draft.Version})
	if issueAgain.Code != http.StatusConflict || !strings.Contains(issueAgain.Body.String(), `"code":"draft_issued"`) {
		t.Fatalf("second issue = %d %s", issueAgain.Code, issueAgain.Body.String())
	}
	duplicateHTTP := writeAuthoring(t, service, http.MethodPost, "/api/initiatives/0004/epics/E02/drafts", "human-a", authoringBody("0004-E02-T90", "forbidden duplicate"))
	duplicate := decodeBody[draftResponse](t, duplicateHTTP).Draft
	duplicateIssue := writeAuthoring(t, service, http.MethodPost, "/api/drafts/"+duplicate.ID+"/issue", "human-a", map[string]any{"expected_version": duplicate.Version})
	if duplicateIssue.Code != http.StatusConflict || !strings.Contains(duplicateIssue.Body.String(), `"code":"write_conflict"`) {
		t.Fatalf("duplicate packet issue = %d %s", duplicateIssue.Code, duplicateIssue.Body.String())
	}
	originalAfterAttempts := get(t, service, "/api/authored/packets/0004-E02-T90", "human-a")
	if originalAfterAttempts.Code != http.StatusOK || !strings.Contains(originalAfterAttempts.Body.String(), `"goal":"Goal final"`) || strings.Contains(originalAfterAttempts.Body.String(), "forbidden") {
		t.Fatalf("original after edit attempts = %d %s", originalAfterAttempts.Code, originalAfterAttempts.Body.String())
	}
	t.Logf("2. issued draft edit HTTP %d; repeated issue HTTP %d; duplicate-id issue HTTP %d; original goal remains %q",
		editIssued.Code, issueAgain.Code, duplicateIssue.Code, issued.Packet.Goal)

	replacementHTTP := writeAuthoring(t, service, http.MethodPost,
		"/api/initiatives/0004/epics/E02/packets/0004-E02-T90/supersessions", "human-a",
		authoringBody("0004-E02-T91", "replacement"))
	if replacementHTTP.Code != http.StatusCreated {
		t.Fatalf("create supersession = %d %s", replacementHTTP.Code, replacementHTTP.Body.String())
	}
	replacementDraft := decodeBody[draftResponse](t, replacementHTTP).Draft
	supersededHTTP := writeAuthoring(t, service, http.MethodPost, "/api/drafts/"+replacementDraft.ID+"/issue", "human-a", map[string]any{"expected_version": replacementDraft.Version})
	if supersededHTTP.Code != http.StatusCreated {
		t.Fatalf("issue replacement = %d %s", supersededHTTP.Code, supersededHTTP.Body.String())
	}
	superseded := decodeBody[issuedResponse](t, supersededHTTP)
	if superseded.Parent == nil || superseded.Parent.Goal != "Goal final" || superseded.Parent.SupersededBy == nil || *superseded.Parent.SupersededBy != superseded.Packet.ID {
		t.Fatalf("parent = %+v replacement=%+v", superseded.Parent, superseded.Packet)
	}
	if superseded.Packet.ParentID == nil || *superseded.Packet.ParentID != superseded.Parent.ID || superseded.Parent.Closure == nil || superseded.Parent.Closure.Reason != "superseded" {
		t.Fatalf("supersession links = parent %+v replacement %+v", superseded.Parent, superseded.Packet)
	}
	recovered := get(t, service, "/api/authored/packets/0004-E02-T90", "human-a")
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), `"goal":"Goal final"`) || strings.Contains(recovered.Body.String(), "Goal replacement") {
		t.Fatalf("recovered original = %d %s", recovered.Code, recovered.Body.String())
	}
	t.Logf("3. original=%s unchanged goal=%q replacement=%s parent_link=%s replacement_link=%s closed=%s",
		superseded.Parent.ID, superseded.Parent.Goal, superseded.Packet.ID, *superseded.Packet.ParentID,
		*superseded.Parent.SupersededBy, superseded.Parent.Closure.Reason)
}

func TestIncompleteDraftAndUnknownTenantAreRefusedByHTTP(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	incompleteBody := authoringBody("0004-E02-T92", "incomplete")
	incompleteBody["check"] = ""
	incompleteHTTP := writeAuthoring(t, service, http.MethodPost, "/api/initiatives/0004/epics/E02/drafts", "human-a", incompleteBody)
	incomplete := decodeBody[draftResponse](t, incompleteHTTP).Draft
	refused := writeAuthoring(t, service, http.MethodPost, "/api/drafts/"+incomplete.ID+"/issue", "human-a", map[string]any{"expected_version": incomplete.Version})
	if refused.Code != http.StatusUnprocessableEntity || !strings.Contains(refused.Body.String(), `"code":"draft_incomplete"`) {
		t.Fatalf("incomplete issue = %d %s", refused.Code, refused.Body.String())
	}
	t.Logf("4. no-Check issue refused: HTTP %d %s", refused.Code, strings.TrimSpace(refused.Body.String()))

	unknownHTTP := writeAuthoring(t, service, http.MethodPost, "/api/initiatives/0004/epics/E02/drafts", "human-unknown", authoringBody("0004-E02-T93", "unknown"))
	unknown := decodeBody[draftResponse](t, unknownHTTP).Draft
	unknownIssue := writeAuthoring(t, service, http.MethodPost, "/api/drafts/"+unknown.ID+"/issue", "human-unknown", map[string]any{"expected_version": unknown.Version})
	if unknownIssue.Code != http.StatusForbidden || !strings.Contains(unknownIssue.Body.String(), `"code":"unknown_tenant"`) {
		t.Fatalf("unknown tenant issue = %d %s", unknownIssue.Code, unknownIssue.Body.String())
	}
	t.Logf("5. unknown tenant issue refused: HTTP %d %s", unknownIssue.Code, strings.TrimSpace(unknownIssue.Body.String()))
}

func TestAuthoringRequestsAreStrictAndOwned(t *testing.T) {
	service := testService(t, testSnapshot(t, surfaceClock.Add(-30*time.Minute)), surfaceClock)
	created := writeAuthoring(t, service, http.MethodPost, "/api/initiatives/0004/epics/E02/drafts", "human-a", authoringBody("0004-E02-T94", "owned"))
	draft := decodeBody[draftResponse](t, created).Draft
	other := get(t, service, "/api/drafts/"+draft.ID, "human-retired")
	if other.Code != http.StatusNotFound {
		t.Fatalf("other author read = %d %s", other.Code, other.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/initiatives/0004/epics/E02/drafts", strings.NewReader(`{"packet_id":"0004-E02-T95","target":"work-tracker","goal":"g","boundary":"b","done_when":"d","check":"c","context":"x","extra":true}`))
	request.Header.Set("Authorization", "Bearer human-a")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field = %d %s", response.Code, response.Body.String())
	}
}

func authoringBody(packetID, label string) map[string]any {
	return map[string]any{
		"packet_id": packetID, "target": "work-tracker", "goal": "Goal " + label,
		"boundary": "Boundary " + label, "done_when": "Done " + label,
		"check": "Check " + label, "context": "Context " + label,
	}
}

func writeAuthoring(t *testing.T, service *Service, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	return response
}

func decodeBody[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var decoded T
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response %d %s: %v", response.Code, response.Body.String(), err)
	}
	return decoded
}
