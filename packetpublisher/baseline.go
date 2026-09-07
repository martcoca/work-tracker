package packetpublisher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/martcoca/work-tracker/packetexport"
)

const maximumExportBytes = 16 << 20

// HTTPBaseline fetches the separately published repository migration source. It accepts
// no credential and verifies the exact packet contract before returning bytes.
type HTTPBaseline struct {
	url     string
	client  *http.Client
	timeout time.Duration
}

func NewHTTPBaseline(rawURL string, client *http.Client, timeout time.Duration) (*HTTPBaseline, error) {
	if err := validatePublicEndpoint(rawURL); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		return nil, errors.New("repository export fetch timeout must be positive")
	}
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPBaseline{url: rawURL, client: client, timeout: timeout}, nil
}

func (baseline *HTTPBaseline) Verified(at time.Time) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), baseline.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseline.url, nil)
	if err != nil {
		return nil, errors.New("create repository export request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := baseline.client.Do(request)
	if err != nil {
		return nil, errors.New("fetch repository export")
	}
	defer response.Body.Close()
	if response.Request == nil || validatePublicEndpoint(response.Request.URL.String()) != nil {
		return nil, errors.New("repository export redirected to an unsafe endpoint")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("repository export returned HTTP %d", response.StatusCode)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err == nil && mediaType != "" && mediaType != "application/json" {
		return nil, fmt.Errorf("repository export returned %s", mediaType)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximumExportBytes+1))
	if err != nil {
		return nil, errors.New("read repository export")
	}
	if len(contents) > maximumExportBytes {
		return nil, errors.New("repository export exceeds size limit")
	}
	if _, err := packetexport.Verify(contents, at); err != nil {
		return nil, err
	}
	return contents, nil
}

func validatePublicEndpoint(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("repository export endpoint must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	address := net.ParseIP(parsed.Hostname())
	if parsed.Scheme == "http" && (strings.EqualFold(parsed.Hostname(), "localhost") || address != nil && address.IsLoopback()) {
		return nil
	}
	return errors.New("repository export endpoint must use HTTPS")
}
