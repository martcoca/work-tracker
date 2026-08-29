// Package packetexport serializes packet projections for offline session consumption.
package packetexport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packet"
)

const (
	Schema   = "martcoca.tracker.packets/1"
	FileName = "packets.json"
)

// Record is the complete session-readable projection of one packet.
type Record struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	Goal         string         `json:"goal"`
	Boundary     string         `json:"boundary"`
	DoneWhen     string         `json:"done_when"`
	Check        string         `json:"check"`
	Context      string         `json:"context"`
	Status       string         `json:"status"`
	Version      uint64         `json:"version"`
	TakenBy      *string        `json:"taken_by"`
	Comments     []Comment      `json:"comments"`
	Evidence     []string       `json:"evidence"`
	ParentID     *string        `json:"parent_id"`
	SupersededBy *string        `json:"superseded_by"`
	Closure      *Closure       `json:"closure"`
	History      []HistoryEvent `json:"history"`
}

type Comment struct {
	EventID   string `json:"event_id"`
	Timestamp string `json:"timestamp"`
	Actor     string `json:"actor"`
	Text      string `json:"text"`
}

type Closure struct {
	EventID   string `json:"event_id"`
	Timestamp string `json:"timestamp"`
	Actor     string `json:"actor"`
	Reason    string `json:"reason"`
}

type Body struct {
	Goal     string `json:"goal"`
	Boundary string `json:"boundary"`
	DoneWhen string `json:"done_when"`
	Check    string `json:"check"`
	Context  string `json:"context"`
}

