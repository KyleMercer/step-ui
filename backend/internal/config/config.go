package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	CAURL               string
	CARootFingerprint   string
	ProvisionerName     string
	ProvisionerPassword string
	DBPath              string
	Port                int

	// APIAuthToken, if set, requires every /api/* request to present it as
	// `Authorization: Bearer <token>`. Optional/off by default so existing
	// installs aren't broken by upgrading, but strongly recommended for
	// anything reachable beyond localhost -- this API can issue, sign, and
	// revoke certificates with no other access control.
	APIAuthToken string

	// CORSAllowedOrigin is the single origin allowed to call this API from
	// a browser. Defaults to the frontend's own default address rather
	// than "*", which previously let any website's JS make authenticated
	// (from the browser's perspective) requests to this API from a
	// visitor's browser.
	CORSAllowedOrigin string

	// InitialAdminUsername/InitialAdminPassword seed the very first admin
	// account on startup, but only when the users table is completely
	// empty (see cmd/main.go) -- there's no self-registration flow, so
	// this is the one bootstrap mechanism to get from "no accounts exist"
	// to "there's an admin who can log in and create everyone else's
	// accounts from Settings." Once that first account exists, these two
	// env vars are never consulted again; changing them later does nothing.
	InitialAdminUsername string
	InitialAdminPassword string

	// OIDC single sign-on (e.g. Authentik). Entirely optional: SSO is
	// switched on only when OIDCIssuerURL, OIDCClientID, OIDCClientSecret
	// and OIDCRedirectURL are all set. Local username/password login keeps
	// working either way, as a break-glass path for when the identity
	// provider is down. See the "Single sign-on" section of the README.
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	// OIDCRedirectURL is the backend's own callback, as the BROWSER reaches
	// it (through any reverse proxy) -- e.g.
	// https://step-ui-api.example.com/api/auth/oidc/callback. Must match the
	// redirect URI registered at the identity provider exactly.
	OIDCRedirectURL string
	// Groups claim values that grant each role. Admin wins if a user is in
	// both. Someone in neither group is refused, even if the identity
	// provider let them through.
	OIDCAdminGroup  string
	OIDCViewerGroup string
	OIDCGroupsClaim string
	// OIDCUsernameClaim picks the claim used as the step-ui username
	// (falls back to email, then sub, if the claim is missing).
	OIDCUsernameClaim string
	OIDCScopes        []string
	// OIDCProviderName is only a label, for the login button.
	OIDCProviderName string
	// OIDCCAFile optionally names a PEM file of extra CA certificates to
	// trust when the backend talks to the identity provider -- only needed
	// if the provider's certificate isn't publicly trusted (e.g. issued by
	// your own step-ca). The container's normal trust store is still used.
	OIDCCAFile string
}

// OIDCEnabled reports whether enough OIDC settings are present to offer SSO.
func (c *Config) OIDCEnabled() bool {
	return c.OIDCIssuerURL != "" && c.OIDCClientID != "" && c.OIDCClientSecret != "" && c.OIDCRedirectURL != ""
}

func Load() *Config {
	port, _ := strconv.Atoi(getEnv("PORT", "8080"))

	return &Config{
		CAURL:                getEnv("CA_URL", ""),
		CARootFingerprint:    getEnv("CA_ROOT_FINGERPRINT", ""),
		ProvisionerName:      getEnv("PROVISIONER_NAME", "ui-admin"),
		ProvisionerPassword:  getEnv("PROVISIONER_PASSWORD", ""),
		DBPath:               getEnv("DB_PATH", "./data/certs.db"),
		Port:                 port,
		APIAuthToken:         getEnv("API_AUTH_TOKEN", ""),
		CORSAllowedOrigin:    getEnv("CORS_ALLOWED_ORIGIN", "http://localhost:3000"),
		InitialAdminUsername: getEnv("INITIAL_ADMIN_USERNAME", ""),
		InitialAdminPassword: getEnv("INITIAL_ADMIN_PASSWORD", ""),

		OIDCIssuerURL:     getEnv("OIDC_ISSUER_URL", ""),
		OIDCClientID:      getEnv("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:  getEnv("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:   getEnv("OIDC_REDIRECT_URL", ""),
		OIDCAdminGroup:    getEnv("OIDC_ADMIN_GROUP", ""),
		OIDCViewerGroup:   getEnv("OIDC_VIEWER_GROUP", ""),
		OIDCGroupsClaim:   getEnv("OIDC_GROUPS_CLAIM", "groups"),
		OIDCUsernameClaim: getEnv("OIDC_USERNAME_CLAIM", "preferred_username"),
		OIDCScopes:        strings.Fields(strings.ReplaceAll(getEnv("OIDC_SCOPES", "openid profile email"), ",", " ")),
		OIDCProviderName:  getEnv("OIDC_PROVIDER_NAME", "SSO"),
		OIDCCAFile:        getEnv("OIDC_CA_FILE", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
