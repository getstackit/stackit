package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/getstackit/stackit/internal/github"
)

// buildAppProvider constructs a GitHub App installation-token provider from the
// environment, or returns (nil, nil) when no App is configured. The App
// authenticates onboarding clones and background repo syncs.
//
// STACKIT_GITHUB_APP_ID is the numeric App ID. The private key comes from
// STACKIT_GITHUB_APP_PRIVATE_KEY (PEM contents) or, when that is empty,
// STACKIT_GITHUB_APP_PRIVATE_KEY_FILE (a path to the PEM file).
func buildAppProvider() (*github.AppTokenProvider, error) {
	idStr := strings.TrimSpace(os.Getenv("STACKIT_GITHUB_APP_ID"))
	if idStr == "" {
		return nil, nil
	}
	appID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("STACKIT_GITHUB_APP_ID: %w", err)
	}

	keyPEM := []byte(os.Getenv("STACKIT_GITHUB_APP_PRIVATE_KEY"))
	if len(keyPEM) == 0 {
		path := strings.TrimSpace(os.Getenv("STACKIT_GITHUB_APP_PRIVATE_KEY_FILE"))
		if path == "" {
			return nil, fmt.Errorf("STACKIT_GITHUB_APP_ID is set but no private key (set STACKIT_GITHUB_APP_PRIVATE_KEY or STACKIT_GITHUB_APP_PRIVATE_KEY_FILE)")
		}
		keyPEM, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read STACKIT_GITHUB_APP_PRIVATE_KEY_FILE: %w", err)
		}
	}

	return github.NewAppTokenProvider(appID, keyPEM)
}
