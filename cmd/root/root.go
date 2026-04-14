// Package root defines the root configuration shared by all gowheels commands.
package root

import (
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
)

// ExitError is returned by commands that want a specific non-zero exit code
// without printing an additional error message. run() in main.go checks for
// ExitError with errors.As and calls os.Exit(int(e)) directly, bypassing the
// default "error: ..." printer.
type ExitError int

// Config holds injected I/O streams and the root ff.Command.
// All subcommand configs embed *Config to inherit I/O and shared flags.
type Config struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Flags   *ff.FlagSet
	Command *ff.Command

	// Shared flags — bound once here; all subcommands inherit via SetParent.
	DryRun bool
	Debug  bool
}

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// New returns a root Config with injected I/O and shared flags registered.
func New(stdin io.Reader, stdout, stderr io.Writer) *Config {
	var cfg Config
	cfg.Stdin = stdin
	cfg.Stdout = stdout
	cfg.Stderr = stderr

	cfg.Flags = ff.NewFlagSet("gowheels")
	cfg.Flags.BoolVar(
		&cfg.DryRun,
		0,
		"dry-run",
		"print what would happen without uploading or writing files",
	)
	cfg.Flags.BoolVar(&cfg.Debug, 0, "debug", "enable debug-level structured logging")

	cfg.Command = &ff.Command{
		Name:      "gowheels",
		Usage:     "gowheels <SUBCOMMAND> ...",
		ShortHelp: "package Go binaries as Python wheels and publish to PyPI",
		Flags:     cfg.Flags,
	}
	return &cfg
}
