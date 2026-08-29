package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/martcoca/work-tracker/identity"
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
	packetPath := valueOrDefault("PACKET_EXPORT_PATH", "/data/packets.json")
	directoryPath := valueOrDefault("TENANT_DIRECTORY_PATH", "/data/tenant-directory.json")
	packetContents, err := os.ReadFile(packetPath)
	if err != nil {
		return fmt.Errorf("read packet export: %w", err)
	}
	directoryContents, err := os.ReadFile(directoryPath)
	if err != nil {
		return fmt.Errorf("read tenant directory: %w", err)
	}
	snapshot, err := surface.NewSnapshot(packetContents, directoryContents)
	if err != nil {
		return fmt.Errorf("load held exports: %w", err)
	}
	verifier, err := identity.NewFirebaseVerifier(projectID, nil)
	if err != nil {
		return err
	}
	service, err := surface.NewService(snapshot, verifier)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              ":" + valueOrDefault("PORT", "8080"),
		Handler:           service.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("read-only tracker API listening on %s", server.Addr)
	return server.ListenAndServe()
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