// HistoryEvent is a tagged event record. Fields that do not apply to an event kind are
// absent, while metadata is present on every event.
type HistoryEvent struct {
	Kind          string   `json:"kind"`
	EventID       string   `json:"event_id"`
	Timestamp     string   `json:"timestamp"`
	Actor         string   `json:"actor"`
	TenantID      *string  `json:"tenant_id,omitempty"`
	Body          *Body    `json:"body,omitempty"`
	ParentID      *string  `json:"parent_id,omitempty"`
	Text          *string  `json:"text,omitempty"`
	From          *string  `json:"from,omitempty"`
	To            *string  `json:"to,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	ReplacementID *string  `json:"replacement_id,omitempty"`
	Reason        *string  `json:"reason,omitempty"`
}

// Verified contains an integrity- and freshness-checked packet export.
type Verified struct {
	Envelope contract.Envelope
	Packets  []Record
}

// Build snapshots every packet and wraps the sorted records in the shared envelope.
func Build(tracker *packet.Tracker, publication contract.Publication) (contract.Envelope, error) {
	packets, err := tracker.Packets()
	if err != nil {
		return contract.Envelope{}, err
	}
	records := make([]Record, 0, len(packets))
	for _, projected := range packets {
		history, err := tracker.History(projected.ID())
		if err != nil {
			return contract.Envelope{}, err
		}
		records = append(records, makeRecord(projected, history))
	}
	return buildRecords(records, publication)
}

func buildRecords(records []Record, publication contract.Publication) (contract.Envelope, error) {
	records = cloneRecords(records)
	sort.Slice(records, func(left, right int) bool {
		return records[left].ID < records[right].ID
	})
	return contract.Build(Schema, records, publication)
}

// Serialize snapshots and canonically serializes every packet.
func Serialize(tracker *packet.Tracker, publication contract.Publication) ([]byte, contract.Envelope, error) {
	envelope, err := Build(tracker, publication)
	if err != nil {
		return nil, contract.Envelope{}, err
	}
	serialized, err := contract.Serialize(envelope)
	if err != nil {
		return nil, contract.Envelope{}, err
	}
	return serialized, envelope, nil
}

// Verify validates export bytes and decodes the packet records only after freshness and
// digest checks succeed.
func Verify(contents []byte, now time.Time) (Verified, error) {
	envelope, err := contract.Verify(contents, Schema, now)
	if err != nil {
		return Verified{}, err
	}
	return decodeVerified(envelope)
}

// VerifyFile reads a local export and preserves the shared not-found distinction.
func VerifyFile(path string, now time.Time) (Verified, error) {
	envelope, err := contract.VerifyFile(path, Schema, now)
	if err != nil {
		return Verified{}, err
	}
	return decodeVerified(envelope)
}

// Publish resolves real git provenance, then writes packets.json to a local directory.
// Provenance is resolved before the output directory is created, so failure emits nothing.
func Publish(ctx context.Context, tracker *packet.Tracker, outputDirectory, repositoryRoot string, publishedAt time.Time) (string, contract.Envelope, error) {
	source, err := contract.ResolveGitSource(ctx, repositoryRoot)
	if err != nil {
		return "", contract.Envelope{}, err
	}
	serialized, envelope, err := Serialize(tracker, contract.Publication{PublishedAt: publishedAt, Source: source})
	if err != nil {
		return "", contract.Envelope{}, err
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return "", contract.Envelope{}, err
	}
	path := filepath.Join(outputDirectory, FileName)
	if err := writeAtomic(path, serialized); err != nil {
		return "", contract.Envelope{}, err
	}
	return path, envelope, nil
}

func decodeVerified(envelope contract.Envelope) (Verified, error) {
	decoder := json.NewDecoder(bytes.NewReader(envelope.Payload))
	decoder.DisallowUnknownFields()
	var records []Record
	if err := decoder.Decode(&records); err != nil {
		return Verified{}, fmt.Errorf("%w: decode packet payload: %v", contract.ErrInvalidExport, err)
	}
	if err := requireEOF(decoder); err != nil {
		return Verified{}, fmt.Errorf("%w: %v", contract.ErrInvalidExport, err)
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if err := validateRecord(record); err != nil {
			return Verified{}, err
		}
		if _, duplicate := seen[record.ID]; duplicate {
			return Verified{}, fmt.Errorf("%w: duplicate packet id %q", contract.ErrInvalidExport, record.ID)
		}
		seen[record.ID] = struct{}{}
	}
	return Verified{Envelope: envelope, Packets: cloneRecords(records)}, nil
}

func makeRecord(projected packet.Packet, history []packet.Event) Record {
	body := projected.Body()
	record := Record{
		ID:       string(projected.ID()),
		TenantID: string(projected.TenantID()),
		Goal:     body.Goal,
		Boundary: body.Boundary,
		DoneWhen: body.DoneWhen,
		Check:    body.Check,
		Context:  body.Context,
		Status:   string(projected.Status()),
		Version:  uint64(projected.Version()),
		Comments: make([]Comment, 0, len(projected.Comments())),
		Evidence: make([]string, 0, len(projected.Evidence())),
		History:  make([]HistoryEvent, 0, len(history)),
	}
	if takenBy, ok := projected.TakenBy(); ok {
		record.TakenBy = stringPointer(string(takenBy))
	}
	for _, comment := range projected.Comments() {
		record.Comments = append(record.Comments, Comment{
			EventID: string(comment.EventID), Timestamp: formatTime(comment.At), Actor: string(comment.Actor), Text: comment.Text,
		})
	}
	for _, evidence := range projected.Evidence() {
		record.Evidence = append(record.Evidence, string(evidence))
	}
	if parentID, ok := projected.ParentID(); ok {
		record.ParentID = stringPointer(string(parentID))
	}
	if replacementID, ok := projected.SupersededBy(); ok {
		record.SupersededBy = stringPointer(string(replacementID))
	}
	if closure, ok := projected.Closure(); ok {
		record.Closure = &Closure{
			EventID: string(closure.EventID), Timestamp: formatTime(closure.At), Actor: string(closure.Actor), Reason: string(closure.Reason),
		}
	}
	for _, event := range history {
		record.History = append(record.History, makeHistoryEvent(event))
	}
	return record
}

func makeHistoryEvent(event packet.Event) HistoryEvent {
	meta := event.Metadata()
	record := HistoryEvent{
		Kind: string(event.Kind()), EventID: string(meta.ID), Timestamp: formatTime(meta.At), Actor: string(meta.Actor),
	}
	switch event := event.(type) {
	case packet.PacketIssued:
		record.TenantID = stringPointer(string(event.TenantID))
		record.Body = &Body{
			Goal: event.Body.Goal, Boundary: event.Body.Boundary, DoneWhen: event.Body.DoneWhen,
			Check: event.Body.Check, Context: event.Body.Context,
		}
		if event.ParentID != "" {
			record.ParentID = stringPointer(string(event.ParentID))
		}
	case packet.PacketCommented:
		record.Text = stringPointer(event.Text)
	case packet.PacketStatusTransitioned:
		record.From = stringPointer(string(event.From))
		record.To = stringPointer(string(event.To))
		for _, evidence := range event.Evidence {
			record.Evidence = append(record.Evidence, string(evidence))
		}
	case packet.PacketSuperseded:
		record.ReplacementID = stringPointer(string(event.ReplacementID))
	case packet.PacketClosed:
		record.Reason = stringPointer(string(event.Reason))
	}
	return record
}

func validateRecord(record Record) error {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.TenantID) == "" {
		return fmt.Errorf("%w: packet id and tenant_id are required", contract.ErrInvalidExport)
	}
	switch packet.Status(record.Status) {
	case packet.StatusNotStarted, packet.StatusInProgress, packet.StatusDone, packet.StatusBlocked:
	default:
		return fmt.Errorf("%w: packet %q has unsupported status %q", contract.ErrInvalidExport, record.ID, record.Status)
	}
	if record.Version != uint64(len(record.History)) {
		return fmt.Errorf("%w: packet %q version %d does not match %d history events", contract.ErrInvalidExport, record.ID, record.Version, len(record.History))
	}
	for _, event := range record.History {
		if event.Kind == "" || event.EventID == "" || event.Actor == "" {
			return fmt.Errorf("%w: packet %q history metadata is incomplete", contract.ErrInvalidExport, record.ID)
		}
		if _, err := time.Parse(time.RFC3339, event.Timestamp); err != nil {
			return fmt.Errorf("%w: packet %q history timestamp must be RFC 3339", contract.ErrInvalidExport, record.ID)
		}
	}
	return nil
}

func cloneRecords(records []Record) []Record {
	if records == nil {
		return nil
	}
	cloned := make([]Record, len(records))
	for index, record := range records {
		cloned[index] = record
		if record.Comments != nil {
			cloned[index].Comments = append([]Comment{}, record.Comments...)
		}
		if record.Evidence != nil {
			cloned[index].Evidence = append([]string{}, record.Evidence...)
		}
		if record.History != nil {
			cloned[index].History = make([]HistoryEvent, len(record.History))
		}
		for eventIndex, event := range record.History {
			cloned[index].History[eventIndex] = event
			if event.Evidence != nil {
				cloned[index].History[eventIndex].Evidence = append([]string{}, event.Evidence...)
			}
			if event.Body != nil {
				body := *event.Body
				cloned[index].History[eventIndex].Body = &body
			}
			cloned[index].History[eventIndex].TenantID = cloneString(event.TenantID)
			cloned[index].History[eventIndex].ParentID = cloneString(event.ParentID)
			cloned[index].History[eventIndex].Text = cloneString(event.Text)
			cloned[index].History[eventIndex].From = cloneString(event.From)
			cloned[index].History[eventIndex].To = cloneString(event.To)
			cloned[index].History[eventIndex].ReplacementID = cloneString(event.ReplacementID)
			cloned[index].History[eventIndex].Reason = cloneString(event.Reason)
		}
		if record.Closure != nil {
			closure := *record.Closure
			cloned[index].Closure = &closure
		}
		cloned[index].TakenBy = cloneString(record.TakenBy)
		cloned[index].ParentID = cloneString(record.ParentID)
		cloned[index].SupersededBy = cloneString(record.SupersededBy)
	}
	return cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPointer(*value)
}

func stringPointer(value string) *string { return &value }

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

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

func writeAtomic(path string, contents []byte) (result error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".packets-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if result != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
