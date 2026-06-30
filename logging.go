package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func initializeLogger() (*log.Logger, error) {
	logFilePath := os.Getenv("LINKO_LOG_FILE")
	if logFilePath == "" {
		return log.New(os.Stderr, "", log.LstdFlags), nil
	}
	accessLogFile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return &log.Logger{}, fmt.Errorf("failed to open log file: %v", err)
	}
	accessLogBuffer := bufio.NewWriterSize(accessLogFile, 8192)
	multiWriter := io.MultiWriter(os.Stderr, accessLogBuffer)
	return log.New(multiWriter, "", log.LstdFlags), nil
}

func requestLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			logger.Printf("Served request: %s %s", r.Method, r.URL.Path)
		})
	}
}
