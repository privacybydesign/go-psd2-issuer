package logging

import (
	"log/slog"
	"os"
	"strings"
)

var logger *slog.Logger
var currentLevel slog.Level

func init() {
	// Default to INFO level
	InitLogger(slog.LevelInfo)
}

// InitLogger initializes the global logger with the specified level
func InitLogger(level slog.Level) {
	currentLevel = level

	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewTextHandler(os.Stderr, opts)
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// GetLogger returns the global logger instance
func GetLogger() *slog.Logger {
	return logger
}

// GetLevel returns the current log level
func GetLevel() slog.Level {
	return currentLevel
}

// MaskIdentifier masks an identifier such as a consent id or IBAN for logging
// purposes, keeping only the first and last four characters visible.
func MaskIdentifier(id string) string {
	const visible = 4
	if len(id) <= 2*visible {
		return strings.Repeat("*", len(id))
	}
	return id[:visible] + strings.Repeat("*", len(id)-2*visible) + id[len(id)-visible:]
}
