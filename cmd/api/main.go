package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	form_courier "github.com/nazarhussain/form-courier/internal"
)

func main() {
	logger := newLogger()

	config := form_courier.GetConfig()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", form_courier.HandleHealth)

	// POST /v1/contact/{siteKey}
	mux.HandleFunc("/v1/contact/", form_courier.HandleContact)

	handler := loggingMiddleware(logger, secHeaders(mux))

	s := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("form-mailer listening", "addr", config.ListenAddr, "sites", len(config.Sites))

	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func secHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer-when-downgrade")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(baseLogger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestLogger := baseLogger.With(
			"method", r.Method,
			"path", r.URL.Path,
		)

		ctx := form_courier.ContextWithLogger(r.Context(), requestLogger)
		r = r.WithContext(ctx)

		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if rec := recover(); rec != nil {
				requestLogger.Error("panic recovered",
					"err", rec,
					"type", fmt.Sprintf("%T", rec),
					"stack", string(debug.Stack()),
				)
				lrw.WriteHeader(http.StatusInternalServerError)
			}
			duration := time.Since(start)
			level := slog.LevelInfo
			switch {
			case lrw.status >= 500:
				level = slog.LevelError
			case lrw.status >= 400:
				level = slog.LevelWarn
			}
			requestLogger.Log(ctx, level, "request completed",
				"status", lrw.status,
				"duration_ms", duration.Milliseconds(),
				"bytes", lrw.length,
			)
		}()

		next.ServeHTTP(lrw, r)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	length int
	wrote  bool
}

func (lrw *loggingResponseWriter) WriteHeader(status int) {
	if !lrw.wrote {
		lrw.ResponseWriter.WriteHeader(status)
		lrw.wrote = true
	}
	lrw.status = status
}

func (lrw *loggingResponseWriter) Write(p []byte) (int, error) {
	if !lrw.wrote {
		lrw.WriteHeader(http.StatusOK)
	}
	n, err := lrw.ResponseWriter.Write(p)
	lrw.length += n
	return n, err
}

func newLogger() *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: logLevelFromEnv(),
	}

	var handler slog.Handler
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func logLevelFromEnv() slog.Leveler {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}
