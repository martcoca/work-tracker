package packetpublisher

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestHostingDestinationClonesLiveVersionBeforeReplacingPacketExport(t *testing.T) {
	var mu sync.Mutex
	requests := make([]string, 0)
	var uploaded []byte
	var wantedHash string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1beta1/projects/-/sites/tracker-site/channels/live":
			writeFixtureJSON(response, map[string]any{"release": map[string]any{"version": map[string]string{"name": "sites/tracker-site/versions/live-old"}}})
		case request.Method == http.MethodPost && request.URL.Path == "/v1beta1/projects/-/sites/tracker-site/versions:clone":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["sourceVersion"] != "sites/tracker-site/versions/live-old" || body["finalize"] != false {
				t.Errorf("clone body = %#v", body)
			}
			writeFixtureJSON(response, map[string]any{"name": "projects/synthetic/operations/clone-1"})
		case request.Method == http.MethodGet && request.URL.Path == "/v1beta1/projects/synthetic/operations/clone-1":
			writeFixtureJSON(response, map[string]any{
				"name": "projects/synthetic/operations/clone-1", "done": true,
				"response": map[string]string{"name": "sites/tracker-site/versions/app-new"},
			})
		case request.Method == http.MethodPost && request.URL.Path == "/v1beta1/sites/tracker-site/versions/app-new:populateFiles":
			var body struct {
				Files map[string]string `json:"files"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)
			wantedHash = body.Files["/packets.json"]
			writeFixtureJSON(response, map[string]any{
				"uploadRequiredHashes": []string{wantedHash},
				"uploadUrl":            serverURL(request) + "/upload/sites/tracker-site/versions/app-new/files",
			})
		case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/upload/sites/tracker-site/versions/app-new/files/"):
			if !strings.HasSuffix(request.URL.Path, "/"+wantedHash) {
				t.Errorf("upload path = %s", request.URL.Path)
			}
			compressed, _ := io.ReadAll(request.Body)
			reader, err := gzip.NewReader(strings.NewReader(string(compressed)))
			if err != nil {
				t.Errorf("uploaded content is not gzip: %v", err)
			} else {
				uploaded, _ = io.ReadAll(reader)
				_ = reader.Close()
			}
			writeFixtureJSON(response, map[string]any{})
		case request.Method == http.MethodPatch && request.URL.Path == "/v1beta1/sites/tracker-site/versions/app-new":
			if request.URL.Query().Get("updateMask") != "status" {
				t.Errorf("update mask = %q", request.URL.Query().Get("updateMask"))
			}
			writeFixtureJSON(response, map[string]string{"status": "FINALIZED"})
		case request.Method == http.MethodPost && request.URL.Path == "/v1beta1/projects/-/sites/tracker-site/channels/live/releases":
			if request.URL.Query().Get("versionName") != "sites/tracker-site/versions/app-new" {
				t.Errorf("release version = %q", request.URL.Query().Get("versionName"))
			}
			writeFixtureJSON(response, map[string]any{"version": map[string]string{"name": "sites/tracker-site/versions/app-new"}})
		default:
			http.Error(response, "unexpected fixture request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	destination, err := NewHostingDestination("tracker-site", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	destination.baseURL = server.URL
	destination.pollDelay = 0
	contents := []byte(`{"schema":"synthetic"}`)
	if err := destination.Publish(context.Background(), contents, "app publication"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !reflect.DeepEqual(uploaded, contents) {
		t.Fatalf("uploaded = %q", uploaded)
	}
	want := []string{
		"GET /v1beta1/projects/-/sites/tracker-site/channels/live",
		"POST /v1beta1/projects/-/sites/tracker-site/versions:clone",
		"GET /v1beta1/projects/synthetic/operations/clone-1",
		"POST /v1beta1/sites/tracker-site/versions/app-new:populateFiles",
		"POST /upload/sites/tracker-site/versions/app-new/files/" + wantedHash,
		"PATCH /v1beta1/sites/tracker-site/versions/app-new?updateMask=status",
		"POST /v1beta1/projects/-/sites/tracker-site/channels/live/releases?versionName=" + url.QueryEscape("sites/tracker-site/versions/app-new"),
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests:\n got %v\nwant %v", requests, want)
	}
}

func TestHostingDestinationDoesNotReleaseAfterUploadFailure(t *testing.T) {
	released := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/v1beta1/projects/-/sites/tracker-site/channels/live":
			writeFixtureJSON(response, map[string]any{"release": map[string]any{"version": map[string]string{"name": "sites/tracker-site/versions/live-old"}}})
		case request.URL.Path == "/v1beta1/projects/-/sites/tracker-site/versions:clone":
			writeFixtureJSON(response, map[string]any{"name": "projects/synthetic/operations/clone-1", "done": true, "response": map[string]string{"name": "sites/tracker-site/versions/app-new"}})
		case strings.HasSuffix(request.URL.Path, ":populateFiles"):
			var body struct {
				Files map[string]string `json:"files"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)
			hash := body.Files["/packets.json"]
			writeFixtureJSON(response, map[string]any{"uploadRequiredHashes": []string{hash}, "uploadUrl": serverURL(request) + "/upload"})
		case strings.HasPrefix(request.URL.Path, "/upload/"):
			http.Error(response, "synthetic", http.StatusServiceUnavailable)
		case strings.HasSuffix(request.URL.Path, "/releases"):
			released = true
			writeFixtureJSON(response, map[string]any{})
		default:
			http.Error(response, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()
	destination, _ := NewHostingDestination("tracker-site", server.Client())
	destination.baseURL = server.URL
	destination.pollDelay = 0
	if err := destination.Publish(context.Background(), []byte("export"), "message"); err == nil {
		t.Fatal("upload failure was accepted")
	}
	if released {
		t.Fatal("failed upload created a live release")
	}
}

func serverURL(request *http.Request) string {
	return fmt.Sprintf("http://%s", request.Host)
}

func writeFixtureJSON(response http.ResponseWriter, value any) {
	_ = json.NewEncoder(response).Encode(value)
}
