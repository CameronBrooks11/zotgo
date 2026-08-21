// Command zot is the zotgo CLI: a zero-dependency binary that drives a running
// Zotero 7+ through its HTTP contracts.
//
// Read commands query the Local API (or the Web API under --web); the item,
// collection, and tag write commands go through Zotero's local write API on a
// build that supports it. `zot doctor` reports what the endpoint can do.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

// resolveVersion reports the build's version. Release archives stamp it via
// ldflags; a `go install …@vX.Y.Z` build does not, but the Go toolchain records
// the module version in the binary, so fall back to that rather than reporting
// "dev". A plain in-module `go build`/`go run` has module version "(devel)",
// which we leave as "dev".
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

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
		Name:    "zot",
		Usage:   "a CLI for a running Zotero 7+, over its HTTP API",
		Version: resolveVersion(),
		Description: "Start with `zot doctor` to check the endpoint and its capabilities.\n\n" +
			"Find items with `zot search` or `zot list`, inspect an item with `zot show` or " +
			"the item-detail commands, browse organization with `zot collections`, and export " +
			"with `zot export`. Local writes live under `zot item`, `zot collection`, and `zot tag`; " +
			"`zot grant` authorizes non-interactive writes.\n\n" +
			"Run `zot <command> --help` for authoritative syntax, options, and limitations.",
		EnableShellCompletion: true,
		Suggest:               true,
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
		Commands: discoveryCommands(),
	}
}

// discoveryCommands assigns every top-level command to a task-oriented help
// category and enables typo suggestions throughout the command tree.
func discoveryCommands() []*cli.Command {
	groups := []struct {
		category string
		commands []*cli.Command
	}{
		{"Get started", []*cli.Command{doctorCommand()}},
		{"Find and inspect", []*cli.Command{listCommand(), showCommand(), searchCommand(), collectionsCommand(), statsCommand()}},
		{"Item details", []*cli.Command{attachmentCommand(), noteCommand(), relationCommand(), annotationCommand()}},
		{"Export", []*cli.Command{exportCommand()}},
		{"Organize collections", []*cli.Command{collectionCommand()}},
		{"Modify (local only)", []*cli.Command{itemCommand(), tagCommand()}},
		{"Authorize automation", []*cli.Command{grantCommand()}},
	}
	commands := make([]*cli.Command, 0, 15)
	for _, group := range groups {
		for _, command := range group.commands {
			command.Category = group.category
			enableSuggestions(command)
			commands = append(commands, command)
		}
	}
	return commands
}

func enableSuggestions(command *cli.Command) {
	command.Suggest = true
	for _, child := range command.Commands {
		enableSuggestions(child)
	}
}

// subcommandRequired is the Action for a noun command that only groups
// subcommands. With an unrecognized argument it names the likely subcommand
// instead of cli's bare "No help topic for 'X'" (e.g. `zot attachment KEY` when
// the user meant `zot attachment show KEY`); with no argument it shows help.
func subcommandRequired(suggest string) cli.ActionFunc {
	return func(_ context.Context, cmd *cli.Command) error {
		if arg := cmd.Args().First(); arg != "" {
			return fmt.Errorf("unknown %s subcommand %q; did you mean %q? (see `zot %s --help`)", cmd.Name, arg, suggest, cmd.Name)
		}
		return cli.ShowSubcommandHelp(cmd)
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
	var c *zotero.Client
	if cmd.Bool("web") {
		key := os.Getenv("ZOTGO_API_KEY")
		if key == "" {
			return nil, errors.New("--web needs a Zotero API key; set ZOTGO_API_KEY (create one at https://www.zotero.org/settings/keys)")
		}
		c = zotero.NewWeb(cmd.String("url"), key)
	} else {
		c = zotero.New(cmd.String("url"))
	}
	// The client denies writes until a command establishes authority
	// (ensureWriteAuthority: allow-all for an interactive human-confirmed write, a
	// write lease for a non-interactive one). Leaving the default in place means a
	// write path that never establishes authority fails closed. Reads ignore the
	// authorizer entirely.
	return c, nil
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
	case errors.Is(err, zotero.ErrTransport):
		return errors.New("lost the connection to Zotero mid-request — is it still running? (try `zot doctor`)")
	}
	// Rate limiting only ever comes from the Web API; the local API does not 429.
	var status zotero.StatusError
	if errors.As(err, &status) && (status.StatusCode == 429 || status.StatusCode == 503) {
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return errors.New("Zotero is rate-limiting the request — wait a moment and retry")
	}
	return err
}
