package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	// LogStdout is the log file path sentinel that selects standard output.
	LogStdout = "stdout"
	// DefaultLogLevel is the log level used when none is configured.
	DefaultLogLevel = "info"
)

// LogConfig configures the application logger.
type LogConfig struct {
	Level string
	// FilePath is a file to append logs to, or LogStdout.
	FilePath string
}

func (l *LogConfig) validate() error {
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[l.Level] {
		return fmt.Errorf("invalid log level: %q", l.Level)
	}

	switch l.FilePath {
	case "":
		return fmt.Errorf("log file path cannot be empty: use %q or a file path", LogStdout)
	case LogStdout:
		return nil
	}

	dir := filepath.Dir(l.FilePath)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("log file directory does not exist: %s", dir)
	}
	return nil
}

// ParseLogLevel converts a string log level to slog.Level
func ParseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // Default to info if invalid level is provided
	}
}

// OpenLogOutput returns the writer for the configured log destination and a
// function that releases it. LogStdout selects os.Stdout, which the returned
// close function leaves open. Log files are created with 0600 permissions.
func OpenLogOutput(filePath string) (io.Writer, func() error, error) {
	if filePath == LogStdout {
		return os.Stdout, func() error { return nil }, nil
	}
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}
