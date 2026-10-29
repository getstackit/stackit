//go:build linux

// Package browser opens URLs in the user's default web browser.
package browser

import (
	"os/exec"
)

// Open opens a URL in the default browser on Linux
func Open(url string) error {
	return exec.Command("xdg-open", url).Run()
}
