// Package contract implements the shared offline export envelope used across products.
package contract

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FreshnessBound is shared with the identity product. It is part of the wire contract.
const FreshnessBound = time.Hour

var (
	ErrExportNotFound    = errors.New("export not found")
	ErrInvalidExport     = errors.New("invalid export")
	ErrStaleExport       = errors.New("stale export")
	ErrDigestMismatch    = errors.New("export payload digest mismatch")
	ErrInvalidProvenance = errors.New("invalid publication provenance")
)

// Source identifies the exact repository revision that produced an export.
type Source struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
}

// Publication supplies the facts that vary between publications.
type Publication struct {
	PublishedAt time.Time
	Source      Source
}

// Envelope is the shared six-field export envelope. Payload contains canonical JSON and
// is copied on construction so later caller mutation cannot invalidate its digest.
type Envelope struct {
	Schema      string          `json:"schema"`
	PublishedAt string          `json:"published_at"`
	ExpiresAt   string          `json:"expires_at"`
	Source      Source          `json:"source"`
	Digest      string          `json:"digest"`
	Payload     json.RawMessage `json:"payload"`
}

// Build creates an envelope around a logical payload.
func Build(schema string, payload any, publication Publication) (Envelope, error) {
	if strings.TrimSpace(schema) == "" {
		return Envelope{}, fmt.Errorf("%w: schema is required", ErrInvalidExport)
	}
	if publication.PublishedAt.IsZero() {
		return Envelope{}, fmt.Errorf("%w: published_at is required", ErrInvalidExport)
	}
	if err := ValidateSource(publication.Source); err != nil {
		return Envelope{}, err
	}
	canonicalPayload, err := CanonicalJSON(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: canonical payload: %v", ErrInvalidExport, err)
	}
	publishedAt := publication.PublishedAt.UTC()
	expiresAt := publishedAt.Add(FreshnessBound)
	return Envelope{
		Schema:      schema,
		PublishedAt: formatTime(publishedAt),
		ExpiresAt:   formatTime(expiresAt),
		Source:      publication.Source,
		Digest:      DigestCanonical(canonicalPayload),
		Payload:     append(json.RawMessage(nil), canonicalPayload...),
	}, nil
}

// Serialize returns canonical JSON for the entire envelope.
func Serialize(envelope Envelope) ([]byte, error) {
	return CanonicalJSON(envelope)
}

// CanonicalJSON implements the shared canonical form: object keys sort recursively,
// arrays retain their order, and JSON numbers are normalized.
func CanonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return Canonicalize(raw)
}

// Canonicalize converts one JSON value to the shared canonical form.
func Canonicalize(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	var result bytes.Buffer
	if err := encodeCanonical(&result, value); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

// DigestCanonical hashes bytes already in canonical payload form.
func DigestCanonical(canonicalPayload []byte) string {
	digest := sha256.Sum256(canonicalPayload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// VerifyFile reads and verifies an envelope without fetching or contacting its source.
func VerifyFile(path, expectedSchema string, now time.Time) (Envelope, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Envelope{}, fmt.Errorf("%w: %s", ErrExportNotFound, path)
		}
		return Envelope{}, err
	}
	return Verify(contents, expectedSchema, now)
}

// Verify validates freshness before integrity, matching the shared consumer contract.
func Verify(contents []byte, expectedSchema string, now time.Time) (Envelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("%w: decode envelope: %v", ErrInvalidExport, err)
	}
	if err := requireEOF(decoder); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrInvalidExport, err)
	}
	if envelope.Schema != expectedSchema {
		return Envelope{}, fmt.Errorf("%w: unsupported schema %q", ErrInvalidExport, envelope.Schema)
	}
	publishedAt, err := time.Parse(time.RFC3339, envelope.PublishedAt)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: published_at must be RFC 3339", ErrInvalidExport)
	}
	expiresAt, err := time.Parse(time.RFC3339, envelope.ExpiresAt)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: expires_at must be RFC 3339", ErrInvalidExport)
	}
	if expiresAt.Sub(publishedAt) != FreshnessBound {
		return Envelope{}, fmt.Errorf("%w: expires_at must be exactly one hour after published_at", ErrInvalidExport)
	}
	if now.IsZero() {
		return Envelope{}, fmt.Errorf("%w: current time is required", ErrInvalidExport)
	}
	if !now.Before(expiresAt) {
		return Envelope{}, fmt.Errorf("%w: expired at %s", ErrStaleExport, envelope.ExpiresAt)
	}
	if len(envelope.Payload) == 0 {
		return Envelope{}, fmt.Errorf("%w: payload is required", ErrInvalidExport)
	}
	canonicalPayload, err := Canonicalize(envelope.Payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: payload is not valid JSON", ErrInvalidExport)
	}
	actualDigest := DigestCanonical(canonicalPayload)
	if !equalDigest(envelope.Digest, actualDigest) {
		return Envelope{}, fmt.Errorf("%w: expected %s, computed %s", ErrDigestMismatch, envelope.Digest, actualDigest)
	}
	if err := ValidateSource(envelope.Source); err != nil {
		return Envelope{}, err
	}
	envelope.Payload = append(json.RawMessage(nil), canonicalPayload...)
	return envelope, nil
}

func equalDigest(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func encodeCanonical(destination *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case nil:
		destination.WriteString("null")
	case bool:
		if value {
			destination.WriteString("true")
		} else {
			destination.WriteString("false")
		}
	case string:
		encoded, _ := json.Marshal(value)
		destination.Write(encoded)
	case json.Number:
		normalized, err := normalizeNumber(value)
		if err != nil {
			return err
		}
		destination.WriteString(normalized)
	case []any:
		destination.WriteByte('[')
		for index, item := range value {
			if index != 0 {
				destination.WriteByte(',')
			}
			if err := encodeCanonical(destination, item); err != nil {
				return err
			}
		}
		destination.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		destination.WriteByte('{')
		for index, key := range keys {
			if index != 0 {
				destination.WriteByte(',')
			}
			encodedKey, _ := json.Marshal(key)
			destination.Write(encodedKey)
			destination.WriteByte(':')
			if err := encodeCanonical(destination, value[key]); err != nil {
				return err
			}
		}
		destination.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func normalizeNumber(number json.Number) (string, error) {
	text := number.String()
	if !strings.ContainsAny(text, ".eE") {
		return text, nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", fmt.Errorf("invalid JSON number %q", text)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
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

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
