// Package app wires configuration, security, and the HTTP surface into a
// runnable Athenaeum process.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"athenaeum/internal/config"
	"athenaeum/internal/documents"
	"athenaeum/internal/httpapi"
	"athenaeum/internal/security"
	"athenaeum/internal/session"
	"athenaeum/internal/workspace"
	"athenaeum/web"
)

// sessionTTL bounds how long a bootstrapped browser session stays valid.
const sessionTTL = 12 * time.Hour

// shutdownGrace bounds graceful shutdown before outstanding requests are cut.
const shutdownGrace = 5 * time.Second

// Options are the resolved launch options for a single run.
type Options struct {
	Config        *config.Config
	Bind          string
	Port          int
	Remote        bool
	AuthTokenFile string
	OpenBrowser   bool
	Version       string
	Logger        *slog.Logger
	// Stdout receives the launch banner. Tests may substitute a buffer.
	Stdout *os.File

	// documentCount is filled in after enumeration, for the launch banner.
	documentCount int
	// pendingRecovery counts unsaved buffers found at startup.
	pendingRecovery int
}

// Run starts the server and blocks until the context is cancelled or an
// interrupt arrives.
func Run(ctx context.Context, opts Options) error {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}

	if err := validateRuntimeMode(&opts); err != nil {
		return err
	}

	// Runtime checks that depend on launch options, such as raw HTML combined
	// with remote mode (spec 05 section 6).
	if diags := opts.Config.ValidateRuntime(opts.Remote); diags.HasErrors() {
		diags.Write(os.Stderr)
		return errors.New("the workspace configuration is not safe for this launch mode")
	}

	ws, err := workspace.Open(opts.Config)
	if err != nil {
		return err
	}
	for _, d := range ws.Diagnostics() {
		opts.Logger.Warn("workspace", "field", d.Field, "detail", d.Message)
	}
	docs := documents.New(ws)

	// Personal state lives outside the workspace (spec 03 section 1). A failure
	// here degrades crash recovery but must not stop a workspace opening.
	var recovery *session.RecoveryStore
	key := session.NewWorkspaceKey(opts.Config.AbsRoot, "")
	if dirs, err := session.ResolveDirs(key); err != nil {
		opts.Logger.Warn("crash recovery unavailable", "error", err)
	} else if store, err := session.NewRecoveryStore(dirs); err != nil {
		opts.Logger.Warn("crash recovery unavailable", "error", err)
	} else {
		recovery = store
		if pending := store.Count(); pending > 0 {
			opts.Logger.Info("unsaved buffers are available to recover", "count", pending)
			opts.pendingRecovery = pending
		}
	}

	sessions, err := newSessions(opts)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(opts.Bind, fmt.Sprint(opts.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()

	actual := listener.Addr().(*net.TCPAddr)
	origin := originFor(opts.Bind, actual.Port)
	origins := allowedOrigins(origin, opts.Logger)

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("open embedded frontend: %w", err)
	}
	built := web.Built()
	if !built {
		opts.Logger.Warn("no compiled frontend is embedded in this binary; run `make build` for a release executable")
	}

	srv := &http.Server{
		Handler: httpapi.New(httpapi.Options{
			Sessions:      sessions,
			Origins:       security.NewOriginPolicy(origins),
			Frontend:      assets,
			FrontendBuilt: built,
			Version:       opts.Version,
			WorkspaceName: opts.Config.Name,
			Remote:        opts.Remote,
			Logger:        opts.Logger,
			Workspace:     ws,
			Documents:     docs,
			Recovery:      recovery,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	launchURL := origin + httpapi.BootstrapPath + "?t=" + sessions.BootstrapToken()
	opts.documentCount = ws.Count()
	printBanner(opts, origin, launchURL)

	if opts.OpenBrowser {
		// A failure to launch a browser is not fatal: the URL is on stdout.
		if err := openBrowser(launchURL); err != nil {
			opts.Logger.Warn("could not open a browser automatically", "error", err)
		}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// validateRuntimeMode enforces the loopback-by-default rule and the remote
// preconditions (C7, R14, acceptance A3 and K1).
func validateRuntimeMode(opts *Options) error {
	if opts.Bind == "" {
		opts.Bind = "127.0.0.1"
	}
	loopback := isLoopback(opts.Bind)

	if !opts.Remote && !loopback {
		return fmt.Errorf("bind address %s is not loopback; pass --remote to serve a workspace beyond this machine", opts.Bind)
	}
	if opts.Remote {
		if loopback {
			return fmt.Errorf("--remote requires an explicit non-loopback --bind address, but %s is loopback", opts.Bind)
		}
		if opts.AuthTokenFile == "" {
			return errors.New("--remote requires --auth-token-file; Athenaeum refuses to serve remotely without authentication")
		}
	}
	return nil
}

// newSessions builds the session manager, sourcing the credential from the
// token file in remote mode and generating one locally otherwise.
func newSessions(opts Options) (*security.SessionManager, error) {
	sessions, err := security.NewSessionManager(sessionTTL, false)
	if err != nil {
		return nil, fmt.Errorf("generate session secret: %w", err)
	}
	if !opts.Remote {
		return sessions, nil
	}

	raw, err := os.ReadFile(opts.AuthTokenFile)
	if err != nil {
		return nil, fmt.Errorf("read --auth-token-file %s: %w", opts.AuthTokenFile, err)
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 16 {
		return nil, fmt.Errorf("the token in %s is too short; use at least 16 characters", opts.AuthTokenFile)
	}
	return security.NewSessionManagerWithToken(token, sessionTTL, false)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// allowedOrigins returns the origins permitted to mutate state. The serving
// origin is always allowed. ATHENAEUM_DEV_ORIGIN adds the Vite dev server as
// the explicit dev-origin allow-list required by spec 02 section 8; it is a
// development affordance and is logged loudly so it cannot pass unnoticed.
func allowedOrigins(serving string, log *slog.Logger) []string {
	origins := []string{serving}
	dev := strings.TrimSpace(os.Getenv("ATHENAEUM_DEV_ORIGIN"))
	if dev == "" {
		return origins
	}
	log.Warn("development origin allowed for mutating requests; do not use this in a release", "origin", dev)
	return append(origins, dev)
}

func originFor(host string, port int) string {
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return fmt.Sprintf("http://[%s]:%d", host, port)
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func printBanner(opts Options, origin, launchURL string) {
	fmt.Fprintf(opts.Stdout, "Athenaeum %s\n", opts.Version)
	fmt.Fprintf(opts.Stdout, "  workspace  %s\n", opts.Config.Name)
	fmt.Fprintf(opts.Stdout, "  root       %s\n", opts.Config.AbsRoot)
	fmt.Fprintf(opts.Stdout, "  documents  %d\n", opts.documentCount)
	if opts.pendingRecovery > 0 {
		fmt.Fprintf(opts.Stdout, "  recovery   %d unsaved buffer(s) awaiting your decision\n", opts.pendingRecovery)
	}
	fmt.Fprintf(opts.Stdout, "  listening  %s\n", origin)
	if opts.Remote {
		// Spec 03 section 11: no automatic browser bootstrap carrying the
		// token, and a visible persistent remote-mode warning.
		fmt.Fprintf(opts.Stdout, "\n  REMOTE MODE: this workspace is reachable beyond this machine.\n")
		fmt.Fprintf(opts.Stdout, "  Authenticate with the token in %s via %s%s?t=<token>\n",
			opts.AuthTokenFile, origin, httpapi.BootstrapPath)
		return
	}
	fmt.Fprintf(opts.Stdout, "\n  Open: %s\n", launchURL)
}
