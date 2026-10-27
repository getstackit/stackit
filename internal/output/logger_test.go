package output

import (
	"log/slog"
	"testing"
)

func TestGetLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected slog.Level
	}{
		{
			name:     "empty defaults to info",
			envValue: "",
			expected: slog.LevelInfo,
		},
		{
			name:     "debug level",
			envValue: "debug",
			expected: slog.LevelDebug,
		},
		{
			name:     "DEBUG uppercase",
			envValue: "DEBUG",
			expected: slog.LevelDebug,
		},
		{
			name:     "info level",
			envValue: "info",
			expected: slog.LevelInfo,
		},
		{
			name:     "INFO uppercase",
			envValue: "INFO",
			expected: slog.LevelInfo,
		},
		{
			name:     "warn level",
			envValue: "warn",
			expected: slog.LevelWarn,
		},
		{
			name:     "warning level",
			envValue: "warning",
			expected: slog.LevelWarn,
		},
		{
			name:     "error level",
			envValue: "error",
			expected: slog.LevelError,
		},
		{
			name:     "ERROR uppercase",
			envValue: "ERROR",
			expected: slog.LevelError,
		},
		{
			name:     "invalid value defaults to info",
			envValue: "invalid",
			expected: slog.LevelInfo,
		},
		{
			name:     "mixed case Debug",
			envValue: "Debug",
			expected: slog.LevelDebug,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("STACKIT_LOG_LEVEL", tt.envValue)

			got := getLogLevel()
			if got != tt.expected {
				t.Errorf("getLogLevel() = %v, want %v", got, tt.expected)
			}
		})
	}
}
