package api

import (
	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine, handlers *Handlers) {
	// Health check
	r.GET("/health", handlers.Health)

	// API routes
	api := r.Group("/api")
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

		// Build identity (version, commit, build date) for the UI footer
		api.GET("/version", handlers.Version)
	}
}
