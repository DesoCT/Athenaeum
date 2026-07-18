// Command athenaeum is the Athenaeum local command centre.
//
// Commands (spec 05 section 4):
//
//	athenaeum open     [path-to-athenaeum.toml]   start and open a browser
//	athenaeum serve    [path-to-athenaeum.toml]   start without opening a browser
//	athenaeum validate [path-to-athenaeum.toml]   check configuration and exit
//	athenaeum version                             print the build version
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"athenaeum/internal/app"
	"athenaeum/internal/config"
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

	cfg, err := config.Load(f.configPath)
	if err != nil {
		return err
	}
	if f.safeMode {
		cfg.ApplySafeMode()
		logger.Info("safe mode active: Git, remote assets, raw HTML, and Mermaid are disabled")
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
	})
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
		// Non-zero exit with an actionable message (acceptance B4).
		return err
	}

	fmt.Printf("configuration is valid\n")
	fmt.Printf("  file       %s\n", cfg.SourcePath)
	fmt.Printf("  workspace  %s\n", cfg.Name)
	fmt.Printf("  root       %s\n", cfg.AbsRoot)
	fmt.Printf("  include    %d pattern(s)\n", len(cfg.Include))
	fmt.Printf("  exclude    %d pattern(s)\n", len(cfg.Exclude))
	fmt.Printf("  groups     %d\n", len(cfg.Groups))
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
  athenaeum open     [path-to-athenaeum.toml]   start and open a browser
  athenaeum serve    [path-to-athenaeum.toml]   start without opening a browser
  athenaeum validate [path-to-athenaeum.toml]   check configuration and exit
  athenaeum version                             print the build version

Flags for open and serve:
  --no-open                 do not open a browser
  --bind <address>          address to bind (default 127.0.0.1)
  --port <number>           port to bind (default 7777; 0 chooses a free port)
  --remote                  serve beyond loopback; requires --bind and --auth-token-file
  --auth-token-file <path>  file holding the remote-mode token
  --log-level <level>       debug, info, warn, or error
  --safe-mode               disable Git, remote assets, raw HTML, and Mermaid

Environment:
  ATHENAEUM_CONFIG, ATHENAEUM_BIND, ATHENAEUM_PORT,
  ATHENAEUM_AUTH_TOKEN_FILE, ATHENAEUM_LOG_LEVEL, ATHENAEUM_NO_OPEN
`)
}
