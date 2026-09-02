// Package runtimeexport fetches public export files away from request handling and keeps
// the last verified copies in memory.
package runtimeexport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/surface"
	"github.com/martcoca/work-tracker/tenant"
)

const (
	DefaultPacketURL          = "https://tracker.martcoca.com/packets.json"
	DefaultTenantDirectoryURL = "https://identity.martcoca.com/tenant-directory.json"
	DefaultAgentGrantsURL     = "https://identity.martcoca.com/agent-grants.json"
	AgentGrantsSchema         = "martcoca.identity.agent-grants/1"
	DefaultRefreshInterval    = 5 * time.Minute
	DefaultFetchTimeout       = 5 * time.Second
	maximumExportBytes        = 16 << 20
)

var (
	ErrNoUsableExport    = errors.New("no usable export is held")
	errExportUnavailable = errors.New("export endpoint is unavailable")
)

type ExportName string

const (
	Packets         ExportName = "packets"
	TenantDirectory ExportName = "tenant-directory"
	AgentGrants     ExportName = "agent-grants"
)

var allExports = []ExportName{Packets, TenantDirectory, AgentGrants}

// Config makes every endpoint and timing choice replaceable without rebuilding the
// service. The defaults name the public static objects used by the deployed reader.
type Config struct {
	PacketURL          string
	TenantDirectoryURL string
	AgentGrantsURL     string
	RefreshInterval    time.Duration
	FetchTimeout       time.Duration
}

func DefaultConfig() Config {
	return Config{
		PacketURL: DefaultPacketURL, TenantDirectoryURL: DefaultTenantDirectoryURL,
		AgentGrantsURL: DefaultAgentGrantsURL, RefreshInterval: DefaultRefreshInterval,
		FetchTimeout: DefaultFetchTimeout,
	}
}

// ConfigFromEnvironment reads only public endpoint and duration settings. No credential
// is accepted because export publication is deliberately public and static.
func ConfigFromEnvironment() (Config, error) {
	config := DefaultConfig()
	config.PacketURL = valueOrDefault("PACKET_EXPORT_URL", config.PacketURL)
	config.TenantDirectoryURL = valueOrDefault("TENANT_DIRECTORY_URL", config.TenantDirectoryURL)
	config.AgentGrantsURL = valueOrDefault("AGENT_GRANTS_URL", config.AgentGrantsURL)
	var err error
	if config.RefreshInterval, err = durationOrDefault("EXPORT_REFRESH_INTERVAL", config.RefreshInterval); err != nil {
		return Config{}, err
	}
	if config.FetchTimeout, err = durationOrDefault("EXPORT_FETCH_TIMEOUT", config.FetchTimeout); err != nil {
		return Config{}, err
	}
	return config, nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}

type source struct {
	name              ExportName
	url               string
	schema            string
	serviceOwned      bool
	requiredAtStartup bool
}

func (configured source) allowsAbsence() bool {
	return configured.serviceOwned && !configured.requiredAtStartup
}

type heldCopy struct {
	contents     []byte
	publishedAt  time.Time
	expiresAt    time.Time
	lastAttempt  time.Time
	lastSuccess  time.Time
	refreshError error
}

type fetchResult struct {
	source source
	copy   heldCopy
	err    error
}

// Reader is both the background refresher and the surface's immutable snapshot source.
// Network and verification happen before the mutex is taken, so CurrentSnapshot never
// waits for an outbound request.
type Reader struct {
	config Config
	client *http.Client
	report func(error)
	now    func() time.Time

	refreshMu sync.Mutex
	mu        sync.RWMutex
	copies    map[ExportName]heldCopy
	snapshot  *surface.Snapshot
}

