package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"boot.dev/linko/internal/linkoerr"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	pkgerr "github.com/pkg/errors"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	logContextKey      contextKey = "log_context"
	requestIDHeaderKey string     = "X-Request-ID"
)

type closeFunc func() error

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

type LogContext struct {
	Username string
	Error    error
}

func appendErrorStack(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}
		if multiErr, ok := errors.AsType[multiError](err); ok {
			var errors []slog.Attr
			for i, e := range multiErr.Unwrap() {
				errors = append(errors, slog.GroupAttrs(
					fmt.Sprintf("error_%d", i+1),
					errorAttrs(e)...,
				))
			}
			return slog.GroupAttrs("errors", errors...)
		}
		return slog.GroupAttrs("error", errorAttrs(err)...)
	}
	return a
}

func errorAttrs(err error) []slog.Attr {
	attrs := linkoerr.Attrs(err)
	attrs = append(attrs, slog.Attr{
		Key:   "message",
		Value: slog.StringValue(err.Error()),
	})
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.Attr{
			Key:   "stack_trace",
			Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
		})
	}
	return attrs
}

func httpError(ctx context.Context, w http.ResponseWriter, status int, err error) {
	if logCtx, ok := ctx.Value(logContextKey).(*LogContext); ok {
		logCtx.Error = err
	}
	http.Error(w, err.Error(), status)
}

func initializeLogger() (*slog.Logger, closeFunc, error) {
	nilCloseFunc := func() error { return nil }
	noColor := !isatty.IsCygwinTerminal(os.Stderr.Fd()) && !isatty.IsTerminal(os.Stderr.Fd())
	debuggerOpts := &tint.Options{Level: slog.LevelDebug, ReplaceAttr: appendErrorStack, NoColor: noColor}
	debugHandler := tint.NewHandler(os.Stderr, debuggerOpts)
	logFilePath := os.Getenv("LINKO_LOG_FILE")
	if logFilePath == "" {
		return slog.New(debugHandler), nilCloseFunc, nil
	}
	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    1,
		MaxAge:     28,
		MaxBackups: 10,
		LocalTime:  false,
		Compress:   true,
	}
	bufferCloseFunc := func() error {
		if err := lumberjackLogger.Close(); err != nil {
			return err
		}
		return nil
	}
	infoHandler := slog.NewJSONHandler(lumberjackLogger, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: appendErrorStack})
	multiHandler := slog.NewMultiHandler(debugHandler, infoHandler)
	return slog.New(multiHandler), bufferCloseFunc, nil
}

func requestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(requestIDHeaderKey)
			if requestID == "" {
				requestID = rand.Text()
			}
			w.Header().Set(requestIDHeaderKey, requestID)
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logCtx := &LogContext{}
			ctx := context.WithValue(r.Context(), logContextKey, logCtx)
			r = r.WithContext(ctx)
			spyReader := &spyReadCloser{ReadCloser: r.Body}
			r.Body = spyReader
			spyWriter := &spyResponseWriter{ResponseWriter: w}
			next.ServeHTTP(spyWriter, r)
			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("client_ip", r.RemoteAddr),
				slog.Int("request_body_bytes", spyReader.bytesRead),
				slog.Int("response_status", spyWriter.statusCode),
				slog.Int("response_body_bytes", spyWriter.bytesWritten),
				slog.Duration("duration", time.Since(start)),
			}
			if logCtx.Username != "" {
				attrs = append(attrs, slog.String("user", logCtx.Username))
			}
			if logCtx.Error != nil {
				attrs = append(attrs, slog.Any("error", logCtx.Error))
			}
			attrs = append(attrs, slog.String("request_id", w.Header().Get(requestIDHeaderKey)))
			logger.Info("Served request", attrs...)
		})
	}
}
