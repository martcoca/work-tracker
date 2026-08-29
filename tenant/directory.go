// Package tenant reads and validates 0000's published tenant-directory contract.
package tenant

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/martcoca/work-tracker/contract"
)

const Schema = "martcoca.identity.tenant-directory/1"

var (
	ErrInvalidDirectory = errors.New("invalid tenant directory")
	ErrUnknownTenant    = errors.New("unknown tenant")
	ErrRetiredTenant    = errors.New("retired tenant")
)

// Status exactly matches 0000's tenant status vocabulary.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusRetired   Status = "retired"
)

// Record exactly matches martcoca.identity.tenant-directory/1.
type Record struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Status      Status `json:"status"`
	CreatedAt   string `json:"created_at"`
	Version     int64  `json:"version"`
}

// Directory is an immutable, verified tenant-directory projection.
type Directory struct {
	records   map[string]Record
	expiresAt time.Time
}

// Load reads and verifies a directory file locally.
func Load(path string, now time.Time) (*Directory, error) {
	envelope, err := contract.VerifyFile(path, Schema, now)
	if err != nil {
		return nil, err
	}
	return fromEnvelope(envelope)
}

// Parse verifies directory bytes without performing I/O.
func Parse(contents []byte, now time.Time) (*Directory, error) {
	envelope, err := contract.Verify(contents, Schema, now)
	if err != nil {
		return nil, err
	}
	return fromEnvelope(envelope)
}

func fromEnvelope(envelope contract.Envelope) (*Directory, error) {
	decoder := json.NewDecoder(bytes.NewReader(envelope.Payload))
	decoder.DisallowUnknownFields()
	var records []Record
	if err := decoder.Decode(&records); err != nil {
		return nil, fmt.Errorf("%w: decode payload: %v", ErrInvalidDirectory, err)
	}
	if err := requireEOF(decoder); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDirectory, err)
	}
	byID := make(map[string]Record, len(records))
	for _, record := range records {
		if err := validateRecord(record); err != nil {
			return nil, err
		}
		if _, duplicate := byID[record.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate tenant id %q", ErrInvalidDirectory, record.ID)
		}
		byID[record.ID] = record
	}
	expiresAt, _ := time.Parse(time.RFC3339, envelope.ExpiresAt)
	return &Directory{records: byID, expiresAt: expiresAt}, nil
}

// ValidateTenantID satisfies packet.TenantValidator. Retired remains distinguishable
// from absence, and freshness is checked again at every issue rather than only at load.
func (directory *Directory) ValidateTenantID(id string, at time.Time) error {
	if directory == nil {
		return fmt.Errorf("%w: directory is unavailable", ErrInvalidDirectory)
	}
	if !at.Before(directory.expiresAt) {
		return fmt.Errorf("%w: tenant directory expired at %s", contract.ErrStaleExport, directory.expiresAt.Format(time.RFC3339Nano))
	}
	record, exists := directory.records[id]
	if !exists {
		return fmt.Errorf("%w: %q is absent from the directory", ErrUnknownTenant, id)
	}
	if record.Status == StatusRetired {
		return fmt.Errorf("%w: %q is explicitly retired", ErrRetiredTenant, id)
	}
	return nil
}

// Records returns defensive copies sorted by stable tenant id.
func (directory *Directory) Records() []Record {
	if directory == nil {
		return nil
	}
	records := make([]Record, 0, len(directory.records))
	for _, record := range directory.records {
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool { return records[left].ID < records[right].ID })
	return records
}

func validateRecord(record Record) error {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Slug) == "" || strings.TrimSpace(record.DisplayName) == "" {
		return fmt.Errorf("%w: id, slug, and display_name are required", ErrInvalidDirectory)
	}
	switch record.Status {
	case StatusActive, StatusSuspended, StatusRetired:
	default:
		return fmt.Errorf("%w: tenant %q has unsupported status %q", ErrInvalidDirectory, record.ID, record.Status)
	}
	if _, err := time.Parse(time.RFC3339, record.CreatedAt); err != nil {
		return fmt.Errorf("%w: tenant %q created_at must be RFC 3339", ErrInvalidDirectory, record.ID)
	}
	if record.Version < 1 {
		return fmt.Errorf("%w: tenant %q version must be positive", ErrInvalidDirectory, record.ID)
	}
	return nil
}

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