func New(config Config, client *http.Client, report func(error)) (*Reader, error) {
	if config.RefreshInterval <= 0 || config.FetchTimeout <= 0 {
		return nil, errors.New("refresh interval and fetch timeout must be positive")
	}
	for _, raw := range []string{config.PacketURL, config.TenantDirectoryURL, config.AgentGrantsURL} {
		if err := validateEndpoint(raw); err != nil {
			return nil, err
		}
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Reader{
		config: config, client: client, report: report, now: time.Now,
		copies: make(map[ExportName]heldCopy),
	}, nil
}

func validateEndpoint(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("export endpoint must be an absolute URL")
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("export endpoint must not contain credentials, a query, or a fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopback(parsed.Hostname()) {
		return nil
	}
	return errors.New("export endpoint must use HTTPS (plain HTTP is allowed only for a local fixture)")
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func (reader *Reader) sources() []source {
	return []source{
		{
			name: Packets, url: reader.config.PacketURL, schema: packetexport.Schema,
			serviceOwned: true, requiredAtStartup: false,
		},
		{
			name: TenantDirectory, url: reader.config.TenantDirectoryURL, schema: tenant.Schema,
			serviceOwned: false, requiredAtStartup: true,
		},
		{
			name: AgentGrants, url: reader.config.AgentGrantsURL, schema: AgentGrantsSchema,
			serviceOwned: false, requiredAtStartup: true,
		},
	}
}

// Start performs the cold-start refresh synchronously. Authority-owned dependencies must
// have verified, unexpired copies. The tracker-owned packet export may be absent, but a
// present invalid packet export is still a refusal. Future refreshes run in the background.
func (reader *Reader) Start(ctx context.Context) error {
	refreshErr := reader.Refresh(ctx)
	if readyErr := reader.Ready(reader.now().UTC()); readyErr != nil {
		if refreshErr != nil {
			return fmt.Errorf("startup refused: %w: %v", readyErr, refreshErr)
		}
		return fmt.Errorf("startup refused: %w", readyErr)
	}
	go reader.run(ctx)
	return nil
}

func (reader *Reader) run(ctx context.Context) {
	ticker := time.NewTicker(reader.config.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := reader.Refresh(ctx); err != nil && reader.report != nil {
				reader.report(err)
			}
		}
	}
}

// Refresh fetches all three sources concurrently and swaps only copies that verify. A bad
// or unreachable source leaves its previous good copy untouched. Before any packet copy has
// existed, an unavailable tracker-owned packet source constructs an explicit empty snapshot.
func (reader *Reader) Refresh(ctx context.Context) error {
	reader.refreshMu.Lock()
	defer reader.refreshMu.Unlock()

	now := reader.now().UTC()
	configuredSources := reader.sources()
	results := make(chan fetchResult, len(configuredSources))
	var wait sync.WaitGroup
	for _, configured := range configuredSources {
		configured := configured
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- reader.fetch(ctx, configured, now)
		}()
	}
	wait.Wait()
	close(results)

	byName := make(map[ExportName]fetchResult, len(configuredSources))
	for result := range results {
		byName[result.source.name] = result
	}

	reader.mu.RLock()
	candidate := cloneCopies(reader.copies)
	currentSnapshot := reader.snapshot
	previousPacket, hadPreviousPacket := reader.copies[Packets]
	hadPreviousPacket = hadPreviousPacket && len(previousPacket.contents) != 0
	reader.mu.RUnlock()
	for name, result := range byName {
		if result.err == nil {
			candidate[name] = result.copy
		}
	}

	nextSnapshot := currentSnapshot
	packetCopy, hasPackets := candidate[Packets]
	directoryCopy, hasDirectory := candidate[TenantDirectory]
	hasPackets = hasPackets && len(packetCopy.contents) != 0
	hasDirectory = hasDirectory && len(directoryCopy.contents) != 0
	if hasPackets && hasDirectory {
		built, err := surface.NewSnapshot(packetCopy.contents, directoryCopy.contents)
		if err != nil {
			for _, name := range []ExportName{Packets, TenantDirectory} {
				if result := byName[name]; result.err == nil {
					result.err = fmt.Errorf("construct reader snapshot: %w", err)
					byName[name] = result
				}
			}
			if !hadPreviousPacket {
				nextSnapshot = nil
			}
		} else {
			nextSnapshot = built
		}
	} else if hasDirectory && !hasPackets {
		packetResult := byName[Packets]
		if errors.Is(packetResult.err, errExportUnavailable) {
			built, err := surface.NewEmptySnapshot(directoryCopy.contents)
			if err != nil {
				if result := byName[TenantDirectory]; result.err == nil {
					result.err = fmt.Errorf("construct empty reader snapshot: %w", err)
					byName[TenantDirectory] = result
				}
				nextSnapshot = nil
			} else {
				nextSnapshot = built
			}
		} else if !hadPreviousPacket {
			nextSnapshot = nil
		}
	}

	reader.mu.Lock()
	for _, name := range allExports {
		result := byName[name]
		held := reader.copies[name]
		held.lastAttempt = now
		if result.err == nil {
			held = result.copy
			held.lastAttempt = now
			held.lastSuccess = now
			held.refreshError = nil
		} else {
			held.refreshError = result.err
		}
		reader.copies[name] = held
	}
	reader.snapshot = nextSnapshot
	reader.mu.Unlock()

	var failures []error
	for _, configured := range configuredSources {
		expectedAbsence := configured.name == Packets && !hadPreviousPacket && configured.allowsAbsence()
		if err := byName[configured.name].err; err != nil && !(expectedAbsence && errors.Is(err, errExportUnavailable)) {
			failures = append(failures, fmt.Errorf("%s: %w", configured.name, err))
		}
	}
	return errors.Join(failures...)
}

