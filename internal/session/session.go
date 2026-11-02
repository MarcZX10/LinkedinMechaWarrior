// Package session stores the LinkedIn session cookies.
//
// The session cookie (li_at) is as sensitive as a password, so it is kept in
// the system keyring when one is available, and otherwise in a file that only
// the current user can read.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
)

const (
	keyringService = "linkedin-mecha-warrior"
	keyringUser    = "session"
)

// KeptCookies are the cookies lmw stores and sends, in the order they are sent.
// li_at is the session; JSESSIONID doubles as the CSRF token; the others
// identify the browser and the routing, as in a real browser session.
var KeptCookies = []string{"bcookie", "bscookie", "li_gc", "lang", "liap", "JSESSIONID", "li_at", "lidc"}

// ErrNoSession means no session has been imported yet.
var ErrNoSession = errs.New(errs.AuthRequired, "no LinkedIn session yet. Run `lmw auth import` first")

// Session is a set of LinkedIn cookies.
type Session struct {
	Cookies    map[string]string `json:"cookies"`
	Source     string            `json:"source"`
	ImportedAt time.Time         `json:"imported_at"`
}

// New keeps only the cookies lmw needs and checks that the session ones are present.
func New(cookies map[string]string, source string) (*Session, error) {
	kept := map[string]string{}
	for _, name := range KeptCookies {
		if value := strings.TrimSpace(cookies[name]); value != "" {
			kept[name] = value
		}
	}
	s := &Session{Cookies: kept, Source: source, ImportedAt: time.Now().UTC()}
	return s, s.Validate()
}

// Validate checks that the cookies required for API calls are present.
func (s *Session) Validate() error {
	var missing []string
	for _, name := range []string{"li_at", "JSESSIONID"} {
		if s.Cookies[name] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return errs.New(errs.AuthRequired, "the LinkedIn session is incomplete: missing %s", strings.Join(missing, " and "))
	}
	return nil
}

// CSRFToken is the JSESSIONID value without quotes, as LinkedIn's web app sends it.
func (s *Session) CSRFToken() string {
	return strings.Trim(s.Cookies["JSESSIONID"], `"`)
}

// CookieHeader renders the Cookie request header.
func (s *Session) CookieHeader() string {
	parts := make([]string, 0, len(s.Cookies))
	for _, name := range KeptCookies {
		if value, ok := s.Cookies[name]; ok {
			parts = append(parts, name+"="+value)
		}
	}
	return strings.Join(parts, "; ")
}

// Update applies a cookie set by LinkedIn. It reports whether anything changed.
// A deleted li_at means LinkedIn ended the session.
func (s *Session) Update(name, value string, deleted bool) bool {
	if !isKept(name) {
		return false
	}
	old, existed := s.Cookies[name]
	if deleted || value == "" || value == `"delete me"` || value == "delete me" {
		delete(s.Cookies, name)
		return existed
	}
	s.Cookies[name] = value
	return !existed || old != value
}

func isKept(name string) bool {
	for _, kept := range KeptCookies {
		if kept == name {
			return true
		}
	}
	return false
}

// Store loads and saves a Session.
type Store interface {
	Load() (*Session, error)
	// Save stores the session and returns a description of where it went.
	Save(*Session) (string, error)
	Delete() error
}

// NewStore returns the store for a mode: "keyring", "file" or "auto".
func NewStore(mode, filePath string) Store {
	file := FileStore{Path: filePath}
	switch mode {
	case "keyring":
		return KeyringStore{}
	case "file":
		return file
	default:
		return AutoStore{Keyring: KeyringStore{}, File: file}
	}
}

// KeyringStore keeps the session in the system keyring.
type KeyringStore struct{}

func (KeyringStore) Load() (*Session, error) {
	raw, err := keyring.Get(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, errs.Wrap(errs.Storage, err, "cannot read the session from the system keyring: %v", err)
	}
	return decode([]byte(raw), "system keyring")
}

func (KeyringStore) Save(s *Session) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if err := keyring.Set(keyringService, keyringUser, string(raw)); err != nil {
		return "", errs.Wrap(errs.Storage, err, "cannot save the session in the system keyring: %v", err)
	}
	return "the system keyring", nil
}

func (KeyringStore) Delete() error {
	err := keyring.Delete(keyringService, keyringUser)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return errs.Wrap(errs.Storage, err, "cannot delete the session from the system keyring: %v", err)
	}
	return nil
}

// FileStore keeps the session in a file readable only by the current user.
type FileStore struct{ Path string }

func (f FileStore) Load() (*Session, error) {
	raw, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, errs.Wrap(errs.Storage, err, "cannot read %s", f.Path)
	}
	return decode(raw, f.Path)
}

func (f FileStore) Save(s *Session) (string, error) {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", errs.Wrap(errs.Storage, err, "cannot write %s", f.Path)
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		return "", errs.Wrap(errs.Storage, err, "cannot write %s", f.Path)
	}
	return f.Path, nil
}

func (f FileStore) Delete() error {
	err := os.Remove(f.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errs.Wrap(errs.Storage, err, "cannot delete %s", f.Path)
	}
	return nil
}

// AutoStore prefers the keyring and falls back to a file when it is unavailable.
type AutoStore struct {
	Keyring KeyringStore
	File    FileStore
}

func (a AutoStore) Load() (*Session, error) {
	s, err := a.Keyring.Load()
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, ErrNoSession) {
		slog.Debug("system keyring unavailable, trying the session file", "error", err)
	}
	return a.File.Load()
}

func (a AutoStore) Save(s *Session) (string, error) {
	where, err := a.Keyring.Save(s)
	if err == nil {
		// Don't leave an older copy behind in the fallback file.
		_ = a.File.Delete()
		return where, nil
	}
	slog.Warn("system keyring unavailable; storing the session in a private file instead", "error", err)
	return a.File.Save(s)
}

func (a AutoStore) Delete() error {
	fileErr := a.File.Delete()
	if err := a.Keyring.Delete(); err != nil {
		// Without a usable keyring there is nothing stored there to delete.
		// Only report the failure if the session can still be read back.
		if _, loadErr := a.Keyring.Load(); loadErr == nil {
			return errors.Join(err, fileErr)
		}
		slog.Debug("system keyring unavailable while deleting the session", "error", err)
	}
	return fileErr
}

func decode(raw []byte, where string) (*Session, error) {
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errs.Wrap(errs.Storage, err, "the session stored in %s is corrupted; run `lmw auth import` again", where)
	}
	if s.Cookies == nil {
		s.Cookies = map[string]string{}
	}
	return &s, nil
}

// Describe summarizes the session without revealing cookie values.
func (s *Session) Describe() string {
	return fmt.Sprintf("imported from %s on %s", s.Source, s.ImportedAt.Local().Format("2006-01-02 15:04"))
}
