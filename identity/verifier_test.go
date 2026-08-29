package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFirebaseVerifierAuthenticatesSignedTenantClaim(t *testing.T) {
	clock := time.Date(2035, time.April, 5, 12, 0, 0, 0, time.UTC)
	privateKey, certificate := testCertificate(t, clock)
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requests++
		body, _ := json.Marshal(map[string]string{"synthetic-key": certificate})
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK",
			Header: http.Header{"Cache-Control": []string{"public, max-age=3600"}},
			Body:   io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}

	verifier, err := NewFirebaseVerifier("synthetic-project", client)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return clock }
	token := signedToken(t, privateKey, "synthetic-key", map[string]any{
		"aud": "synthetic-project", "iss": "https://securetoken.google.com/synthetic-project",
		"sub": "human-synthetic", "tenant_id": "tenant-synthetic",
		"exp": clock.Add(time.Hour).Unix(), "iat": clock.Add(-time.Minute).Unix(), "auth_time": clock.Add(-time.Minute).Unix(),
	})

	for range 2 {
		principal, err := verifier.Verify(context.Background(), token)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if principal.Subject != "human-synthetic" || principal.TenantID != "tenant-synthetic" {
			t.Fatalf("principal = %+v", principal)
		}
	}
	if requests != 1 {
		t.Fatalf("certificate requests = %d, want cached single request", requests)
	}
}

func TestFirebaseVerifierRefusesTamperingAndInvalidClaims(t *testing.T) {
	clock := time.Date(2035, time.April, 5, 12, 0, 0, 0, time.UTC)
	privateKey, certificate := testCertificate(t, clock)
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]string{"synthetic-key": certificate})
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Cache-Control": []string{"max-age=60"}},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}
	verifier, _ := NewFirebaseVerifier("synthetic-project", client)
	verifier.now = func() time.Time { return clock }
	baseClaims := map[string]any{
		"aud": "synthetic-project", "iss": "https://securetoken.google.com/synthetic-project",
		"sub": "human-synthetic", "tenant_id": "tenant-synthetic",
		"exp": clock.Add(time.Hour).Unix(), "iat": clock.Unix(), "auth_time": clock.Unix(),
	}

	valid := signedToken(t, privateKey, "synthetic-key", baseClaims)
	tampered := valid[:len(valid)-1] + "A"
	if _, err := verifier.Verify(context.Background(), tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered error = %v", err)
	}

	missingTenant := cloneClaims(baseClaims)
	delete(missingTenant, "tenant_id")
	if _, err := verifier.Verify(context.Background(), signedToken(t, privateKey, "synthetic-key", missingTenant)); !errors.Is(err, ErrTenantClaimMissing) {
		t.Fatalf("missing tenant error = %v", err)
	}

	wrongAudience := cloneClaims(baseClaims)
	wrongAudience["aud"] = "different-project"
	if _, err := verifier.Verify(context.Background(), signedToken(t, privateKey, "synthetic-key", wrongAudience)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong audience error = %v", err)
	}

	expired := cloneClaims(baseClaims)
	expired["exp"] = clock.Unix()
	if _, err := verifier.Verify(context.Background(), signedToken(t, privateKey, "synthetic-key", expired)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired error = %v", err)
	}
}

func testCertificate(t *testing.T, at time.Time) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic.invalid"},
		NotBefore: at.Add(-time.Hour), NotAfter: at.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func signedToken(t *testing.T, key *rsa.PrivateKey, keyID string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": keyID, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256Sum([]byte(unsigned))
	signature, err := key.Sign(rand.Reader, digest, crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func sha256Sum(value []byte) []byte {
	digest := crypto.SHA256.New()
	_, _ = digest.Write(value)
	return digest.Sum(nil)
}

func cloneClaims(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
