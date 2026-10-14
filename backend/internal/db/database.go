package db

import (
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Database struct {
	DB *gorm.DB
}

func NewDatabase(dbPath string) (*Database, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Auto-migrate the schema
	if err := db.AutoMigrate(&Certificate{}, &AuditEvent{}, &CASettings{}, &User{}, &Session{}); err != nil {
		return nil, err
	}

	return &Database{DB: db}, nil
}

func (d *Database) CreateCertificate(cert *Certificate) error {
	return d.DB.Create(cert).Error
}

func (d *Database) GetCertificate(id string) (*Certificate, error) {
	var cert Certificate
	err := d.DB.Where("id = ?", id).First(&cert).Error
	return &cert, err
}

func (d *Database) ListCertificates(limit, offset int, status string) ([]Certificate, error) {
	var certs []Certificate
	query := d.DB.Order("created_at DESC")

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Find(&certs).Error
	return certs, err
}

func (d *Database) UpdateCertificate(cert *Certificate) error {
	return d.DB.Save(cert).Error
}

func (d *Database) DeleteCertificate(id string) error {
	return d.DB.Where("id = ?", id).Delete(&Certificate{}).Error
}

func (d *Database) LogAuditEvent(event *AuditEvent) error {
	return d.DB.Create(event).Error
}

func (d *Database) GetAuditEvents(certID string, limit int) ([]AuditEvent, error) {
	var events []AuditEvent
	query := d.DB.Order("timestamp DESC")

	if certID != "" {
		query = query.Where("cert_id = ?", certID)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&events).Error
	return events, err
}

// --- Users ---

func (d *Database) CreateUser(user *User) error {
	return d.DB.Create(user).Error
}

func (d *Database) GetUserByUsername(username string) (*User, error) {
	var user User
	err := d.DB.Where("username = ?", username).First(&user).Error
	return &user, err
}

func (d *Database) GetUserByID(id uint) (*User, error) {
	var user User
	err := d.DB.First(&user, id).Error
	return &user, err
}

func (d *Database) ListUsers() ([]User, error) {
	var users []User
	err := d.DB.Order("username").Find(&users).Error
	return users, err
}

func (d *Database) CountUsers() (int64, error) {
	var count int64
	err := d.DB.Model(&User{}).Count(&count).Error
	return count, err
}

func (d *Database) CountUsersByRole(role Role) (int64, error) {
	var count int64
	err := d.DB.Model(&User{}).Where("role = ?", role).Count(&count).Error
	return count, err
}

// CountLocalAdmins counts admin accounts that can sign in with a password.
// Used to keep at least one break-glass admin that doesn't depend on the
// identity provider being up. Rows with a blank auth_source predate SSO
// support and are local.
func (d *Database) CountLocalAdmins() (int64, error) {
	var count int64
	err := d.DB.Model(&User{}).
		Where("role = ? AND (auth_source = ? OR auth_source = '' OR auth_source IS NULL)", RoleAdmin, AuthSourceLocal).
		Count(&count).Error
	return count, err
}

func (d *Database) GetUserByOIDCSubject(subject string) (*User, error) {
	var user User
	err := d.DB.Where("oidc_subject = ?", subject).First(&user).Error
	return &user, err
}

func (d *Database) UpdateUser(user *User) error {
	return d.DB.Save(user).Error
}

func (d *Database) DeleteUser(id uint) error {
	return d.DB.Where("id = ?", id).Delete(&User{}).Error
}

// --- Sessions ---

func (d *Database) CreateSession(session *Session) error {
	return d.DB.Create(session).Error
}

func (d *Database) GetSession(token string) (*Session, error) {
	var session Session
	err := d.DB.Where("token = ?", token).First(&session).Error
	return &session, err
}

func (d *Database) DeleteSession(token string) error {
	return d.DB.Where("token = ?", token).Delete(&Session{}).Error
}

// DeleteSessionsForUser invalidates every session belonging to a user --
// used on password change and account deletion so a leaked/stolen session
// doesn't survive either one.
func (d *Database) DeleteSessionsForUser(userID uint) error {
	return d.DB.Where("user_id = ?", userID).Delete(&Session{}).Error
}
