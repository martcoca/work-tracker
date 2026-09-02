// Command publish-packet-export builds the transitional repository packet source through
// the product model and writes the existing packet export contract into a Hosting tree.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/martcoca/work-tracker/packet"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/repositorypacket"
	"github.com/martcoca/work-tracker/tenant"
)

const (
	defaultTenantDirectoryURL = "https://identity.martcoca.com/tenant-directory.json"
	maximumDirectoryBytes     = 16 << 20
)

type config struct {
	repositoryRoot     string
	outputDirectory    string
	tenantDirectoryURL string
}

type publication struct {
	path        string
	packetCount int
	schema      string
	expiresAt   string
	repository  string
	commit      string
}

func main() {
	configuration := config{}
	flag.StringVar(&configuration.repositoryRoot, "repository", ".", "repository root containing packets/")
	flag.StringVar(&configuration.outputDirectory, "output", "dist", "Hosting output directory")
	flag.StringVar(&configuration.tenantDirectoryURL, "tenant-directory-url", valueOrDefault("TENANT_DIRECTORY_URL", defaultTenantDirectoryURL), "verified tenant-directory export URL")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := publish(ctx, configuration, nil, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "publish packet export: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("published %s schema=%s packets=%d source=%s commit=%s expires_at=%s\n",
		result.path, result.schema, result.packetCount, result.repository, result.commit, result.expiresAt)
}

func publish(ctx context.Context, configuration config, client *http.Client, now time.Time) (publication, error) {
	if now.IsZero() {
		return publication{}, errors.New("publication time is required")
	}
	repositoryRoot, err := filepath.Abs(configuration.repositoryRoot)
	if err != nil {
		return publication{}, fmt.Errorf("resolve repository root: %w", err)
	}
	outputDirectory := configuration.outputDirectory
	if !filepath.IsAbs(outputDirectory) {
		outputDirectory = filepath.Join(repositoryRoot, outputDirectory)
	}
	directory, err := fetchTenantDirectory(ctx, client, configuration.tenantDirectoryURL, now)
	if err != nil {
		return publication{}, err
	}
	tenantID, err := soleActiveTenant(directory)
	if err != nil {
		return publication{}, err
	}
	tracker, err := repositorypacket.Load(repositoryRoot, directory, tenantID)
	if err != nil {
		return publication{}, fmt.Errorf("load repository packets: %w", err)
	}
	path, envelope, err := packetexport.Publish(ctx, tracker, outputDirectory, repositoryRoot, now)
	if err != nil {
		return publication{}, err
	}
	verified, err := packetexport.VerifyFile(path, now)
	if err != nil {
		return publication{}, fmt.Errorf("verify emitted packet export: %w", err)
	}
	return publication{
		path: path, packetCount: len(verified.Packets), schema: envelope.Schema,
		expiresAt: envelope.ExpiresAt, repository: envelope.Source.Repository, commit: envelope.Source.Commit,
	}, nil
}

func fetchTenantDirectory(ctx context.Context, client *http.Client, rawURL string, now time.Time) (*tenant.Directory, error) {
	if err := validateEndpoint(rawURL); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			return validateEndpoint(request.URL.String())
		}}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("create tenant-directory request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("fetch tenant directory")
	}
	defer response.Body.Close()
	if response.Request == nil || validateEndpoint(response.Request.URL.String()) != nil {
		return nil, errors.New("tenant directory redirected to an unsafe endpoint")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tenant directory returned HTTP %d", response.StatusCode)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximumDirectoryBytes+1))
	if err != nil {
		return nil, errors.New("read tenant directory")
	}
	if len(contents) > maximumDirectoryBytes {
		return nil, errors.New("tenant directory exceeds size limit")
	}
	directory, err := tenant.Parse(contents, now)
	if err != nil {
		return nil, fmt.Errorf("verify tenant directory: %w", err)
	}
	return directory, nil
}

func soleActiveTenant(directory *tenant.Directory) (packet.TenantID, error) {
	var active []tenant.Record
	for _, record := range directory.Records() {
		if record.Status == tenant.StatusActive {
			active = append(active, record)
		}
	}
	if len(active) != 1 {
		return "", fmt.Errorf("tenant directory must contain exactly one active tenant; found %d", len(active))
	}
	return packet.TenantID(active[0].ID), nil
}

func validateEndpoint(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" {
		return errors.New("tenant-directory endpoint must be an absolute URL")
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("tenant-directory endpoint must not contain credentials, a query, or a fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopback(parsed.Hostname()) {
		return nil
	}
	return errors.New("tenant-directory endpoint must use HTTPS (plain HTTP is allowed only for a local fixture)")
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
