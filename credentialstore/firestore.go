// Package credentialstore provides durable adapters for agent credentials.
package credentialstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	cloudfirestore "cloud.google.com/go/firestore"
	"github.com/martcoca/work-tracker/agentcredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DefaultDatabaseID = "(default)"
	// Workload binding changed the stored authority shape. A separate namespace keeps
	// pre-workload documents from authenticating or breaking a list decode.
	DefaultNamespace = "agent-credentials-v2"
	defaultTimeout   = 10 * time.Second
	documentSchema   = int64(2)
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type Config struct {
	ProjectID        string
	DatabaseID       string
	Namespace        string
	OperationTimeout time.Duration
}

// Firestore stores only credential digests and metadata. Authentication and revocation
// use transactions so a credential cannot be accepted from a stale pre-revocation read.
type Firestore struct {
	client      *cloudfirestore.Client
	timeout     time.Duration
	credentials *cloudfirestore.CollectionRef
}

type credentialDocument struct {
	SchemaVersion   int64      `firestore:"schema_version"`
	ID              string     `firestore:"id"`
	TenantID        string     `firestore:"tenant_id"`
	PacketID        string     `firestore:"packet_id"`
	AttemptID       string     `firestore:"attempt_id"`
	WorkloadIssuer  string     `firestore:"workload_issuer"`
	WorkloadSubject string     `firestore:"workload_subject"`
	IssuedAt        time.Time  `firestore:"issued_at"`
	ExpiresAt       time.Time  `firestore:"expires_at"`
	CreatedBy       string     `firestore:"created_by"`
	CreatedAt       time.Time  `firestore:"created_at"`
	CredentialHash  []byte     `firestore:"credential_hash"`
	RevokedBy       string     `firestore:"revoked_by,omitempty"`
	RevokedAt       *time.Time `firestore:"revoked_at,omitempty"`
	LastUsedAt      *time.Time `firestore:"last_used_at,omitempty"`
}

func NewFirestore(ctx context.Context, config Config) (*Firestore, error) {
	config.ProjectID = strings.TrimSpace(config.ProjectID)
	config.DatabaseID = strings.TrimSpace(config.DatabaseID)
	config.Namespace = strings.TrimSpace(config.Namespace)
	if config.ProjectID == "" {
		return nil, errors.New("Firestore project id is required")
	}
	if config.DatabaseID == "" {
		config.DatabaseID = DefaultDatabaseID
	}
	if config.Namespace == "" {
		config.Namespace = DefaultNamespace
	}
	if !namespacePattern.MatchString(config.Namespace) {
		return nil, errors.New("Firestore namespace must be a lowercase path-safe name")
	}
	if config.OperationTimeout <= 0 {
		config.OperationTimeout = defaultTimeout
	}
	client, err := cloudfirestore.NewClientWithDatabase(ctx, config.ProjectID, config.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("create Firestore credential client: %w", err)
	}
	root := client.Collection("work_tracker_credential_stores").Doc(config.Namespace)
	return &Firestore{
		client: client, timeout: config.OperationTimeout,
		credentials: root.Collection("credentials"),
	}, nil
}

func (store *Firestore) Close() error { return store.client.Close() }

func (store *Firestore) Create(ctx context.Context, record agentcredential.StoredRecord) error {
	if err := agentcredential.ValidateStoredRecord(record); err != nil {
		return err
	}
	document := encodeDocument(record)
	ctx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	if _, err := store.credentials.Doc(pathID(record.Metadata.ID)).Create(ctx, document); err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return agentcredential.ErrCredentialExists
		}
		return fmt.Errorf("create Firestore credential: %w", err)
	}
	return nil
}

func (store *Firestore) Get(ctx context.Context, tenantID, identifier string) (agentcredential.StoredRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	snapshot, err := store.credentials.Doc(pathID(identifier)).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return agentcredential.StoredRecord{}, agentcredential.ErrUnknownCredential
	}
	if err != nil {
		return agentcredential.StoredRecord{}, fmt.Errorf("read Firestore credential: %w", err)
	}
	record, err := decodeSnapshot(snapshot)
	if err != nil {
		return agentcredential.StoredRecord{}, err
	}
	if record.Metadata.TenantID != tenantID {
		return agentcredential.StoredRecord{}, agentcredential.ErrUnknownCredential
	}
	return record, nil
}

func (store *Firestore) List(ctx context.Context, tenantID string) ([]agentcredential.StoredRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	snapshots, err := store.credentials.Where("tenant_id", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("list Firestore credentials: %w", err)
	}
	records := make([]agentcredential.StoredRecord, 0, len(snapshots))
	for _, snapshot := range snapshots {
		record, err := decodeSnapshot(snapshot)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool {
		if !records[left].Metadata.CreatedAt.Equal(records[right].Metadata.CreatedAt) {
			return records[left].Metadata.CreatedAt.Before(records[right].Metadata.CreatedAt)
		}
		return records[left].Metadata.ID < records[right].Metadata.ID
	})
	return records, nil
}

