// Package repositorypacket imports the repository's transitional Markdown packet source
// into the tracker model. E04 will retire this adapter when exports become authoritative.
package repositorypacket

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/martcoca/work-tracker/packet"
)

const importActor = packet.Actor("repository-import")

var metadataLine = regexp.MustCompile(`^- \*\*([^*]+):\*\*\s*(.*)$`)

type specification struct {
	id         packet.PacketID
	status     string
	supersedes packet.PacketID
	body       packet.Body
}

// Load reads every packets/*.md file, validates its repository metadata, and replays the
// represented state through the public packet model API.
func Load(repositoryRoot string, validator packet.TenantValidator, tenantID packet.TenantID) (*packet.Tracker, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return nil, fmt.Errorf("repository root is required")
	}
	if strings.TrimSpace(string(tenantID)) == "" {
		return nil, fmt.Errorf("tenant id is required")
	}
	specifications, err := readSpecifications(repositoryRoot)
	if err != nil {
		return nil, err
	}
	tracker, err := packet.NewTracker(validator)
	if err != nil {
		return nil, err
	}
	if err := replay(tracker, specifications, tenantID, repositoryRoot); err != nil {
		return nil, err
	}
	return tracker, nil
}

func readSpecifications(repositoryRoot string) (map[packet.PacketID]specification, error) {
	paths, err := filepath.Glob(filepath.Join(repositoryRoot, "packets", "*.md"))
	if err != nil {
		return nil, fmt.Errorf("locate repository packets: %w", err)
	}
	sort.Strings(paths)
	result := make(map[packet.PacketID]specification, len(paths))
	for _, path := range paths {
		if filepath.Base(path) == "README.md" {
			continue
		}
		spec, err := parse(path)
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[spec.id]; duplicate {
			return nil, fmt.Errorf("packet %q is defined more than once", spec.id)
		}
		result[spec.id] = spec
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no packet specifications found in %s", filepath.Join(repositoryRoot, "packets"))
	}
	return result, nil
}

func parse(path string) (specification, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return specification{}, fmt.Errorf("read %s: %w", path, err)
	}
	sections := make(map[string][]string)
	metadata := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	section := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if section == "" {
			if match := metadataLine.FindStringSubmatch(line); match != nil {
				metadata[strings.ToLower(strings.TrimSpace(match[1]))] = unquoteMetadata(match[2])
			}
			continue
		}
		sections[section] = append(sections[section], line)
	}
	if err := scanner.Err(); err != nil {
		return specification{}, fmt.Errorf("scan %s: %w", path, err)
	}

	id := packet.PacketID(metadata["packet id"])
	if id == "" {
		return specification{}, fmt.Errorf("%s: Packet id metadata is required", path)
	}
	wantFile := string(id) + ".md"
	if filepath.Base(path) != wantFile {
		return specification{}, fmt.Errorf("%s: packet id %q requires filename %s", path, id, wantFile)
	}
	status := strings.ToLower(strings.TrimSpace(metadata["status"]))
	switch status {
	case string(packet.StatusNotStarted), string(packet.StatusInProgress), string(packet.StatusDone), string(packet.StatusBlocked), "superseded":
	default:
		return specification{}, fmt.Errorf("%s: unsupported Status %q", path, status)
	}
	body := packet.Body{
		Context:  joinSection(sections, "Context"),
		Goal:     joinSection(sections, "Goal"),
		Boundary: joinSection(sections, "Boundary"),
		DoneWhen: joinSection(sections, "Done when"),
		Check:    joinSection(sections, "Check"),
	}
	for name, value := range map[string]string{
		"Context": body.Context, "Goal": body.Goal, "Boundary": body.Boundary,
		"Done when": body.DoneWhen, "Check": body.Check,
	} {
		if value == "" {
			return specification{}, fmt.Errorf("%s: section %q is required", path, name)
		}
	}
	return specification{
		id: id, status: status, supersedes: packet.PacketID(metadata["supersedes"]), body: body,
	}, nil
}