func cloneCopies(copies map[ExportName]heldCopy) map[ExportName]heldCopy {
	cloned := make(map[ExportName]heldCopy, len(copies))
	for name, held := range copies {
		held.contents = append([]byte(nil), held.contents...)
		cloned[name] = held
	}
	return cloned
}

func (reader *Reader) fetch(parent context.Context, configured source, now time.Time) fetchResult {
	ctx, cancel := context.WithTimeout(parent, reader.config.FetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, configured.url, nil)
	if err != nil {
		return fetchResult{source: configured, err: errors.New("create fetch request")}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := reader.client.Do(request)
	if err != nil {
		return fetchResult{source: configured, err: unavailableError("fetch request failed")}
	}
	defer response.Body.Close()
	if response.Request == nil || validateEndpoint(response.Request.URL.String()) != nil {
		return fetchResult{source: configured, err: errors.New("fetch redirected to an unsafe endpoint")}
	}
	if response.StatusCode == http.StatusNotFound {
		return fetchResult{source: configured, err: unavailableError("endpoint returned HTTP 404")}
	}
	if response.StatusCode != http.StatusOK {
		return fetchResult{source: configured, err: fmt.Errorf("endpoint returned HTTP %d", response.StatusCode)}
	}
	// A single-page host answers every unmatched path with its index document and HTTP 200,
	// so an export that simply is not there arrives as HTML rather than as a 404. Treating
	// that as a malformed export refused startup for an export the service is allowed not to
	// have yet; treating it as unavailable routes it into the same absence rule as a 404,
	// which still refuses absence for anything required at startup.
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err == nil &&
		mediaType != "" && mediaType != "application/json" {
		return fetchResult{source: configured, err: unavailableError(fmt.Sprintf("endpoint returned %s, not an export", mediaType))}
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximumExportBytes+1))
	if err != nil {
		return fetchResult{source: configured, err: errors.New("read response failed")}
	}
	if len(contents) > maximumExportBytes {
		return fetchResult{source: configured, err: errors.New("export exceeds size limit")}
	}
	envelope, err := verify(configured, contents, now)
	if err != nil {
		return fetchResult{source: configured, err: err}
	}
	publishedAt, _ := time.Parse(time.RFC3339, envelope.PublishedAt)
	expiresAt, _ := time.Parse(time.RFC3339, envelope.ExpiresAt)
	return fetchResult{source: configured, copy: heldCopy{
		contents: append([]byte(nil), contents...), publishedAt: publishedAt,
		expiresAt: expiresAt,
	}}
}

type unavailableError string

func (failure unavailableError) Error() string { return string(failure) }

func (failure unavailableError) Is(target error) bool { return target == errExportUnavailable }

