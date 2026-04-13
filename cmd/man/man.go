// Package man implements the "man" CLI command.
package man

import (
	"context"
	"fmt"

	mff "github.com/StevenACoffman/mango-ff"
	"github.com/muesli/roff"
	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd/root"
)

// Config holds the configuration for the man command.
type Config struct {
	*root.Config
	Section int
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the man command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("man").SetParent(parent.Flags)
	cfg.Flags.IntVar(&cfg.Section, 0, "section", 1, "man page section number (1–8)")
	cfg.Command = &ff.Command{
		Name:      "man",
		Usage:     "gowheels man [--section N]",
		ShortHelp: "print the man page to stdout",
		LongHelp:  "Print the man page for gowheels in roff format to stdout.\n\nPipe to the man pager:  gowheels man | man -l -",
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	manPage, err := mff.NewManPage(uint(cfg.Section), cfg.Config.Command)
	if err != nil {
		return fmt.Errorf("man: %w", err)
	}
	_, _ = fmt.Fprint(cfg.Stdout, manPage.Build(roff.NewDocument()))
	return nil
}
