package agentcredential

import (
	"context"
	"crypto/sha256"
	"sort"
	"sync"
	"time"
)

// MemoryStore exercises the full store contract without external I/O. Production uses
// the Firestore adapter.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]StoredRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]StoredRecord)}
}

func (store *MemoryStore) Create(_ context.Context, record StoredRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ValidateStoredRecord(record); err != nil {
		return err
	}
	if _, exists := store.records[record.Metadata.ID]; exists {
		return ErrCredentialExists
	}
	store.records[record.Metadata.ID] = cloneRecord(record)
	return nil
}

func (store *MemoryStore) Get(_ context.Context, tenantID, identifier string) (StoredRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, exists := store.records[identifier]
	if !exists || record.Metadata.TenantID != tenantID {
		return StoredRecord{}, ErrUnknownCredential
	}
	return cloneRecord(record), nil
}

func (store *MemoryStore) List(_ context.Context, tenantID string) ([]StoredRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]StoredRecord, 0)
	for _, record := range store.records {
		if record.Metadata.TenantID == tenantID {
			result = append(result, cloneRecord(record))
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if !result[left].Metadata.CreatedAt.Equal(result[right].Metadata.CreatedAt) {
			return result[left].Metadata.CreatedAt.Before(result[right].Metadata.CreatedAt)
		}
		return result[left].Metadata.ID < result[right].Metadata.ID
	})
	return result, nil
}

func (store *MemoryStore) Authenticate(_ context.Context, identifier string, hash [sha256.Size]byte, at time.Time) (StoredRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, exists := store.records[identifier]
	if !exists {
		return StoredRecord{}, ErrUnknownCredential
	}
	if err := ValidateAuthentication(record, hash, at); err != nil {
		return StoredRecord{}, err
	}
	used := at.UTC()
	if record.Metadata.LastUsedAt == nil || used.After(*record.Metadata.LastUsedAt) {
		record.Metadata.LastUsedAt = &used
		store.records[identifier] = record
	}
	return cloneRecord(record), nil
}

func (store *MemoryStore) Revoke(_ context.Context, tenantID, identifier, actor string, at time.Time) (StoredRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, exists := store.records[identifier]
	if !exists || record.Metadata.TenantID != tenantID {
		return StoredRecord{}, ErrUnknownCredential
	}
	if record.Metadata.RevokedAt == nil {
		revoked := at.UTC()
		record.Metadata.RevokedAt = &revoked
		record.Metadata.RevokedBy = actor
		store.records[identifier] = record
	}
	return cloneRecord(record), nil
}

func cloneRecord(record StoredRecord) StoredRecord {
	record.Metadata = cloneMetadata(record.Metadata)
	return record
}
