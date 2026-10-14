// Package catrust fetches step-ca's own root certificate so step-ui can
// trust services whose certificates were issued by that CA -- an internal
// ntfy server or MQTT broker for alerts, an identity provider for SSO.
//
// The root is downloaded from CA_URL and only accepted if its SHA-256
// fingerprint matches CA_ROOT_FINGERPRINT, the same trust-on-fingerprint
// check the step CLI itself uses. Without a fingerprint configured nothing
// is fetched: an unverified root is worse than none.
package catrust

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// cacheFor: how long a fetched root is reused before fetching again.
const cacheFor = 24 * time.Hour

type Fetcher struct {
	caURL string
	fp    string

	mu      sync.Mutex
	pem     []byte
	at      time.Time
	lastErr string
}

func New(caURL, fingerprint string) *Fetcher {
	return &Fetcher{
		caURL: strings.TrimRight(caURL, "/"),
		fp:    strings.ToLower(strings.ReplaceAll(strings.TrimSpace(fingerprint), ":", "")),
	}
}

// Root returns the CA root as PEM, fetching it if not cached.
func (f *Fetcher) Root(ctx context.Context) ([]byte, error) {
	f.mu.Lock()
	if f.pem != nil && time.Since(f.at) < cacheFor {
		defer f.mu.Unlock()
		return f.pem, nil
	}
	f.mu.Unlock()

	pemBytes, err := f.fetch(ctx)

	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.lastErr = err.Error()
		if f.pem != nil {
			// Keep using the previously verified root rather than
			// dropping trust because one refresh failed.
			return f.pem, nil
		}
		return nil, err
	}
	f.pem, f.at, f.lastErr = pemBytes, time.Now(), ""
	return pemBytes, nil
}

func (f *Fetcher) fetch(ctx context.Context) ([]byte, error) {
	if f.caURL == "" || f.fp == "" {
		return nil, errors.New("CA_URL and CA_ROOT_FINGERPRINT are needed to trust the CA root")
	}
	// The root can't be verified by TLS before we have it -- that's the
	// point of the fingerprint check below.
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // verified by fingerprint below
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.caURL+"/root/"+f.fp, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching CA root: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching CA root: HTTP %d", resp.StatusCode)
	}
	var body struct {
		CA string `json:"ca"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("fetching CA root: %w", err)
	}
	block, _ := pem.Decode([]byte(body.CA))
	if block == nil {
		return nil, errors.New("CA root response contained no certificate")
	}
	sum := sha256.Sum256(block.Bytes)
	if hex.EncodeToString(sum[:]) != f.fp {
		return nil, errors.New("CA root fingerprint doesn't match CA_ROOT_FINGERPRINT -- not trusting it")
	}
	return pem.EncodeToMemory(block), nil
}

// Trusted reports whether a verified root is currently held.
func (f *Fetcher) Trusted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pem != nil
}

// LastError is the most recent fetch failure, or "".
func (f *Fetcher) LastError() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastErr
}

// Pool builds a certificate pool from the system roots plus, optionally,
// step-ca's root and any extra PEM certificates. A failure to get the CA
// root is returned as a warning alongside a pool that still works for
// everything else.
func (f *Fetcher) Pool(ctx context.Context, trustCARoot bool, extraPEM string) (pool *x509.CertPool, warning error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if trustCARoot {
		if root, err := f.Root(ctx); err != nil {
			warning = err
		} else {
			pool.AppendCertsFromPEM(root)
		}
	}
	if strings.TrimSpace(extraPEM) != "" {
		pool.AppendCertsFromPEM([]byte(extraPEM))
	}
	return pool, warning
}

// ValidatePEM checks that s contains at least one certificate.
func ValidatePEM(s string) error {
	rest := []byte(s)
	found := 0
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("invalid certificate in PEM: %w", err)
		}
		found++
	}
	if found == 0 {
		return errors.New("no certificates found -- paste one or more -----BEGIN CERTIFICATE----- blocks")
	}
	return nil
}
