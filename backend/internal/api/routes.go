package api

import (
	"github.com/gin-gonic/gin"
)

// SetupRoutes wires up the routes. authToken is applied to every /api/*
// route via RequireBearerToken -- /health is deliberately left open so
// container/orchestrator healthchecks don't need the token. Pass "" to
// leave the API unauthenticated (the default; see API_AUTH_TOKEN in
// example.env).
func SetupRoutes(r *gin.Engine, handlers *Handlers, authToken string) {
	// Health check (intentionally not behind auth)
	r.GET("/health", handlers.Health)

	// API routes
	api := r.Group("/api")
	api.Use(RequireBearerToken(authToken))
	{
		// Certificate operations
		api.POST("/certs/issue", handlers.IssueCertificate)
		api.POST("/certs/sign-csr", handlers.SignCSR)
		api.GET("/certs", handlers.ListCertificates)
		api.GET("/certs/:id", handlers.GetCertificate)
		api.POST("/certs/:id/renew", handlers.RenewCertificate)
		api.POST("/certs/:id/revoke", handlers.RevokeCertificate)

		// Settings
		api.GET("/settings/ca", handlers.GetCASettings)
	}
}
