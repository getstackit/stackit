//go:build darwin

package browser

import (
	"os/exec"
)

// Open opens a URL in the default browser on macOS
func Open(url string) error {
	return exec.Command("open", url).Run()
}
