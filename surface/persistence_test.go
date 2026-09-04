package surface

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packet"
)

func TestDurableServiceRefusesMissingAuthorityBeforeReadingStore(t *testing.T) {
	store := &recordingEventStore{}
	source := unavailableSnapshotSource{}
	verifier := verifierFunc(func(context.Context, string) (identity.Principal, error) {
		return identity.Principal{}, identity.ErrInvalidToken
	})

	if _, err := NewServiceFromSourceWithStore(source, verifier, store); err == nil {
		t.Fatal("service started without a verified authority snapshot")
	}
	if store.loadCalled {
		t.Fatal("event store was read before authority was available")
	}
}

type unavailableSnapshotSource struct{}

func (unavailableSnapshotSource) CurrentSnapshot() *Snapshot { return nil }
func (unavailableSnapshotSource) ExportStatuses(time.Time) []HeldExportStatus {
	return nil
}

type recordingEventStore struct{ loadCalled bool }

func (store *recordingEventStore) Load(...packet.PacketID) ([]packet.EventRecord, error) {
	store.loadCalled = true
	return nil, errors.New("store must not be read")
}

func (*recordingEventStore) Append(map[packet.PacketID]packet.Version, []packet.EventRecord) error {
	return nil
}
