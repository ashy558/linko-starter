package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
)

type closeFunc func() error

func initializeLogger() (*slog.Logger, closeFunc, error) {
	nilCloseFunc := func() error { return nil }
	logFilePath := os.Getenv("LINKO_LOG_FILE")
	if logFilePath == "" {
		return slog.New(slog.NewTextHandler(os.Stderr, nil)), nilCloseFunc, nil
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
	multiWriter := io.MultiWriter(os.Stderr, accessLogBuffer)
	return slog.New(slog.NewTextHandler(multiWriter, nil)), bufferCloseFunc, nil
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			logger.Info(fmt.Sprintf("Served request: %s %s", r.Method, r.URL.Path))
		})
	}
}