func unquoteMetadata(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, "`") && strings.HasSuffix(value, "`") {
		return strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func joinSection(sections map[string][]string, name string) string {
	return strings.TrimSpace(strings.Join(sections[name], "\n"))
}

func replay(tracker *packet.Tracker, specifications map[packet.PacketID]specification, tenantID packet.TenantID, repositoryRoot string) error {
	children := make(map[packet.PacketID]packet.PacketID)
	for id, spec := range specifications {
		if spec.supersedes == "" {
			continue
		}
		parent, exists := specifications[spec.supersedes]
		if !exists {
			return fmt.Errorf("packet %q supersedes absent packet %q", id, spec.supersedes)
		}
		if parent.status != "superseded" {
			return fmt.Errorf("packet %q supersedes %q whose Status is %q, not superseded", id, spec.supersedes, parent.status)
		}
		if existing, duplicate := children[spec.supersedes]; duplicate {
			return fmt.Errorf("packet %q has multiple replacements %q and %q", spec.supersedes, existing, id)
		}
		children[spec.supersedes] = id
	}
	for id, spec := range specifications {
		_, hasReplacement := children[id]
		if spec.status == "superseded" && !hasReplacement {
			return fmt.Errorf("superseded packet %q has no replacement", id)
		}
	}

	ids := sortedIDs(specifications)
	issued := make(map[packet.PacketID]bool, len(specifications))
	visiting := make(map[packet.PacketID]bool, len(specifications))
	var issueChain func(packet.PacketID) error
	issueChain = func(id packet.PacketID) error {
		if issued[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("supersession cycle contains packet %q", id)
		}
		visiting[id] = true
		spec := specifications[id]
		if spec.supersedes == "" {
			if _, err := tracker.Issue(packet.IssueCommand{
				PacketID: id, TenantID: tenantID, Body: spec.body, Actor: importActor,
			}); err != nil {
				return fmt.Errorf("issue packet %q: %w", id, err)
			}
		} else {
			if err := issueChain(spec.supersedes); err != nil {
				return err
			}
			parent, err := tracker.Packet(spec.supersedes)
			if err != nil {
				return err
			}
			if _, _, err := tracker.Supersede(packet.SupersedeCommand{
				PacketID: spec.supersedes, ExpectedVersion: parent.Version(), ReplacementID: id,
				ReplacementTenant: tenantID, ReplacementBody: spec.body, Actor: importActor,
			}); err != nil {
				return fmt.Errorf("supersede packet %q with %q: %w", spec.supersedes, id, err)
			}
		}
		issued[id] = true
		visiting[id] = false
		return nil
	}
	for _, id := range ids {
		if err := issueChain(id); err != nil {
			return err
		}
	}

	for _, id := range ids {
		spec := specifications[id]
		if spec.status == "superseded" || spec.status == string(packet.StatusNotStarted) {
			continue
		}
		current, err := tracker.Packet(id)
		if err != nil {
			return err
		}
		switch spec.status {
		case string(packet.StatusInProgress):
			_, err = tracker.Take(packet.TakeCommand{PacketID: id, ExpectedVersion: current.Version(), Actor: importActor})
		case string(packet.StatusBlocked):
			_, err = tracker.Transition(packet.TransitionCommand{
				PacketID: id, ExpectedVersion: current.Version(), Actor: importActor, To: packet.StatusBlocked,
			})
		case string(packet.StatusDone):
			current, err = tracker.Take(packet.TakeCommand{PacketID: id, ExpectedVersion: current.Version(), Actor: importActor})
			if err == nil {
				evidence := filepath.Join(repositoryRoot, "evidence", string(id)+".md")
				if _, statErr := os.Stat(evidence); statErr != nil {
					return fmt.Errorf("done packet %q evidence: %w", id, statErr)
				}
				_, err = tracker.Transition(packet.TransitionCommand{
					PacketID: id, ExpectedVersion: current.Version(), Actor: importActor, To: packet.StatusDone,
					Evidence: []packet.Evidence{packet.Evidence("evidence/" + string(id) + ".md")},
				})
			}
		}
		if err != nil {
			return fmt.Errorf("apply Status %q to packet %q: %w", spec.status, id, err)
		}
	}
	return nil
}

func sortedIDs(specifications map[packet.PacketID]specification) []packet.PacketID {
	ids := make([]packet.PacketID, 0, len(specifications))
	for id := range specifications {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids
}
