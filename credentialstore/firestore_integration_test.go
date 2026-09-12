package credentialstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/agentcredential"
	"github.com/martcoca/work-tracker/credentialstore"
)

// TestFirestoreCredentialLifecycle is skipped unless an authorized operator supplies an
// existing Firestore project. It creates one uniquely named credential and leaves the
// hash-only document in place as durable evidence.
func TestFirestoreCredentialLifecycle(t *testing.T) {
	projectID := os.Getenv("FIRESTORE_INTEGRATION_PROJECT_ID")
	if projectID == "" {
		t.Skip("FIRESTORE_INTEGRATION_PROJECT_ID is not set; real-store proof was not run")
	}
	databaseID := os.Getenv("FIRESTORE_INTEGRATION_DATABASE_ID")
	if databaseID == "" {
		databaseID = credentialstore.DefaultDatabaseID
	}
	namespace := fmt.Sprintf("e03-t04-%d", time.Now().UTC().UnixNano())
	store, err := credentialstore.NewFirestore(context.Background(), credentialstore.Config{
		ProjectID: projectID, DatabaseID: databaseID, Namespace: namespace,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := agentcredential.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	issued, err := manager.Create(context.Background(), agentcredential.CreateCommand{
		TenantID: "tenant-integration", PacketID: "0004-E03-T05",
		AttemptID: namespace, CreatedBy: "human-integration", ExpiresAt: now.Add(time.Hour),
		WorkloadTenantID: "tenant-integration",
		Workload: agentcredential.Workload{
			Kind:   agentcredential.WorkloadKind,
			Issuer: "https://identity.invalid", Subject: "workload-integration",
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Authenticate(context.Background(), "Bearer "+issued.Credential, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.Revoke(context.Background(), "tenant-integration", issued.Metadata.ID, "human-integration", now.Add(2*time.Second))
	if err != nil || metadata.LastUsedAt == nil || metadata.RevokedAt == nil {
		t.Fatalf("metadata=%#v error=%v", metadata, err)
	}
	if _, err := manager.Authenticate(context.Background(), "Bearer "+issued.Credential, now.Add(3*time.Second)); !errors.Is(err, agentcredential.ErrRevokedCredential) {
		t.Fatalf("post-revocation error=%v", err)
	}
	t.Logf("real Firestore namespace %q retained hash-only metadata, last use, and immediate revocation", namespace)
}
