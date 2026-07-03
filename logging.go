package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
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
	obfuscatedErrCodes := map[int]struct{}{
		401: {},
		403: {},
		500: {},
	}
	if _, ok := obfuscatedErrCodes[status]; ok {
		http.Error(w, http.StatusText(status), status)
		return
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

func redactIP(hostport string) (string, error) {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", fmt.Errorf("failed splitting hostport: %w", err)
	}
	parsedIP := net.ParseIP(host)
	if parsedIP == nil {
		return "", fmt.Errorf("failed parsing host: %w", err)
	}
	if parsedIP.DefaultMask() == nil { // returns nil if not IPv4
		return parsedIP.String(), nil
	}
	splitIP := strings.Split(parsedIP.String(), ".")
	splitIP[3] = "x"
	return strings.Join(splitIP, "."), nil
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
				slog.Int("request_body_bytes", spyReader.bytesRead),
				slog.Int("response_status", spyWriter.statusCode),
				slog.Int("response_body_bytes", spyWriter.bytesWritten),
				slog.Duration("duration", time.Since(start)),
			}
			redactedIP, err := redactIP(r.RemoteAddr)
			if err != nil {
				logger.Error("failed redacting IP", slog.Any("error", err))
				attrs = append(attrs, slog.Any("error", fmt.Errorf("failed redacting IP: %w", err)))
			} else {
				attrs = append(attrs, slog.String("client_ip", redactedIP))
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
