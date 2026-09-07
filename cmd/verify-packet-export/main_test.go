package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packetexport"
)

func TestVerifyAndCopyPreservesExactVerifiedBytes(t *testing.T) {
	now := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	envelope, err := packetexport.BuildRecords([]packetexport.Record{}, contract.Publication{
		PublishedAt: now,
		Source: contract.Source{
			Repository: "martcoca/work-tracker",
			Commit:     strings.Repeat("a", 40),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := contract.Serialize(envelope)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "downloaded.json")
	output := filepath.Join(t.TempDir(), "dist", "packets.json")
	if err := os.WriteFile(input, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyAndCopy(input, output, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(copied, contents) {
		t.Fatal("verified copy changed bytes")
	}

	tampered := bytes.Replace(contents, []byte(`"payload":[]`), []byte(`"payload":[{}]`), 1)
	if err := os.WriteFile(input, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	refused := filepath.Join(t.TempDir(), "must-not-exist.json")
	if _, err := verifyAndCopy(input, refused, now.Add(time.Hour)); err == nil {
		t.Fatal("tampered export was copied")
	}
	if _, err := os.Stat(refused); !os.IsNotExist(err) {
		t.Fatalf("refused output exists: %v", err)
	}
}

func TestRenewAndCopyPreservesExpiredAppUnionAndPayloadDigest(t *testing.T) {
	published := time.Date(2035, time.March, 4, 10, 0, 0, 0, time.UTC)
	record := packetexport.Record{
		ID: "0004-E07-T90", TenantID: "tenant-synthetic", Goal: "app only", Boundary: "boundary",
		DoneWhen: "done", Check: "check", Context: "context", Status: "not started", Version: 1,
		Comments: []packetexport.Comment{}, Evidence: []string{}, History: []packetexport.HistoryEvent{{
			Kind: "packet issued", EventID: "event-app", Timestamp: published.Format(time.RFC3339), Actor: "actor",
		}},
	}
	oldEnvelope, err := packetexport.BuildRecords([]packetexport.Record{record}, contract.Publication{
		PublishedAt: published,
		Source:      contract.Source{Repository: "tracker.martcoca.com/app", Commit: strings.Repeat("a", 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, _ := contract.Serialize(oldEnvelope)
	input := filepath.Join(t.TempDir(), "expired-app-union.json")
	output := filepath.Join(t.TempDir(), "dist", "packets.json")
	if err := os.WriteFile(input, oldBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	now := published.Add(2 * contract.FreshnessBound)
	renewed, err := renewAndCopy(input, output, now, contract.Source{
		Repository: "martcoca/work-tracker", Commit: strings.Repeat("b", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Envelope.Digest != oldEnvelope.Digest || renewed.Packets[0].Goal != "app only" {
		t.Fatalf("renewal lost app union: %+v", renewed)
	}
	if renewed.Envelope.Source.Repository != "martcoca/work-tracker" || renewed.Envelope.PublishedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("renewed provenance/time = %+v", renewed.Envelope)
	}
	if _, err := packetexport.VerifyFile(output, now); err != nil {
		t.Fatalf("renewed output: %v", err)
	}
}
