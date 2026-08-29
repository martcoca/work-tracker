package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testSchema = "martcoca.synthetic.records/1"

var testPublication = Publication{
	PublishedAt: time.Date(2030, time.February, 1, 12, 0, 0, 0, time.UTC),
	Source: Source{
		Repository: "synthetic/repository",
		Commit:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	},
}

func TestSharedEnvelopeAndExactFreshnessBound(t *testing.T) {
	envelope, err := Build(testSchema, []map[string]any{{"id": "record-a"}}, testPublication)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	serialized, err := Serialize(envelope)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(serialized, &fields); err != nil {
		t.Fatalf("decode fields: %v", err)
	}
	gotKeys := make([]string, 0, len(fields))
	for key := range fields {
		gotKeys = append(gotKeys, key)
	}
	// Serialization is canonical, so the byte-level key order is asserted separately.
	wantKeys := []string{"digest", "expires_at", "payload", "published_at", "schema", "source"}
	if gotCanonical := string(serialized); !strings.HasPrefix(gotCanonical, `{"digest":`) {
		t.Fatalf("envelope is not canonical: %s", gotCanonical)
	}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("envelope keys = %v", gotKeys)
	}
	for _, key := range wantKeys {
		if _, ok := fields[key]; !ok {
			t.Errorf("envelope missing %q", key)
		}
	}

	publishedAt, err := time.Parse(time.RFC3339, envelope.PublishedAt)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt, err := time.Parse(time.RFC3339, envelope.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if got := expiresAt.Sub(publishedAt); got != time.Hour {
		t.Fatalf("expires_at - published_at = %s, want %s", got, time.Hour)
	}
	if envelope.Digest != DigestCanonical(envelope.Payload) {
		t.Fatalf("digest %q does not cover canonical payload %s", envelope.Digest, envelope.Payload)
	}
}

func TestCanonicalJSONAndDigestIgnoreObjectInsertionOrder(t *testing.T) {
	forward := map[string]any{
		"z": map[string]any{"second": 2, "first": 1},
		"a": []any{"value", true},
	}
	reverse := map[string]any{}
	reverse["a"] = []any{"value", true}
	reverse["z"] = map[string]any{"first": 1, "second": 2}

	first, err := CanonicalJSON(forward)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalJSON(reverse)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":["value",true],"z":{"first":1,"second":2}}`
	if string(first) != want || string(second) != want {
		t.Fatalf("canonical JSON differs\nfirst:  %s\nsecond: %s\nwant:   %s", first, second, want)
	}
	if DigestCanonical(first) != DigestCanonical(second) {
		t.Fatalf("digests differ: %s != %s", DigestCanonical(first), DigestCanonical(second))
	}
}

func TestVerifierDistinguishesTamperedStaleAndMissingExports(t *testing.T) {
	envelope, err := Build(testSchema, []map[string]string{{"id": "record-alpha"}}, testPublication)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := Serialize(envelope)
	if err != nil {
		t.Fatal(err)
	}
	freshTime := testPublication.PublishedAt.Add(30 * time.Minute)
	if _, err := Verify(serialized, testSchema, freshTime); err != nil {
		t.Fatalf("verify fresh: %v", err)
	}

	tampered := []byte(strings.Replace(string(serialized), "record-alpha", "record-bravo", 1))
	if _, err := Verify(tampered, testSchema, freshTime); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("tampered error = %v, want ErrDigestMismatch", err)
	}
	if _, err := Verify(tampered, testSchema, testPublication.PublishedAt.Add(time.Hour)); !errors.Is(err, ErrStaleExport) {
		t.Fatalf("stale error = %v, want ErrStaleExport", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := VerifyFile(missing, testSchema, freshTime); !errors.Is(err, ErrExportNotFound) {
		t.Fatalf("missing error = %v, want ErrExportNotFound", err)
	}
}

func TestVerifierRejectsMalformedEnvelopeFacts(t *testing.T) {
	envelope, err := Build(testSchema, []any{}, testPublication)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Envelope)
	}{
		{name: "schema", mutate: func(value *Envelope) { value.Schema = "unsupported/1" }},
		{name: "lifetime", mutate: func(value *Envelope) {
			value.ExpiresAt = testPublication.PublishedAt.Add(2 * time.Hour).Format(time.RFC3339)
		}},
		{name: "repository", mutate: func(value *Envelope) { value.Source.Repository = "placeholder" }},
		{name: "commit", mutate: func(value *Envelope) { value.Source.Commit = "placeholder" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := envelope
			test.mutate(&candidate)
			serialized, serializeErr := Serialize(candidate)
			if serializeErr != nil {
				t.Fatal(serializeErr)
			}
			if _, verifyErr := Verify(serialized, testSchema, testPublication.PublishedAt); !errors.Is(verifyErr, ErrInvalidExport) && !errors.Is(verifyErr, ErrInvalidProvenance) {
				t.Fatalf("error = %v, want invalid export or provenance", verifyErr)
			}
		})
	}

	serialized, err := Serialize(envelope)
	if err != nil {
		t.Fatal(err)
	}
	withExtraField := []byte(strings.Replace(string(serialized), `{"digest":`, `{"added":"field","digest":`, 1))
	if _, err := Verify(withExtraField, testSchema, testPublication.PublishedAt); !errors.Is(err, ErrInvalidExport) {
		t.Fatalf("extra field error = %v, want ErrInvalidExport", err)
	}
}

func TestBuildCopiesPayloadAndRejectsPlaceholderProvenance(t *testing.T) {
	payload := []map[string]string{{"id": "original"}}
	envelope, err := Build(testSchema, payload, testPublication)
	if err != nil {
		t.Fatal(err)
	}
	payload[0]["id"] = "edited"
	if strings.Contains(string(envelope.Payload), "edited") {
		t.Fatalf("payload changed after build: %s", envelope.Payload)
	}

	for _, source := range []Source{
		{Repository: "", Commit: testPublication.Source.Commit},
		{Repository: "synthetic/repository", Commit: "placeholder"},
	} {
		publication := testPublication
		publication.Source = source
		if _, err := Build(testSchema, []any{}, publication); !errors.Is(err, ErrInvalidProvenance) {
			t.Fatalf("source %+v error = %v, want ErrInvalidProvenance", source, err)
		}
	}
}

func TestResolveGitSourceDerivesBothFactsOrFails(t *testing.T) {
	commit := strings.Repeat("b", 40)
	runner := func(_ context.Context, _ string, _ string, arguments ...string) ([]byte, error) {
		switch strings.Join(arguments, " ") {
		case "remote get-url origin":
			return []byte("git@github.com:synthetic/repository.git\n"), nil
		case "rev-parse HEAD":
			return []byte(commit + "\n"), nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", arguments)
		}
	}
	source, err := resolveGitSource(context.Background(), "/synthetic", runner)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if want := (Source{Repository: "synthetic/repository", Commit: commit}); !reflect.DeepEqual(source, want) {
		t.Fatalf("source = %+v, want %+v", source, want)
	}

	failing := func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	if _, err := resolveGitSource(context.Background(), "/synthetic", failing); !errors.Is(err, ErrInvalidProvenance) {
		t.Fatalf("failure error = %v, want ErrInvalidProvenance", err)
	}
}
