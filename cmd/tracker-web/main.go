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
	"github.com/martcoca/work-tracker/credentialstore"
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
	// renewalAttemptTimeout bounds one renewal check, including a Hosting release.
	renewalAttemptTimeout = 2 * time.Minute
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
	databaseID := valueOrDefault("FIRESTORE_DATABASE_ID", eventstore.DefaultDatabaseID)
	packetStore, storeErr := eventstore.NewFirestore(context.Background(), eventstore.Config{
		ProjectID:  projectID,
		DatabaseID: databaseID,
	})
	var credentialStore *credentialstore.Firestore
	if storeErr == nil {
		credentialStore, storeErr = credentialstore.NewFirestore(context.Background(), credentialstore.Config{
			ProjectID: projectID, DatabaseID: databaseID,
		})
	}
	var service *surface.Service
	if storeErr == nil {
		service, storeErr = surface.NewServiceFromSourceWithStores(exports, verifier, packetStore, credentialStore)
	}
	var publisher *packetpublisher.Publisher
	if storeErr == nil {
		publisher, storeErr = enableAppPublication(context.Background(), service, exports, config.FetchTimeout)
	}
	if storeErr != nil {
		if credentialStore != nil {
			_ = credentialStore.Close()
		}
		if packetStore != nil {
			_ = packetStore.Close()
		}
		// The public export is a separate durable copy. A Firestore or publisher outage
		// therefore degrades the process to reads from that last verified copy; it must
		// never enable the in-memory authoring store used by local callers and tests.
		log.Printf("durable authoring unavailable; starting from last verified export: %v", storeErr)
		service, err = surface.NewReadOnlyServiceFromSource(exports, verifier)
		if err != nil {
			return err
		}
	} else {
		defer packetStore.Close()
		defer credentialStore.Close()
		// Nothing else renews packets.json between deploys, and it expires. Check at once and
		// then on every refresh interval while this instance lives. The check runs in the
		// background: blocking startup on a Hosting release could fail the startup probe.
		if err := publisher.Keep(context.Background(), config.RefreshInterval, renewalAttemptTimeout, func(result packetpublisher.Result, released bool, err error) {
			switch {
			case err != nil:
				log.Printf("packet export renewal refused; last good export retained: %v", err)
			case released:
				log.Printf("renewed app packet export: packets=%d digest=%s", result.PacketCount, result.Digest)
				if refreshErr := exports.Refresh(context.Background()); refreshErr != nil {
					log.Printf("renewed packet export; local reader will retry refresh: %v", refreshErr)
				}
			}
		}); err != nil {
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

func enableAppPublication(ctx context.Context, service *surface.Service, exports *runtimeexport.Reader, fetchTimeout time.Duration) (*packetpublisher.Publisher, error) {
	siteID := os.Getenv("HOSTING_SITE_ID")
	commit := os.Getenv("SOURCE_COMMIT")
	source := contract.Source{Repository: appProvenanceRepository, Commit: commit}
	if err := contract.ValidateSource(source); err != nil {
		return nil, err
	}
	repository, err := packetpublisher.NewHTTPBaseline(
		valueOrDefault("REPOSITORY_PACKET_EXPORT_URL", defaultRepositoryPacketURL), nil, fetchTimeout,
	)
	if err != nil {
		return nil, err
	}
	// Refuse authoring at startup if the migration source is not intact. The public union
	// remains readable, but a new issue could not safely retain git-only packets without
	// this independently published source.
	if _, err := repository.Verified(time.Now().UTC()); err != nil {
		return nil, err
	}
	authenticatedClient, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/firebase.hosting")
	if err != nil {
		return nil, fmt.Errorf("initialize keyless Hosting client: %w", err)
	}
	destination, err := packetpublisher.NewHostingDestination(siteID, authenticatedClient)
	if err != nil {
		return nil, err
	}
	publisher, err := packetpublisher.New(
		service.AuthoredTracker(),
		func(time.Time) ([]byte, error) { return exports.IntactCopy(runtimeexport.Packets) },
		repository.Verified,
		destination,
		source,
	)
	if err != nil {
		return nil, err
	}
	return publisher, service.EnableIssuePublication(func(requestContext context.Context) {
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
