package packetpublisher

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packetexport"
)

func TestHTTPBaselineReturnsOnlyAContractVerifiedPublicExport(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	contents := exportRecords(t, now, contract.Source{
		Repository: "martcoca/work-tracker", Commit: strings.Repeat("a", 40),
	}, []packetexport.Record{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(contents)
	}))
	defer server.Close()

	baseline, err := NewHTTPBaseline(server.URL, server.Client(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := baseline.Verified(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if string(verified) != string(contents) {
		t.Fatal("baseline changed verified export bytes")
	}

	contents = []byte(`{"schema":"tampered"}`)
	if _, err := baseline.Verified(now.Add(time.Hour)); err == nil {
		t.Fatal("invalid repository export was accepted")
	}
}

func TestHTTPBaselineRefusesCredentialAndQueryBearingEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"https://user:secret@example.test/packets.json",
		"https://example.test/packets.json?token=secret",
		"http://example.test/packets.json",
	} {
		if _, err := NewHTTPBaseline(endpoint, nil, time.Second); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
}
