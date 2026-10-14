package api

import (
	"step-ca-webui/internal/db"

	"github.com/gin-gonic/gin"
)

// SetupRoutes wires up the routes. authToken (RequireBearerToken) and user
// sessions (RequireSession/RequireRole) are layered, not alternatives: the
// bearer token is a shared secret between the frontend and backend
// containers (see API_AUTH_TOKEN in example.env), while sessions identify
// an actual logged-in person. /health is deliberately left open behind
// neither, so container/orchestrator healthchecks don't need either one.
// RouteExtension lets a self-contained feature package (e.g. alerts) add
// its own routes without this file knowing about it: loggedIn already
// requires a session, admin additionally requires the admin role. A
// feature removed from cmd/main.go simply stops being registered.
type RouteExtension func(loggedIn, admin *gin.RouterGroup)

func SetupRoutes(r *gin.Engine, handlers *Handlers, authToken string, extensions ...RouteExtension) {
	// Health check (intentionally not behind auth)
	r.GET("/health", handlers.Health)

	// OIDC single sign-on. These two are deliberately NOT behind
	// RequireBearerToken: they're full-page browser navigations (a plain
	// link to /login, then the identity provider's redirect back to
	// /callback), and a browser navigation can't carry an Authorization
	// header. Their protection is the OIDC flow itself -- one-time state
	// bound to the browser by cookie, PKCE, a nonce, and a signature check
	// on the ID token. Both answer 404 when SSO isn't configured.
	r.GET("/api/auth/oidc/login", handlers.OIDCLogin)
	r.GET("/api/auth/oidc/callback", handlers.OIDCCallback)

	api := r.Group("/api")
	api.Use(RequireBearerToken(authToken))
	{
		// Login is the one endpoint that must work without a session
		// already -- it's how a session gets created in the first place.
		api.POST("/auth/login", handlers.Login)
		// Logout is safe to call with no session at all (it just no-ops),
		// so it doesn't need RequireSession either.
		api.POST("/auth/logout", handlers.Logout)
		// Which sign-in options the login page should show (needs no
		// session -- nobody is signed in yet when it's asked).
		api.GET("/auth/config", handlers.AuthConfig)

		// Everything else requires an actual logged-in account.
		loggedIn := api.Group("")
		loggedIn.Use(RequireSession(handlers.db))
		{
			loggedIn.GET("/auth/me", handlers.Me)

			// Read-only: any logged-in account, admin or viewer.
			loggedIn.GET("/certs", handlers.ListCertificates)
			loggedIn.GET("/certs/:id", handlers.GetCertificate)
			loggedIn.GET("/settings/ca", handlers.GetCASettings)

			// Build identity (version, commit, build date) for the UI footer
			loggedIn.GET("/version", handlers.Version)

			// Mutating certificate operations and user management:
			// admin-only -- viewer accounts can look but not touch.
			admin := loggedIn.Group("")
			admin.Use(RequireRole(db.RoleAdmin))
			{
				admin.POST("/certs/issue", handlers.IssueCertificate)
				admin.POST("/certs/sign-csr", handlers.SignCSR)
				admin.POST("/certs/:id/renew", handlers.RenewCertificate)
				admin.POST("/certs/:id/revoke", handlers.RevokeCertificate)

				admin.GET("/users", handlers.ListUsers)
				admin.POST("/users", handlers.CreateUser)
				admin.PUT("/users/:id", handlers.UpdateUser)
				admin.DELETE("/users/:id", handlers.DeleteUser)

				admin.GET("/settings/sso", handlers.GetSSOSettings)
				admin.PUT("/settings/sso", handlers.PutSSOSettings)
				admin.POST("/settings/sso/test", handlers.TestSSOSettings)
			}

			// Optional feature packages (see RouteExtension).
			featureAdmin := loggedIn.Group("")
			featureAdmin.Use(RequireRole(db.RoleAdmin))
			for _, ext := range extensions {
				ext(loggedIn, featureAdmin)
			}
		}
	}
}
