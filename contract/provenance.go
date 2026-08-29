package contract

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

var (
	repositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	commitID       = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

type commandRunner func(context.Context, string, string, ...string) ([]byte, error)

// ValidateSource rejects absent or placeholder-shaped provenance.
func ValidateSource(source Source) error {
	parts := strings.Split(source.Repository, "/")
	if len(parts) != 2 || !repositoryPart.MatchString(parts[0]) || !repositoryPart.MatchString(parts[1]) {
		return fmt.Errorf("%w: source.repository must be owner/name", ErrInvalidProvenance)
	}
	if !commitID.MatchString(source.Commit) {
		return fmt.Errorf("%w: source.commit must be a full git object id", ErrInvalidProvenance)
	}
	return nil
}

// ResolveGitSource derives provenance from the repository itself and fails if either fact
// cannot be obtained. It performs local git reads only.
func ResolveGitSource(ctx context.Context, directory string) (Source, error) {
	return resolveGitSource(ctx, directory, runGit)
}

func resolveGitSource(ctx context.Context, directory string, run commandRunner) (Source, error) {
	remote, err := run(ctx, directory, "git", "remote", "get-url", "origin")
	if err != nil {
		return Source{}, fmt.Errorf("%w: resolve origin: %v", ErrInvalidProvenance, err)
	}
	commit, err := run(ctx, directory, "git", "rev-parse", "HEAD")
	if err != nil {
		return Source{}, fmt.Errorf("%w: resolve commit: %v", ErrInvalidProvenance, err)
	}
	repository, err := normalizeRepository(strings.TrimSpace(string(remote)))
	if err != nil {
		return Source{}, err
	}
	source := Source{Repository: repository, Commit: strings.TrimSpace(string(commit))}
	if err := ValidateSource(source); err != nil {
		return Source{}, err
	}
	return source, nil
}

func runGit(ctx context.Context, directory, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	return command.Output()
}

func normalizeRepository(remote string) (string, error) {
	candidate := strings.TrimSpace(remote)
	if at := strings.Index(candidate, "@"); at >= 0 && !strings.Contains(candidate, "://") {
		if colon := strings.Index(candidate[at:], ":"); colon >= 0 {
			candidate = candidate[at+colon+1:]
		}
	} else if strings.Contains(candidate, "://") {
		parsed, err := url.Parse(candidate)
		if err != nil {
			return "", fmt.Errorf("%w: parse source repository: %v", ErrInvalidProvenance, err)
		}
		candidate = parsed.Path
	}
	candidate = strings.Trim(strings.TrimSuffix(candidate, ".git"), "/")
	parts := strings.Split(candidate, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: origin must identify owner/name", ErrInvalidProvenance)
	}
	return strings.Join(parts, "/"), nil
}
