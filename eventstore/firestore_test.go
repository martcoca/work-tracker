package eventstore

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/packet"
)

func TestEventDocumentsRoundTripEveryPacketEvent(t *testing.T) {
	meta := packet.Metadata{
		ID: "event-round-trip", At: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC), Actor: "actor-synthetic",
	}
	events := []packet.Event{
		packet.PacketIssued{Meta: meta, PacketID: "0004-E07-T99", TenantID: "tenant-synthetic", Body: packet.Body{Goal: "goal"}, ParentID: "0004-E07-T98"},
		packet.PacketTaken{Meta: meta, PacketID: "0004-E07-T99"},
		packet.PacketCommented{Meta: meta, PacketID: "0004-E07-T99", Text: "comment"},
		packet.PacketStatusTransitioned{Meta: meta, PacketID: "0004-E07-T99", From: packet.StatusInProgress, To: packet.StatusDone, Evidence: []packet.Evidence{"evidence/synthetic.md"}},
		packet.PacketSuperseded{Meta: meta, PacketID: "0004-E07-T99", ReplacementID: "0004-E07-T100"},
		packet.PacketClosed{Meta: meta, PacketID: "0004-E07-T99", Reason: packet.CloseReasonDone},
	}
	for _, event := range events {
		t.Run(string(event.Kind()), func(t *testing.T) {
			payload, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			document := eventDocument{
				SchemaVersion: eventSchema, PacketID: "0004-E07-T99", StreamVersion: 1,
				Kind: string(event.Kind()), Payload: payload,
			}
			record, err := decodeRecord(document)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(record.Event, event) {
				t.Fatalf("round trip differs\n got: %#v\nwant: %#v", record.Event, event)
			}
		})
	}
}

func TestEventDocumentsFailClosedOnUnknownSchemaAndKind(t *testing.T) {
	for name, document := range map[string]eventDocument{
		"schema": {SchemaVersion: eventSchema + 1, PacketID: "packet", StreamVersion: 1, Kind: string(packet.EventPacketIssued), Payload: []byte(`{}`)},
		"kind":   {SchemaVersion: eventSchema, PacketID: "packet", StreamVersion: 1, Kind: "invented", Payload: []byte(`{}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRecord(document); err == nil {
				t.Fatal("invalid durable event was accepted")
			}
		})
	}
}

func TestPathIDCannotCreateNestedFirestorePaths(t *testing.T) {
	if got := pathID("packet/with/slashes"); got == "" || got == "packet/with/slashes" {
		t.Fatalf("path id = %q", got)
	}
}
