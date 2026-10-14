package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"step-ca-webui/internal/alerts"
	"step-ca-webui/internal/api"
	"step-ca-webui/internal/auth"
	"step-ca-webui/internal/config"
	"step-ca-webui/internal/db"
	"step-ca-webui/internal/settings"
	"step-ca-webui/internal/step"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Load configuration
	cfg := config.Load()

	log.Printf("Starting with CA_URL=%s, PROVISIONER_NAME=%s, CA root fingerprint configured=%t",
		cfg.CAURL, cfg.ProvisionerName, cfg.CARootFingerprint != "")

	// Initialize database
	database, err := db.NewDatabase(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Bootstrap the first admin account. There's no self-registration
	// flow (this is an admin-managed user list, not a public sign-up
	// form), so this env-var seed is the one way to get from "no accounts
	// exist" to "there's an admin who can log in and create everyone
	// else's accounts from Settings." Only runs against a genuinely empty
	// users table -- once any account exists, these two env vars are
	// never consulted again, so changing them later does nothing.
	if count, err := database.CountUsers(); err != nil {
		log.Printf("WARNING: failed to check for existing user accounts: %v", err)
	} else if count == 0 {
		if cfg.InitialAdminUsername == "" || cfg.InitialAdminPassword == "" {
			log.Println("WARNING: no user accounts exist yet and INITIAL_ADMIN_USERNAME/INITIAL_ADMIN_PASSWORD are not set -- nobody can log in to step-ui. Set both in your .env and restart to create the first admin account.")
		} else if hash, err := auth.HashPassword(cfg.InitialAdminPassword); err != nil {
			log.Printf("WARNING: failed to hash initial admin password: %v", err)
		} else {
			admin := &db.User{
				Username:     cfg.InitialAdminUsername,
				PasswordHash: hash,
				Role:         db.RoleAdmin,
				AuthSource:   db.AuthSourceLocal,
			}
			if err := database.CreateUser(admin); err != nil {
				log.Printf("WARNING: failed to create initial admin account %q: %v", cfg.InitialAdminUsername, err)
			} else {
				log.Printf("Created initial admin account %q", cfg.InitialAdminUsername)
			}
		}
	}

	// Initialize step client
	stepClient := step.NewStepClient(
		cfg.CAURL,
		cfg.CARootFingerprint,
		cfg.ProvisionerName,
		cfg.ProvisionerPassword,
	)

	// Initialize handlers
	handlers := api.NewHandlers(database, stepClient)

	// Optional OIDC single sign-on (e.g. Authentik). A problem here never
	// stops the backend: local login keeps working regardless.
	if cfg.OIDCEnabled() {
		oidcAuth, err := api.NewOIDCAuth(cfg, cfg.CORSAllowedOrigin)
		if err != nil {
			log.Printf("WARNING: SSO disabled: %v", err)
		} else {
			handlers.EnableOIDC(oidcAuth)
			log.Printf("SSO enabled: issuer=%s, redirect=%s, admin group=%q, viewer group=%q",
				cfg.OIDCIssuerURL, cfg.OIDCRedirectURL, cfg.OIDCAdminGroup, cfg.OIDCViewerGroup)
			if cfg.OIDCAdminGroup == "" && cfg.OIDCViewerGroup == "" {
				log.Println("WARNING: neither OIDC_ADMIN_GROUP nor OIDC_VIEWER_GROUP is set -- every SSO sign-in will be refused until at least one is.")
			}
			go oidcAuth.Probe()
		}
	} else if cfg.OIDCIssuerURL != "" || cfg.OIDCClientID != "" {
		log.Println("WARNING: some OIDC_* settings are set but not all of OIDC_ISSUER_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET and OIDC_REDIRECT_URL -- SSO stays off.")
	}

	if cfg.APIAuthToken == "" {
		log.Println("WARNING: API_AUTH_TOKEN is not set -- the API is unauthenticated. Anyone who can reach it can issue, sign, and revoke certificates. Set API_AUTH_TOKEN in your .env to require a bearer token.")
	}

	// Setup Gin router
	r := gin.Default()

	// CORS: restricted to CORS_ALLOWED_ORIGIN (defaults to the frontend's
	// own default address) instead of the previous "*", which let any
	// website's client-side JS call this API from a visitor's browser.
	r.Use(api.CORS(cfg.CORSAllowedOrigin))

	// Settings store: runtime configuration edited from the web UI, with
	// secrets encrypted at rest (see internal/settings).
	var extensions []api.RouteExtension
	store, err := settings.Open(database.DB, cfg.SettingsKeyFile, filepath.Dir(cfg.DBPath))
	if err != nil {
		log.Printf("WARNING: settings store unavailable, so features configured from the UI (expiry alerts) are disabled: %v", err)
	} else {
		log.Printf("Settings store ready (encryption key: %s)", store.KeySource())

		// Expiry alerts -- self-contained feature, switched on and
		// configured from its own page in the UI.
		if alertSvc, err := alerts.New(database, store, cfg.CAURL, cfg.CARootFingerprint, cfg.CORSAllowedOrigin); err != nil {
			log.Printf("WARNING: expiry alerts disabled: %v", err)
		} else {
			alertSvc.Start(context.Background())
			extensions = append(extensions, alertSvc.Routes())
		}
	}

	// Setup routes (authToken == "" leaves /api/* unauthenticated, matching
	// the previous behavior, but see the warning logged above)
	api.SetupRoutes(r, handlers, cfg.APIAuthToken, extensions...)

	// Start server
	log.Printf("Starting server on port %d", cfg.Port)
	if err := r.Run(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
