package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/igolaizola/bulkai/pkg/gui"
	"github.com/peterbourgon/ff/v3"
	"github.com/peterbourgon/ff/v3/ffcli"
)

var Version = "dev"

func main() {
	// Create signal based context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Launch command
	cmd := newCommand()
	if err := cmd.ParseAndRun(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func newCommand() *ffcli.Command {
	fs := flag.NewFlagSet("bulkaigui", flag.ExitOnError)

	return &ffcli.Command{
		ShortUsage: "bulkaigui [flags] <key> <value data...>",
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ff.PlainParser),
			ff.WithEnvVarPrefix("BULKAI"),
		},
		ShortHelp: "launch gui",
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return gui.Run(ctx, Version)
		},
	}
}
