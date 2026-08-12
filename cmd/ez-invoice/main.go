// Command ez-invoice starts the local time tracking and invoice application.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/cli"
	"github.com/Jairik/ez-invoice/internal/tui"
	"github.com/Jairik/ez-invoice/internal/web"
)

// run bootstraps the app and routes TUI, web, or helper commands.
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
	if command[0] == "serve" || command[0] == "web" {
		addr := normalizeAddr("")
		if len(command) > 1 {
			addr = normalizeAddr(command[1])
		}
		fmt.Fprintf(stderr, "ez-invoice web interface running at http://%s\n", addr)
		return web.Serve(application, addr)
	}
	return cli.Run(context.Background(), application, command, stdout, stderr)
}

// normalizeAddr accepts a bare port and returns a loopback listen address.
func normalizeAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:9090"
	}
	if !strings.Contains(addr, ":") {
		return "127.0.0.1:" + addr
	}
	return addr
}

// main reports command failures as non-zero process exits.
func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "ez-invoice:", err)
		os.Exit(1)
	}
}
