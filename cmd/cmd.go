// Package cmd is the dispatcher for the gowheels CLI. It registers all
// subcommands and routes incoming arguments to the matching implementation.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"

	"github.com/StevenACoffman/gowheels/cmd/man"
	"github.com/StevenACoffman/gowheels/cmd/npm"
	"github.com/StevenACoffman/gowheels/cmd/pypi"
	"github.com/StevenACoffman/gowheels/cmd/root"
	"github.com/StevenACoffman/gowheels/cmd/version"
)

// Run parses args and dispatches to the matching command.
// args must not include the executable name (pass os.Args[1:]).
//
// Every flag can be set via a GOWHEELS_-prefixed environment variable.
// The mapping rule is: prepend GOWHEELS_, uppercase, replace dashes with
// underscores.  Commonly used:
//
//	GOWHEELS_PYPI_TOKEN    sets --pypi-token
//	GOWHEELS_GITHUB_TOKEN  sets --github-token
//	GOWHEELS_PYPI_URL      sets --pypi-url
//	GOWHEELS_DRY_RUN       sets --dry-run
//	GOWHEELS_DEBUG         sets --debug
//
// Flags supplied on the command line always take precedence over env vars.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	r := root.New(stdin, stdout, stderr)
	version.New(r)
	pypi.New(r)
	npm.New(r)
	man.New(r)
	// register new commands here

	if err := r.Command.Parse(args, ff.WithEnvVarPrefix("GOWHEELS")); err != nil {
		_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command))
		return fmt.Errorf("parse: %w", err)
	}

	if err := r.Command.Run(ctx); err != nil {
		// Don't print usage help for ErrNoExec (no subcommand given) or
		// ExitError (command already reported its own outcome).
		var exitErr root.ExitError
		if !errors.Is(err, ff.ErrNoExec) && !errors.As(err, &exitErr) {
			_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		}
		return err
	}

	return nil
}
