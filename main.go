package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/build"
	"boot.dev/linko/internal/store"
	"github.com/joho/godotenv"
)

const (
	listenPort = 8899
)

type multiError interface {
	error
	Unwrap() []error
}

func deferWrapper(deferredFunc closeFunc) {
	if err := deferredFunc(); err != nil {
		fmt.Fprintf(os.Stderr, "error closing logger: %v", err)
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "error loading env variables: %v", err)
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", listenPort, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	logger, closeLogger, err := initializeLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	}
	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
	)
	defer deferWrapper(closeLogger)
	st, err := store.New(logger, dataDir)
	if err != nil {
		logger.Error("failed to create store", slog.Any("error", err))
		return 1
	}
	s := newServer(logger, *st, httpPort, cancel)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", slog.Any("error", err))
		return 1
	}
	if serverErr != nil {
		logger.Error("server error", slog.Any("error", serverErr))
		return 1
	}
	return 0
}
