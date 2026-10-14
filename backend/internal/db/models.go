package db

import (
	"time"
)

type Certificate struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	CN          string    `gorm:"index" json:"cn"`
	SANs        string    `json:"sans"`                // JSON array
	Serial      string    `gorm:"index" json:"serial"` // needed to actually revoke via step-ca
	NotAfter    time.Time `gorm:"index" json:"not_after"`
	Status      string    `json:"status"`       // active, revoked, expired
	KeyStrategy string    `json:"key_strategy"` // server, csr
	StorageRef  string    `json:"storage_ref"`  // ephemeral, or file path
	OwnerUser   string    `json:"owner_user"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Certificate statuses. Only "active" and "revoked" are ever stored by
// step-ui itself; "expired" is derived at read time (see EffectiveStatus).
const (
	CertStatusActive  = "active"
	CertStatusRevoked = "revoked"
	CertStatusExpired = "expired"
)

// EffectiveStatus is the status to show and filter on. Nothing ever writes
// "expired" to the database -- a certificate simply passes its NotAfter
// time -- so an "active" row past that point is reported as "expired"
// here, at read time, rather than relying on a background job to flip the
// stored value. Revoked stays revoked even after it also expires: that's
// the more important fact about it.
func (c *Certificate) EffectiveStatus(now time.Time) string {
	if c.Status == CertStatusActive && !now.Before(c.NotAfter) {
		return CertStatusExpired
	}
	return c.Status
}

type AuditEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CertID    string    `gorm:"index" json:"cert_id"`
	Who       string    `json:"who"`
	Action    string    `json:"action"`
	Details   string    `json:"details"`
	Timestamp time.Time `json:"timestamp"`
}

type CASettings struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	CAURL           string    `json:"ca_url"`
	RootFingerprint string    `json:"root_fingerprint"`
	ACMEDirectories []string  `gorm:"type:text" json:"acme_directories"` // JSON array
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Role is deliberately a small closed set (just these two), not a free-text
// permissions system -- admin can do everything, viewer is read-only. See
// the api package's RequireRole for where that's actually enforced.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

// AuthSource records how an account signs in. Local accounts have a
// password stored here; OIDC accounts are created automatically the first
// time someone signs in through the configured identity provider (e.g.
// Authentik), have no password at all, and get their role re-derived from
// their identity-provider groups on every sign-in.
type AuthSource string

const (
	AuthSourceLocal AuthSource = "local"
	AuthSourceOIDC  AuthSource = "oidc"
)

type User struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Username     string     `gorm:"uniqueIndex" json:"username"`
	PasswordHash string     `json:"-"` // bcrypt; never serialized back to a client. Empty for OIDC accounts.
	Role         Role       `json:"role"`
	AuthSource   AuthSource `gorm:"default:local" json:"auth_source"`
	// OIDCSubject is the identity provider's stable "sub" claim -- the real
	// identity key for an OIDC account. Usernames can be renamed on the
	// provider side; "sub" never changes. Pointer so local accounts store
	// NULL, which SQLite's unique index allows any number of.
	OIDCSubject *string   `gorm:"column:oidc_subject;uniqueIndex:idx_users_oidc_subject" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// IsLocal treats a blank AuthSource as local too -- accounts created before
// the column existed are all local, whatever the migration filled in.
func (u *User) IsLocal() bool {
	return u.AuthSource != AuthSourceOIDC
}

// Session is an opaque server-side session record, not a JWT -- the token
// is just a random identifier looked up here on every request (see
// api.RequireSession). That makes revocation trivial and immediate: logout,
// a password change, or an admin deleting an account all just delete the
// matching row(s), with nothing client-side to expire or invalidate.
type Session struct {
	Token     string    `gorm:"primaryKey" json:"-"`
	UserID    uint      `gorm:"index" json:"-"`
	ExpiresAt time.Time `json:"-"`
	CreatedAt time.Time `json:"-"`
}
