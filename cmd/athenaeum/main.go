// Command athenaeum is the Athenaeum local command centre.
//
// Commands (spec 05 section 4):
//
//	athenaeum open       [path-to-athenaeum.toml]   start and open a browser
//	athenaeum serve      [path-to-athenaeum.toml]   start without opening a browser
//	athenaeum validate   [path-to-athenaeum.toml]   check configuration and exit
//	athenaeum workspaces                            list the workspace registry
//	athenaeum version                               print the build version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"athenaeum/internal/app"
	"athenaeum/internal/config"
	"athenaeum/internal/registry"
	"athenaeum/internal/workspace"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "athenaeum: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return fmt.Errorf("a command is required")
	}

	command := args[0]
	rest := args[1:]

	switch command {
	case "version", "--version", "-v":
		fmt.Printf("athenaeum %s\n", version)
		return nil
	case "help", "--help", "-h":
		usage(os.Stdout)
		return nil
	case "open":
		return runServer(rest, true)
	case "serve":
		return runServer(rest, false)
	case "validate":
		return runValidate(rest)
	case "workspaces":
		return runWorkspaces(rest)
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", command)
	}
}

// flags holds the launch flags shared by open and serve.
type flags struct {
	noOpen        bool
	bind          string
	port          int
	remote        bool
	authTokenFile string
	logLevel      string
	safeMode      bool
	configPath    string
	pick          bool
	registryPath  string
	// explicitPath records that the workspace was named, rather than inferred
	// from the working directory. Only an explicit path is allowed to make a
	// missing configuration a hard error.
	explicitPath bool
}

func parseFlags(command string, args []string, defaultOpen bool) (*flags, error) {
	positional, flagArgs, err := splitArgs(args)
	if err != nil {
		return nil, err
	}

	f := &flags{}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.BoolVar(&f.noOpen, "no-open", false, "do not open a browser")
	fs.StringVar(&f.bind, "bind", envOr("ATHENAEUM_BIND", "127.0.0.1"), "address to bind")
	fs.StringVar(&f.logLevel, "log-level", envOr("ATHENAEUM_LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	fs.StringVar(&f.authTokenFile, "auth-token-file", os.Getenv("ATHENAEUM_AUTH_TOKEN_FILE"), "file holding the remote-mode token")
	fs.BoolVar(&f.remote, "remote", false, "serve beyond loopback (requires --bind and --auth-token-file)")
	fs.BoolVar(&f.safeMode, "safe-mode", false, "disable Git, remote assets, raw HTML, Mermaid, and user overrides")
	fs.BoolVar(&f.pick, "pick", false, "start at the workspace picker, ignoring any athenaeum.toml here")
	fs.StringVar(&f.registryPath, "registry", os.Getenv("ATHENAEUM_REGISTRY"), "workspace registry file (default <user-config>/athenaeum/workspaces.toml)")

	defaultPort, err := strconv.Atoi(envOr("ATHENAEUM_PORT", "7777"))
	if err != nil {
		return nil, fmt.Errorf("ATHENAEUM_PORT must be a number: %w", err)
	}
	fs.IntVar(&f.port, "port", defaultPort, "port to bind (0 chooses a free port)")

	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected at most one workspace path, got %d: %v", len(positional), positional)
	}
	f.configPath = firstArg(positional, os.Getenv("ATHENAEUM_CONFIG"))
	f.explicitPath = f.configPath != ""

	if os.Getenv("ATHENAEUM_NO_OPEN") != "" {
		f.noOpen = true
	}
	if !defaultOpen {
		f.noOpen = true
	}
	return f, nil
}

func runServer(args []string, defaultOpen bool) error {
	command := "serve"
	if defaultOpen {
		command = "open"
	}
	f, err := parseFlags(command, args, defaultOpen)
	if err != nil {
		return err
	}

	logger, err := newLogger(f.logLevel)
	if err != nil {
		return err
	}

	cfg, err := resolveWorkspace(f)
	if err != nil {
		return err
	}
	if cfg != nil {
		if diags := cfg.Validate(); diags.HasErrors() {
			diags.Write(os.Stderr)
			errCount, _ := diags.Counts()
			return fmt.Errorf("%s is not valid: %d error(s); run `athenaeum validate` for detail",
				cfg.SourcePath, errCount)
		}
		if f.safeMode {
			cfg.ApplySafeMode()
			logger.Info("safe mode active: Git, remote assets, raw HTML, and Mermaid are disabled")
		}
	}

	return app.Run(context.Background(), app.Options{
		Config:        cfg,
		Bind:          f.bind,
		Port:          f.port,
		Remote:        f.remote,
		AuthTokenFile: f.authTokenFile,
		OpenBrowser:   !f.noOpen,
		Version:       version,
		Logger:        logger,
		RegistryPath:  f.registryPath,
		SafeMode:      f.safeMode,
	})
}

// resolveWorkspace decides which workspace a launch opens (ADR-0004).
//
// The order is chosen so that no command that worked before behaves
// differently:
//
//  1. an explicit path, including ATHENAEUM_CONFIG — open it, and fail loudly
//     if it cannot be opened, exactly as before;
//  2. no path but ./athenaeum.toml exists — open it, exactly as before;
//  3. no path and no local configuration — start at the picker, where this was
//     previously nothing but an error.
//
// --pick forces the picker regardless of the working directory, which is how
// one drills out from a shell already inside a workspace. An earlier draft of
// ADR-0004 had the picker win over a local athenaeum.toml; that was rejected
// because it would quietly change what `athenaeum open` means inside a
// repository.
//
// A nil config means "start at the picker" and is not an error.
func resolveWorkspace(f *flags) (*config.Config, error) {
	if f.pick {
		if f.explicitPath {
			return nil, fmt.Errorf("--pick starts at the workspace picker, so it cannot be combined with the workspace path %q", f.configPath)
		}
		return nil, nil
	}

	if f.explicitPath {
		return config.Load(f.configPath)
	}

	if _, err := os.Stat(config.DefaultFileName); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", config.DefaultFileName, err)
	}
	return config.Load(config.DefaultFileName)
}

