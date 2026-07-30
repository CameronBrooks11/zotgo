package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func itemCommand() *cli.Command {
	return &cli.Command{
		Name:  "item",
		Usage: "create and modify items (local endpoint only)",
		Commands: []*cli.Command{
			itemCreateCommand(),
		},
	}
}

func itemCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "create items from JSON (a single object or an array) on stdin or --file",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "read item JSON from this file instead of stdin"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be created without writing"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt"},
		},
		Action: itemCreateAction,
	}
}

func itemCreateAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("web") {
		return errors.New("writes are local-only; the --web profile is read-only")
	}

	file := cmd.String("file")
	fromStdin := file == ""
	raw, err := readItemInput(file)
	if err != nil {
		return err
	}
	items, err := parseItemsInput(raw)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("no items to create")
	}
	if len(items) > zotero.MaxWriteObjects {
		return fmt.Errorf("%d items exceeds the %d-item batch limit", len(items), zotero.MaxWriteObjects)
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	if k := loadLocalKey(); k != "" {
		c.SetLocalKey(k)
	}

	w := out(cmd)
	fmt.Fprintf(w, "Target: %s — %s\n", lib.Name, c.BaseURL())
	printItemSummary(w, items)

	if cmd.Bool("dry-run") {
		fmt.Fprintln(w, "\nDry run — nothing was written.")
		return nil
	}

	if !cmd.Bool("yes") {
		if fromStdin {
			return errors.New("item JSON was read from stdin, so I cannot prompt — re-run with --yes (or --dry-run), or use --file")
		}
		if !confirm(os.Stdin, w, fmt.Sprintf("Create %d item(s) in %s?", len(items), lib.Name)) {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	if err := ensureLocalKey(ctx, c); err != nil {
		return err
	}

	res, err := c.CreateItems(ctx, lib, items)
	if err != nil {
		return writeFriendly(err)
	}
	reportWriteResult(w, res)
	if !res.Ok() {
		return cli.Exit("", 1)
	}
	return nil
}

// readItemInput reads the item JSON from a file, or from stdin when file is "".
func readItemInput(file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	return io.ReadAll(os.Stdin)
}

// parseItemsInput accepts either a single item object or an array of them and
// normalizes to a slice of item objects.
func parseItemsInput(raw []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("no item JSON provided")
	}
	if trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, fmt.Errorf("invalid item JSON array: %w", err)
		}
		return arr, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("invalid item JSON: %w", err)
	}
	return []json.RawMessage{json.RawMessage(trimmed)}, nil
}

// printItemSummary lists what will be created, so the target is never a mystery.
func printItemSummary(w io.Writer, items []json.RawMessage) {
	for _, it := range items {
		var m struct {
			ItemType string `json:"itemType"`
			Title    string `json:"title"`
		}
		_ = json.Unmarshal(it, &m)
		fmt.Fprintf(w, "  + %s: %s\n", orDash(m.ItemType), orDash(m.Title))
	}
}

func reportWriteResult(w io.Writer, res zotero.WriteResult) {
	for _, i := range sortedIndices(res.Successful) {
		fmt.Fprintf(w, "created %s\n", res.Successful[i].Key)
	}
	for _, i := range sortedIndices(res.Unchanged) {
		fmt.Fprintf(w, "unchanged %s\n", res.Unchanged[i])
	}
	for _, i := range sortedIndices(res.Failed) {
		f := res.Failed[i]
		fmt.Fprintf(w, "failed [%s] %s: %s (code %d)\n", i, f.Key, f.Message, f.Code)
	}
}

// sortedIndices returns the map's string-integer keys in numeric order, so the
// report follows request order rather than Go's random map iteration.
func sortedIndices[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		return a < b
	})
	return keys
}

// ensureLocalKey makes sure the client can write: it reuses a persisted key, or
// prompts the user in Zotero and persists the key if they chose to remember it.
func ensureLocalKey(ctx context.Context, c *zotero.Client) error {
	if c.HasLocalKey() {
		return nil
	}
	fmt.Fprintln(os.Stderr, "Authorizing with Zotero — approve the prompt in the app…")
	remember, err := c.Authorize(ctx, "zotgo")
	if err != nil {
		return writeFriendly(err)
	}
	if remember {
		if err := saveLocalKey(c.LocalKey()); err != nil {
			fmt.Fprintf(os.Stderr, "zot: warning: could not save the local key: %v\n", err)
		}
	}
	return nil
}

// confirm asks a yes/no question, defaulting to no.
func confirm(in io.Reader, w io.Writer, question string) bool {
	fmt.Fprintf(w, "%s [y/N] ", question)
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// writeFriendly turns the write-path sentinels into actionable messages.
func writeFriendly(err error) error {
	switch {
	case errors.Is(err, zotero.ErrAuthorizeDenied):
		return errors.New("authorization denied in Zotero")
	case errors.Is(err, zotero.ErrWriteUnauthorized):
		_ = clearLocalKey() // a stale/consumed key is worse than none
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return errors.New("Zotero rejected the local key (it may have expired) — re-run to re-authorize")
	case errors.Is(err, zotero.ErrPreconditionRequired):
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return errors.New("Zotero requires a write precondition zotgo did not supply (this is a bug)")
	case errors.Is(err, zotero.ErrPreconditionFailed):
		return errors.New("the library changed since this write was prepared — re-run")
	}
	return friendly(err)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
