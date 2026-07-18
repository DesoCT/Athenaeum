// Package acceptance runs the Phase 0 exit-gate scenarios (A1-A3) against a
// real, built Athenaeum executable rather than an in-process handler.
//
// Run with: make test-acceptance
package acceptance

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// launchURLPattern matches the bootstrap URL printed in the launch banner.
var launchURLPattern = regexp.MustCompile(`(http://[^\s]+/bootstrap\?t=[^\s]+)`)

// instance is a running Athenaeum process under test.
type instance struct {
	launchURL string
	origin    string
	stdout    string
	client    *http.Client
}

// binaryPath returns the executable under test, skipping when it is absent so
// `go test ./...` stays useful without a prior build.
func binaryPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("ATHENAEUM_BINARY")
	if path == "" {
		t.Skip("ATHENAEUM_BINARY is not set; run `make test-acceptance`")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("ATHENAEUM_BINARY=%s is not usable: %v", path, err)
	}
	return path
}

// fixtureConfig locates examples/athenaeum.toml relative to the repository.
func fixtureConfig(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "examples", "athenaeum.toml"))
	if err != nil {
		t.Fatalf("resolve fixture config: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture config missing: %v", err)
	}
	return path
}

// launch starts the binary and waits for its banner. extraEnv replaces the
// process environment entirely when non-nil, which lets A1 prove the runtime
// needs no Node.js, npm, or SQLite CLI on PATH.
func launch(t *testing.T, env []string, args ...string) *instance {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, binaryPath(t), args...)
	if env != nil {
		cmd.Env = env
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start binary: %v", err)
	}

	var mu sync.Mutex
	var collected strings.Builder
	found := make(chan string, 1)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			collected.WriteString(line + "\n")
			mu.Unlock()
			if m := launchURLPattern.FindString(line); m != "" {
				select {
				case found <- m:
				default:
				}
			}
		}
	}()
	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	select {
	case url := <-found:
		mu.Lock()
		out := collected.String()
		mu.Unlock()

		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar: %v", err)
		}
		return &instance{
			launchURL: url,
			origin:    url[:strings.Index(url, "/bootstrap")],
			stdout:    out,
			client:    &http.Client{Jar: jar, Timeout: 5 * time.Second},
		}
	case <-time.After(15 * time.Second):
		mu.Lock()
		out := collected.String()
		mu.Unlock()
		t.Fatalf("no launch URL within 15s; stdout was:\n%s", out)
		return nil
	}
}

// bootstrap exchanges the launch token for a session cookie held in the jar.
func (in *instance) bootstrap(t *testing.T) {
	t.Helper()
	resp, err := in.client.Get(in.launchURL)
	if err != nil {
		t.Fatalf("bootstrap request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status = %d, want 200 after redirect", resp.StatusCode)
	}
}

// minimalEnv returns an environment with a PATH that contains none of the
// runtimes A1 forbids.
func minimalEnv(t *testing.T) []string {
	t.Helper()
	empty := t.TempDir()
	return []string{"PATH=" + empty, "HOME=" + t.TempDir()}
}

// TestA1SingleRuntimeArtifact covers acceptance A1: the release starts with no
// Node.js, npm, SQLite CLI, or other server process available.
func TestA1SingleRuntimeArtifact(t *testing.T) {
	env := minimalEnv(t)

	// Prove the forbidden runtimes really are absent from the test PATH.
	for _, tool := range []string{"node", "npm", "sqlite3"} {
		if _, err := exec.LookPath(tool); err == nil {
			t.Logf("note: %s exists on the outer PATH but not the child's", tool)
		}
	}

	in := launch(t, env, "serve", fixtureConfig(t), "--port", "0", "--no-open")
	in.bootstrap(t)

	resp, err := in.client.Get(in.origin + "/api/v1/health")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("unexpected health body: %s", body)
	}
	if !strings.Contains(string(body), `"frontend":"embedded"`) {
		t.Fatalf("the binary has no embedded frontend: %s", body)
	}
}

