// Package security implements the local authentication and origin controls
// required by the Athenaeum data and security rules (spec 03 sections 10-11)
// and requirement R14.
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"
)

// CookieName is the session cookie issued after a successful bootstrap.
const CookieName = "athenaeum_session"

// tokenBytes is the entropy of both the bootstrap token and each session ID.
// 32 bytes is well beyond the guessing budget of a local attacker.
const tokenBytes = 32

// ErrNoSession reports a request that carried no usable session credential.
var ErrNoSession = errors.New("no valid session")

// SessionManager issues and validates browser sessions for a single running
// Athenaeum process. Sessions are deliberately in-memory: they must not
// outlive the process, and they must never be written to the workspace.
type SessionManager struct {
	bootstrapToken string
	ttl            time.Duration
	secure         bool

	mu       sync.RWMutex
	sessions map[string]time.Time
}

// NewSessionManager creates a manager with a freshly generated bootstrap
// token. secure marks issued cookies Secure, which is only correct when TLS
// terminates in front of the process.
func NewSessionManager(ttl time.Duration, secure bool) (*SessionManager, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	return &SessionManager{
		bootstrapToken: token,
		ttl:            ttl,
		secure:         secure,
		sessions:       make(map[string]time.Time),
	}, nil
}

// NewSessionManagerWithToken creates a manager whose bootstrap credential is
// supplied rather than generated. Remote mode uses this so the token comes
// from the operator's protected file (spec 03 section 11).
func NewSessionManagerWithToken(token string, ttl time.Duration, secure bool) (*SessionManager, error) {
	if token == "" {
		return nil, errors.New("bootstrap token must not be empty")
	}
	return &SessionManager{
		bootstrapToken: token,
		ttl:            ttl,
		secure:         secure,
		sessions:       make(map[string]time.Time),
	}, nil
}

// BootstrapToken returns the one-time launch credential. It is placed in the
// launch URL only; spec 03 section 12 forbids logging it.
func (m *SessionManager) BootstrapToken() string {
	return m.bootstrapToken
}

// RedeemBootstrap exchanges the launch token for a new session ID. The
// comparison is constant time so a wrong token leaks no timing signal.
func (m *SessionManager) RedeemBootstrap(token string) (string, error) {
	if subtle.ConstantTimeCompare([]byte(token), []byte(m.bootstrapToken)) != 1 {
		return "", ErrNoSession
	}
	return m.newSession()
}

func (m *SessionManager) newSession() (string, error) {
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = time.Now().Add(m.ttl)
	return id, nil
}

// Validate reports whether the request carries a live session cookie.
func (m *SessionManager) Validate(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false
	}
	m.mu.RLock()
	expiry, ok := m.sessions[c.Value]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		m.mu.Lock()
		delete(m.sessions, c.Value)
		m.mu.Unlock()
		return false
	}
	return true
}

// IssueCookie writes the session cookie for a redeemed bootstrap.
func (m *SessionManager) IssueCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(m.ttl),
	})
}

func randomToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