// runWorkspaces lists the registry without opening a browser (ADR-0004).
func runWorkspaces(args []string) error {
	positional, flagArgs, err := splitArgs(args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("workspaces", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	registryPath := fs.String("registry", os.Getenv("ATHENAEUM_REGISTRY"),
		"workspace registry file (default <user-config>/athenaeum/workspaces.toml)")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) > 0 {
		return fmt.Errorf("workspaces takes no arguments, got %v", positional)
	}

	path := *registryPath
	if path == "" {
		path, err = registry.DefaultPath()
		if err != nil {
			return err
		}
	}

	reg, err := registry.Load(path)
	if err != nil {
		return err
	}

	if !reg.Present {
		fmt.Printf("no workspace registry at %s\n\n", reg.SourcePath)
		fmt.Printf("Create it with entries like:\n\n")
		fmt.Printf("  [[workspace]]\n  name = \"Athenaeum\"\n  path = \"~/dev/athenaeum\"\n")
		return nil
	}

	fmt.Printf("registry %s\n\n", reg.SourcePath)
	if len(reg.Entries) == 0 {
		fmt.Printf("no workspaces are registered; add a [[workspace]] table with name and path\n")
	}
	for _, entry := range reg.Entries {
		if entry.Available {
			fmt.Printf("  %-24s %s\n", entry.Name, entry.Path)
			continue
		}
		// An unavailable entry is shown with its reason rather than omitted, so
		// a mistyped path is visible and fixable (ADR-0004, R1).
		fmt.Printf("  %-24s %s\n", entry.Name, entry.RawPath)
		fmt.Printf("  %-24s   unavailable: %s (%s)\n", "", entry.Reason, entry.Code)
		if entry.Remedy != "" {
			fmt.Printf("  %-24s   remedy: %s\n", "", entry.Remedy)
		}
	}

	if len(reg.Diagnostics) > 0 {
		fmt.Fprintln(os.Stderr)
		reg.Diagnostics.Write(os.Stderr)
	}
	return nil
}

func runValidate(args []string) error {
	positional, flagArgs, err := splitArgs(args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	path := firstArg(positional, os.Getenv("ATHENAEUM_CONFIG"))
	cfg, err := config.Load(path)
	if err != nil {
		// Parse-level failure: non-zero exit with an actionable message.
		return err
	}

	// Structural validation, then enumeration, which adds warnings that need
	// the filesystem such as include patterns matching nothing.
	diags := cfg.Validate()

	var ws *workspace.Workspace
	if !diags.HasErrors() {
		ws, err = workspace.Open(cfg)
		if err != nil {
			return err
		}
		diags = append(diags, ws.Diagnostics()...)
	}

	errCount, warnCount := diags.Counts()
	if len(diags) > 0 {
		diags.Write(os.Stderr)
		fmt.Fprintln(os.Stderr)
	}

	if errCount > 0 {
		return fmt.Errorf("%s is not valid: %d error(s), %d warning(s)", cfg.SourcePath, errCount, warnCount)
	}

	fmt.Printf("configuration is valid\n")
	fmt.Printf("  file       %s\n", cfg.SourcePath)
	fmt.Printf("  workspace  %s\n", cfg.Name)
	fmt.Printf("  root       %s\n", cfg.AbsRoot)
	fmt.Printf("  include    %d pattern(s)\n", len(cfg.Include))
	fmt.Printf("  exclude    %d pattern(s)\n", len(cfg.Exclude))
	fmt.Printf("  groups     %d\n", len(cfg.Groups))
	if ws != nil {
		fmt.Printf("  documents  %d\n", ws.Count())
	}
	if warnCount > 0 {
		fmt.Printf("  warnings   %d (listed above)\n", warnCount)
	}
	return nil
}

func newLogger(level string) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid --log-level %q: use debug, info, warn, or error", level)
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstArg(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}

func usage(w *os.File) {
	fmt.Fprint(w, `Athenaeum — a local-first command centre for Markdown workspaces.

Usage:
  athenaeum open       [path-to-athenaeum.toml]   start and open a browser
  athenaeum serve      [path-to-athenaeum.toml]   start without opening a browser
  athenaeum validate   [path-to-athenaeum.toml]   check configuration and exit
  athenaeum workspaces                            list the workspace registry
  athenaeum version                               print the build version

Without a path, open and serve use ./athenaeum.toml when it exists, and
otherwise start at the workspace picker.

Flags for open and serve:
  --no-open                 do not open a browser
  --pick                    start at the workspace picker, ignoring ./athenaeum.toml
  --registry <path>         workspace registry file
  --bind <address>          address to bind (default 127.0.0.1)
  --port <number>           port to bind (default 7777; 0 chooses a free port)
  --remote                  serve beyond loopback; requires --bind and --auth-token-file
  --auth-token-file <path>  file holding the remote-mode token
  --log-level <level>       debug, info, warn, or error
  --safe-mode               disable Git, remote assets, raw HTML, and Mermaid

Environment:
  ATHENAEUM_CONFIG, ATHENAEUM_BIND, ATHENAEUM_PORT, ATHENAEUM_REGISTRY,
  ATHENAEUM_AUTH_TOKEN_FILE, ATHENAEUM_LOG_LEVEL, ATHENAEUM_NO_OPEN
`)
}
