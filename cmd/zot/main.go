// Command zot is the zotgo CLI: a zero-dependency binary that drives a running
// Zotero 7+ through its HTTP contracts.
//
// M0 ships a single command, `zot doctor`, which reports whether zotgo can
// reach Zotero and whether its Local API is enabled. Read and write commands
// arrive in later milestones (see working/plan.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "doctor":
		return cmdDoctor(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "zot %s\n", version)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "zot: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `zot — a CLI for a running Zotero 7+, over its HTTP API.

Usage:
  zot <command> [flags]

Commands:
  doctor    Check that Zotero is running and its Local API is enabled
  version   Print the zot version
  help      Show this help

Run "zot <command> -h" for command flags.
`)
}

func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseURL := fs.String("url", zotero.DefaultBaseURL, "Zotero HTTP server base URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	h := zotero.New(*baseURL).CheckHealth(context.Background())
	renderHealth(stdout, h)
	if h.Ready() {
		return 0
	}
	return 1
}
