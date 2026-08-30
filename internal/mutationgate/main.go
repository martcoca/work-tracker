// Command mutationgate verifies that tests detect removed domain and read-path enforcement.
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
	{
		name:        "export_lifetime_is_one_hour",
		path:        "contract/envelope.go",
		original:    `	expiresAt := publishedAt.Add(FreshnessBound)`,
		replacement: `	expiresAt := publishedAt.Add(2 * FreshnessBound)`,
	},
	{
		name:        "canonical_keys_are_sorted",
		path:        "contract/envelope.go",
		original:    `		sort.Strings(keys)`,
		replacement: `		sort.Sort(sort.Reverse(sort.StringSlice(keys)))`,
	},
	{
		name:        "unsupported_export_schema_rejected",
		path:        "contract/envelope.go",
		original:    `	if envelope.Schema != expectedSchema {`,
		replacement: `	if false && envelope.Schema != expectedSchema {`,
	},
	{
		name:        "stale_export_rejected",
		path:        "contract/envelope.go",
		original:    `	if !now.Before(expiresAt) {`,
		replacement: `	if false && !now.Before(expiresAt) {`,
	},
	{
		name:        "tampered_payload_rejected",
		path:        "contract/envelope.go",
		original:    `	if !equalDigest(envelope.Digest, actualDigest) {`,
		replacement: `	if false && !equalDigest(envelope.Digest, actualDigest) {`,
	},
	{
		name:        "missing_export_distinguished",
		path:        "contract/envelope.go",
		original:    `			return Envelope{}, fmt.Errorf("%w: %s", ErrExportNotFound, path)`,
		replacement: `			return Envelope{}, fmt.Errorf("%w: %s", ErrInvalidExport, path)`,
	},
	{
		name:        "publication_provenance_required",
		path:        "contract/envelope.go",
		original:    `	if err := ValidateSource(publication.Source); err != nil {`,
		replacement: `	if err := ValidateSource(publication.Source); false && err != nil {`,
	},
	{
		name:        "provenance_repository_is_real_shaped",
		path:        "contract/provenance.go",
		original:    `	if len(parts) != 2 || !repositoryPart.MatchString(parts[0]) || !repositoryPart.MatchString(parts[1]) {`,
		replacement: `	if false && (len(parts) != 2 || !repositoryPart.MatchString(parts[0]) || !repositoryPart.MatchString(parts[1])) {`,
	},
	{
		name:        "provenance_commit_is_full_object_id",
		path:        "contract/provenance.go",
		original:    `	if !commitID.MatchString(source.Commit) {`,
		replacement: `	if false && !commitID.MatchString(source.Commit) {`,
	},
	{
		name:        "tracker_requires_tenant_validator",
		path:        "packet/tracker.go",
		original:    `	if tenantValidator == nil {`,
		replacement: `	if false && tenantValidator == nil {`,
	},
	{
		name: "tenant_checked_at_issue",
		path: "packet/tracker.go",
		original: `	if err := t.tenantValidator.ValidateTenantID(string(command.TenantID), t.now().UTC()); err != nil {
		return Packet{}, err
	}`,
		replacement: `	// tenant validation removed by mutation`,
	},
	{
		name: "replacement_tenant_checked_at_issue",
		path: "packet/tracker.go",
		original: `	if err := t.tenantValidator.ValidateTenantID(string(command.ReplacementTenant), t.now().UTC()); err != nil {
		return Packet{}, Packet{}, err
	}`,
		replacement: `	// replacement tenant validation removed by mutation`,
	},
	{
		name:        "tenant_directory_exact_fields",
		path:        "tenant/directory.go",
		original:    `	decoder.DisallowUnknownFields()`,
		replacement: `	// unknown tenant fields allowed by mutation`,
	},
	{
		name:        "unknown_tenant_refused",
		path:        "tenant/directory.go",
		original:    `	if !exists {`,
		replacement: `	if false && !exists {`,
	},
	{
		name:        "retired_tenant_refused_distinctly",
		path:        "tenant/directory.go",
		original:    `	if record.Status == StatusRetired {`,
		replacement: `	if false && record.Status == StatusRetired {`,
	},
	{
		name:        "stale_tenant_directory_refused_at_issue",
		path:        "tenant/directory.go",
		original:    `	if !at.Before(directory.expiresAt) {`,
		replacement: `	if false && !at.Before(directory.expiresAt) {`,
	},
	{
		name: "tenant_status_vocabulary_exact",
		path: "tenant/directory.go",
		original: `	case StatusActive, StatusSuspended, StatusRetired:
	default:`,
		replacement: `	case StatusActive, StatusSuspended, StatusRetired:
	case Status("deleted"):
	default:`,
	},
	{
		name:        "packet_export_schema_exact",
		path:        "packetexport/export.go",
		original:    `	Schema   = "martcoca.tracker.packets/1"`,
		replacement: `	Schema   = "martcoca.tracker.packets/2"`,
	},
	{
		name: "packet_payload_sorted_by_id",
		path: "packetexport/export.go",
		original: `	sort.Slice(records, func(left, right int) bool {
		return records[left].ID < records[right].ID
	})`,
		replacement: `	sort.Slice(records, func(left, right int) bool {
		return false
	})`,
	},
	{
		name:        "identity_tenant_claim_required",
		path:        "identity/verifier.go",
		original:    `	if strings.TrimSpace(claims.TenantID) == "" {`,
		replacement: `	if false && strings.TrimSpace(claims.TenantID) == "" {`,
	},
	{
		name:        "render_path_filters_tenant",
		path:        "surface/view.go",
		original:    `		if indexed.record.TenantID == principal.TenantID {`,
		replacement: `		if true {`,
	},
	{
		name: "stale_directory_fails_closed",
		path: "surface/view.go",
		original: `	status := snapshot.directoryStatus(now)
	if status.Stale {
		return nil, status, ErrDirectoryStale
	}`,
		replacement: `	status := snapshot.directoryStatus(now)
	if false && status.Stale {
		return nil, status, ErrDirectoryStale
	}`,
	},
	{
		name:        "built_routes_are_get_only",
		path:        "surface/handler.go",
		original:    `	{Name: "packet", Method: http.MethodGet, Pattern: "/api/initiatives/{initiative}/epics/{epic}/packets/{packet}"},`,
		replacement: `	{Name: "packet", Method: http.MethodPost, Pattern: "/api/initiatives/{initiative}/epics/{epic}/packets/{packet}"},`,
	},
	{
		name:        "issued_draft_is_frozen",
		path:        "authoring/workspace.go",
		original:    `	if draft.State == StateIssued {`,
		replacement: `	if false && draft.State == StateIssued {`,
	},
	{
		name: "draft_complete_before_issue",
		path: "authoring/workspace.go",
		original: `	if err := validateComplete(draft); err != nil {
		return IssueResult{}, err
	}`,
		replacement: `	// completeness check removed by mutation`,
	},
	{
		name: "authoring_scope_checked_at_issue",
		path: "authoring/workspace.go",
		original: `	if err := workspace.scope.ValidateScope(draft.TenantID, draft.InitiativeID, draft.EpicID, draft.Target, workspace.now().UTC()); err != nil {
		return IssueResult{}, err
	}`,
		replacement: `	// scope check removed by mutation`,
	},
	{
		name:        "issue_actor_is_signed_subject",
		path:        "authoring/workspace.go",
		original:    `	actor := packet.Actor(principal.Subject)`,
		replacement: `	actor := packet.Actor("wrong-actor")`,
	},
	{
		name:        "draft_updates_replace_body",
		path:        "authoring/workspace.go",
		original:    `	draft.Body = command.Body`,
		replacement: `	// body update removed by mutation`,
	},
	{
		name:        "authoring_mutations_are_allowlisted",
		path:        "surface/handler.go",
		original:    `		if !allowed || expectedName != route.Name {`,
		replacement: `		if false && (!allowed || expectedName != route.Name) {`,
	},
	{
		name:        "all_runtime_exports_are_fetched",
		path:        "runtimeexport/reader.go",
		original:    `		{name: AgentGrants, url: reader.config.AgentGrantsURL, schema: AgentGrantsSchema},`,
		replacement: `		// agent-grants fetch removed by mutation`,
	},
	{
		name:        "cold_start_requires_held_exports",
		path:        "runtimeexport/reader.go",
		original:    `	if readyErr := reader.Ready(reader.now().UTC()); readyErr != nil {`,
		replacement: `	if readyErr := reader.Ready(reader.now().UTC()); false && readyErr != nil {`,
	},
	{
		name:        "runtime_refresh_is_scheduled",
		path:        "runtimeexport/reader.go",
		original:    `	go reader.run(ctx)`,
		replacement: `	// scheduled refresh removed by mutation`,
	},
	{
		name: "failed_refresh_keeps_last_good_copy",
		path: "runtimeexport/reader.go",
		original: `		if result.err == nil {
			held = result.copy`,
		replacement: `		if true {
			held = result.copy`,
	},
	{
		name:        "held_export_expiry_is_rechecked",
		path:        "runtimeexport/reader.go",
		original:    `	if !at.Before(held.expiresAt) {`,
		replacement: `	if false && !at.Before(held.expiresAt) {`,
	},
	{
		name: "requests_never_wait_for_refresh",
		path: "runtimeexport/reader.go",
		original: `func (reader *Reader) CurrentSnapshot() *surface.Snapshot {
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	return reader.snapshot
}`,
		replacement: `func (reader *Reader) CurrentSnapshot() *surface.Snapshot {
	reader.refreshMu.Lock()
	defer reader.refreshMu.Unlock()
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	return reader.snapshot
}`,
	},
	{
		name:        "packet_endpoint_is_configurable",
		path:        "runtimeexport/reader.go",
		original:    `	config.PacketURL = valueOrDefault("PACKET_EXPORT_URL", config.PacketURL)`,
		replacement: `	config.PacketURL = DefaultPacketURL`,
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
			if entry.Name() == ".terraform" || relative == ".git" || relative == "node_modules" || relative == "dist" ||
				strings.HasPrefix(relative, ".git"+string(filepath.Separator)) ||
				strings.HasPrefix(relative, "node_modules"+string(filepath.Separator)) ||
				strings.HasPrefix(relative, "dist"+string(filepath.Separator)) {
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
