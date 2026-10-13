// Package auth holds password hashing and session-token generation --
// deliberately kept small and separate from the api/db packages so the
// actual cryptographic bits live in one obvious place.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// DummyPasswordHash is a real bcrypt hash with no corresponding real
// password -- used only to burn the same CPU time as a genuine password
// check when a login attempt names a username that doesn't exist. Without
// this, a login endpoint that skips hashing entirely for unknown usernames
// responds measurably faster for "no such user" than for "wrong password",
// which lets an attacker enumerate valid usernames by timing alone.
const DummyPasswordHash = "$2a$10$yXOTJAmOqJuWbqk/Y3k8.evjQ89b4Yenwt18tiIP8ppmIRY7bxnJe"

// HashPassword uses bcrypt's default cost (10), deliberately not a higher
// one: this runs on a Raspberry Pi, not a beefy server, and step-ui already
// shells out to `step`/openssl per request elsewhere -- a needlessly high
// bcrypt cost would make every login noticeably slow for no real benefit
// against the actual threat model here (a homelab tool, not a public
// multi-tenant service).
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether password matches hash. Safe to call with
// DummyPasswordHash when there's no real user to check against -- see its
// doc comment above.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateSessionToken returns a random 256-bit token, hex-encoded, for use
// as an opaque session identifier stored in db.Session and looked up on
// every authenticated request (see api.RequireSession). Not a JWT:
// nothing is decoded or trusted client-side, so there's no signing key or
// verification scheme to get wrong, and revoking a session is just
// deleting the matching database row.
func GenerateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
