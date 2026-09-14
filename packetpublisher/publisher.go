// Package packetpublisher reconciles the transitional repository packet export with the
// app's durable event store and publishes the same verified wire contract.
package packetpublisher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packet"
	"github.com/martcoca/work-tracker/packetexport"
)

// RenewalAge is how old the live export may grow before Renew republishes an unchanged one.
// Half the lifetime leaves the other half for a renewal that fails or is not reached.
const RenewalAge = contract.FreshnessBound / 2

// Destination replaces packets.json atomically while retaining the previously released
// copy if any step fails.
type Destination interface {
	Publish(context.Context, []byte, string) error
}

// Baseline returns an earlier packet export as a merge input: the last public packets.json,
// or the repository migration source. It must be intact; it need not be fresh, because the
// publisher verifies what it releases in full and freshness binds what it publishes, not
// what it merges.
type Baseline func(time.Time) ([]byte, error)

type Publisher struct {
	tracker     *packet.Tracker
	lastGood    Baseline
	repository  Baseline
	destination Destination
	source      contract.Source
	now         func() time.Time
	mu          sync.Mutex

	// observed identifies the live export the last completed renewal check compared against.
	observed string
	// released is the last export this publisher released, and releasedOver the live export
	// it replaced. Together they tell a live copy that has not caught up with a release apart
	// from one that something else has since overwritten.
	released     Result
	releasedOver string
}

type Result struct {
	Digest      string
	PacketCount int
	PublishedAt time.Time
}

type candidate struct {
	serialized []byte
	envelope   contract.Envelope
	count      int
	observed   string
}

func New(tracker *packet.Tracker, lastGood, repository Baseline, destination Destination, source contract.Source) (*Publisher, error) {
	if tracker == nil || lastGood == nil || repository == nil || destination == nil {
		return nil, errors.New("tracker, verified public and repository baselines, and publication destination are required")
	}
	if err := contract.ValidateSource(source); err != nil {
		return nil, err
	}
	return &Publisher{
		tracker: tracker, lastGood: lastGood, repository: repository, destination: destination, source: source, now: time.Now,
	}, nil
}

// Publish rebuilds from the durable log before touching Hosting. A failed store read or
// failed baseline verification therefore cannot replace the last good public export with
// an incomplete or empty payload.
func (publisher *Publisher) Publish(ctx context.Context) (Result, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	now := publisher.now().UTC()
	live, err := publisher.liveExport(now)
	if err != nil {
		return Result{}, err
	}
	built, err := publisher.build(live, now)
	if err != nil {
		return Result{}, err
	}
	return publisher.release(ctx, built, now)
}

// Renew republishes only when the live export is expired or older than RenewalAge, or when
// it no longer matches what its sources reconcile to — which is how a deploy that renewed a
// stale union, or an export nobody has published for days, gets corrected without anyone
// issuing a packet. It reports whether it released anything.
//
// While the live export is unchanged since the last completed check and not yet due, Renew
// returns without reading the store: every change to the store that belongs in the export
// already publishes on its own path.
func (publisher *Publisher) Renew(ctx context.Context) (Result, bool, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	now := publisher.now().UTC()
	live, err := publisher.liveExport(now)
	if err != nil {
		return Result{}, false, err
	}
	due, err := renewalDue(live.Envelope, now)
	if err != nil {
		return Result{}, false, err
	}
	observed := observation(live.Envelope)
	if observed == publisher.observed && !due {
		return Result{}, false, nil
	}
	built, err := publisher.build(live, now)
	if err != nil {
		return Result{}, false, err
	}
	publisher.observed = observed
	if built.envelope.Digest == live.Envelope.Digest && !due {
		return Result{}, false, nil
	}
	if built.envelope.Digest == publisher.released.Digest && observed == publisher.releasedOver &&
		now.Before(publisher.released.PublishedAt.Add(RenewalAge)) {
		// The live copy is still the one this publisher already replaced with the same
		// payload; it has not caught up yet. Releasing again would only add a version.
		return Result{}, false, nil
	}
	result, err := publisher.release(ctx, built, now)
	if err != nil {
		return Result{}, false, err
	}
	return result, true, nil
}

