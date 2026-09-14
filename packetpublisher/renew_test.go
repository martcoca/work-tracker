package packetpublisher

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packet"
	"github.com/martcoca/work-tracker/packetexport"
)

var repositorySource = contract.Source{
	Repository: "martcoca/work-tracker",
	Commit:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
}

// liveHost stands in for Hosting: what the publisher releases becomes, when the test says
// so, what the next read of the live export returns.
type liveHost struct {
	mu         sync.Mutex
	live       []byte
	repository []byte
	released   [][]byte
}

func (host *liveHost) Publish(_ context.Context, contents []byte, _ string) error {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.released = append(host.released, append([]byte(nil), contents...))
	return nil
}

func (host *liveHost) liveCopy(time.Time) ([]byte, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	return append([]byte(nil), host.live...), nil
}

func (host *liveHost) repositoryCopy(time.Time) ([]byte, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	return append([]byte(nil), host.repository...), nil
}

func (host *liveHost) releases() int {
	host.mu.Lock()
	defer host.mu.Unlock()
	return len(host.released)
}

// catchUp makes the last release the live export, as Hosting does once it serves it.
func (host *liveHost) catchUp() {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.live = append([]byte(nil), host.released[len(host.released)-1]...)
}

func (host *liveHost) setLive(contents []byte) {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.live = contents
}

func (host *liveHost) setRepository(contents []byte) {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.repository = contents
}

type countingStore struct {
	mu    sync.Mutex
	store *packet.MemoryEventStore
	loads int
	fail  bool
}

func (store *countingStore) Load(ids ...packet.PacketID) ([]packet.EventRecord, error) {
	store.mu.Lock()
	store.loads++
	fail := store.fail
	store.mu.Unlock()
	if fail {
		return nil, errSyntheticStore
	}
	return store.store.Load(ids...)
}

func (store *countingStore) Append(expected map[packet.PacketID]packet.Version, records []packet.EventRecord) error {
	return store.store.Append(expected, records)
}

func (store *countingStore) loadCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.loads
}

type syntheticStoreError struct{}

func (syntheticStoreError) Error() string { return "synthetic store unavailable" }

var errSyntheticStore = syntheticStoreError{}

type renewFixture struct {
	host      *liveHost
	store     *countingStore
	publisher *Publisher
	now       time.Time
}

func newRenewFixture(t *testing.T, now time.Time, live, repository []byte) *renewFixture {
	t.Helper()
	store := &countingStore{store: packet.NewMemoryEventStore()}
	tracker, err := packet.NewTrackerWithStore(allowTenant{}, store)
	if err != nil {
		t.Fatal(err)
	}
	host := &liveHost{live: live, repository: repository}
	publisher, err := New(tracker, host.liveCopy, host.repositoryCopy, host, appSource)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &renewFixture{host: host, store: store, publisher: publisher, now: now}
	publisher.now = func() time.Time { return fixture.now }
	return fixture
}

func (fixture *renewFixture) renew(t *testing.T) bool {
	t.Helper()
	_, released, err := fixture.publisher.Renew(context.Background())
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	return released
}

func twoRepositoryPackets() []packetexport.Record {
	return []packetexport.Record{
		record("0004-E01-T01", "first", "event-first"),
		record("0004-E01-T02", "second", "event-second"),
	}
}

func TestRenewRepublishesAStaleUnionOnceAndThenLeavesItAlone(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	frozen := exportRecords(t, now.Add(-time.Hour), appSource, twoRepositoryPackets()[:1])
	repository := exportRecords(t, now.Add(-time.Hour), repositorySource, twoRepositoryPackets())
	fixture := newRenewFixture(t, now, frozen, repository)

	if !fixture.renew(t) {
		t.Fatal("a union missing a repository packet was not republished")
	}
	verified, err := packetexport.Verify(fixture.host.released[0], now)
	if err != nil {
		t.Fatalf("released export does not verify: %v", err)
	}
	if len(verified.Packets) != 2 || verified.Envelope.Source != appSource {
		t.Fatalf("released %d packets from %+v", len(verified.Packets), verified.Envelope.Source)
	}

	fixture.host.catchUp()
	fixture.now = now.Add(5 * time.Minute)
	if fixture.renew(t) {
		t.Fatal("an export that already matches its sources was republished")
	}
	loads := fixture.store.loadCount()
	fixture.now = now.Add(10 * time.Minute)
	if fixture.renew(t) || fixture.host.releases() != 1 {
		t.Fatalf("unchanged export republished: releases=%d", fixture.host.releases())
	}
	if fixture.store.loadCount() != loads {
		t.Fatalf("an unchanged, undue export still read the store: %d loads, was %d", fixture.store.loadCount(), loads)
	}
}

func TestRenewRepublishesAnExpiredExportFromIntactExpiredInputs(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	longAgo := now.Add(-3 * contract.FreshnessBound)
	live := exportRecords(t, longAgo, appSource, twoRepositoryPackets())
	repository := exportRecords(t, longAgo, repositorySource, twoRepositoryPackets())
	if _, err := packetexport.Verify(live, now); err == nil {
		t.Fatal("fixture export is not expired")
	}
	fixture := newRenewFixture(t, now, live, repository)

	if !fixture.renew(t) {
		t.Fatal("an expired export was not renewed")
	}
	verified, err := packetexport.Verify(fixture.host.released[0], now)
	if err != nil {
		t.Fatalf("renewed export is not fresh: %v", err)
	}
	if verified.Envelope.PublishedAt != now.Format(time.RFC3339Nano) || len(verified.Packets) != 2 {
		t.Fatalf("renewed envelope = %+v with %d packets", verified.Envelope, len(verified.Packets))
	}
}

