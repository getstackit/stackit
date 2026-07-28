//go:build windows

package utils

import (
	"os/exec"
)

// openBrowser opens a URL in the default browser on Windows
func openBrowser(url string) error {
	return exec.Command("cmd", "/c", "start", url).Run()
}
