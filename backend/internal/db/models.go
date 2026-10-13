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

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex" json:"username"`
	PasswordHash string    `json:"-"` // bcrypt; never serialized back to a client
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
