package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/eventstore"
	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packetpublisher"
	"github.com/martcoca/work-tracker/runtimeexport"
	"github.com/martcoca/work-tracker/surface"
	"golang.org/x/oauth2/google"
)

const (
	defaultRepositoryPacketURL = "https://tracker.martcoca.com/repository-packets.json"
	appProvenanceRepository    = "tracker.martcoca.com/app"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		return errors.New("FIREBASE_PROJECT_ID is required")
	}
	config, err := runtimeexport.ConfigFromEnvironment()
	if err != nil {
		return err
	}
	exports, err := runtimeexport.New(config, nil, func(err error) {
		log.Printf("export refresh failed; retaining last verified copies: %v", err)
	})
	if err != nil {
		return err
	}
	if err := exports.Start(context.Background()); err != nil {
		return err
	}
	verifier, err := identity.NewFirebaseVerifier(projectID, nil)
	if err != nil {
		return err
	}
	store, storeErr := eventstore.NewFirestore(context.Background(), eventstore.Config{
		ProjectID:  projectID,
		DatabaseID: valueOrDefault("FIRESTORE_DATABASE_ID", eventstore.DefaultDatabaseID),
	})
	var service *surface.Service
	if storeErr == nil {
		defer store.Close()
		service, storeErr = surface.NewServiceFromSourceWithStore(exports, verifier, store)
	}
	if storeErr == nil {
		storeErr = enableAppPublication(context.Background(), service, exports, config.FetchTimeout)
	}
	if storeErr != nil {
		// The public export is a separate durable copy. A Firestore or publisher outage
		// therefore degrades the process to reads from that last verified copy; it must
		// never enable the in-memory authoring store used by local callers and tests.
		log.Printf("durable authoring unavailable; starting from last verified export: %v", storeErr)
		service, err = surface.NewReadOnlyServiceFromSource(exports, verifier)
		if err != nil {
			return err
		}
	}
	server := &http.Server{
		Addr:              ":" + valueOrDefault("PORT", "8080"),
		Handler:           service.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("tracker human API listening on %s", server.Addr)
	return server.ListenAndServe()
}

func enableAppPublication(ctx context.Context, service *surface.Service, exports *runtimeexport.Reader, fetchTimeout time.Duration) error {
	siteID := os.Getenv("HOSTING_SITE_ID")
	commit := os.Getenv("SOURCE_COMMIT")
	source := contract.Source{Repository: appProvenanceRepository, Commit: commit}
	if err := contract.ValidateSource(source); err != nil {
		return err
	}
	repository, err := packetpublisher.NewHTTPBaseline(
		valueOrDefault("REPOSITORY_PACKET_EXPORT_URL", defaultRepositoryPacketURL), nil, fetchTimeout,
	)
	if err != nil {
		return err
	}
	// Refuse authoring at startup if the migration source is not currently verifiable.
	// The public union remains readable, but a new issue could not safely retain git-only
	// packets without this independently published source.
	if _, err := repository.Verified(time.Now().UTC()); err != nil {
		return err
	}
	authenticatedClient, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/firebase.hosting")
	if err != nil {
		return fmt.Errorf("initialize keyless Hosting client: %w", err)
	}
	destination, err := packetpublisher.NewHostingDestination(siteID, authenticatedClient)
	if err != nil {
		return err
	}
	publisher, err := packetpublisher.New(
		service.AuthoredTracker(),
		func(at time.Time) ([]byte, error) { return exports.VerifiedCopy(runtimeexport.Packets, at) },
		repository.Verified,
		destination,
		source,
	)
	if err != nil {
		return err
	}
	return service.EnableIssuePublication(func(requestContext context.Context) {
		result, publishErr := publisher.Publish(requestContext)
		if publishErr != nil {
			log.Printf("packet issue is durable but publication refused; last good export retained: %v", publishErr)
			return
		}
		log.Printf("published app packet export: packets=%d digest=%s", result.PacketCount, result.Digest)
		if refreshErr := exports.Refresh(requestContext); refreshErr != nil {
			log.Printf("published packet export; local reader will retry refresh: %v", refreshErr)
		}
	})
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
