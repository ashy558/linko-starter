package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

const (
	listenPort = 8899
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", listenPort, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	stdLogger := log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
	currentDirPath, err := os.Getwd()
	if err != nil {
		stdLogger.Printf("failed to fetch current directory path: %v", err)
		return 1
	}
	accessLogPath := filepath.Join(currentDirPath, "linko.access.log")
	accessLogFile, err := os.Create(accessLogPath)
	if err != nil {
		stdLogger.Printf("failed to create access log file: %v", err)
		return 1
	}
	defer accessLogFile.Close()
	accessLogger := log.New(accessLogFile, "INFO: ", log.LstdFlags)
	st, err := store.New(stdLogger, dataDir)
	if err != nil {
		stdLogger.Printf("failed to create store: %v", err)
		return 1
	}
	s := newServer(accessLogger, *st, httpPort, cancel)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		stdLogger.Printf("failed to shutdown server: %v", err)
		return 1
	}
	if serverErr != nil {
		stdLogger.Printf("server error: %v", serverErr)
		return 1
	}
	stdLogger.Print("Linko is shutting down")
	return 0
}
