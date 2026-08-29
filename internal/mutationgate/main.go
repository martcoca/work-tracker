// Command mutationgate verifies that packet-model tests detect removed enforcement.
// It uses exact, named mutations so it remains standard-library-only and works offline.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type mutation struct {
	name        string
	path        string
	original    string
	replacement string
}

var mutations = []mutation{
	{
		name: "complete_event_metadata",
		path: "packet/tracker.go",
		original: `	if err := validateMetadata(event.Metadata()); err != nil {
		return err
	}`,
		replacement: `	// metadata validation removed by mutation`,
	},
	{
		name:        "tenant_required",
		path:        "packet/tracker.go",
		original:    `		if event.TenantID == "" {`,
		replacement: `		if false {`,
	},
	{
		name:        "projection_replays_every_event",
		path:        "packet/tracker.go",
		original:    `	for _, stored := range t.events {`,
		replacement: `	for _, stored := range t.events[:len(t.events)-1] {`,
	},
	{
		name:        "stale_version_rejected",
		path:        "packet/tracker.go",
		original:    `	if actual != expected {`,
		replacement: `	if false && actual != expected {`,
	},
	{
		name:        "illegal_transition_rejected",
		path:        "packet/tracker.go",
		original:    `		if !legalTransition(packet.status, event.To) {`,
		replacement: `		if false && !legalTransition(packet.status, event.To) {`,
	},
	{
		name:        "done_evidence_required",
		path:        "packet/tracker.go",
		original:    `			if len(event.Evidence) == 0 {`,
		replacement: `			if false && len(event.Evidence) == 0 {`,
	},
	{
		name:        "blank_evidence_rejected",
		path:        "packet/tracker.go",
		original:    `				if strings.TrimSpace(string(evidence)) == "" {`,
		replacement: `				if false && strings.TrimSpace(string(evidence)) == "" {`,
	},
	{
		name:        "evidence_forbidden_before_done",
		path:        "packet/tracker.go",
		original:    `		} else if len(event.Evidence) != 0 {`,
		replacement: `		} else if false && len(event.Evidence) != 0 {`,
	},
	{
		name: "comments_are_projected",
		path: "packet/tracker.go",
		original: `		packet.comments = append(packet.comments, Comment{
			EventID: event.Meta.ID,
			At:      event.Meta.At,
			Actor:   event.Meta.Actor,
			Text:    event.Text,
		})`,
		replacement: `		// comment projection removed by mutation`,
	},
	{
		name:        "replacement_links_to_parent",
		path:        "packet/tracker.go",
		original:    `		packet.parentID = event.ParentID`,
		replacement: `		// parent link removed by mutation`,
	},
	{
		name:        "parent_links_to_replacement",
		path:        "packet/tracker.go",
		original:    `		packet.supersededBy = event.ReplacementID`,
		replacement: `		// replacement link removed by mutation`,
	},
	{
		name:        "supersession_preserves_original_body",
		path:        "packet/tracker.go",
		original:    `		packet.supersededBy = event.ReplacementID`,
		replacement: "\t\tpacket.body = Body{}\n\t\tpacket.supersededBy = event.ReplacementID",
	},
	{
		name: "done_appends_close_event",
		path: "packet/tracker.go",
		original: `	if command.To == StatusDone {
		closedMeta, err := t.nextMetadataLocked(command.Actor)`,
		replacement: `	if false && command.To == StatusDone {
		closedMeta, err := t.nextMetadataLocked(command.Actor)`,
	},
	{
		name: "supersession_closes_parent",
		path: "packet/tracker.go",
		original: `		PacketClosed{
			Meta:     closedMeta,
			PacketID: command.PacketID,
			Reason:   CloseReasonSuperseded,
		},`,
		replacement: `		// parent close removed by mutation`,
	},
	{
		name:        "take_is_recorded",
		path:        "packet/tracker.go",
		original:    `		PacketTaken{Meta: takenMeta, PacketID: command.PacketID},`,
		replacement: `		// taken event removed by mutation`,
	},
}

func main() {
	root, err := moduleRoot()
	if err != nil {
		fatal(err)
	}

	// A missing mutation point is conservatively a survivor: the gate can no longer
	// demonstrate that deleting that rule is caught. This also makes a removed guard fail
	// loudly instead of silently shrinking mutation coverage.
	missing := false
	for _, candidate := range mutations {
		contents, readErr := os.ReadFile(filepath.Join(root, candidate.path))
		if readErr != nil {
			fatal(readErr)
		}
		if strings.Count(string(contents), candidate.original) != 1 {
			fmt.Printf("SURVIVED %s: mutation point missing from %s; rule is no longer proven\n", candidate.name, candidate.path)
			missing = true
		}
	}
	if missing {
		os.Exit(1)
	}

	if output, runErr := runTests(root); runErr != nil {
		fmt.Printf("BASELINE FAILED\n%s", output)
		os.Exit(1)
	}

	survivors := 0
	for _, candidate := range mutations {
		temporary, tempErr := os.MkdirTemp("", "work-tracker-mutation-")
		if tempErr != nil {
			fatal(tempErr)
		}
		if copyErr := copyModule(root, temporary); copyErr != nil {
			os.RemoveAll(temporary)
			fatal(copyErr)
		}
		if mutateErr := applyMutation(temporary, candidate); mutateErr != nil {
			os.RemoveAll(temporary)
			fatal(mutateErr)
		}
		output, runErr := runTests(temporary)
		if removeErr := os.RemoveAll(temporary); removeErr != nil {
			fatal(removeErr)
		}
		if runErr == nil {
			fmt.Printf("SURVIVED %s\n%s", candidate.name, output)
			survivors++
			continue
		}
		fmt.Printf("KILLED %s\n", candidate.name)
	}

	if survivors != 0 {
		fmt.Printf("FAIL: mutation gate (%d survivor(s))\n", survivors)
		os.Exit(1)
	}
	fmt.Printf("PASS: mutation gate (%d mutants killed, 0 survivors)\n", len(mutations))
}

func moduleRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(directory, "go.mod")); statErr == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("go.mod not found from working directory")
		}
		directory = parent
	}
}

func copyModule(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(destination, relative), 0o755)
		}
		if relative != "go.mod" && filepath.Ext(relative) != ".go" {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, relative), contents, info.Mode().Perm())
	})
}

func applyMutation(root string, candidate mutation) error {
	path := filepath.Join(root, candidate.path)
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if count := strings.Count(string(contents), candidate.original); count != 1 {
		return fmt.Errorf("mutation %s matched %d locations in %s", candidate.name, count, candidate.path)
	}
	mutated := strings.Replace(string(contents), candidate.original, candidate.replacement, 1)
	return os.WriteFile(path, []byte(mutated), 0o644)
}

func runTests(root string) (string, error) {
	command := exec.Command("go", "test", "-count=1", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	output, err := command.CombinedOutput()
	return string(output), err
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "mutation gate: %v\n", err)
	os.Exit(1)
}