func (store *Firestore) Authenticate(ctx context.Context, identifier string, hash [sha256.Size]byte, at time.Time) (agentcredential.StoredRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	ref := store.credentials.Doc(pathID(identifier))
	var authenticated agentcredential.StoredRecord
	err := store.client.RunTransaction(ctx, func(_ context.Context, transaction *cloudfirestore.Transaction) error {
		snapshot, err := transaction.Get(ref)
		if status.Code(err) == codes.NotFound {
			return agentcredential.ErrUnknownCredential
		}
		if err != nil {
			return fmt.Errorf("read Firestore credential for authentication: %w", err)
		}
		record, err := decodeSnapshot(snapshot)
		if err != nil {
			return err
		}
		if err := agentcredential.ValidateAuthentication(record, hash, at); err != nil {
			return err
		}
		used := at.UTC()
		if record.Metadata.LastUsedAt == nil || used.After(*record.Metadata.LastUsedAt) {
			if err := transaction.Update(ref, []cloudfirestore.Update{{Path: "last_used_at", Value: used}}); err != nil {
				return fmt.Errorf("record Firestore credential use: %w", err)
			}
			record.Metadata.LastUsedAt = &used
		}
		authenticated = record
		return nil
	})
	if err != nil {
		return agentcredential.StoredRecord{}, err
	}
	return authenticated, nil
}

func (store *Firestore) Revoke(ctx context.Context, tenantID, identifier, actor string, at time.Time) (agentcredential.StoredRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	ref := store.credentials.Doc(pathID(identifier))
	var revoked agentcredential.StoredRecord
	err := store.client.RunTransaction(ctx, func(_ context.Context, transaction *cloudfirestore.Transaction) error {
		snapshot, err := transaction.Get(ref)
		if status.Code(err) == codes.NotFound {
			return agentcredential.ErrUnknownCredential
		}
		if err != nil {
			return fmt.Errorf("read Firestore credential for revocation: %w", err)
		}
		record, err := decodeSnapshot(snapshot)
		if err != nil {
			return err
		}
		if record.Metadata.TenantID != tenantID {
			return agentcredential.ErrUnknownCredential
		}
		if record.Metadata.RevokedAt == nil {
			revokedAt := at.UTC()
			if err := transaction.Update(ref, []cloudfirestore.Update{
				{Path: "revoked_at", Value: revokedAt},
				{Path: "revoked_by", Value: actor},
			}); err != nil {
				return fmt.Errorf("revoke Firestore credential: %w", err)
			}
			record.Metadata.RevokedAt = &revokedAt
			record.Metadata.RevokedBy = actor
		}
		revoked = record
		return nil
	})
	if err != nil {
		return agentcredential.StoredRecord{}, err
	}
	return revoked, nil
}

func encodeDocument(record agentcredential.StoredRecord) credentialDocument {
	return credentialDocument{
		SchemaVersion: documentSchema, ID: record.Metadata.ID,
		TenantID: record.Metadata.TenantID, PacketID: record.Metadata.Binding.PacketID,
		AttemptID:      record.Metadata.Binding.AttemptID,
		WorkloadIssuer: record.Metadata.Workload.Issuer, WorkloadSubject: record.Metadata.Workload.Subject,
		IssuedAt: record.Metadata.Binding.IssuedAt, ExpiresAt: record.Metadata.Binding.ExpiresAt,
		CreatedBy: record.Metadata.CreatedBy, CreatedAt: record.Metadata.CreatedAt,
		CredentialHash: append([]byte(nil), record.Hash[:]...),
		RevokedBy:      record.Metadata.RevokedBy, RevokedAt: cloneTime(record.Metadata.RevokedAt),
		LastUsedAt: cloneTime(record.Metadata.LastUsedAt),
	}
}

func decodeSnapshot(snapshot *cloudfirestore.DocumentSnapshot) (agentcredential.StoredRecord, error) {
	var document credentialDocument
	if err := snapshot.DataTo(&document); err != nil {
		return agentcredential.StoredRecord{}, fmt.Errorf("decode Firestore credential %q: %w", snapshot.Ref.ID, err)
	}
	return decodeDocument(document)
}

func decodeDocument(document credentialDocument) (agentcredential.StoredRecord, error) {
	if document.SchemaVersion != documentSchema {
		return agentcredential.StoredRecord{}, fmt.Errorf("unsupported credential schema %d", document.SchemaVersion)
	}
	if document.ID == "" || document.TenantID == "" || document.PacketID == "" || document.AttemptID == "" ||
		document.WorkloadIssuer == "" || document.WorkloadSubject == "" ||
		document.CreatedBy == "" || document.IssuedAt.IsZero() || document.ExpiresAt.IsZero() || document.CreatedAt.IsZero() ||
		len(document.CredentialHash) != sha256.Size {
		return agentcredential.StoredRecord{}, errors.New("stored credential is incomplete")
	}
	if document.RevokedAt == nil && document.RevokedBy != "" || document.RevokedAt != nil && document.RevokedBy == "" {
		return agentcredential.StoredRecord{}, errors.New("stored credential revocation metadata is inconsistent")
	}
	var hash [sha256.Size]byte
	copy(hash[:], document.CredentialHash)
	record := agentcredential.StoredRecord{Metadata: agentcredential.Metadata{
		ID: document.ID, TenantID: document.TenantID,
		Workload: agentcredential.Workload{
			Kind:   agentcredential.WorkloadKind,
			Issuer: document.WorkloadIssuer, Subject: document.WorkloadSubject,
		},
		Binding: agentcredential.Binding{
			PacketID: document.PacketID, AttemptID: document.AttemptID,
			IssuedAt: document.IssuedAt.UTC(), ExpiresAt: document.ExpiresAt.UTC(),
		},
		CreatedBy: document.CreatedBy, CreatedAt: document.CreatedAt.UTC(),
		RevokedBy: document.RevokedBy, RevokedAt: cloneTime(document.RevokedAt),
		LastUsedAt: cloneTime(document.LastUsedAt),
	}, Hash: hash}
	if err := agentcredential.ValidateStoredRecord(record); err != nil {
		return agentcredential.StoredRecord{}, fmt.Errorf("stored credential failed validation: %w", err)
	}
	return record, nil
}

func pathID(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
