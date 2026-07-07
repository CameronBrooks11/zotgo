// Command zot is the zotgo CLI: a zero-dependency binary that drives a running
// Zotero 7+ through its HTTP contracts.
//
// Read commands query Zotero's Local API; `zot doctor` checks connectivity.
// Write commands arrive in later milestones (see working/plan.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	err := rootCommand().Run(context.Background(), os.Args)
	if err == nil {
		return
	}
	var coder cli.ExitCoder
	if errors.As(err, &coder) {
		if msg := coder.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, "zot: "+msg)
		}
		os.Exit(coder.ExitCode())
	}
	fmt.Fprintln(os.Stderr, "zot: "+err.Error())
	os.Exit(1)
}

func rootCommand() *cli.Command {
	return &cli.Command{
		Name:                  "zot",
		Usage:                 "a CLI for a running Zotero 7+, over its HTTP API",
		Version:               version,
		EnableShellCompletion: true,
		// main() owns error printing and exit codes; keep urfave from also
		// printing or calling os.Exit.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "url",
				Usage:   "Zotero HTTP server base URL",
				Value:   zotero.DefaultBaseURL,
				Sources: cli.EnvVars("ZOTGO_BASE_URL"),
			},
			&cli.StringFlag{
				Name:    "library",
				Aliases: []string{"L"},
				Usage:   "library selector: 'me', a group name, or a group id",
				Sources: cli.EnvVars("ZOTGO_LIBRARY"),
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "output JSON instead of a table",
			},
		},
		Commands: []*cli.Command{
			doctorCommand(),
			listCommand(),
			showCommand(),
			searchCommand(),
			collectionsCommand(),
			statsCommand(),
		},
	}
}

// out returns the writer commands should print results to (os.Stdout by
// default; overridable in tests via the root command's Writer).
func out(cmd *cli.Command) io.Writer {
	if w := cmd.Root().Writer; w != nil {
		return w
	}
	return os.Stdout
}

// resolveLibrary builds a client from --url and resolves --library to a route.
func resolveLibrary(ctx context.Context, cmd *cli.Command) (*zotero.Client, zotero.LibraryRef, error) {
	c := zotero.New(cmd.String("url"))
	lib, err := c.ResolveLibrary(ctx, cmd.String("library"))
	if err != nil {
		return nil, zotero.LibraryRef{}, friendly(err)
	}
	return c, lib, nil
}

// friendly turns the SDK's connectivity sentinels into actionable CLI messages.
func friendly(err error) error {
	switch {
	case errors.Is(err, zotero.ErrZoteroDown):
		return errors.New("cannot reach Zotero — is the desktop app running? (try `zot doctor`)")
	case errors.Is(err, zotero.ErrLocalAPIDisabled):
		return errors.New("Zotero's Local API is disabled — run `zot doctor` for setup steps")
	}
	return err
}
