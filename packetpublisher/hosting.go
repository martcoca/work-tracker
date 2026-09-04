package packetpublisher

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	hostingAPI       = "https://firebasehosting.googleapis.com"
	maximumAPIBytes  = 2 << 20
	defaultPollDelay = 250 * time.Millisecond
)

var siteIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)

// HostingDestination updates only packets.json. It clones the live version first, so
// the frontend, headers, and pinned Cloud Run rewrite remain byte-for-byte/config-for-
// config identical. A release is created only after upload and finalization succeed.
type HostingDestination struct {
	siteID    string
	client    *http.Client
	baseURL   string
	pollDelay time.Duration
}

func NewHostingDestination(siteID string, client *http.Client) (*HostingDestination, error) {
	if !siteIDPattern.MatchString(siteID) {
		return nil, errors.New("Firebase Hosting site id is required and must be a hostname label")
	}
	if client == nil {
		return nil, errors.New("authenticated Hosting HTTP client is required")
	}
	return &HostingDestination{siteID: siteID, client: client, baseURL: hostingAPI, pollDelay: defaultPollDelay}, nil
}

func (destination *HostingDestination) Publish(ctx context.Context, contents []byte, message string) error {
	if len(contents) == 0 {
		return errors.New("refuse empty packets.json")
	}
	currentVersion, err := destination.currentVersion(ctx)
	if err != nil {
		return err
	}
	version, err := destination.cloneVersion(ctx, currentVersion)
	if err != nil {
		return err
	}
	compressed, hash, err := gzipHash(contents)
	if err != nil {
		return err
	}
	uploadURL, uploadRequired, err := destination.populate(ctx, version, hash)
	if err != nil {
		return err
	}
	if uploadRequired {
		if err := destination.upload(ctx, uploadURL, hash, compressed); err != nil {
			return err
		}
	}
	if err := destination.finalize(ctx, version); err != nil {
		return err
	}
	return destination.release(ctx, version, message)
}

type channelResponse struct {
	Release struct {
		Version struct {
			Name string `json:"name"`
		} `json:"version"`
	} `json:"release"`
}

func (destination *HostingDestination) currentVersion(ctx context.Context) (string, error) {
	var channel channelResponse
	path := destination.sitePath("/channels/live")
	if err := destination.request(ctx, http.MethodGet, path, nil, &channel); err != nil {
		return "", fmt.Errorf("read live Hosting channel: %w", err)
	}
	if !destination.validVersionName(channel.Release.Version.Name) {
		return "", errors.New("live Hosting channel returned no version for this site")
	}
	return channel.Release.Version.Name, nil
}

