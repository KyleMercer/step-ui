package config

import (
	"os"
	"strconv"
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
}

func Load() *Config {
	port, _ := strconv.Atoi(getEnv("PORT", "8080"))

	return &Config{
		CAURL:               getEnv("CA_URL", ""),
		CARootFingerprint:   getEnv("CA_ROOT_FINGERPRINT", ""),
		ProvisionerName:     getEnv("PROVISIONER_NAME", "ui-admin"),
		ProvisionerPassword: getEnv("PROVISIONER_PASSWORD", ""),
		DBPath:              getEnv("DB_PATH", "./data/certs.db"),
		Port:                port,
		APIAuthToken:        getEnv("API_AUTH_TOKEN", ""),
		CORSAllowedOrigin:   getEnv("CORS_ALLOWED_ORIGIN", "http://localhost:3000"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
