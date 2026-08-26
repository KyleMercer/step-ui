package main

import (
	"fmt"
	"log"

	"step-ca-webui/internal/api"
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

	// DEBUG: Print configuration at startup
	log.Printf("=== CONFIGURATION ===")
	log.Printf("CA_URL: %s", cfg.CAURL)
	log.Printf("CA_ROOT_FINGERPRINT: %s", cfg.CARootFingerprint)
	log.Printf("PROVISIONER_NAME: %s", cfg.ProvisionerName)
	log.Printf("====================")

	// Initialize database
	database, err := db.NewDatabase(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize step client
	stepClient := step.NewStepClient(
		cfg.CAURL,
		cfg.CARootFingerprint,
		cfg.ProvisionerName,
		cfg.ProvisionerPassword,
	)

	log.Printf("StepClient initialized with fingerprint: %s", stepClient.CARootFingerprint)

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
