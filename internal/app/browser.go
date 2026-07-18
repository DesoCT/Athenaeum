package app

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser launches the user's default browser at url.
//
// The command is invoked directly with an argument vector — never through a
// shell — so a URL can never be interpreted as shell syntax.
func openBrowser(url string) error {
	var name string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "linux":
		name, args = "xdg-open", []string{url}
	case "windows":
		// rundll32 avoids cmd.exe entirely.
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("no browser launcher is known for %s", runtime.GOOS)
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not available on PATH: %w", name, err)
	}
	return exec.Command(path, args...).Start()
}
