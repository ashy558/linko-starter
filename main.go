package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
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
	st, err := store.New(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create store: %v\n", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel)
	var serverErr error
	go func() {
		serverErr = s.start()
		fmt.Printf("Linko is running on http://localhost:%d\n", listenPort)
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to shutdown server: %v\n", err)
		return 1
	}
	if serverErr != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", serverErr)
		return 1
	}
	fmt.Print("Linko is shutting down\n")
	return 0
}
