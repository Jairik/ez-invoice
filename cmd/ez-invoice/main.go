// Command ez-invoice starts the local time tracking and invoice application.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/cli"
	"github.com/Jairik/ez-invoice/internal/tui"
)

// run bootstraps the app and routes TUI or helper commands.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("ez-invoice", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", "", "override the application data directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	application, err := app.Open(*dataDir)
	if err != nil {
		return err
	}
	defer application.Close()

	command := flags.Args()
	if len(command) == 0 || command[0] == "tui" {
		return tui.Run(application)
	}
	return cli.Run(context.Background(), application, command, stdout, stderr)
}

// main reports command failures as non-zero process exits.
func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "ez-invoice:", err)
		os.Exit(1)
	}
}