// Keep runs one renewal check at once and then one per interval until ctx ends, in the
// background. It never blocks its caller: a Hosting release can outlast a cold start's
// startup probe, and an instance that cannot become ready renews nothing. Each check is
// bounded by attemptTimeout, and every outcome, including a check that released nothing,
// goes to report.
func (publisher *Publisher) Keep(ctx context.Context, interval, attemptTimeout time.Duration, report func(Result, bool, error)) error {
	if interval <= 0 || attemptTimeout <= 0 {
		return errors.New("renewal interval and attempt timeout must be positive")
	}
	if report == nil {
		report = func(Result, bool, error) {}
	}
	attempt := func() {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		defer cancel()
		report(publisher.Renew(attemptCtx))
	}
	go func() {
		attempt()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				attempt()
			}
		}
	}()
	return nil
}

func (publisher *Publisher) liveExport(now time.Time) (packetexport.Verified, error) {
	baselineBytes, err := publisher.lastGood(now)
	if err != nil {
		return packetexport.Verified{}, fmt.Errorf("refuse publication without last verified export: %w", err)
	}
	baseline, err := packetexport.VerifyIntegrity(baselineBytes)
	if err != nil {
		return packetexport.Verified{}, fmt.Errorf("refuse publication with invalid baseline: %w", err)
	}
	return baseline, nil
}

func (publisher *Publisher) build(live packetexport.Verified, now time.Time) (candidate, error) {
	repositoryBytes, err := publisher.repository(now)
	if err != nil {
		return candidate{}, fmt.Errorf("refuse publication without verified repository migration source: %w", err)
	}
	repository, err := packetexport.VerifyIntegrity(repositoryBytes)
	if err != nil {
		return candidate{}, fmt.Errorf("refuse publication with invalid repository migration source: %w", err)
	}
	appRecords, err := packetexport.Records(publisher.tracker)
	if err != nil {
		return candidate{}, fmt.Errorf("refuse publication while durable store is unavailable: %w", err)
	}
	// The newest repository export replaces repository projections retained in the last
	// public union; the durable app log then wins packets that have actually migrated.
	reconciled := packetexport.Reconcile(live.Packets, repository.Packets)
	reconciled = packetexport.Reconcile(reconciled, appRecords)
	serialized, envelope, err := packetexport.SerializeRecords(reconciled, contract.Publication{
		PublishedAt: now,
		Source:      publisher.source,
	})
	if err != nil {
		return candidate{}, fmt.Errorf("build reconciled packet export: %w", err)
	}
	// Verify with the shipped reader, freshness included, before any network write. This
	// catches a publisher regression at the same boundary a downstream session would reject.
	verified, err := packetexport.Verify(serialized, now)
	if err != nil {
		return candidate{}, fmt.Errorf("refuse unverifiable packet export: %w", err)
	}
	return candidate{
		serialized: serialized, envelope: envelope, count: len(verified.Packets),
		observed: observation(live.Envelope),
	}, nil
}

func (publisher *Publisher) release(ctx context.Context, built candidate, now time.Time) (Result, error) {
	message := fmt.Sprintf("app packets digest=%s source=%s commit=%s", built.envelope.Digest, built.envelope.Source.Repository, built.envelope.Source.Commit)
	if err := publisher.destination.Publish(ctx, built.serialized, message); err != nil {
		return Result{}, fmt.Errorf("publish packet export: %w", err)
	}
	result := Result{Digest: built.envelope.Digest, PacketCount: built.count, PublishedAt: now}
	publisher.released = result
	publisher.releasedOver = built.observed
	return result, nil
}

func renewalDue(live contract.Envelope, now time.Time) (bool, error) {
	publishedAt, err := time.Parse(time.RFC3339, live.PublishedAt)
	if err != nil {
		return false, fmt.Errorf("refuse publication with invalid baseline: %w", contract.ErrInvalidExport)
	}
	return !now.Before(publishedAt.Add(RenewalAge)), nil
}

func observation(live contract.Envelope) string {
	return live.Digest + " " + live.PublishedAt
}
