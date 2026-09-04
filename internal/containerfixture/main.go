// Command containerfixture serves fresh, synthetic exports for the container smoke test.
// It is compiled only into Dockerfile's fixture target, never into the production image.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/runtimeexport"
	"github.com/martcoca/work-tracker/tenant"
	"google.golang.org/grpc"
)

func main() {
	if err := serveEmptyFirestore(); err != nil {
		log.Fatal(err)
	}
	documents, err := buildDocuments(time.Now().UTC().Truncate(time.Second))
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	for path, contents := range documents {
		contents := contents
		mux.HandleFunc("GET "+path, func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write(contents)
		})
	}
	server := &http.Server{
		Addr: ":18080", Handler: mux, ReadHeaderTimeout: 2 * time.Second,
	}
	log.Printf("synthetic container exports listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

// serveEmptyFirestore gives the production binary a protocol-real, credential-free empty
// database during its container smoke test. It is compiled only into the fixture target;
// the production binary has no memory-store or emulator fallback.
func serveEmptyFirestore() error {
	listener, err := net.Listen("tcp", ":18081")
	if err != nil {
		return fmt.Errorf("listen for synthetic Firestore: %w", err)
	}
	server := grpc.NewServer()
	firestorepb.RegisterFirestoreServer(server, emptyFirestore{})
	go func() {
		if err := server.Serve(listener); err != nil {
			log.Printf("synthetic Firestore stopped: %v", err)
		}
	}()
	return nil
}

type emptyFirestore struct {
	firestorepb.UnimplementedFirestoreServer
}

func (emptyFirestore) RunQuery(*firestorepb.RunQueryRequest, firestorepb.Firestore_RunQueryServer) error {
	return nil
}

func buildDocuments(publishedAt time.Time) (map[string][]byte, error) {
	tenantID := "tenant-synthetic"
	body := packetexport.Body{
		Goal:     "Prove the production container serves verified runtime exports.",
		Boundary: "Synthetic container fixture only.",
		DoneWhen: "The health endpoint and packet view respond.",
		Check:    "scripts/container/check-runtime.sh",
		Context:  "The fixture is not part of the production image.",
	}
	record := packetexport.Record{
		ID: "0004-E02-T03", TenantID: tenantID, Goal: body.Goal, Boundary: body.Boundary,
		DoneWhen: body.DoneWhen, Check: body.Check, Context: body.Context,
		Status: "not started", Version: 1, Comments: []packetexport.Comment{}, Evidence: []string{},
		History: []packetexport.HistoryEvent{{
			Kind: "packet issued", EventID: "event-container-fixture", Timestamp: publishedAt.Format(time.RFC3339),
			Actor: "human-synthetic", TenantID: &tenantID, Body: &body,
		}},
	}
	directory := []tenant.Record{{
		ID: tenantID, Slug: "tenant-synthetic", DisplayName: "Synthetic Tenant",
		Status: tenant.StatusActive, CreatedAt: "2025-01-01T00:00:00Z", Version: 1,
	}}
	grants := []map[string]any{{
		"subject": "agent-synthetic", "tenant_id": tenantID, "version": 1,
	}}
	source := contract.Source{Repository: "synthetic/source", Commit: strings.Repeat("a", 40)}
	inputs := []struct {
		path    string
		schema  string
		payload any
	}{
		{path: "/packets.json", schema: packetexport.Schema, payload: []packetexport.Record{record}},
		{path: "/tenant-directory.json", schema: tenant.Schema, payload: directory},
		{path: "/agent-grants.json", schema: runtimeexport.AgentGrantsSchema, payload: grants},
	}
	documents := make(map[string][]byte, len(inputs))
	for _, input := range inputs {
		envelope, err := contract.Build(input.schema, input.payload, contract.Publication{
			PublishedAt: publishedAt, Source: source,
		})
		if err != nil {
			return nil, fmt.Errorf("build %s: %w", input.path, err)
		}
		serialized, err := contract.Serialize(envelope)
		if err != nil {
			return nil, fmt.Errorf("serialize %s: %w", input.path, err)
		}
		documents[input.path] = serialized
	}
	return documents, nil
}
