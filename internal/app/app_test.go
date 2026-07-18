package app

import (
	"strings"
	"testing"
)

// TestValidateRuntimeModeLoopbackDefault covers the loopback-by-default half of
// acceptance A3 and constitution C7.
func TestValidateRuntimeModeLoopbackDefault(t *testing.T) {
	opts := Options{}
	if err := validateRuntimeMode(&opts); err != nil {
		t.Fatalf("default launch was rejected: %v", err)
	}
	if opts.Bind != "127.0.0.1" {
		t.Fatalf("default bind = %q, want 127.0.0.1", opts.Bind)
	}
}

func TestValidateRuntimeModeRejectsNonLoopbackWithoutRemote(t *testing.T) {
	for _, bind := range []string{"0.0.0.0", "192.168.1.10", "::"} {
		t.Run(bind, func(t *testing.T) {
			opts := Options{Bind: bind}
			err := validateRuntimeMode(&opts)
			if err == nil {
				t.Fatalf("bind %s was accepted without --remote", bind)
			}
			if !strings.Contains(err.Error(), "--remote") {
				t.Errorf("error does not mention the remedy: %v", err)
			}
		})
	}
}

// TestRemoteRequiresAuthToken covers acceptance K1: remote mode without a token
// must fail startup.
func TestRemoteRequiresAuthToken(t *testing.T) {
	opts := Options{Bind: "192.168.1.10", Remote: true}
	err := validateRuntimeMode(&opts)
	if err == nil {
		t.Fatal("remote mode started without --auth-token-file")
	}
	if !strings.Contains(err.Error(), "auth-token-file") {
		t.Errorf("error does not name the missing flag: %v", err)
	}
}

func TestRemoteRejectsLoopbackBind(t *testing.T) {
	opts := Options{Bind: "127.0.0.1", Remote: true, AuthTokenFile: "/tmp/token"}
	if err := validateRuntimeMode(&opts); err == nil {
		t.Fatal("--remote was accepted with a loopback bind address")
	}
}

func TestRemoteWithBindAndTokenIsAccepted(t *testing.T) {
	opts := Options{Bind: "192.168.1.10", Remote: true, AuthTokenFile: "/tmp/token"}
	if err := validateRuntimeMode(&opts); err != nil {
		t.Fatalf("a correctly configured remote launch was rejected: %v", err)
	}
}

func TestIsLoopback(t *testing.T) {
	loopback := []string{"127.0.0.1", "127.0.0.53", "::1", "localhost"}
	routable := []string{"0.0.0.0", "192.168.1.10", "::", "example.com", ""}

	for _, host := range loopback {
		if !isLoopback(host) {
			t.Errorf("isLoopback(%q) = false, want true", host)
		}
	}
	for _, host := range routable {
		if isLoopback(host) {
			t.Errorf("isLoopback(%q) = true, want false", host)
		}
	}
}

func TestOriginFor(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"127.0.0.1", 7777, "http://127.0.0.1:7777"},
		{"::1", 7777, "http://[::1]:7777"},
		{"localhost", 8080, "http://localhost:8080"},
	}
	for _, tc := range tests {
		if got := originFor(tc.host, tc.port); got != tc.want {
			t.Errorf("originFor(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}
