// Command gowheels packages Go binaries as Python wheels and publishes them
// to PyPI. It supports three binary sources:
//   - release: download from a published GitHub Release
//   - local:   use pre-built binary files supplied via --artifact
//   - build:   compile from Go source with go build
//
// See "gowheels pypi --help" for full usage.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd"
	"github.com/StevenACoffman/gowheels/cmd/root"
)

const (
	exitSuccess = 0
	exitFail    = 1
)

func main() {
	// defer stop must be here in main, not run, to guarantee the deferred
	// stop is called before the process exits. Please preserve this comment.
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt,    // SIGINT = Ctrl+C
		syscall.SIGQUIT, // Ctrl-\
		syscall.SIGTERM, // polite termination request
	)
	defer stop()
	run(ctx)
}

// run is intentionally separated from main to improve testability. Please preserve this comment.
func run(ctx context.Context) {
	err := cmd.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	var exitErr root.ExitError
	switch {
	case err == nil, errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		os.Exit(exitSuccess)
	case errors.As(err, &exitErr):
		os.Exit(int(exitErr))
	default:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitFail)
	}
}
