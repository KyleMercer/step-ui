// Package settings is a small key/value store for configuration that admins
// change from the web UI, kept in step-ui's own database rather than in
// environment variables. Environment variables remain for what step-ui
// needs before it can run at all (CA connection, database path, first
// admin, API token); everything a feature lets you tune at runtime belongs
// here instead.
//
// Each feature owns one key (e.g. "alerts") holding a JSON document, so
// features stay separate: removing one means dropping its key, nothing
// else.
//
// Secret values (API tokens, passwords, webhook URLs with keys in them) are
// encrypted with AES-256-GCM before they're stored -- see Seal/Open. The
// key comes from a file: by default one created automatically next to the
// database, or a Docker secret / any other file named by SETTINGS_KEY_FILE.
// A copy of the database on its own (a backup, say) therefore doesn't
// expose the secrets; someone with both the database and the key file
// does.
package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Setting is one feature's settings document. Table name kept explicit and
// distinct from the existing ca_settings table.
type Setting struct {
	Key       string `gorm:"primaryKey"`
	Value     string `gorm:"type:text"`
	UpdatedAt time.Time
	UpdatedBy string
}

func (Setting) TableName() string { return "app_settings" }

// sealedPrefix marks an encrypted value, versioned so the scheme can change
// later without guessing what an old value is.
const sealedPrefix = "enc:v1:"

// ErrUndecryptable means a stored secret exists but can't be decrypted with
// the current key -- almost always because the key file was replaced or
// lost. The UI shows such secrets as needing to be re-entered.
var ErrUndecryptable = errors.New("stored secret can't be decrypted with the current settings key")

type Store struct {
	db      *gorm.DB
	aead    cipher.AEAD
	keyFrom string
	mu      sync.Mutex
}

// Open prepares the store: migrates its table and loads (or creates) the
// encryption key.
//
// keyFile: if non-empty (SETTINGS_KEY_FILE), the key is read from that file
// and it must exist -- typical for a Docker secret mounted at
// /run/secrets/<name>. If empty, defaultDir/settings.key is used, and
// created with a fresh random key on first start.
//
// Any file content works as a key (it's hashed with SHA-256 to 32 bytes),
// but use something long and random, e.g. `openssl rand -hex 32`.
func Open(db *gorm.DB, keyFile, defaultDir string) (*Store, error) {
	if err := db.AutoMigrate(&Setting{}); err != nil {
		return nil, fmt.Errorf("migrating settings table: %w", err)
	}

	var raw []byte
	var from string
	if keyFile != "" {
		b, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("reading SETTINGS_KEY_FILE %q: %w", keyFile, err)
		}
		raw, from = b, keyFile
	} else {
		path := filepath.Join(defaultDir, "settings.key")
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return nil, fmt.Errorf("generating settings key: %w", err)
			}
			b = []byte(hex.EncodeToString(buf) + "\n")
			// O_EXCL: never overwrite a key that appeared in the meantime.
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return nil, fmt.Errorf("creating settings key file %q: %w", path, err)
			}
			if _, err := f.Write(b); err != nil {
				f.Close()
				return nil, fmt.Errorf("writing settings key file %q: %w", path, err)
			}
			if err := f.Close(); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, fmt.Errorf("reading settings key file %q: %w", path, err)
		}
		raw, from = b, path
	}

	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) < 16 {
		return nil, fmt.Errorf("settings key in %q is too short (use at least 32 random characters, e.g. `openssl rand -hex 32`)", from)
	}
	sum := sha256.Sum256([]byte(trimmed))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, aead: aead, keyFrom: from}, nil
}

// KeySource reports where the encryption key came from (for logs and the
// UI), never the key itself.
func (s *Store) KeySource() string { return s.keyFrom }

// Get loads key's document into v. found=false (and v untouched) if the key
// has never been saved.
func (s *Store) Get(key string, v any) (found bool, err error) {
	var row Setting
	err = s.db.Where("key = ?", key).Limit(1).Find(&row).Error
	if err != nil {
		return false, err
	}
	if row.Key == "" {
		return false, nil
	}
	if err := json.Unmarshal([]byte(row.Value), v); err != nil {
		return true, fmt.Errorf("decoding settings %q: %w", key, err)
	}
	return true, nil
}

// Put replaces key's document with v.
func (s *Store) Put(key string, v any, updatedBy string) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Save(&Setting{Key: key, Value: string(b), UpdatedAt: time.Now(), UpdatedBy: updatedBy}).Error
}

// Seal encrypts a secret for storage. The empty string stays empty, so
// "not set" needs no special encoding.
func (s *Store) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := s.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return sealedPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Open decrypts a value produced by Seal. Returns ErrUndecryptable if the
// key no longer matches.
func (s *Store) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealed, sealedPrefix) {
		return "", ErrUndecryptable
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, sealedPrefix))
	if err != nil || len(ct) < s.aead.NonceSize() {
		return "", ErrUndecryptable
	}
	nonce, body := ct[:s.aead.NonceSize()], ct[s.aead.NonceSize():]
	pt, err := s.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(pt), nil
}