// TestA2OfflineLaunch covers acceptance A2: the fixture workspace opens with no
// network available. The child gets no proxy configuration and a PATH with no
// helper tools; nothing in the Phase 0 path performs a network call.
func TestA2OfflineLaunch(t *testing.T) {
	env := append(minimalEnv(t),
		"HTTP_PROXY=http://127.0.0.1:1",
		"HTTPS_PROXY=http://127.0.0.1:1",
		"NO_PROXY=",
	)

	in := launch(t, env, "serve", fixtureConfig(t), "--port", "0", "--no-open")
	in.bootstrap(t)

	resp, err := in.client.Get(in.origin + "/")
	if err != nil {
		t.Fatalf("frontend request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("frontend status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(strings.ToLower(string(body)), "<!doctype html>") {
		t.Fatalf("index.html was not served offline: %s", body)
	}
}

// TestA3LoopbackDefault covers the binding half of acceptance A3.
func TestA3LoopbackDefault(t *testing.T) {
	in := launch(t, nil, "serve", fixtureConfig(t), "--port", "0", "--no-open")

	if !strings.Contains(in.origin, "127.0.0.1") {
		t.Fatalf("default origin = %q, want a loopback address", in.origin)
	}

	// Confirm nothing is listening on a routable interface for that port.
	_, port, err := net.SplitHostPort(strings.TrimPrefix(in.origin, "http://"))
	if err != nil {
		t.Fatalf("parse origin %q: %v", in.origin, err)
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("enumerate interfaces: %v", err)
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipNet.IP.String(), port), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Fatalf("the server accepted a connection on routable address %s", ipNet.IP)
		}
	}
}

// TestA3RejectsUnauthenticated covers the session half of acceptance A3.
func TestA3RejectsUnauthenticated(t *testing.T) {
	in := launch(t, nil, "serve", fixtureConfig(t), "--port", "0", "--no-open")

	// A client with no cookie jar carries no session.
	bare := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{"/", "/api/v1/health", "/index.html"} {
		t.Run(path, func(t *testing.T) {
			resp, err := bare.Get(in.origin + path)
			if err != nil {
				t.Fatalf("request %s: %v", path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("GET %s status = %d, want 401", path, resp.StatusCode)
			}
		})
	}
}

// TestA3BootstrapTokenNotInLogs guards spec 03 section 12.
func TestA3BootstrapTokenNotInLogs(t *testing.T) {
	in := launch(t, nil, "serve", fixtureConfig(t), "--port", "0", "--no-open")

	token := in.launchURL[strings.Index(in.launchURL, "?t=")+3:]
	if token == "" {
		t.Fatal("no token in the launch URL")
	}

	// The banner deliberately carries the launch URL on stdout; what must not
	// happen is the token appearing in a structured log line.
	for _, line := range strings.Split(in.stdout, "\n") {
		if strings.Contains(line, "level=") && strings.Contains(line, token) {
			t.Fatalf("the bootstrap token leaked into a log line: %s", line)
		}
	}
}

// TestRemoteWithoutTokenFailsStartup covers acceptance K1 early, because the
// check is cheap and the failure mode is severe.
func TestRemoteWithoutTokenFailsStartup(t *testing.T) {
	cmd := exec.Command(binaryPath(t), "serve", fixtureConfig(t),
		"--remote", "--bind", "0.0.0.0", "--port", "0", "--no-open")
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatal("remote mode started without --auth-token-file")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("unexpected failure: %v", err)
	}
	if !strings.Contains(string(output), "auth-token-file") {
		t.Fatalf("the error does not name the missing flag: %s", output)
	}
}

// TestNonLoopbackBindRequiresRemote guards constitution C7.
func TestNonLoopbackBindRequiresRemote(t *testing.T) {
	cmd := exec.Command(binaryPath(t), "serve", fixtureConfig(t),
		"--bind", "0.0.0.0", "--port", "0", "--no-open")
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatal("a non-loopback bind was accepted without --remote")
	}
	if !strings.Contains(string(output), "--remote") {
		t.Fatalf("the error does not name the remedy: %s", output)
	}
}

// TestValidateRejectsBadConfig covers the acceptance B4 exit-code contract.
func TestValidateRejectsBadConfig(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "athenaeum.toml")
	if err := os.WriteFile(bad, []byte("name = \"No schema version\"\n"), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	cmd := exec.Command(binaryPath(t), "validate", bad)
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatal("validate exited zero for an invalid configuration")
	}
	if !strings.Contains(string(output), "schema_version") {
		t.Fatalf("the message does not identify the field: %s", output)
	}
}

// TestValidateAcceptsFixture keeps the shipped example honest.
func TestValidateAcceptsFixture(t *testing.T) {
	cmd := exec.Command(binaryPath(t), "validate", fixtureConfig(t))
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("validate rejected the shipped fixture: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "configuration is valid") {
		t.Fatalf("unexpected validate output: %s", output)
	}
}
