// Package tui provides terminal user interface components and utilities.
package tui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/natefinch/lumberjack.v2"
)

// simpleHandler is a custom slog handler that writes messages without timestamps or level prefixes
type simpleHandler struct {
	writer    io.Writer
	debugMode bool
	quiet     *bool // Pointer to quiet flag so it can be changed dynamically
}

func (h *simpleHandler) Enabled(_ context.Context, level slog.Level) bool {
	// Debug messages only enabled in debug mode
	if level == slog.LevelDebug {
		return h.debugMode
	}
	// Info, Warn, and Error are always enabled
	return true
}

func (h *simpleHandler) Handle(_ context.Context, record slog.Record) error {
	if *h.quiet {
		return nil // Suppress output when in quiet mode
	}
	_, err := fmt.Fprintln(h.writer, record.Message)
	return err
}

func (h *simpleHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *simpleHandler) WithGroup(_ string) slog.Handler {
	return h
}

// createLumberjackLogger creates a lumberjack logger with configuration from environment variables
func createLumberjackLogger(logFilePath string) *lumberjack.Logger {
	config := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    1,     // 1MB (in megabytes) - default
		MaxBackups: 2,     // Keep 2 old files - default
		MaxAge:     30,    // Keep for 30 days - default
		Compress:   false, // Never compress logs - default
	}

	// Override with environment variables
	if maxSizeStr := os.Getenv("STACKIT_LOG_MAX_SIZE"); maxSizeStr != "" {
		if maxSize, err := strconv.Atoi(maxSizeStr); err == nil && maxSize > 0 {
			config.MaxSize = maxSize
		}
	}

	if maxBackupsStr := os.Getenv("STACKIT_LOG_MAX_BACKUPS"); maxBackupsStr != "" {
		if maxBackups, err := strconv.Atoi(maxBackupsStr); err == nil && maxBackups >= 0 {
			config.MaxBackups = maxBackups
		}
	}

	if maxAgeStr := os.Getenv("STACKIT_LOG_MAX_AGE"); maxAgeStr != "" {
		if maxAge, err := strconv.Atoi(maxAgeStr); err == nil && maxAge > 0 {
			config.MaxAge = maxAge
		}
	}

	return config
}

