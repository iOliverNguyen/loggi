package main

import (
	"fmt"
	"os"

	"github.com/iOliverNguyen/loggi/internal/app"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "loggi [files...]",
		Short: "loggi — local-first log viewer",
		Long: `loggi runs a local server and serves a web UI for viewing JSON
and ANSI-colored logs from files, stdin, or docker. Multiple clients can
attach to the same server; the server is auto-started on demand.

Quick start:
  loggi                                  — open the web UI
  loggi <file> ...                       — tail one or more files
  tail -f app.log | loggi                — pipe stdin in (stdin is optional)
  cat a.log | loggi --name 'Log A'       — name the piped source
  cat a.log | loggi b.log                — both: tail b.log AND ingest stdin`,
		Args: cobra.ArbitraryArgs,
		RunE: app.RunDefault,
	}
	root.Flags().String("name", "stdin", "source name when piping stdin")
	root.Flags().Bool("no-open", false, "don't open the browser")

	root.AddCommand(app.NewServerCmd())
	root.AddCommand(app.NewTailCmd())
	root.AddCommand(app.NewStdinCmd())
	root.AddCommand(app.NewDockerCmd())
	root.AddCommand(app.NewWebCmd())
	root.AddCommand(app.NewInitCmd())
	root.AddCommand(app.NewProfileCmd())
	root.AddCommand(app.NewConfigCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