type operation struct {
	Name     string          `json:"name"`
	Done     bool            `json:"done"`
	Error    *operationError `json:"error,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
}

type operationError struct {
	Code   int    `json:"code"`
	Status string `json:"status"`
}

type versionResponse struct {
	Name string `json:"name"`
}

func (destination *HostingDestination) cloneVersion(ctx context.Context, source string) (string, error) {
	body := map[string]any{"sourceVersion": source, "finalize": false}
	var pending operation
	path := destination.sitePath("/versions:clone")
	if err := destination.request(ctx, http.MethodPost, path, body, &pending); err != nil {
		return "", fmt.Errorf("clone live Hosting version: %w", err)
	}
	if pending.Name == "" {
		return "", errors.New("clone live Hosting version returned no operation")
	}
	completed, err := destination.waitOperation(ctx, pending)
	if err != nil {
		return "", err
	}
	var version versionResponse
	if err := json.Unmarshal(completed.Response, &version); err != nil || !destination.validVersionName(version.Name) {
		return "", errors.New("clone operation returned no created version for this site")
	}
	return version.Name, nil
}

func (destination *HostingDestination) waitOperation(ctx context.Context, pending operation) (operation, error) {
	for {
		if pending.Done {
			if pending.Error != nil {
				return operation{}, fmt.Errorf("Hosting clone operation failed: code=%d status=%s", pending.Error.Code, pending.Error.Status)
			}
			return pending, nil
		}
		if !validResourceName(pending.Name, "projects/", "/operations/") {
			return operation{}, errors.New("Hosting clone returned an invalid operation name")
		}
		timer := time.NewTimer(destination.pollDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return operation{}, ctx.Err()
		case <-timer.C:
		}
		if err := destination.request(ctx, http.MethodGet, "/v1beta1/"+pending.Name, nil, &pending); err != nil {
			return operation{}, fmt.Errorf("poll Hosting clone operation: %w", err)
		}
	}
}

type populateResponse struct {
	UploadRequiredHashes []string `json:"uploadRequiredHashes"`
	UploadURL            string   `json:"uploadUrl"`
}

func (destination *HostingDestination) populate(ctx context.Context, version, hash string) (string, bool, error) {
	var populated populateResponse
	body := map[string]any{"files": map[string]string{"/packets.json": hash}}
	if err := destination.request(ctx, http.MethodPost, "/v1beta1/"+version+":populateFiles", body, &populated); err != nil {
		return "", false, fmt.Errorf("attach packets.json to Hosting version: %w", err)
	}
	required := false
	for _, candidate := range populated.UploadRequiredHashes {
		if candidate != hash {
			return "", false, errors.New("Hosting requested an unknown content hash")
		}
		required = true
	}
	if required {
		if err := destination.validateUploadURL(populated.UploadURL); err != nil {
			return "", false, err
		}
	}
	return populated.UploadURL, required, nil
}

func (destination *HostingDestination) upload(ctx context.Context, uploadURL, hash string, compressed []byte) error {
	target := strings.TrimRight(uploadURL, "/") + "/" + hash
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(compressed))
	if err != nil {
		return errors.New("create Hosting upload request")
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := destination.client.Do(request)
	if err != nil {
		return errors.New("send Hosting upload request")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumAPIBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("upload packets.json: Hosting returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (destination *HostingDestination) finalize(ctx context.Context, version string) error {
	path := "/v1beta1/" + version + "?updateMask=status"
	var finalized struct {
		Status string `json:"status"`
	}
	if err := destination.request(ctx, http.MethodPatch, path, map[string]string{"status": "FINALIZED"}, &finalized); err != nil {
		return fmt.Errorf("finalize Hosting version: %w", err)
	}
	if finalized.Status != "FINALIZED" {
		return errors.New("Hosting version did not finalize")
	}
	return nil
}

func (destination *HostingDestination) release(ctx context.Context, version, message string) error {
	query := url.Values{"versionName": []string{version}}
	path := destination.sitePath("/channels/live/releases?") + query.Encode()
	var released struct {
		Version versionResponse `json:"version"`
	}
	if err := destination.request(ctx, http.MethodPost, path, map[string]string{"message": message}, &released); err != nil {
		return fmt.Errorf("release Hosting version: %w", err)
	}
	if released.Version.Name != version {
		return errors.New("Hosting released an unexpected version")
	}
	return nil
}

func (destination *HostingDestination) request(ctx context.Context, method, path string, body any, result any) error {
	var encoded io.Reader
	if body != nil {
		contents, err := json.Marshal(body)
		if err != nil {
			return errors.New("encode Hosting request")
		}
		encoded = bytes.NewReader(contents)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(destination.baseURL, "/")+path, encoded)
	if err != nil {
		return errors.New("create Hosting API request")
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := destination.client.Do(request)
	if err != nil {
		return errors.New("send Hosting API request")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumAPIBytes))
		return fmt.Errorf("Hosting returned HTTP %d", response.StatusCode)
	}
	if result == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumAPIBytes))
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumAPIBytes))
	if err := decoder.Decode(result); err != nil {
		return errors.New("decode Hosting API response")
	}
	return nil
}

func (destination *HostingDestination) validVersionName(name string) bool {
	parts := strings.Split(name, "/")
	return len(parts) == 6 && parts[0] == "projects" && parts[1] != "" &&
		parts[2] == "sites" && parts[3] == destination.siteID &&
		parts[4] == "versions" && parts[5] != "" &&
		!strings.Contains(name, "..") && !strings.ContainsAny(name, "?#")
}

func (destination *HostingDestination) sitePath(suffix string) string {
	return fmt.Sprintf("/v1beta1/projects/-/sites/%s%s", destination.siteID, suffix)
}

func validResourceName(name, prefix, infix string) bool {
	if !strings.HasPrefix(name, prefix) || strings.Contains(name, "..") || strings.ContainsAny(name, "?#") {
		return false
	}
	rest := strings.TrimPrefix(name, prefix)
	if infix == "" {
		return rest != "" && !strings.Contains(rest, "/")
	}
	parts := strings.Split(rest, infix)
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[1], "/")
}

func (destination *HostingDestination) validateUploadURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Hosting returned an unsafe upload URL")
	}
	if destination.baseURL == hostingAPI {
		if parsed.Scheme != "https" || parsed.Host != "upload-firebasehosting.googleapis.com" {
			return errors.New("Hosting returned an unsafe upload URL")
		}
		return nil
	}
	base, _ := url.Parse(destination.baseURL)
	if parsed.Scheme != base.Scheme || parsed.Host != base.Host {
		return errors.New("Hosting fixture returned an unexpected upload URL")
	}
	return nil
}

func gzipHash(contents []byte) ([]byte, string, error) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(contents); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(compressed.Bytes())
	return compressed.Bytes(), hex.EncodeToString(digest[:]), nil
}
