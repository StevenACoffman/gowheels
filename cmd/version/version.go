// Package version implements the "version" CLI command.
package version

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd/root"
)

// Config holds the configuration for the version command.
type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the version command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("version").SetParent(parent.Flags)
	cfg.Command = &ff.Command{
		Name:      "version",
		Usage:     "gowheels version",
		ShortHelp: "print version information",
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Fprintln(cfg.Stdout, "gowheels (version unknown)")
		return nil
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		v = "devel"
	}
	fmt.Fprintf(cfg.Stdout, "gowheels %s (%s)\n", v, info.GoVersion)
	return nil
}
