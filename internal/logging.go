package form_mailer

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

var fallbackLogger = slog.Default()

// ContextWithLogger attaches a logger to the context; handlers can retrieve it later.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFromContext returns the request-scoped logger or a fallback logger.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return fallbackLogger
	}
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return fallbackLogger
}
