package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
	cli "github.com/urfave/cli/v3"
)

func grantCommand() *cli.Command {
	return &cli.Command{
		Name:  "grant",
		Usage: "grant a time-boxed, scoped write lease so non-interactive writes can proceed (humans only)",
		Description: "Mint a write lease: a human approves Zotero's authorize modal and a scope, and\n" +
			"non-interactive writes (`--yes`/machine-mode commands — a script, CI job, or agent)\n" +
			"are then allowed only within it until it expires. Interactive writes you confirm\n" +
			"yourself never need a lease. See docs/design/write-authority.md.",
		Flags: []cli.Flag{
			&cli.DurationFlag{Name: "ttl", Value: DefaultLeaseTTL, Usage: "how long the lease is valid (max 24h)"},
			&cli.StringSliceFlag{Name: "operations", Aliases: []string{"o"}, Usage: "operations to allow (default: all non-destructive writes)"},
			&cli.StringFlag{Name: "note", Usage: "a note recorded in the lease and its audit log"},
		},
		Action: grantAction,
		Commands: []*cli.Command{
			grantStatusCommand(),
			grantRevokeCommand(),
		},
	}
}

func grantAction(ctx context.Context, cmd *cli.Command) error {
	// Minting is deliberately the inverse of every other write command: it demands
	// an interactive human. An agent cannot approve Zotero's modal, and --yes does
	// not apply here.
	if !isTerminal(os.Stdin) {
		return errors.New("zot grant must run in an interactive terminal: a human approves Zotero's authorize modal and confirms the scope, so it cannot be granted non-interactively (--yes does not apply here)")
	}
	if cmd.Bool("web") {
		return errors.New("zot grant authorizes local writes; a Web API key is scoped and revoked at https://www.zotero.org/settings/keys")
	}
	ttl := cmd.Duration("ttl")
	if ttl <= 0 {
		return errors.New("--ttl must be positive")
	}
	if ttl > MaxLeaseTTL {
		return fmt.Errorf("--ttl %s exceeds the maximum %s", ttl, MaxLeaseTTL)
	}
	ops, err := resolveGrantOperations(cmd.StringSlice("operations"))
	if err != nil {
		return err
	}

	c, lib, err := resolveLibrary(ctx, cmd)
	if err != nil {
		return err
	}
	if err := c.RequireWriteCapability(ctx); err != nil {
		return writeFriendly(err)
	}

	w := out(cmd)
	expires := time.Now().Add(ttl)
	fmt.Fprintln(w, "About to grant a write lease:")
	fmt.Fprintf(w, "  library:    %s (%s)\n", lib.Name, libraryToken(lib))
	if _, page, err := c.Items(ctx, lib, zotero.ItemsOptions{Limit: 1}); err == nil {
		fmt.Fprintf(w, "  in scope:   %d item(s) — the whole library\n", page.TotalResults)
	}
	fmt.Fprintf(w, "  operations: %s\n", strings.Join(ops, ", "))
	fmt.Fprintf(w, "  expires:    %s (in %s)\n", expires.Format(time.RFC3339), ttl)
	if !confirm(os.Stdin, w, "Approve this scope and authorize in Zotero?") {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}

	fmt.Fprintln(os.Stderr, "Authorizing with Zotero — approve the prompt in the app…")
	remember, err := c.Authorize(ctx, "zotgo grant")
	if err != nil {
		return writeFriendly(err)
	}
	if !remember {
		fmt.Fprintln(os.Stderr, "zot: warning: Zotero granted a single-use key (you chose \"Allow\"); the lease will authorize only one write. Re-run and choose \"Always Allow\" for the full window.")
	}

	id, err := newLeaseID()
	if err != nil {
		return err
	}
	l := &lease{
		ID:      id,
		Created: time.Now(),
		Expires: expires,
		Scope: leaseScope{
			Libraries:  []string{libraryToken(lib)},
			Operations: ops,
		},
		WriteKey: c.LocalKey(),
		Note:     cmd.String("note"),
	}
	if err := saveLease(l); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nGranted lease %s — expires %s.\n", l.ID, l.Expires.Format(time.RFC3339))
	fmt.Fprintln(w, "Inspect it with 'zot grant status'; end it early with 'zot grant revoke'.")
	return nil
}

func grantStatusCommand() *cli.Command {
	return &cli.Command{
		Name:   "status",
		Usage:  "show the active write lease, its scope, and its audit summary",
		Action: grantStatusAction,
	}
}

func grantStatusAction(_ context.Context, cmd *cli.Command) error {
	w := out(cmd)
	l, err := loadActiveLease()
	if errors.Is(err, ErrNoActiveLease) {
		fmt.Fprintln(w, "No active write lease. Non-interactive writes are refused; run 'zot grant' to authorize.")
		return nil
	}
	if err != nil {
		return err
	}
	state := "active"
	if time.Now().After(l.Expires) {
		state = "EXPIRED"
	}
	fmt.Fprintf(w, "Lease %s (%s)\n", l.ID, state)
	fmt.Fprintf(w, "  created:    %s\n", l.Created.Format(time.RFC3339))
	fmt.Fprintf(w, "  expires:    %s\n", l.Expires.Format(time.RFC3339))
	fmt.Fprintf(w, "  libraries:  %s\n", strings.Join(l.Scope.Libraries, ", "))
	fmt.Fprintf(w, "  operations: %s\n", strings.Join(l.Scope.Operations, ", "))
	if l.Note != "" {
		fmt.Fprintf(w, "  note:       %s\n", l.Note)
	}
	allowed, refused := auditCounts(l.ID)
	if path, err := auditPathFor(l.ID); err == nil {
		fmt.Fprintf(w, "  audit:      %s\n", path)
	}
	// These count authorization decisions, not confirmed writes — an allowed write
	// can still fail Zotero's own preconditions afterwards.
	fmt.Fprintf(w, "              %d allowed, %d refused (authorization decisions)\n", allowed, refused)
	return nil
}

func grantRevokeCommand() *cli.Command {
	return &cli.Command{
		Name:   "revoke",
		Usage:  "end the active write lease immediately",
		Action: grantRevokeAction,
	}
}

func grantRevokeAction(_ context.Context, cmd *cli.Command) error {
	w := out(cmd)
	_, err := loadActiveLease()
	if errors.Is(err, ErrNoActiveLease) {
		fmt.Fprintln(w, "No active write lease to revoke.")
		return nil
	}
	if err := revokeLease(); err != nil {
		return err
	}
	fmt.Fprintln(w, "Revoked the active write lease; its bound key is gone with it.")
	fmt.Fprintln(w, "If you clicked \"Always Allow\" in Zotero, revoke that key in Zotero's settings too — the Local API has no remote revoke.")
	return nil
}

// auditCounts tallies allowed and refused decisions in a lease's audit log. A
// missing or unreadable log counts as zero, since the summary is best-effort.
func auditCounts(id string) (allowed, refused int) {
	path, err := auditPathFor(id)
	if err != nil {
		return 0, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var rec auditRecord
		if json.Unmarshal(scanner.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Decision {
		case "allowed":
			allowed++
		case "refused":
			refused++
		}
	}
	return allowed, refused
}

func newLeaseID() (string, error) {
	b := make([]byte, 6)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return "lease_" + hex.EncodeToString(b), nil
}
