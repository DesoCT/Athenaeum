package main

import (
	"slices"
	"testing"
)

// TestSplitArgsPermutesFlags is the regression test for a bug in which Go's
// flag package stopped parsing at the workspace path, so
// `athenaeum serve workspace.toml --safe-mode` silently ignored --safe-mode.
func TestSplitArgsPermutesFlags(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantPositional []string
		wantFlags      []string
	}{
		{
			name:           "flags after path",
			args:           []string{"workspace.toml", "--safe-mode", "--no-open"},
			wantPositional: []string{"workspace.toml"},
			wantFlags:      []string{"--safe-mode", "--no-open"},
		},
		{
			name:           "flags before path",
			args:           []string{"--safe-mode", "workspace.toml"},
			wantPositional: []string{"workspace.toml"},
			wantFlags:      []string{"--safe-mode"},
		},
		{
			name:           "value flag after path",
			args:           []string{"workspace.toml", "--port", "0"},
			wantPositional: []string{"workspace.toml"},
			wantFlags:      []string{"--port", "0"},
		},
		{
			name:           "value flag surrounds path",
			args:           []string{"--bind", "127.0.0.1", "workspace.toml", "--port", "9999"},
			wantPositional: []string{"workspace.toml"},
			wantFlags:      []string{"--bind", "127.0.0.1", "--port", "9999"},
		},
		{
			name:           "equals form",
			args:           []string{"workspace.toml", "--port=9999"},
			wantPositional: []string{"workspace.toml"},
			wantFlags:      []string{"--port=9999"},
		},
		{
			name:           "single dash form",
			args:           []string{"workspace.toml", "-safe-mode"},
			wantPositional: []string{"workspace.toml"},
			wantFlags:      []string{"-safe-mode"},
		},
		{
			name:           "double dash terminator",
			args:           []string{"--no-open", "--", "--not-a-flag.toml"},
			wantPositional: []string{"--not-a-flag.toml"},
			wantFlags:      []string{"--no-open"},
		},
		{
			name:           "no arguments",
			args:           []string{},
			wantPositional: nil,
			wantFlags:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			positional, flags, err := splitArgs(tc.args)
			if err != nil {
				t.Fatalf("splitArgs: %v", err)
			}
			if !slices.Equal(positional, tc.wantPositional) {
				t.Errorf("positional = %v, want %v", positional, tc.wantPositional)
			}
			if !slices.Equal(flags, tc.wantFlags) {
				t.Errorf("flags = %v, want %v", flags, tc.wantFlags)
			}
		})
	}
}

// TestSplitArgsRejectsDanglingValueFlag keeps a truncated command line from
// silently binding a default.
func TestSplitArgsRejectsDanglingValueFlag(t *testing.T) {
	if _, _, err := splitArgs([]string{"workspace.toml", "--port"}); err == nil {
		t.Fatal("a value flag with no value was accepted")
	}
}

// TestSplitArgsPassesUnknownFlagsThrough lets the flag package emit the
// standard diagnostic rather than this helper inventing one.
func TestSplitArgsPassesUnknownFlagsThrough(t *testing.T) {
	_, flags, err := splitArgs([]string{"workspace.toml", "--nonsense"})
	if err != nil {
		t.Fatalf("splitArgs: %v", err)
	}
	if !slices.Contains(flags, "--nonsense") {
		t.Fatalf("unknown flag was dropped: %v", flags)
	}
}

func TestParseFlagsAppliesFlagsAfterPath(t *testing.T) {
	f, err := parseFlags("serve", []string{"workspace.toml", "--safe-mode", "--port", "0"}, false)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !f.safeMode {
		t.Error("--safe-mode after the path was ignored")
	}
	if f.port != 0 {
		t.Errorf("port = %d, want 0", f.port)
	}
	if f.configPath != "workspace.toml" {
		t.Errorf("configPath = %q, want workspace.toml", f.configPath)
	}
}

func TestParseFlagsRejectsExtraPositional(t *testing.T) {
	if _, err := parseFlags("serve", []string{"a.toml", "b.toml"}, false); err == nil {
		t.Fatal("two workspace paths were accepted")
	}
}

// TestServeNeverOpensBrowser encodes the rule in spec 05 section 4.
func TestServeNeverOpensBrowser(t *testing.T) {
	f, err := parseFlags("serve", []string{"workspace.toml"}, false)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !f.noOpen {
		t.Error("serve would have opened a browser")
	}
}

func TestOpenDefaultsToOpeningBrowser(t *testing.T) {
	t.Setenv("ATHENAEUM_NO_OPEN", "")
	f, err := parseFlags("open", []string{"workspace.toml"}, true)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if f.noOpen {
		t.Error("open would not have opened a browser")
	}
}

func TestNoOpenEnvironmentOverride(t *testing.T) {
	t.Setenv("ATHENAEUM_NO_OPEN", "1")
	f, err := parseFlags("open", []string{"workspace.toml"}, true)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !f.noOpen {
		t.Error("ATHENAEUM_NO_OPEN was ignored")
	}
}
