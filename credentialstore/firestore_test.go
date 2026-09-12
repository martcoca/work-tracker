package credentialstore

import (
	"crypto/sha256"
	"reflect"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/agentcredential"
)

func TestCredentialDocumentStoresDigestAndMetadataWithoutBearerValue(t *testing.T) {
	issuedAt := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	revokedAt := issuedAt.Add(20 * time.Minute)
	lastUsedAt := issuedAt.Add(10 * time.Minute)
	record := agentcredential.StoredRecord{Metadata: agentcredential.Metadata{
		ID: "00112233445566778899aabbccddeeff", TenantID: "tenant-a",
		Workload: agentcredential.Workload{
			Kind:   agentcredential.WorkloadKind,
			Issuer: "https://identity.invalid", Subject: "workload-a",
		},
		Binding: agentcredential.Binding{
			PacketID: "0004-E03-T05", AttemptID: "attempt-a",
			IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
		},
		CreatedBy: "human-a", CreatedAt: issuedAt, RevokedBy: "human-a",
		RevokedAt: &revokedAt, LastUsedAt: &lastUsedAt,
	}, Hash: sha256.Sum256([]byte("one-time-value"))}

	document := encodeDocument(record)
	if len(document.CredentialHash) != sha256.Size {
		t.Fatalf("stored digest length = %d", len(document.CredentialHash))
	}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(document)) {
		if field.Name == "Credential" || field.Name == "Value" {
			t.Fatalf("durable document exposes recoverable field %q", field.Name)
		}
	}
	decoded, err := decodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, record) {
		t.Fatalf("round trip differs\n got: %#v\nwant: %#v", decoded, record)
	}
}

func TestCredentialDocumentsFailClosed(t *testing.T) {
	issuedAt := time.Now().UTC()
	valid := credentialDocument{
		SchemaVersion: documentSchema, ID: "00112233445566778899aabbccddeeff", TenantID: "tenant-a",
		PacketID: "0004-E03-T05", AttemptID: "attempt-a",
		WorkloadIssuer: "https://identity.invalid", WorkloadSubject: "workload-a",
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
		CreatedBy: "human-a", CreatedAt: issuedAt, CredentialHash: make([]byte, sha256.Size),
	}
	for name, document := range map[string]credentialDocument{
		"schema":          func() credentialDocument { value := valid; value.SchemaVersion++; return value }(),
		"short digest":    func() credentialDocument { value := valid; value.CredentialHash = []byte{1}; return value }(),
		"workload absent": func() credentialDocument { value := valid; value.WorkloadSubject = ""; return value }(),
		"partial revoke": func() credentialDocument {
			value := valid
			value.RevokedBy = "human-a"
			return value
		}(),
		"overlong principal": func() credentialDocument {
			value := valid
			value.ExpiresAt = value.IssuedAt.Add(agentcredential.MaximumLifetime + time.Nanosecond)
			return value
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeDocument(document); err == nil {
				t.Fatal("invalid durable credential was accepted")
			}
		})
	}
}

func TestCredentialPathCannotCreateNestedFirestorePaths(t *testing.T) {
	if got := pathID("credential/with/slashes"); got == "" || got == "credential/with/slashes" {
		t.Fatalf("path id = %q", got)
	}
}

func TestWorkloadCredentialsUseASeparateStoredSchema(t *testing.T) {
	if documentSchema != 2 || DefaultNamespace != "agent-credentials-v2" {
		t.Fatalf("schema=%d namespace=%q", documentSchema, DefaultNamespace)
	}
}
