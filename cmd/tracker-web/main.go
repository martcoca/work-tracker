package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/runtimeexport"
	"github.com/martcoca/work-tracker/surface"
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
	service, err := surface.NewServiceFromSource(exports, verifier)
	if err != nil {
		return err
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

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
