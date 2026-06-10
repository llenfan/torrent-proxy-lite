package logging

import (
	"fmt"
	"io"
	"log/slog"
)

func New(level, format string, w io.Writer) (*slog.Logger, error) {
	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: parsedLevel}
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("unknown log format %q", format)
	}
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", level)
	}
}