func TestRenewRepublishesAnUnchangedExportOnceItReachesRenewalAge(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	publishedAt := now.Add(-RenewalAge + time.Minute)
	live := exportRecords(t, publishedAt, appSource, twoRepositoryPackets())
	repository := exportRecords(t, publishedAt, repositorySource, twoRepositoryPackets())
	fixture := newRenewFixture(t, now, live, repository)

	if fixture.renew(t) {
		t.Fatal("an unchanged export younger than RenewalAge was republished")
	}
	fixture.now = publishedAt.Add(RenewalAge)
	if !fixture.renew(t) {
		t.Fatal("an unchanged export at RenewalAge was not renewed")
	}
}

func TestRenewWaitsForAReleaseToGoLiveButNotForAnOverwrite(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	stale := exportRecords(t, now.Add(-time.Hour), appSource, twoRepositoryPackets()[:1])
	repository := exportRecords(t, now.Add(-time.Hour), repositorySource, twoRepositoryPackets())
	fixture := newRenewFixture(t, now, stale, repository)

	// Issuing a packet publishes on its own path; the live copy has not caught up.
	if _, err := fixture.publisher.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.now = now.Add(5 * time.Minute)
	if fixture.renew(t) {
		t.Fatal("republished the same payload over a live copy that had not caught up yet")
	}

	// A deploy renews the stale union: same payload, new publication time.
	fixture.host.setLive(exportRecords(t, now.Add(6*time.Minute), appSource, twoRepositoryPackets()[:1]))
	fixture.now = now.Add(7 * time.Minute)
	if !fixture.renew(t) {
		t.Fatal("a stale union written over the release was not corrected")
	}
	if fixture.host.releases() != 2 {
		t.Fatalf("releases = %d, want 2", fixture.host.releases())
	}
}

func TestRenewReleasesNothingWhenAnInputCannotBeTrusted(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	live := exportRecords(t, now.Add(-time.Hour), appSource, twoRepositoryPackets()[:1])
	repository := exportRecords(t, now.Add(-time.Hour), repositorySource, twoRepositoryPackets())
	tamper := func(contents []byte) []byte {
		return []byte(strings.Replace(string(contents), `"goal":"first"`, `"goal":"forged"`, 1))
	}
	tests := []struct {
		name  string
		setup func(*renewFixture)
		want  string
	}{
		{name: "store unavailable", setup: func(f *renewFixture) { f.store.fail = true }, want: "durable store is unavailable"},
		{name: "tampered live export", setup: func(f *renewFixture) { f.host.setLive(tamper(live)) }, want: "invalid baseline"},
		{name: "tampered repository export", setup: func(f *renewFixture) { f.host.setRepository(tamper(repository)) }, want: "invalid repository migration source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRenewFixture(t, now, live, repository)
			test.setup(fixture)
			_, released, err := fixture.publisher.Renew(context.Background())
			if err == nil || released || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("renew released=%v err=%v, want refusal containing %q", released, err, test.want)
			}
			if fixture.host.releases() != 0 {
				t.Fatalf("destination touched %d times", fixture.host.releases())
			}
		})
	}
}

func TestKeepRenewsAtOnceWithoutBlockingAndThenOnItsInterval(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	stale := exportRecords(t, now.Add(-time.Hour), appSource, twoRepositoryPackets()[:1])
	repository := exportRecords(t, now.Add(-time.Hour), repositorySource, twoRepositoryPackets())

	t.Run("first check does not wait for the interval", func(t *testing.T) {
		fixture := newRenewFixture(t, now, stale, repository)
		released := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := fixture.publisher.Keep(ctx, time.Hour, time.Second, func(_ Result, ok bool, err error) {
			if err != nil {
				t.Errorf("renewal: %v", err)
			}
			if ok {
				released <- struct{}{}
			}
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-released:
		case <-time.After(5 * time.Second):
			t.Fatal("the first renewal waited for the interval")
		}
	})

	t.Run("does not block its caller", func(t *testing.T) {
		fixture := newRenewFixture(t, now, stale, repository)
		gate := make(chan struct{})
		fixture.publisher.destination = blockingDestination{gate: gate}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		returned := make(chan struct{})
		go func() {
			_ = fixture.publisher.Keep(ctx, time.Hour, time.Minute, nil)
			close(returned)
		}()
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
			t.Fatal("Keep blocked its caller on a release that had not finished")
		}
		close(gate)
	})

	t.Run("keeps checking on its interval", func(t *testing.T) {
		fixture := newRenewFixture(t, now, stale, repository)
		releases := make(chan struct{}, 4)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := fixture.publisher.Keep(ctx, 5*time.Millisecond, time.Second, func(_ Result, ok bool, err error) {
			if err != nil {
				t.Errorf("renewal: %v", err)
			}
			if ok {
				releases <- struct{}{}
			}
		}); err != nil {
			t.Fatal(err)
		}
		<-releases
		fixture.host.catchUp()
		fixture.host.setRepository(exportRecords(t, now, repositorySource, append(twoRepositoryPackets(),
			record("0004-E01-T03", "third", "event-third"))))
		// A deploy renews the live export, which is what makes the next check look again.
		fixture.host.setLive(exportRecords(t, now.Add(time.Minute), appSource, twoRepositoryPackets()))
		select {
		case <-releases:
		case <-time.After(5 * time.Second):
			t.Fatalf("scheduled renewal never released the changed union: releases=%d", fixture.host.releases())
		}
	})

	fixture := newRenewFixture(t, now, stale, repository)
	if err := fixture.publisher.Keep(context.Background(), 0, time.Second, nil); err == nil {
		t.Fatal("a zero interval was accepted")
	}
}

type blockingDestination struct{ gate chan struct{} }

func (destination blockingDestination) Publish(ctx context.Context, _ []byte, _ string) error {
	select {
	case <-destination.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