func verify(configured source, contents []byte, now time.Time) (contract.Envelope, error) {
	envelope, err := contract.Verify(contents, configured.schema, now)
	if err != nil {
		return contract.Envelope{}, err
	}
	switch configured.name {
	case Packets:
		if _, err := packetexport.Verify(contents, now); err != nil {
			return contract.Envelope{}, err
		}
	case TenantDirectory:
		if _, err := tenant.Parse(contents, now); err != nil {
			return contract.Envelope{}, err
		}
	case AgentGrants:
		// T04 needs only the shared envelope. E03 owns the grant payload contract and
		// will decode it before any grant can authorize a session operation.
	default:
		return contract.Envelope{}, errors.New("unsupported export dependency")
	}
	return envelope, nil
}

// Ready checks held metadata only. It does no network or payload work and is therefore
// safe for startup gating and diagnostics.
func (reader *Reader) Ready(at time.Time) error {
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	for _, configured := range reader.sources() {
		if _, err := reader.verifiedCopyLocked(configured.name, at); err != nil {
			if configured.requiredAtStartup {
				return err
			}
			if !configured.allowsAbsence() {
				return fmt.Errorf("export policy is invalid: optional %s is not service-owned", configured.name)
			}
			held := reader.copies[configured.name]
			if errors.Is(err, ErrNoUsableExport) && errors.Is(held.refreshError, errExportUnavailable) {
				continue
			}
			if errors.Is(err, ErrNoUsableExport) && held.refreshError != nil {
				return fmt.Errorf("%s: %w", configured.name, held.refreshError)
			}
			return err
		}
	}
	return nil
}

// VerifiedCopy gives a downstream authorizer a defensive copy of one held export while
// enforcing its original freshness bound. It deliberately returns bytes, not a guessed
// agent-grants payload model; E03 owns that decoding contract.
func (reader *Reader) VerifiedCopy(name ExportName, at time.Time) ([]byte, error) {
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	return reader.verifiedCopyLocked(name, at)
}

func (reader *Reader) verifiedCopyLocked(name ExportName, at time.Time) ([]byte, error) {
	held, ok := reader.copies[name]
	if !ok || len(held.contents) == 0 {
		return nil, fmt.Errorf("%w: %s is missing", ErrNoUsableExport, name)
	}
	if !at.Before(held.expiresAt) {
		return nil, fmt.Errorf("%w: %s expired at %s", contract.ErrStaleExport, name, held.expiresAt.Format(time.RFC3339Nano))
	}
	return append([]byte(nil), held.contents...), nil
}

func (reader *Reader) CurrentSnapshot() *surface.Snapshot {
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	return reader.snapshot
}

func (reader *Reader) ExportStatuses(now time.Time) []surface.HeldExportStatus {
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	configuredSources := reader.sources()
	statuses := make([]surface.HeldExportStatus, 0, len(configuredSources))
	for _, configured := range configuredSources {
		name := configured.name
		held, available := reader.copies[name]
		isAvailable := available && len(held.contents) != 0
		absent := !isAvailable && configured.allowsAbsence() && errors.Is(held.refreshError, errExportUnavailable)
		status := surface.HeldExportStatus{
			Name: string(name), Available: isAvailable,
			Required: configured.requiredAtStartup, ServiceOwned: configured.serviceOwned,
			Absent: absent,
		}
		if held.refreshError != nil && !absent {
			status.RefreshError = held.refreshError.Error()
		}
		if !held.lastAttempt.IsZero() {
			status.LastAttempt = held.lastAttempt.Format(time.RFC3339Nano)
		}
		if !held.lastSuccess.IsZero() {
			status.LastSuccess = held.lastSuccess.Format(time.RFC3339Nano)
		}
		if status.Available {
			age := now.Sub(held.publishedAt)
			if age < 0 {
				age = 0
			}
			status.PublishedAt = held.publishedAt.Format(time.RFC3339Nano)
			status.ExpiresAt = held.expiresAt.Format(time.RFC3339Nano)
			status.AgeSeconds = int64(age / time.Second)
			status.Stale = !now.Before(held.expiresAt)
			if status.Stale {
				status.ExpiredBy = int64(now.Sub(held.expiresAt) / time.Second)
			}
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(left, right int) bool { return statuses[left].Name < statuses[right].Name })
	return statuses
}
