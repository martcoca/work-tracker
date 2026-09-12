package agentcredential

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var credentialClock = time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)

func TestCredentialIsReturnedOnceAndOnlyItsHashIsStored(t *testing.T) {
	manager, store := deterministicManager(t)
	issued := createCredential(t, manager, credentialClock.Add(time.Hour))
	if !strings.HasPrefix(issued.Credential, credentialPrefix) {
		t.Fatalf("credential has unexpected format")
	}
	stored := store.records[issued.Metadata.ID]
	if stored.Hash != sha256.Sum256([]byte(issued.Credential)) {
		t.Fatal("stored digest does not match the one-time value")
	}

	fetched, err := manager.Get(context.Background(), "tenant-a", issued.Metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched != issued.Metadata {
		t.Fatalf("retrieved metadata differs\n got: %#v\nwant: %#v", fetched, issued.Metadata)
	}
	listed, err := manager.List(context.Background(), "tenant-a")
	if err != nil || len(listed) != 1 || listed[0] != issued.Metadata {
		t.Fatalf("listed metadata = %#v, error=%v", listed, err)
	}

	// A correctly shaped bearer made from the stored digest proves the digest is not a
	// credential and cannot be used as one.
	storedValue := credentialPrefix + issued.Metadata.ID + "." + base64.RawURLEncoding.EncodeToString(stored.Hash[:])
	if _, err := manager.Authenticate(context.Background(), "Bearer "+storedValue, credentialClock.Add(time.Minute)); !errors.Is(err, ErrUnknownCredential) {
		t.Fatalf("stored digest authentication error = %v, want ErrUnknownCredential", err)
	}
	t.Logf("created credential %s; retrieval exposes metadata only; stored digest was refused", issued.Metadata.ID)
}

func TestValidCredentialResolvesOnlyIdentityAndRecordsLastUse(t *testing.T) {
	manager, _ := deterministicManager(t)
	issued := createCredential(t, manager, credentialClock.Add(time.Hour))
	usedAt := credentialClock.Add(10 * time.Minute)
	identity, err := manager.Authenticate(context.Background(), "Bearer "+issued.Credential, usedAt)
	if err != nil {
		t.Fatal(err)
	}
	want := Identity{
		TenantID: "tenant-a", Workload: issued.Metadata.Workload, Binding: issued.Metadata.Binding,
	}
	if identity != want {
		t.Fatalf("identity = %#v, want %#v", identity, want)
	}
	metadata, err := manager.Get(context.Background(), "tenant-a", issued.Metadata.ID)
	if err != nil || metadata.LastUsedAt == nil || !metadata.LastUsedAt.Equal(usedAt) {
		t.Fatalf("last use = %v, error=%v", metadata.LastUsedAt, err)
	}
	if fields := []any{identity.TenantID, identity.Workload, identity.Binding}; len(fields) != 3 {
		t.Fatal("identity unexpectedly carried authorization")
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), issued.Credential) || strings.Contains(string(encoded), `"credential"`) ||
		strings.Contains(string(encoded), `"scope"`) {
		t.Fatal("authenticated identity exposed credential material or authorization")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 3 ||
		fields["tenant_id"] == nil || fields["workload"] == nil || fields["binding"] == nil {
		t.Fatalf("authenticated identity fields = %v, error=%v", fields, err)
	}
	t.Logf("valid credential resolved to workload=%s/%s packet=%s attempt=%s and last_used_at=%s",
		identity.Workload.Issuer, identity.Workload.Subject, identity.Binding.PacketID,
		identity.Binding.AttemptID, metadata.LastUsedAt.Format(time.RFC3339))
}

func TestCredentialRefusalsAreDistinctAndRevocationIsImmediate(t *testing.T) {
	manager, _ := deterministicManager(t)
	issued := createCredential(t, manager, credentialClock.Add(30*time.Minute))

	cases := []struct {
		name   string
		header string
		at     time.Time
		want   error
	}{
		{name: "malformed header", header: issued.Credential, at: credentialClock, want: ErrMalformedHeader},
		{name: "unknown value", header: "Bearer " + mutateCredential(issued.Credential), at: credentialClock, want: ErrUnknownCredential},
		{name: "expired", header: "Bearer " + issued.Credential, at: credentialClock.Add(30 * time.Minute), want: ErrExpiredCredential},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := manager.Authenticate(context.Background(), test.header, test.at); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}

	if _, err := manager.Authenticate(context.Background(), "Bearer "+issued.Credential, credentialClock.Add(time.Minute)); err != nil {
		t.Fatalf("authenticate before revoke: %v", err)
	}
	metadata, err := manager.Revoke(context.Background(), "tenant-a", issued.Metadata.ID, "human-a", credentialClock.Add(2*time.Minute))
	if err != nil || metadata.RevokedAt == nil {
		t.Fatalf("revoke metadata=%#v error=%v", metadata, err)
	}
	if _, err := manager.Authenticate(context.Background(), "Bearer "+issued.Credential, credentialClock.Add(2*time.Minute)); !errors.Is(err, ErrRevokedCredential) {
		t.Fatalf("next request after revoke error = %v, want ErrRevokedCredential", err)
	}
	t.Log("valid before revocation; the immediately following request was refused as revoked")
}

func TestCredentialCreationRequiresAValidTenantBoundWorkload(t *testing.T) {
	manager, _ := deterministicManager(t)
	for name, command := range map[string]CreateCommand{
		"workload omitted": {
			TenantID: "tenant-a", PacketID: "0004-E03-T05", AttemptID: "attempt-a",
			CreatedBy: "human-a", ExpiresAt: credentialClock.Add(time.Hour),
		},
		"issuer malformed": {
			TenantID: "tenant-a", PacketID: "0004-E03-T05", AttemptID: "attempt-a",
			WorkloadTenantID: "tenant-a", Workload: workload("http://identity.invalid", "subject-a"),
			CreatedBy: "human-a", ExpiresAt: credentialClock.Add(time.Hour),
		},
		"subject absent": {
			TenantID: "tenant-a", PacketID: "0004-E03-T05", AttemptID: "attempt-a",
			WorkloadTenantID: "tenant-a", Workload: workload("https://identity.invalid", ""),
			CreatedBy: "human-a", ExpiresAt: credentialClock.Add(time.Hour),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := manager.Create(context.Background(), command, credentialClock); !errors.Is(err, ErrInvalidWorkload) {
				t.Fatalf("error = %v, want ErrInvalidWorkload", err)
			}
		})
	}

	outside := validCreateCommand(credentialClock.Add(time.Hour))
	outside.WorkloadTenantID = "tenant-b"
	if _, err := manager.Create(context.Background(), outside, credentialClock); !errors.Is(err, ErrWorkloadTenant) {
		t.Fatalf("cross-tenant workload error = %v, want ErrWorkloadTenant", err)
	}

	for name, command := range map[string]CreateCommand{
		"tenant omitted": {PacketID: "0004-E03-T05", AttemptID: "attempt-a", WorkloadTenantID: "tenant-a", Workload: workload("https://identity.invalid", "subject-a"), CreatedBy: "human-a", ExpiresAt: credentialClock.Add(time.Hour)},
		"invalid packet": func() CreateCommand {
			value := validCreateCommand(credentialClock.Add(time.Hour))
			value.PacketID = "packet-a"
			return value
		}(),
		"over one hour": validCreateCommand(credentialClock.Add(time.Hour + time.Nanosecond)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := manager.Create(context.Background(), command, credentialClock); !errors.Is(err, ErrInvalidCredential) {
				t.Fatalf("error = %v, want ErrInvalidCredential", err)
			}
		})
	}
}

func deterministicManager(t *testing.T) (*Manager, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	manager, err := NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, identifierBytes+credentialBytes)
	for index := range random {
		random[index] = byte(index + 1)
	}
	manager.random = bytes.NewReader(random)
	return manager, store
}

func createCredential(t *testing.T, manager *Manager, expiresAt time.Time) Issued {
	t.Helper()
	issued, err := manager.Create(context.Background(), validCreateCommand(expiresAt), credentialClock)
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

func validCreateCommand(expiresAt time.Time) CreateCommand {
	return CreateCommand{
		TenantID: "tenant-a", PacketID: "0004-E03-T05", AttemptID: "attempt-a",
		WorkloadTenantID: "tenant-a", Workload: workload("https://identity.invalid", "workload-a"),
		CreatedBy: "human-a", ExpiresAt: expiresAt,
	}
}

func workload(issuer, subject string) Workload {
	return Workload{Kind: WorkloadKind, Issuer: issuer, Subject: subject}
}

func mutateCredential(value string) string {
	last := value[len(value)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	return value[:len(value)-1] + string(replacement)
}
