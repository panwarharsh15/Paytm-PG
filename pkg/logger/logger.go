package logger

import (
	"log/slog"
	"os"
)

// Logger wraps slog for structured, leveled logging.
// In production this outputs JSON which is consumed by Loki/CloudWatch.
type Logger struct {
	*slog.Logger
}

func New(env string) *Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if env == "production" {
		// JSON format for log aggregation (Loki, CloudWatch, Datadog)
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		// Human-readable for local development
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	}

	return &Logger{slog.New(handler)}
}

func (l *Logger) Fatal(msg string, args ...any) {
	l.Error(msg, args...)
	os.Exit(1)
}
