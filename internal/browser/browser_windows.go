//go:build windows

package browser

import (
	"os/exec"
)

// Open opens a URL in the default browser on Windows
func Open(url string) error {
	return exec.Command("cmd", "/c", "start", url).Run()
}
