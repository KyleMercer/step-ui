package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"step-ca-webui/internal/db"

	"github.com/gin-gonic/gin"
)

// SessionCookieName is the cookie the session token travels in. HttpOnly
// (never touched by JS) and, deliberately, not marked Secure: this is meant
// to run over plain HTTP on a home LAN behind Dockhand, same as everything
// else in this app -- requiring HTTPS here would just break logins for
// everyone not running a reverse proxy with TLS in front of it.
const SessionCookieName = "step_ui_session"

// SessionDuration is how long a session lasts before requiring another
// login. Fixed at creation time (not refreshed on activity) -- deliberately
// kept simple (no idle-timeout tracking) rather than a sliding expiry, since
// the actual security boundary here is the login screen itself, not how
// long a session inside it stays open. 3 days keeps a leaked/stolen session
// cookie's useful window short without adding that complexity.
const SessionDuration = 3 * 24 * time.Hour

const userContextKey = "auth_user"

// RequireSession looks up the session cookie against the sessions table
// and, if valid and unexpired, attaches the corresponding user to the gin
// context (retrievable with CurrentUser) for the rest of the request.
// Unlike RequireBearerToken, this has no "off" switch -- once user accounts
// exist, every route it guards requires an actual logged-in account.
func RequireSession(database *db.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(SessionCookieName)
		if err != nil || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not logged in"})
			return
		}

		session, err := database.GetSession(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
			return
		}

		if time.Now().After(session.ExpiresAt) {
			_ = database.DeleteSession(token)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
			return
		}

		user, err := database.GetUserByID(session.UserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}

		c.Set(userContextKey, user)
		c.Next()
	}
}

// RequireRole blocks any logged-in account whose role doesn't match --
// meant to sit after RequireSession in the middleware chain, guarding the
// mutating certificate operations and user management so viewer accounts
// can look but not touch.
func RequireRole(role db.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := CurrentUser(c)
		if user == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not logged in"})
			return
		}
		if user.Role != role {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Next()
	}
}

// CurrentUser returns the logged-in user attached by RequireSession, or nil
// if it never ran (or found nothing) for this request.
func CurrentUser(c *gin.Context) *db.User {
	v, ok := c.Get(userContextKey)
	if !ok {
		return nil
	}
	user, _ := v.(*db.User)
	return user
}

// RequireBearerToken returns a middleware that requires every request to
// present `Authorization: Bearer <token>` matching the configured token
// exactly (compared in constant time, so responding slightly slower for a
// wrong guess can't be used to brute-force the token character by
// character). If token is empty, auth is disabled and this middleware is a
// no-op -- see the API_AUTH_TOKEN doc in example.env for why that's the
// default and why you should set one.
func RequireBearerToken(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}

		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}

		presented := strings.TrimPrefix(header, prefix)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
			return
		}

		c.Next()
	}
}

// CORS returns a middleware that allows browser requests only from
// allowedOrigin, replacing the previous `Access-Control-Allow-Origin: *`
// (which let any website's client-side JS call this API from a visitor's
// browser). Access-Control-Allow-Credentials is required so the session
// cookie can actually flow on requests the browser makes to a different
// port than the page it's running on (frontend:3000 calling backend:8080
// directly) -- browsers refuse to attach or expose cookies on a
// credentialed cross-origin request unless the server explicitly opts in
// here, and this only works because allowedOrigin is a specific origin,
// never "*" (the two are mutually exclusive by spec).
func CORS(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", allowedOrigin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
