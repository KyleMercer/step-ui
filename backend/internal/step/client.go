package step

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// stepCmdTimeout bounds every shell-out to the step CLI (or openssl) so a
// hung or unreachable CA can't block a request forever. step CLI calls go
// over the network to the CA, so this needs to be generous enough for a
// slow connection but not unbounded.
const stepCmdTimeout = 30 * time.Second

// runCommand runs name with args under stepCmdTimeout and returns combined
// stdout+stderr, wrapping the error with that output on failure so callers
// keep the same diagnostics they had before.
func runCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), stepCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("%s command timed out after %s: %s", name, stepCmdTimeout, string(output))
	}
	if err != nil {
		return output, fmt.Errorf("%s command failed: %s, error: %w", name, string(output), err)
	}
	return output, nil
}

type CertBundle struct {
	CN           string
	SANs         []string
	CertPEM      []byte
	KeyPEM       []byte
	ChainPEM     []byte
	FullChainPEM []byte
	PFXData      []byte
	Serial       string
	NotAfter     time.Time
}

// extractJWTToken pulls the JWT out of `step ca token` output. The step CLI
// prints some decorative/status text before the actual token, so we look
// for the line that looks like a JWT (starts with the "ey" of a base64url
// "{" header) rather than trusting the whole output to be just the token.
func extractJWTToken(output []byte) (string, error) {
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ey") {
			return trimmed, nil
		}
	}
	return "", fmt.Errorf("failed to extract JWT token from step ca token output: %s", string(output))
}

// parseCSRSubject decodes a PEM-encoded CSR, verifies its self-signature
// (proving the requester holds the private key for the embedded public
// key), and returns the CN and DNS SANs it's actually requesting.
func parseCSRSubject(csrPEM string) (cn string, sans []string, err error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		return "", nil, fmt.Errorf("failed to decode PEM block from CSR")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse CSR: %w", err)
	}

	if err := csr.CheckSignature(); err != nil {
		return "", nil, fmt.Errorf("CSR signature verification failed: %w", err)
	}

	return csr.Subject.CommonName, csr.DNSNames, nil
}

type StepClient struct {
	CAURL               string
	CARootFingerprint   string
	ProvisionerName     string
	ProvisionerPassword string
}

func NewStepClient(caURL, caRootFingerprint, provisionerName, provisionerPassword string) *StepClient {
	return &StepClient{
		CAURL:               caURL,
		CARootFingerprint:   caRootFingerprint,
		ProvisionerName:     provisionerName,
		ProvisionerPassword: provisionerPassword,
	}
}

