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
		// Reject a contradictory output mode before any command touches the
		// network, so the user sees the flag mistake rather than whatever the
		// request happened to fail with.
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			_, err := outputMode(cmd)
			return ctx, err
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "url",
				Usage:   "API base URL (default: local Zotero; api.zotero.org with --web)",
				Sources: cli.EnvVars("ZOTGO_BASE_URL"),
			},
			&cli.BoolFlag{
				Name:  "web",
				Usage: "use the Zotero Web API; needs an API key in ZOTGO_API_KEY",
			},
			&cli.StringFlag{
				Name:    "library",
				Aliases: []string{"L"},
				Usage:   "library selector: 'me', a group name, or a group id",
				Sources: cli.EnvVars("ZOTGO_LIBRARY"),
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit one versioned JSON document of zotgo DTOs",
			},
			&cli.BoolFlag{
				Name:  "jsonl",
				Usage: "emit one self-describing JSON document per line",
			},
			&cli.BoolFlag{
				Name:  "raw",
				Usage: "emit Zotero's own API response, unshaped and unversioned",
			},
		},
		Commands: []*cli.Command{
			doctorCommand(),
			listCommand(),
			showCommand(),
			searchCommand(),
			collectionsCommand(),
			statsCommand(),
			exportCommand(),
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

// newClient builds a client for the endpoint the flags select: the local Zotero
// by default, or the Web API under --web. The Web API key is read from the
// environment, never a flag, so it cannot leak into `ps` output or shell history.
func newClient(cmd *cli.Command) (*zotero.Client, error) {
	if cmd.Bool("web") {
		key := os.Getenv("ZOTGO_API_KEY")
		if key == "" {
			return nil, errors.New("--web needs a Zotero API key; set ZOTGO_API_KEY (create one at https://www.zotero.org/settings/keys)")
		}
		return zotero.NewWeb(cmd.String("url"), key), nil
	}
	return zotero.New(cmd.String("url")), nil
}

// resolveLibrary builds a client for the selected endpoint and resolves
// --library to a route on it.
func resolveLibrary(ctx context.Context, cmd *cli.Command) (*zotero.Client, zotero.LibraryRef, error) {
	c, err := newClient(cmd)
	if err != nil {
		return nil, zotero.LibraryRef{}, err
	}
	lib, err := c.ResolveLibrary(ctx, cmd.String("library"))
	if err != nil {
		return nil, zotero.LibraryRef{}, friendly(err)
	}
	return c, lib, nil
}

// friendly turns the client's connectivity sentinels into actionable CLI
// messages.
func friendly(err error) error {
	switch {
	case errors.Is(err, zotero.ErrZoteroDown):
		return errors.New("cannot reach Zotero — is the desktop app running? (try `zot doctor`)")
	case errors.Is(err, zotero.ErrLocalAPIDisabled):
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return errors.New("Zotero's Local API is disabled — run `zot doctor` for setup steps")
	case errors.Is(err, zotero.ErrInvalidAPIKey):
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return errors.New("Zotero rejected the API key — check ZOTGO_API_KEY (run `zot doctor --web`)")
	}
	return err
}
