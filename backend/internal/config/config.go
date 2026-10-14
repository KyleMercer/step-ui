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

	// InitialAdminUsername/InitialAdminPassword seed the very first admin
	// account on startup, but only when the users table is completely
	// empty (see cmd/main.go) -- there's no self-registration flow, so
	// this is the one bootstrap mechanism to get from "no accounts exist"
	// to "there's an admin who can log in and create everyone else's
	// accounts from Settings." Once that first account exists, these two
	// env vars are never consulted again; changing them later does nothing.
	InitialAdminUsername string
	InitialAdminPassword string

	// SettingsKeyFile optionally names the file holding the key that
	// encrypts secrets in the settings store (e.g. a Docker secret at
	// /run/secrets/step_ui_settings_key). Empty = a key file created
	// automatically next to the database. See internal/settings.
	SettingsKeyFile string
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
		SettingsKeyFile:      getEnv("SETTINGS_KEY_FILE", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
