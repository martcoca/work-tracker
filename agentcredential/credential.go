// Package agentcredential issues and authenticates product-owned machine credentials.
// It proves identity only; packet permissions remain in 0000's agent-grants export.
package agentcredential

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	MaximumLifetime      = time.Hour
	WorkloadKind         = "workload"
	credentialPrefix     = "wta_"
	identifierBytes      = 16
	credentialBytes      = 32
	maximumWorkloadValue = 512
)

var (
	ErrMalformedHeader   = errors.New("malformed agent authorization header")
	ErrUnknownCredential = errors.New("unknown agent credential")
	ErrRevokedCredential = errors.New("revoked agent credential")
	ErrExpiredCredential = errors.New("expired agent credential")
	ErrInvalidCredential = errors.New("invalid agent credential request")
	ErrInvalidWorkload   = errors.New("invalid workload principal")
	ErrWorkloadTenant    = errors.New("workload belongs to another tenant")
	ErrCredentialExists  = errors.New("agent credential already exists")
	packetIDPattern      = regexp.MustCompile(`^[0-9]{4}-E[0-9]{2}-T[0-9]{2}$`)
	identifierPattern    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Workload is the authority principal a grant names. The enclosing identity supplies the
// tenant without putting packet or attempt details into the grant lookup.
type Workload struct {
	Kind    string `json:"kind"`
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// Binding is enforced by this product and is never part of a grant lookup.
type Binding struct {
	PacketID  string    `json:"packet_id"`
	AttemptID string    `json:"attempt_id"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Identity carries the workload used for grant lookup beside the packet binding enforced
// here. It carries no scope or authorization result.
type Identity struct {
	TenantID string   `json:"tenant_id"`
	Workload Workload `json:"workload"`
	Binding  Binding  `json:"binding"`
}

// Metadata is everything a human may retrieve after creation. The one-time credential
// and its stored digest are deliberately absent.
type Metadata struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	Workload   Workload   `json:"workload"`
	Binding    Binding    `json:"binding"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedBy  string     `json:"revoked_by,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// StoredRecord is the durable form. Hash is a one-way digest of the full random bearer
// value, never the bearer value itself.
type StoredRecord struct {
	Metadata Metadata
	Hash     [sha256.Size]byte `json:"-"`
}

// Issued is returned only by Create. Subsequent calls return Metadata.
type Issued struct {
	Credential string   `json:"credential"`
	Metadata   Metadata `json:"metadata"`
}

type CreateCommand struct {
	TenantID         string
	PacketID         string
	AttemptID        string
	WorkloadTenantID string
	Workload         Workload
	CreatedBy        string
	ExpiresAt        time.Time
}

// Store makes revocation and last-use recording part of the authentication transaction.
type Store interface {
	Create(context.Context, StoredRecord) error
	Get(context.Context, string, string) (StoredRecord, error)
	List(context.Context, string) ([]StoredRecord, error)
	Authenticate(context.Context, string, [sha256.Size]byte, time.Time) (StoredRecord, error)
	Revoke(context.Context, string, string, string, time.Time) (StoredRecord, error)
}

type Manager struct {
	store  Store
	random io.Reader
}

func NewManager(store Store) (*Manager, error) {
	if store == nil {
		return nil, errors.New("agent credential store is required")
	}
	return &Manager{store: store, random: rand.Reader}, nil
}

// Create generates a high-entropy bearer value, stores only its digest, and returns the
// value once alongside retrievable metadata.
func (manager *Manager) Create(ctx context.Context, command CreateCommand, at time.Time) (Issued, error) {
	at = at.UTC()
	command.ExpiresAt = command.ExpiresAt.UTC()
	if err := validateCreate(command, at); err != nil {
		return Issued{}, err
	}

	identifierRaw := make([]byte, identifierBytes)
	valueRaw := make([]byte, credentialBytes)
	if _, err := io.ReadFull(manager.random, identifierRaw); err != nil {
		return Issued{}, fmt.Errorf("generate credential identifier: %w", err)
	}
	if _, err := io.ReadFull(manager.random, valueRaw); err != nil {
		return Issued{}, fmt.Errorf("generate credential value: %w", err)
	}
	identifier := hex.EncodeToString(identifierRaw)
	value := credentialPrefix + identifier + "." + base64.RawURLEncoding.EncodeToString(valueRaw)
	hash := sha256.Sum256([]byte(value))
	record := StoredRecord{Metadata: Metadata{
		ID: identifier, TenantID: command.TenantID,
		Workload: command.Workload,
		Binding: Binding{
			PacketID: command.PacketID, AttemptID: command.AttemptID,
			IssuedAt: at, ExpiresAt: command.ExpiresAt,
		},
		CreatedBy: command.CreatedBy, CreatedAt: at,
	}, Hash: hash}
	if err := ValidateStoredRecord(record); err != nil {
		return Issued{}, err
	}
	if err := manager.store.Create(ctx, record); err != nil {
		return Issued{}, err
	}
	return Issued{Credential: value, Metadata: cloneMetadata(record.Metadata)}, nil
}

// Authenticate resolves a bearer header to identity and atomically records successful
// use. It returns no grant or scope.
func (manager *Manager) Authenticate(ctx context.Context, authorization string, at time.Time) (Identity, error) {
	value, err := bearerValue(authorization)
	if err != nil {
		return Identity{}, err
	}
	identifier, err := credentialIdentifier(value)
	if err != nil {
		return Identity{}, err
	}
	hash := sha256.Sum256([]byte(value))
	record, err := manager.store.Authenticate(ctx, identifier, hash, at.UTC())
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		TenantID: record.Metadata.TenantID,
		Workload: record.Metadata.Workload,
		Binding:  record.Metadata.Binding,
	}, nil
}

func (manager *Manager) Get(ctx context.Context, tenantID, identifier string) (Metadata, error) {
	if !validIdentityValue(tenantID) || !identifierPattern.MatchString(identifier) {
		return Metadata{}, ErrUnknownCredential
	}
	record, err := manager.store.Get(ctx, tenantID, identifier)
	if err != nil {
		return Metadata{}, err
	}
	return cloneMetadata(record.Metadata), nil
}

func (manager *Manager) List(ctx context.Context, tenantID string) ([]Metadata, error) {
	if !validIdentityValue(tenantID) {
		return nil, fmt.Errorf("%w: tenant id is required", ErrInvalidCredential)
	}
	records, err := manager.store.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]Metadata, len(records))
	for index, record := range records {
		result[index] = cloneMetadata(record.Metadata)
	}
	return result, nil
}

func (manager *Manager) Revoke(ctx context.Context, tenantID, identifier, actor string, at time.Time) (Metadata, error) {
	if !validIdentityValue(tenantID) || !validIdentityValue(actor) || !identifierPattern.MatchString(identifier) {
		return Metadata{}, ErrUnknownCredential
	}
	record, err := manager.store.Revoke(ctx, tenantID, identifier, actor, at.UTC())
	if err != nil {
		return Metadata{}, err
	}
	return cloneMetadata(record.Metadata), nil
}

func validateCreate(command CreateCommand, at time.Time) error {
	switch {
	case !validIdentityValue(command.TenantID):
		return fmt.Errorf("%w: tenant id is required", ErrInvalidCredential)
	case !validIdentityValue(command.WorkloadTenantID):
		return fmt.Errorf("%w: workload tenant is required", ErrInvalidWorkload)
	case command.WorkloadTenantID != command.TenantID:
		return ErrWorkloadTenant
	case command.Workload.Kind != WorkloadKind || !validWorkloadIssuer(command.Workload.Issuer) ||
		!validWorkloadSubject(command.Workload.Subject):
		return ErrInvalidWorkload
	case !packetIDPattern.MatchString(command.PacketID):
		return fmt.Errorf("%w: packet id is invalid", ErrInvalidCredential)
	case !validIdentityValue(command.AttemptID) || len(command.AttemptID) > 256:
		return fmt.Errorf("%w: attempt id is invalid", ErrInvalidCredential)
	case !validIdentityValue(command.CreatedBy):
		return fmt.Errorf("%w: creator is required", ErrInvalidCredential)
	case command.ExpiresAt.IsZero() || !command.ExpiresAt.After(at):
		return fmt.Errorf("%w: expiry must be later than issue time", ErrInvalidCredential)
	case command.ExpiresAt.Sub(at) > MaximumLifetime:
		return fmt.Errorf("%w: lifetime must not exceed %s", ErrInvalidCredential, MaximumLifetime)
	default:
		return nil
	}
}

// ValidateStoredRecord keeps every durable adapter fail-closed on the same session shape
// the manager issues.
func ValidateStoredRecord(record StoredRecord) error {
	metadata := record.Metadata
	if !identifierPattern.MatchString(metadata.ID) || !validIdentityValue(metadata.TenantID) ||
		metadata.Workload.Kind != WorkloadKind ||
		!validWorkloadIssuer(metadata.Workload.Issuer) || !validWorkloadSubject(metadata.Workload.Subject) ||
		!packetIDPattern.MatchString(metadata.Binding.PacketID) ||
		!validIdentityValue(metadata.Binding.AttemptID) || len(metadata.Binding.AttemptID) > 256 ||
		!validIdentityValue(metadata.CreatedBy) || metadata.CreatedAt.IsZero() ||
		!metadata.CreatedAt.Equal(metadata.Binding.IssuedAt) ||
		!metadata.Binding.ExpiresAt.After(metadata.Binding.IssuedAt) ||
		metadata.Binding.ExpiresAt.Sub(metadata.Binding.IssuedAt) > MaximumLifetime {
		return fmt.Errorf("%w: stored credential metadata is invalid", ErrInvalidCredential)
	}
	if metadata.RevokedAt == nil && metadata.RevokedBy != "" || metadata.RevokedAt != nil && !validIdentityValue(metadata.RevokedBy) {
		return fmt.Errorf("%w: stored revocation metadata is invalid", ErrInvalidCredential)
	}
	if metadata.RevokedAt != nil && metadata.RevokedAt.Before(metadata.CreatedAt) {
		return fmt.Errorf("%w: revocation predates creation", ErrInvalidCredential)
	}
	if metadata.LastUsedAt != nil && metadata.LastUsedAt.Before(metadata.CreatedAt) {
		return fmt.Errorf("%w: last use predates creation", ErrInvalidCredential)
	}
	return nil
}

// ValidateAuthentication is shared by every store adapter and must run inside the store's
// read/update transaction. Wrong digests are deliberately indistinguishable from unknown
// identifiers.
func ValidateAuthentication(record StoredRecord, hash [sha256.Size]byte, at time.Time) error {
	if subtle.ConstantTimeCompare(record.Hash[:], hash[:]) != 1 {
		return ErrUnknownCredential
	}
	if record.Metadata.RevokedAt != nil {
		return ErrRevokedCredential
	}
	if !at.Before(record.Metadata.Binding.ExpiresAt) {
		return ErrExpiredCredential
	}
	return nil
}

func validIdentityValue(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\x00")
}

func validWorkloadIssuer(value string) bool {
	if !validIdentityValue(value) || len(value) > maximumWorkloadValue {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

func validWorkloadSubject(value string) bool {
	return validIdentityValue(value) && len(value) <= maximumWorkloadValue
}

func bearerValue(header string) (string, error) {
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || value == "" || strings.ContainsAny(value, " \t\r\n") {
		return "", ErrMalformedHeader
	}
	return value, nil
}

func credentialIdentifier(value string) (string, error) {
	withoutPrefix, ok := strings.CutPrefix(value, credentialPrefix)
	if !ok {
		return "", ErrUnknownCredential
	}
	identifier, encoded, found := strings.Cut(withoutPrefix, ".")
	if !found || !identifierPattern.MatchString(identifier) {
		return "", ErrUnknownCredential
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != credentialBytes {
		return "", ErrUnknownCredential
	}
	return identifier, nil
}

func cloneMetadata(metadata Metadata) Metadata {
	if metadata.RevokedAt != nil {
		value := *metadata.RevokedAt
		metadata.RevokedAt = &value
	}
	if metadata.LastUsedAt != nil {
		value := *metadata.LastUsedAt
		metadata.LastUsedAt = &value
	}
	return metadata
}
