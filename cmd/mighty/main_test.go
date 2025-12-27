package main

import (
	"flag"
	"go-mighty/internal/api/router"
	"net/http"
	"os"
	"testing"

	"github.com/rs/zerolog"
)

// resetFlags resets the default flag set so tests don't conflict
func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
}

func TestSetupLogger_DefaultLevelIsInfo(t *testing.T) {
	resetFlags()

	// Simulate running without --debug
	os.Args = []string{"cmd"}

	setupLogger()

	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Fatalf("expected log level INFO, got %v", zerolog.GlobalLevel())
	}
}

func TestSetupLogger_DebugFlagSetsDebugLevel(t *testing.T) {
	resetFlags()

	// Simulate running with --debug
	os.Args = []string{"cmd", "--debug"}

	setupLogger()

	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Fatalf("expected log level DEBUG, got %v", zerolog.GlobalLevel())
	}
}

func TestRouterRoute_ReturnsHandler(t *testing.T) {
	r := router.Route()

	if r == nil {
		t.Fatal("expected router.Route() to return a handler, got nil")
	}

	// Compile-time interface check
	var _ http.Handler = r
}
