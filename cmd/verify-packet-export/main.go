// Command verify-packet-export verifies a local packets.json with the shipped reader and
// optionally copies those exact bytes into a Hosting tree. It lets deployment preserve
// the app's last good union while publishing the repository-only migration source beside it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packetexport"
)

func main() {
	var input string
	var output string
	var renewRepository string
	var renewCommit string
	flag.StringVar(&input, "input", "", "packets.json to verify")
	flag.StringVar(&output, "output", "", "optional path receiving the verified exact bytes")
	flag.StringVar(&renewRepository, "renew-repository", "", "optional git publisher owner/name for a fresh envelope")
	flag.StringVar(&renewCommit, "renew-commit", "", "optional full git commit for a fresh envelope")
	flag.Parse()
	now := time.Now().UTC()
	var verified packetexport.Verified
	var err error
	if renewRepository != "" || renewCommit != "" {
		verified, err = renewAndCopy(input, output, now, contract.Source{Repository: renewRepository, Commit: renewCommit})
	} else {
		verified, err = verifyAndCopy(input, output, now)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify packet export: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("verified packets=%d source=%s commit=%s digest=%s\n",
		len(verified.Packets), verified.Envelope.Source.Repository,
		verified.Envelope.Source.Commit, verified.Envelope.Digest)
}

// renewAndCopy verifies integrity at the envelope's own last valid instant, then builds
// the same canonical payload under a fresh, truthful git publication. It is the deploy
// path for a quiet site whose previous union has expired: app-only records are retained,
// the payload digest stays stable, and readers receive a new FreshnessBound window.
func renewAndCopy(input, output string, now time.Time, source contract.Source) (packetexport.Verified, error) {
	if input == "" || output == "" {
		return packetexport.Verified{}, fmt.Errorf("input and output are required for renewal")
	}
	contents, err := os.ReadFile(input)
	if err != nil {
		return packetexport.Verified{}, err
	}
	var timing struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(contents, &timing); err != nil {
		return packetexport.Verified{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339, timing.ExpiresAt)
	if err != nil {
		return packetexport.Verified{}, err
	}
	previous, err := packetexport.Verify(contents, expiresAt.Add(-time.Nanosecond))
	if err != nil {
		return packetexport.Verified{}, err
	}
	renewed, _, err := packetexport.SerializeRecords(previous.Packets, contract.Publication{PublishedAt: now, Source: source})
	if err != nil {
		return packetexport.Verified{}, err
	}
	verified, err := packetexport.Verify(renewed, now)
	if err != nil {
		return packetexport.Verified{}, err
	}
	if verified.Envelope.Digest != previous.Envelope.Digest {
		return packetexport.Verified{}, fmt.Errorf("renewal changed packet payload digest")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return packetexport.Verified{}, err
	}
	if err := os.WriteFile(output, renewed, 0o644); err != nil {
		return packetexport.Verified{}, err
	}
	return verified, nil
}

func verifyAndCopy(input, output string, now time.Time) (packetexport.Verified, error) {
	if input == "" {
		return packetexport.Verified{}, fmt.Errorf("input is required")
	}
	contents, err := os.ReadFile(input)
	if err != nil {
		return packetexport.Verified{}, err
	}
	verified, err := packetexport.Verify(contents, now)
	if err != nil {
		return packetexport.Verified{}, err
	}
	if output != "" {
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return packetexport.Verified{}, err
		}
		if err := os.WriteFile(output, contents, 0o644); err != nil {
			return packetexport.Verified{}, err
		}
	}
	return verified, nil
}
