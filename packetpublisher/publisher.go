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

// Destination replaces packets.json atomically while retaining the previously released
// copy if any step fails.
type Destination interface {
	Publish(context.Context, []byte, string) error
}

// Baseline returns the last verified public packets.json. It is deliberately required:
// repository-only packets cannot be reconstructed from the app store during migration.
type Baseline func(time.Time) ([]byte, error)

type Publisher struct {
	tracker     *packet.Tracker
	lastGood    Baseline
	repository  Baseline
	destination Destination
	source      contract.Source
	now         func() time.Time
	mu          sync.Mutex
}

type Result struct {
	Digest      string
	PacketCount int
	PublishedAt time.Time
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
	baselineBytes, err := publisher.lastGood(now)
	if err != nil {
		return Result{}, fmt.Errorf("refuse publication without last verified export: %w", err)
	}
	baseline, err := packetexport.Verify(baselineBytes, now)
	if err != nil {
		return Result{}, fmt.Errorf("refuse publication with invalid baseline: %w", err)
	}
	repositoryBytes, err := publisher.repository(now)
	if err != nil {
		return Result{}, fmt.Errorf("refuse publication without verified repository migration source: %w", err)
	}
	repository, err := packetexport.Verify(repositoryBytes, now)
	if err != nil {
		return Result{}, fmt.Errorf("refuse publication with invalid repository migration source: %w", err)
	}
	appRecords, err := packetexport.Records(publisher.tracker)
	if err != nil {
		return Result{}, fmt.Errorf("refuse publication while durable store is unavailable: %w", err)
	}
	// The newest repository export replaces repository projections retained in the last
	// public union; the durable app log then wins packets that have actually migrated.
	reconciled := packetexport.Reconcile(baseline.Packets, repository.Packets)
	reconciled = packetexport.Reconcile(reconciled, appRecords)
	serialized, envelope, err := packetexport.SerializeRecords(reconciled, contract.Publication{
		PublishedAt: now,
		Source:      publisher.source,
	})
	if err != nil {
		return Result{}, fmt.Errorf("build reconciled packet export: %w", err)
	}
	// Verify with the shipped reader before any network write. This catches a publisher
	// regression at the same boundary a downstream session would reject.
	verified, err := packetexport.Verify(serialized, now)
	if err != nil {
		return Result{}, fmt.Errorf("refuse unverifiable packet export: %w", err)
	}
	message := fmt.Sprintf("app packets digest=%s source=%s commit=%s", envelope.Digest, envelope.Source.Repository, envelope.Source.Commit)
	if err := publisher.destination.Publish(ctx, serialized, message); err != nil {
		return Result{}, fmt.Errorf("publish packet export: %w", err)
	}
	return Result{Digest: envelope.Digest, PacketCount: len(verified.Packets), PublishedAt: now}, nil
}