// multiHandler fans out log records to multiple handlers
type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	// Enable if any handler is enabled
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, record slog.Record) error {
	// Send to all handlers
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

// Splog provides structured logging and output
type Splog struct {
	logger     *slog.Logger
	fileLogger *slog.Logger // Separate logger for file output
	writer     io.Writer
	logWriter  io.WriteCloser // Lumberjack logger for file logging
	quiet      bool           // When true, suppresses all output (used during TUI mode)
}

var (
	// DefaultConsoleWriter is the writer used by NewSplog when no writer is specified.
	// This can be overridden in tests to capture output.
	DefaultConsoleWriter io.Writer = os.Stdout
)

// NewSplog creates a new splog instance with console-only logging
func NewSplog() *Splog {
	splog, _ := NewSplogWithFlagsAndWriter("", os.Getenv("DEBUG") != "", false, DefaultConsoleWriter)
	return splog
}

// NewSplogToWriter creates a new splog instance that writes to the given writer.
// This is useful for testing.
func NewSplogToWriter(w io.Writer) *Splog {
	splog := &Splog{
		writer: w,
		quiet:  false,
	}

	consoleHandler := &simpleHandler{
		writer:    w,
		debugMode: false,
		quiet:     &splog.quiet,
	}

	splog.logger = slog.New(consoleHandler)
	return splog
}

// NewSplogWithConfig creates a new splog instance with optional file logging
func NewSplogWithConfig(logFilePath string, _ string) (*Splog, error) {
	return NewSplogWithFlags(logFilePath, os.Getenv("DEBUG") != "", false)
}

// NewSplogWithFlags creates a new splog instance with the given flags
func NewSplogWithFlags(logFilePath string, debugMode, quiet bool) (*Splog, error) {
	return NewSplogWithFlagsAndWriter(logFilePath, debugMode, quiet, os.Stdout)
}

// NewSplogWithFlagsAndWriter creates a new splog instance with the given flags and writer
func NewSplogWithFlagsAndWriter(logFilePath string, debugMode, quiet bool, writer io.Writer) (*Splog, error) {
	splog := &Splog{
		writer: writer,
		quiet:  quiet,
	}

	// Create console handler
	consoleHandler := &simpleHandler{
		writer:    writer,
		debugMode: debugMode,
		quiet:     &splog.quiet,
	}

	var handlers []slog.Handler
	handlers = append(handlers, consoleHandler)

	// Set up file logging if path is provided
	if logFilePath != "" {
		// Ensure log directory exists
		logDir := filepath.Dir(logFilePath)
		if err := os.MkdirAll(logDir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		// Create lumberjack logger for file rotation
		lumberjackLogger := createLumberjackLogger(logFilePath)
		splog.logWriter = lumberjackLogger

		// Create file handler with timestamps
		fileHandler := slog.NewTextHandler(lumberjackLogger, &slog.HandlerOptions{
			Level: slog.LevelDebug, // Always log everything to file
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				// Add timestamps to file logs
				if a.Key == slog.TimeKey {
					return slog.Attr{Key: a.Key, Value: slog.StringValue(a.Value.Time().Format("2006-01-02 15:04:05.000"))}
				}
				return a
			},
		})

		handlers = append(handlers, fileHandler)
		splog.fileLogger = slog.New(fileHandler)
	}

	// Use multi-handler to fan out to both console and file handlers
	multiHandler := &multiHandler{handlers: handlers}
	splog.logger = slog.New(multiHandler)

	return splog, nil
}

// SetQuiet sets the quiet mode for the logger.
// When quiet is true, all output is suppressed (used during TUI mode).
func (s *Splog) SetQuiet(quiet bool) {
	s.quiet = quiet
}

// IsQuiet returns whether the logger is in quiet mode.
func (s *Splog) IsQuiet() bool {
	return s.quiet
}

// logMessage is a helper to log a message using slog without format string validation
func (s *Splog) logMessage(level slog.Level, msg string) {
	s.logger.Log(context.Background(), level, msg)
}

// Info writes an info message
// The format parameter may be a variable string, which is safe as we use fmt.Sprintf internally
// nolint // format string validation is handled internally via fmt.Sprintf
func (s *Splog) Info(format string, args ...interface{}) {
	var msg string
	if len(args) == 0 {
		msg = format
	} else {
		msg = fmt.Sprintf(format, args...)
	}
	s.logMessage(slog.LevelInfo, msg)
}

// Page writes output that should be paged (for now, just print)
func (s *Splog) Page(content string) {
	if s.quiet {
		return
	}
	_, _ = fmt.Fprint(s.writer, content)
}

// Newline writes a newline
func (s *Splog) Newline() {
	if s.quiet {
		return
	}
	_, _ = fmt.Fprintln(s.writer)
}

// Warn writes a warning message
// The format parameter may be a variable string, which is safe as we use fmt.Sprintf internally
// nolint // format string validation is handled internally via fmt.Sprintf
func (s *Splog) Warn(format string, args ...interface{}) {
	var msg string
	if len(args) == 0 {
		msg = "⚠️  " + format
	} else {
		msg = fmt.Sprintf("⚠️  "+format, args...)
	}
	s.logMessage(slog.LevelWarn, msg)
}

// Error writes an error message
// The format parameter may be a variable string, which is safe as we use fmt.Sprintf internally
// nolint // format string validation is handled internally via fmt.Sprintf
func (s *Splog) Error(format string, args ...interface{}) {
	var msg string
	if len(args) == 0 {
		msg = "❌ " + format
	} else {
		msg = fmt.Sprintf("❌ "+format, args...)
	}
	s.logMessage(slog.LevelError, msg)
}

// Debug writes a debug message
// The format parameter may be a variable string, which is safe as we use fmt.Sprintf internally
// nolint // format string validation is handled internally via fmt.Sprintf
func (s *Splog) Debug(format string, args ...interface{}) {
	var msg string
	if len(args) == 0 {
		msg = format
	} else {
		msg = fmt.Sprintf(format, args...)
	}
	s.logMessage(slog.LevelDebug, msg)
}

// Tip writes a tip message
// The format parameter may be a variable string, which is safe as we use fmt.Sprintf internally
// nolint // format string validation is handled internally via fmt.Sprintf
func (s *Splog) Tip(format string, args ...interface{}) {
	var msg string
	if len(args) == 0 {
		msg = "💡 " + format
	} else {
		msg = fmt.Sprintf("💡 "+format, args...)
	}
	s.logMessage(slog.LevelInfo, msg)
}

// Close closes the log file if one was opened
func (s *Splog) Close() error {
	if s.logWriter != nil {
		return s.logWriter.Close()
	}
	return nil
}