func (s *StepClient) IssueCertificate(cn string, sans []string, notAfterDays int) (*CertBundle, error) {
	// Create temporary directory for certificate files
	tempDir, err := os.MkdirTemp("", "step-cert-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	certPath := filepath.Join(tempDir, "cert.crt")
	keyPath := filepath.Join(tempDir, "cert.key")
	passwordFile := filepath.Join(tempDir, "password.txt")
	rootPath := filepath.Join(tempDir, "root.crt")

	// Write password to file. 0600 (not the previous 0644) so it isn't
	// world-readable even though tempDir itself is already 0700.
	if err := os.WriteFile(passwordFile, []byte(s.ProvisionerPassword), 0600); err != nil {
		return nil, fmt.Errorf("failed to write password file: %w", err)
	}

	// Download root certificate
	rootArgs := []string{
		"ca", "root",
		rootPath,
		"--ca-url", s.CAURL,
	}
	if s.CARootFingerprint != "" {
		rootArgs = append(rootArgs, "--fingerprint", s.CARootFingerprint)
	}
	if _, err := runCommand("step", rootArgs...); err != nil {
		return nil, fmt.Errorf("failed to fetch CA root: %w", err)
	}

	// First, generate a token
	// Note: --not-after for token is token validity (default 5m), not certificate validity
	tokenArgs := []string{
		"ca", "token",
		cn,
		"--ca-url", s.CAURL,
		"--root", rootPath,
		"--provisioner", s.ProvisionerName,
		"--provisioner-password-file", passwordFile,
	}

	// Add SANs to token command if provided
	for _, san := range sans {
		tokenArgs = append(tokenArgs, "--san", san)
	}

	tokenOutput, err := runCommand("step", tokenArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA token: %w", err)
	}

	token, err := extractJWTToken(tokenOutput)
	if err != nil {
		return nil, err
	}

	// Now use the token to issue certificate
	certArgs := []string{
		"ca", "certificate",
		cn,
		certPath,
		keyPath,
		"--token", token,
		"--ca-url", s.CAURL,
		"--root", rootPath,
		"--not-after", fmt.Sprintf("%dh", notAfterDays*24),
	}

	if _, err := runCommand("step", certArgs...); err != nil {
		return nil, fmt.Errorf("failed to issue certificate: %w", err)
	}

	// Read certificate and key files
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cert file: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	// Get certificate chain
	chainPEM, err := s.getChain(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain: %w", err)
	}

	// Create full chain
	fullChainPEM := append(certPEM, chainPEM...)

	// Extract serial number and expiry from certificate
	serial, notAfter, err := s.parseCertificate(certPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &CertBundle{
		CN:           cn,
		SANs:         sans,
		CertPEM:      certPEM,
		KeyPEM:       keyPEM,
		ChainPEM:     chainPEM,
		FullChainPEM: fullChainPEM,
		Serial:       serial,
		NotAfter:     notAfter,
	}, nil
}

func (s *StepClient) SignCSR(csrPEM string, notAfterDays int) (*CertBundle, error) {
	cn, sans, err := parseCSRSubject(csrPEM)
	if err != nil {
		return nil, fmt.Errorf("invalid CSR: %w", err)
	}
	if cn == "" {
		return nil, fmt.Errorf("invalid CSR: no common name (CN) in subject")
	}

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "step-csr-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	csrPath := filepath.Join(tempDir, "csr.pem")
	certPath := filepath.Join(tempDir, "cert.crt")
	passwordFile := filepath.Join(tempDir, "password.txt")
	rootPath := filepath.Join(tempDir, "root.crt")

	// Write CSR to file
	if err := os.WriteFile(csrPath, []byte(csrPEM), 0644); err != nil {
		return nil, fmt.Errorf("failed to write CSR file: %w", err)
	}

	// Write password to file
	if err := os.WriteFile(passwordFile, []byte(s.ProvisionerPassword), 0600); err != nil {
		return nil, fmt.Errorf("failed to write password file: %w", err)
	}

	// Download root certificate
	rootArgs := []string{
		"ca", "root",
		rootPath,
		"--ca-url", s.CAURL,
	}
	if s.CARootFingerprint != "" {
		rootArgs = append(rootArgs, "--fingerprint", s.CARootFingerprint)
	}
	if _, err := runCommand("step", rootArgs...); err != nil {
		return nil, fmt.Errorf("failed to fetch CA root: %w", err)
	}

	// Generate a token for the CSR's actual subject (and SANs) -- not a
	// generic placeholder. step-ca checks the CSR's SANs against the
	// token's authorized identifiers when signing, so a token that
	// doesn't carry them would get rejected for any CSR with SANs anyway.
	// Note: --not-after for token is token validity (default 5m), not certificate validity
	tokenArgs := []string{
		"ca", "token",
		cn,
		"--ca-url", s.CAURL,
		"--root", rootPath,
		"--provisioner", s.ProvisionerName,
		"--provisioner-password-file", passwordFile,
	}
	for _, san := range sans {
		tokenArgs = append(tokenArgs, "--san", san)
	}

	tokenOutput, err := runCommand("step", tokenArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA token: %w", err)
	}

	token, err := extractJWTToken(tokenOutput)
	if err != nil {
		return nil, err
	}

	// Now use the token to sign CSR
	args := []string{
		"ca", "sign",
		csrPath,
		certPath,
		"--token", token,
		"--ca-url", s.CAURL,
		"--root", rootPath,
		"--not-after", fmt.Sprintf("%dh", notAfterDays*24),
	}

	if _, err := runCommand("step", args...); err != nil {
		return nil, fmt.Errorf("failed to sign CSR: %w", err)
	}

	// Read certificate file
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cert file: %w", err)
	}

	// Get certificate chain
	chainPEM, err := s.getChain(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain: %w", err)
	}

	// Create full chain
	fullChainPEM := append(certPEM, chainPEM...)

	// Extract serial number and expiry from certificate
	serial, notAfter, err := s.parseCertificate(certPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &CertBundle{
		CN:           cn,
		SANs:         sans,
		CertPEM:      certPEM,
		ChainPEM:     chainPEM,
		FullChainPEM: fullChainPEM,
		Serial:       serial,
		NotAfter:     notAfter,
	}, nil
}

func (s *StepClient) RevokeCertificate(serial string) error {
	if serial == "" {
		return fmt.Errorf("cannot revoke: no serial number on record for this certificate")
	}

	tempDir, err := os.MkdirTemp("", "step-revoke-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	rootPath := filepath.Join(tempDir, "root.crt")
	passwordFile := filepath.Join(tempDir, "password.txt")

	if err := os.WriteFile(passwordFile, []byte(s.ProvisionerPassword), 0600); err != nil {
		return fmt.Errorf("failed to write password file: %w", err)
	}

	rootArgs := []string{
		"ca", "root",
		rootPath,
		"--ca-url", s.CAURL,
	}
	if s.CARootFingerprint != "" {
		rootArgs = append(rootArgs, "--fingerprint", s.CARootFingerprint)
	}
	if _, err := runCommand("step", rootArgs...); err != nil {
		return fmt.Errorf("failed to fetch CA root: %w", err)
	}

	// `step ca revoke` does not accept --provisioner/--provisioner-password-file
	// itself -- those only belong to `step ca token`. Passing them directly to
	// revoke doesn't error as an unknown flag; step's parser just doesn't
	// recognize them for this subcommand and miscounts them as extra
	// positional arguments ("too many positional arguments were provided").
	// Revoking by serial number needs a one-time token scoped specifically
	// for revocation instead, generated the same way IssueCertificate and
	// SignCSR generate their issuance tokens above.
	tokenArgs := []string{
		"ca", "token",
		serial,
		"--revoke",
		"--ca-url", s.CAURL,
		"--root", rootPath,
		"--provisioner", s.ProvisionerName,
		"--provisioner-password-file", passwordFile,
	}

	tokenOutput, err := runCommand("step", tokenArgs...)
	if err != nil {
		return fmt.Errorf("failed to generate revocation token: %w", err)
	}

	token, err := extractJWTToken(tokenOutput)
	if err != nil {
		return err
	}

	args := []string{
		"ca", "revoke",
		serial,
		"--token", token,
		"--ca-url", s.CAURL,
		"--root", rootPath,
	}

	if _, err := runCommand("step", args...); err != nil {
		return fmt.Errorf("failed to revoke certificate: %w", err)
	}

	return nil
}

func (s *StepClient) CreatePFX(certPEM, keyPEM, password string) ([]byte, error) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "step-pfx-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	certPath := filepath.Join(tempDir, "cert.crt")
	keyPath := filepath.Join(tempDir, "cert.key")
	pfxPath := filepath.Join(tempDir, "cert.p12")
	passwordPath := filepath.Join(tempDir, "password.txt")

	// Write certificate and key files
	if err := os.WriteFile(certPath, []byte(certPEM), 0644); err != nil {
		return nil, fmt.Errorf("failed to write cert file: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(keyPEM), 0644); err != nil {
		return nil, fmt.Errorf("failed to write key file: %w", err)
	}
	if err := os.WriteFile(passwordPath, []byte(password), 0600); err != nil {
		return nil, fmt.Errorf("failed to write password file: %w", err)
	}

	// Build step command
	args := []string{
		"certificate", "p12",
		pfxPath,
		certPath,
		keyPath,
		"--password-file", passwordPath,
	}

	if _, err := runCommand("step", args...); err != nil {
		return nil, fmt.Errorf("failed to create PFX bundle: %w", err)
	}

	// Read PFX file
	pfxData, err := os.ReadFile(pfxPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read PFX file: %w", err)
	}

	return pfxData, nil
}

func (s *StepClient) getChain(certPath string) ([]byte, error) {
	// The step ca certificate command already provides the certificate
	// We need to get the CA chain (intermediate + root)
	// Download the root certificate using step ca root

	tempDir, err := os.MkdirTemp("", "step-chain-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	rootPath := filepath.Join(tempDir, "root.crt")

	// Download root certificate
	rootArgs := []string{
		"ca", "root",
		rootPath,
		"--ca-url", s.CAURL,
	}
	if s.CARootFingerprint != "" {
		rootArgs = append(rootArgs, "--fingerprint", s.CARootFingerprint)
	}
	if _, err := runCommand("step", rootArgs...); err != nil {
		return nil, fmt.Errorf("failed to fetch CA root: %w", err)
	}

	// Read the root certificate
	chainPEM, err := os.ReadFile(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read root certificate: %w", err)
	}

	return chainPEM, nil
}

// parseCertificate reads the serial number and expiry out of a PEM
// certificate using Go's own crypto/x509 rather than shelling out to
// openssl. This is not just simpler: openssl's `-serial` output is hex,
// while step-ca's own CLI and revocation API expect the serial as a plain
// decimal integer (see e.g. the decimal serial in `step ca revoke`'s own
// docs/examples) -- so the previous openssl-based version was recording
// certificates under a serial format the CA wouldn't recognize for revoke.
func (s *StepClient) parseCertificate(certPEM []byte) (string, time.Time, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", time.Time{}, fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert.SerialNumber.String(), cert.NotAfter, nil
}

func (s *StepClient) CreateDownloadBundle(bundle *CertBundle, format string, pfxPassword string) ([]byte, error) {
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add certificate files
	if err := s.addFileToZip(zipWriter, "cert.pem", bundle.CertPEM); err != nil {
		return nil, err
	}
	if err := s.addFileToZip(zipWriter, "chain.pem", bundle.ChainPEM); err != nil {
		return nil, err
	}
	if err := s.addFileToZip(zipWriter, "fullchain.pem", bundle.FullChainPEM); err != nil {
		return nil, err
	}

	// Add private key if available
	if len(bundle.KeyPEM) > 0 {
		if err := s.addFileToZip(zipWriter, "privkey.pem", bundle.KeyPEM); err != nil {
			return nil, err
		}
	}

	// Add PFX if requested
	if format == "pfx" && len(bundle.KeyPEM) > 0 {
		pfxData, err := s.CreatePFX(string(bundle.CertPEM), string(bundle.KeyPEM), pfxPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to create PFX: %w", err)
		}
		if err := s.addFileToZip(zipWriter, "cert.p12", pfxData); err != nil {
			return nil, err
		}
	}

	// Add README with installation instructions
	readme := s.generateReadme(format)
	if err := s.addFileToZip(zipWriter, "README.txt", []byte(readme)); err != nil {
		return nil, err
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip writer: %w", err)
	}

	return buf.Bytes(), nil
}

func (s *StepClient) addFileToZip(zipWriter *zip.Writer, filename string, data []byte) error {
	fileWriter, err := zipWriter.Create(filename)
	if err != nil {
		return err
	}
	_, err = fileWriter.Write(data)
	return err
}

func (s *StepClient) generateReadme(format string) string {
	readme := "# Certificate Installation Instructions\n\n"
	readme += "## Files in this bundle:\n"
	readme += "- cert.pem: Your certificate\n"
	readme += "- chain.pem: Certificate chain (intermediate CAs)\n"
	readme += "- fullchain.pem: Certificate + chain (use this for most applications)\n"
	readme += "- privkey.pem: Private key (keep this secure!)\n"

	if format == "pfx" {
		// Deliberately NOT printing pfxPassword here: it used to be written
		// in plaintext into this README, bundled in the very same zip as
		// the password-protected cert.p12 -- which defeats the point of
		// protecting the PFX with a password at all, since anyone who gets
		// the zip gets both.
		readme += "- cert.p12: PFX/PKCS#12 bundle (password: set on the issue/download form -- not stored in this bundle; check the UI or your password manager)\n"
	}

	readme += "\n## Installation Instructions\n\n"
	readme += "### Linux/Ubuntu (Nginx, Apache, etc.)\n"
	readme += "```bash\n"
	readme += "# Copy files to appropriate locations\n"
	readme += "sudo cp fullchain.pem /etc/ssl/certs/your-domain.crt\n"
	readme += "sudo cp privkey.pem /etc/ssl/private/your-domain.key\n\n"
	readme += "# For Nginx, update your server block:\n"
	readme += "# ssl_certificate /etc/ssl/certs/your-domain.crt;\n"
	readme += "# ssl_certificate_key /etc/ssl/private/your-domain.key;\n\n"
	readme += "# Reload nginx\n"
	readme += "sudo nginx -s reload\n"
	readme += "```\n\n"

	readme += "### Windows (IIS)\n"
	readme += "1. Import cert.p12 into Certificate Store\n"
	readme += "2. Use IIS Manager to bind the certificate to your site\n\n"

	readme += "### Trust the CA Root\n"
	readme += "To trust this CA on client systems:\n\n"
	readme += "**Linux/Ubuntu:**\n"
	readme += "```bash\n"
	readme += "sudo cp chain.pem /usr/local/share/ca-certificates/my-ca.crt\n"
	readme += "sudo update-ca-certificates\n"
	readme += "```\n\n"

	readme += "**Windows PowerShell:**\n"
	readme += "```powershell\n"
	readme += "Import-Certificate -FilePath chain.pem -CertStoreLocation Cert:\\LocalMachine\\Root\n"
	readme += "```\n\n"

	readme += "## Verification\n"
	readme += "```bash\n"
	readme += "# Verify certificate chain\n"
	readme += "openssl verify -CAfile chain.pem cert.pem\n\n"
	readme += "# Check certificate details\n"
	readme += "openssl x509 -in cert.pem -text -noout\n\n"
	readme += "# Test SSL connection\n"
	readme += "openssl s_client -connect your-domain:443 -showcerts\n"
	readme += "```\n"

	return readme
}
