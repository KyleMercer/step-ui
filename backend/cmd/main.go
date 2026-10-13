package main

import (
	"fmt"
	"log"

	"step-ca-webui/internal/api"
	"step-ca-webui/internal/auth"
	"step-ca-webui/internal/config"
	"step-ca-webui/internal/db"
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

	if cfg.APIAuthToken == "" {
		log.Println("WARNING: API_AUTH_TOKEN is not set -- the API is unauthenticated. Anyone who can reach it can issue, sign, and revoke certificates. Set API_AUTH_TOKEN in your .env to require a bearer token.")
	}

	// Setup Gin router
	r := gin.Default()

	// CORS: restricted to CORS_ALLOWED_ORIGIN (defaults to the frontend's
	// own default address) instead of the previous "*", which let any
	// website's client-side JS call this API from a visitor's browser.
	r.Use(api.CORS(cfg.CORSAllowedOrigin))

	// Setup routes (authToken == "" leaves /api/* unauthenticated, matching
	// the previous behavior, but see the warning logged above)
	api.SetupRoutes(r, handlers, cfg.APIAuthToken)

	// Start server
	log.Printf("Starting server on port %d", cfg.Port)
	if err := r.Run(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
