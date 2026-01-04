// Package splog provides structured logging and output.
package splog

import (
	"os"
	"path/filepath"
)

// GetLogFilePath returns the path to the log file.
// If STACKIT_LOG_FILE is set, uses that path.
// Otherwise, uses ~/.stackit/logs/stackit.log
func GetLogFilePath() string {
	if customPath := os.Getenv("STACKIT_LOG_FILE"); customPath != "" {
		return customPath
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if we can't get home dir
		return "stackit.log"
	}

	logDir := filepath.Join(homeDir, ".stackit", "logs")
	logFile := filepath.Join(logDir, "stackit.log")

	return logFile
}
