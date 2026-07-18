package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newManager(t *testing.T) *SessionManager {
	t.Helper()
	m, err := NewSessionManager(time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	return m
}

func TestBootstrapTokenIsHighEntropy(t *testing.T) {
	a := newManager(t)
	b := newManager(t)

	if a.BootstrapToken() == b.BootstrapToken() {
		t.Fatal("two processes generated the same bootstrap token")
	}
	// 32 random bytes in base64url is 43 characters.
	if got := len(a.BootstrapToken()); got < 43 {
		t.Fatalf("bootstrap token is %d characters, want at least 43", got)
	}
}

func TestRedeemBootstrapRejectsWrongToken(t *testing.T) {
	m := newManager(t)

	for _, token := range []string{"", "wrong", m.BootstrapToken() + "x"} {
		if _, err := m.RedeemBootstrap(token); err == nil {
			t.Fatalf("RedeemBootstrap(%q) succeeded, want rejection", token)
		}
	}
}

func TestRedeemBootstrapIssuesUniqueSessions(t *testing.T) {
	m := newManager(t)

	first, err := m.RedeemBootstrap(m.BootstrapToken())
	if err != nil {
		t.Fatalf("RedeemBootstrap: %v", err)
	}
	second, err := m.RedeemBootstrap(m.BootstrapToken())
	if err != nil {
		t.Fatalf("RedeemBootstrap: %v", err)
	}
	if first == second {
		t.Fatal("two redemptions produced the same session ID")
	}
}

func TestValidateAcceptsIssuedSessionOnly(t *testing.T) {
	m := newManager(t)
	session, err := m.RedeemBootstrap(m.BootstrapToken())
	if err != nil {
		t.Fatalf("RedeemBootstrap: %v", err)
	}

	tests := []struct {
		name   string
		cookie *http.Cookie
		want   bool
	}{
		{"no cookie", nil, false},
		{"empty cookie", &http.Cookie{Name: CookieName, Value: ""}, false},
		{"forged cookie", &http.Cookie{Name: CookieName, Value: "forged"}, false},
		{"issued cookie", &http.Cookie{Name: CookieName, Value: session}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			if tc.cookie != nil {
				r.AddCookie(tc.cookie)
			}
			if got := m.Validate(r); got != tc.want {
				t.Fatalf("Validate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateRejectsExpiredSession(t *testing.T) {
	m, err := NewSessionManager(-time.Second, false)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	session, err := m.RedeemBootstrap(m.BootstrapToken())
	if err != nil {
		t.Fatalf("RedeemBootstrap: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: session})
	if m.Validate(r) {
		t.Fatal("an expired session was accepted")
	}
}

func TestIssueCookieIsHardened(t *testing.T) {
	m := newManager(t)
	w := httptest.NewRecorder()
	m.IssueCookie(w, "session-id")

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("session cookie SameSite = %v, want Strict", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("session cookie Path = %q, want /", c.Path)
	}
}

func TestNewSessionManagerWithTokenRejectsEmpty(t *testing.T) {
	if _, err := NewSessionManagerWithToken("", time.Hour, false); err == nil {
		t.Fatal("an empty remote token was accepted")
	}
}
