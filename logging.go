package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

type closeFunc func() error

func initializeLogger() (*slog.Logger, closeFunc, error) {
	nilCloseFunc := func() error { return nil }
	appendErrorStack := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "error" {
			err, ok := a.Value.Any().(error)
			if !ok {
				return a
			}
			return slog.String("error", fmt.Sprintf("%+v", err))
		}
		return a
	}
	debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: appendErrorStack})
	logFilePath := os.Getenv("LINKO_LOG_FILE")
	if logFilePath == "" {
		return slog.New(debugHandler), nilCloseFunc, nil
	}
	accessLogFile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return &slog.Logger{}, nilCloseFunc, fmt.Errorf("failed to open log file: %v", err)
	}
	accessLogBuffer := bufio.NewWriterSize(accessLogFile, 8192)
	bufferCloseFunc := func() error {
		if err := accessLogBuffer.Flush(); err != nil {
			return err
		}
		if err := accessLogFile.Close(); err != nil {
			return err
		}
		return nil
	}
	infoHandler := slog.NewJSONHandler(accessLogBuffer, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: appendErrorStack})
	multiHandler := slog.NewMultiHandler(debugHandler, infoHandler)
	return slog.New(multiHandler), bufferCloseFunc, nil
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			logger.Info("Served request", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("client_ip", r.RemoteAddr))
		})
	}
}
