package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

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
// browser).
func CORS(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", allowedOrigin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
