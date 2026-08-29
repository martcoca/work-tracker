// Package identity verifies Identity Platform ID tokens without granting the tracker
// administrative access to human accounts.
package identity

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const firebaseCertificateURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"

var (
	ErrInvalidToken       = errors.New("invalid identity token")
	ErrTenantClaimMissing = errors.New("identity token has no tenant claim")
)

// Principal contains only the signed facts the read path uses.
type Principal struct {
	Subject  string
	TenantID string
}

// Verifier authenticates an Identity Platform ID token.
type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

// FirebaseVerifier validates RS256 Firebase ID tokens and caches Google's public
// certificates for the max-age returned by the certificate endpoint.
type FirebaseVerifier struct {
	projectID      string
	client         *http.Client
	certificateURL string
	now            func() time.Time

	mu         sync.Mutex
	keys       map[string]*rsa.PublicKey
	keysExpire time.Time
}

// NewFirebaseVerifier constructs a verifier for one configured Firebase project.
func NewFirebaseVerifier(projectID string, client *http.Client) (*FirebaseVerifier, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("firebase project id is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &FirebaseVerifier{
		projectID:      projectID,
		client:         client,
		certificateURL: firebaseCertificateURL,
		now:            time.Now,
	}, nil
}

// Verify checks the signature and every Identity Platform claim required by the
// documented Firebase ID-token contract before returning the tenant claim.
func (verifier *FirebaseVerifier) Verify(ctx context.Context, token string) (Principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, fmt.Errorf("%w: token must have three segments", ErrInvalidToken)
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Principal{}, fmt.Errorf("%w: decode header", ErrInvalidToken)
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return Principal{}, fmt.Errorf("%w: decode header", ErrInvalidToken)
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		return Principal{}, fmt.Errorf("%w: unsupported signing header", ErrInvalidToken)
	}
	key, err := verifier.key(ctx, header.KeyID)
	if err != nil {
		return Principal{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Principal{}, fmt.Errorf("%w: decode signature", ErrInvalidToken)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return Principal{}, fmt.Errorf("%w: signature verification failed", ErrInvalidToken)
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Principal{}, fmt.Errorf("%w: decode claims", ErrInvalidToken)
	}
	var claims struct {
		Audience      string `json:"aud"`
		Issuer        string `json:"iss"`
		Subject       string `json:"sub"`
		ExpiresAt     int64  `json:"exp"`
		IssuedAt      int64  `json:"iat"`
		Authenticated int64  `json:"auth_time"`
		TenantID      string `json:"tenant_id"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return Principal{}, fmt.Errorf("%w: decode claims", ErrInvalidToken)
	}
	now := verifier.now().UTC().Unix()
	if claims.Audience != verifier.projectID || claims.Issuer != "https://securetoken.google.com/"+verifier.projectID {
		return Principal{}, fmt.Errorf("%w: wrong audience or issuer", ErrInvalidToken)
	}
	if claims.Subject == "" || len(claims.Subject) > 128 || claims.ExpiresAt <= now ||
		claims.IssuedAt <= 0 || claims.IssuedAt > now || claims.Authenticated <= 0 || claims.Authenticated > now {
		return Principal{}, fmt.Errorf("%w: invalid subject or token times", ErrInvalidToken)
	}
	if strings.TrimSpace(claims.TenantID) == "" {
		return Principal{}, ErrTenantClaimMissing
	}
	return Principal{Subject: claims.Subject, TenantID: claims.TenantID}, nil
}

func (verifier *FirebaseVerifier) key(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	verifier.mu.Lock()
	defer verifier.mu.Unlock()

	now := verifier.now()
	if key := verifier.keys[keyID]; key != nil && now.Before(verifier.keysExpire) {
		return key, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, verifier.certificateURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create certificate request", ErrInvalidToken)
	}
	response, err := verifier.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch signing certificates: %v", ErrInvalidToken, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: certificate endpoint returned %s", ErrInvalidToken, response.Status)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read signing certificates", ErrInvalidToken)
	}
	var certificates map[string]string
	if err := json.Unmarshal(contents, &certificates); err != nil {
		return nil, fmt.Errorf("%w: decode signing certificates", ErrInvalidToken)
	}
	keys := make(map[string]*rsa.PublicKey, len(certificates))
	for id, encoded := range certificates {
		block, _ := pem.Decode([]byte(encoded))
		if block == nil {
			return nil, fmt.Errorf("%w: certificate %q is not PEM", ErrInvalidToken, id)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: parse certificate %q", ErrInvalidToken, id)
		}
		publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: certificate %q is not RSA", ErrInvalidToken, id)
		}
		keys[id] = publicKey
	}
	verifier.keys = keys
	verifier.keysExpire = now.Add(cacheMaxAge(response.Header.Get("Cache-Control")))
	key := keys[keyID]
	if key == nil {
		return nil, fmt.Errorf("%w: signing key %q is unknown", ErrInvalidToken, keyID)
	}
	return key, nil
}

func cacheMaxAge(header string) time.Duration {
	for _, directive := range strings.Split(header, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(directive), "=")
		if found && strings.EqualFold(name, "max-age") {
			seconds, err := strconv.ParseInt(strings.Trim(value, "\""), 10, 64)
			if err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return 5 * time.Minute
}
