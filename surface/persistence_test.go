package surface

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestStoreOutageStartsReadOnlyFromLastVerifiedExportAndRefusesAuthoring(t *testing.T) {
	snapshot := testSnapshot(t, surfaceClock.Add(-30*time.Minute))
	source := staticSnapshotSource{snapshot: snapshot}
	verifier := verifierFunc(func(_ context.Context, token string) (identity.Principal, error) {
		return identity.Principal{Subject: token, TenantID: "tenant-a"}, nil
	})
	failedStore := &recordingEventStore{}
	if _, err := NewServiceFromSourceWithStore(source, verifier, failedStore); err == nil {
		t.Fatal("unreachable store unexpectedly enabled durable authoring")
	}
	service, err := NewReadOnlyServiceFromSource(source, verifier)
	if err != nil {
		t.Fatalf("start read-only service: %v", err)
	}

	read := httptest.NewRequest(http.MethodGet, "/api/initiatives", nil)
	read.Header.Set("Authorization", "Bearer human-a")
	readResponse := httptest.NewRecorder()
	service.Handler().ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusOK || !strings.Contains(readResponse.Body.String(), `"0004"`) {
		t.Fatalf("last good export read = %d %s", readResponse.Code, readResponse.Body.String())
	}

	write := httptest.NewRequest(http.MethodPost, "/api/initiatives/0004/epics/E02/drafts", bytes.NewBufferString(`{}`))
	write.Header.Set("Authorization", "Bearer human-a")
	write.Header.Set("Content-Type", "application/json")
	writeResponse := httptest.NewRecorder()
	service.Handler().ServeHTTP(writeResponse, write)
	if writeResponse.Code != http.StatusServiceUnavailable || !strings.Contains(writeResponse.Body.String(), `"code":"store_unavailable"`) {
		t.Fatalf("authoring refusal = %d %s", writeResponse.Code, writeResponse.Body.String())
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
