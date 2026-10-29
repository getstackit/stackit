// Package testhelpers provides shared test utilities for CLI packages.
package testhelpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// sharedBinary builds the stackit binary at most once per test process, on
// first use, so packages that never spawn it never pay for the build.
var sharedBinary = sync.OnceValues(buildBinary)

// GetSharedBinaryPath returns the shared binary path, building it on first
// call. It returns "" when the build failed; see GetBinaryError.
func GetSharedBinaryPath() string {
	path, _ := sharedBinary()
	return path
}

// GetBinaryError returns any error that occurred during binary building.
func GetBinaryError() error {
	_, err := sharedBinary()
	return err
}

// buildBinary builds the stackit binary and returns its path.
func buildBinary() (string, error) {
	path, _, err := buildBinaryWithCleanup()
	return path, err
}

// findModuleRoot walks up the directory tree from startDir to find the module root
// (directory containing go.mod file).
func findModuleRoot(startDir string) string {
	dir := startDir
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root of filesystem
			break
		}
		dir = parent
	}
	return ""
}

// buildBinaryWithCleanup builds the stackit binary and returns its path and cleanup function.
func buildBinaryWithCleanup() (string, func(), error) {
	if path := os.Getenv("STACKIT_TEST_BINARY"); path != "" {
		if !filepath.IsAbs(path) {
			return "", nil, fmt.Errorf("STACKIT_TEST_BINARY must be an absolute path: %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, fmt.Errorf("invalid STACKIT_TEST_BINARY: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return "", nil, fmt.Errorf("STACKIT_TEST_BINARY is not an executable file: %s", path)
		}
		// The runner owns this binary; individual packages must not delete it.
		return path, func() {}, nil
	}

	// Get the module root (go up from current directory to stackit root)
	wd, err := os.Getwd()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	moduleRoot := findModuleRoot(wd)
	if moduleRoot == "" {
		return "", nil, fmt.Errorf("could not find module root (go.mod) starting from %s", wd)
	}

	// Create temp directory for binary
	tmpDir, err := os.MkdirTemp("", "stackit-test-binary-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	binaryPath := filepath.Join(tmpDir, "stackit")

	// Build the binary
	cmd := exec.Command("go", "build", "-o", binaryPath, "./apps/cli")
	cmd.Dir = moduleRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmpDir) // Ignore cleanup errors
		return "", nil, fmt.Errorf("failed to build: %s: %w", string(output), err)
	}

	// Make it executable
	//nolint:gosec // 0755 is correct for an executable binary
	if err := os.Chmod(binaryPath, 0755); err != nil {
		_ = os.RemoveAll(tmpDir) // Ignore cleanup errors
		return "", nil, fmt.Errorf("failed to chmod: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir) // Ignore cleanup errors
	}

	return binaryPath, cleanup, nil
}
