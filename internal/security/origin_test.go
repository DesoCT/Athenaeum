package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginPolicyAllows(t *testing.T) {
	p := NewOriginPolicy([]string{"http://127.0.0.1:7777"})

	tests := []struct {
		name    string
		origin  string
		referer string
		want    bool
	}{
		{"matching origin", "http://127.0.0.1:7777", "", true},
		{"foreign origin", "https://evil.example", "", false},
		{"different port", "http://127.0.0.1:9999", "", false},
		{"no origin or referer", "", "", false},
		{"matching referer fallback", "", "http://127.0.0.1:7777/index.html", true},
		{"foreign referer fallback", "", "https://evil.example/page", false},
		{"unparseable referer", "", "://bad", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/documents/a.md", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				r.Header.Set("Referer", tc.referer)
			}
			if got := p.Allows(r); got != tc.want {
				t.Fatalf("Allows() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsMutating(t *testing.T) {
	safe := []string{http.MethodGet, http.MethodHead, http.MethodOptions}
	mutating := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

	for _, m := range safe {
		if IsMutating(m) {
			t.Errorf("IsMutating(%s) = true, want false", m)
		}
	}
	for _, m := range mutating {
		if !IsMutating(m) {
			t.Errorf("IsMutating(%s) = false, want true", m)
		}
	}
}
