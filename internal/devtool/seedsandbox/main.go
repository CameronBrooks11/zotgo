// Command seed-sandbox builds a throwaway Zotero library to develop and test
// against, so the live suite never needs to be pointed at anyone's real one.
//
// Run it with `just seed-sandbox`. It creates an isolated HOME with its own
// profile, data directory and port, starts Zotero there, seeds a known corpus,
// and leaves it running with the base URL to export.
//
// Authorization has two paths, and the default is the honest one:
//
//   - By default the tool asks Zotero for a key through POST /api/local/authorize,
//     exactly as any other client would, and a human approves the modal once.
//   - With --unattended it writes a remembered key into the profile it just
//     created, because CI cannot click a modal and the version matrix is
//     impossible without it. That is the one place this reaches into Zotero's own
//     state; see registerKey for why it is drawn where it is.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed-sandbox: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		dir        = flag.String("dir", defaultDir(), "sandbox directory")
		port       = flag.Int("port", 23129, "port for the sandbox's Local API")
		wipe       = flag.Bool("wipe", false, "delete the sandbox and rebuild it from nothing")
		unattended = flag.Bool("unattended", false, "pre-register an API key instead of prompting for Zotero's modal (for CI)")
		keep       = flag.Bool("keep-running", true, "leave Zotero running after seeding")
	)
	flag.Parse()

	binary, err := findZotero()
	if err != nil {
		return err
	}
	box := newSandbox(*dir, *port, binary)

	if *wipe {
		fmt.Printf("Wiping %s\n", box.root)
		if err := box.wipe(); err != nil {
			return err
		}
	}

	// Reuse a key this sandbox already has. Zotero caches the key file at startup,
	// so replacing it under a running instance yields a key the server does not
	// know, and the failure surfaces later as an unauthorized write.
	key := box.existingKey()
	if *unattended && key == "" {
		if key, err = randomKey(); err != nil {
			return err
		}
	}
	if err := box.create(key); err != nil {
		return err
	}

	fmt.Printf("Sandbox:  %s\n", box.root)
	fmt.Printf("Library:  %s\n", box.dataDir)
	fmt.Printf("Zotero:   %s\n", binary)
	fmt.Printf("Endpoint: %s\n\n", box.baseURL())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	logPath := filepath.Join(box.root, "zotero.log")
	var process *exec.Cmd
	if box.alreadyServing() {
		fmt.Println("A sandbox Zotero is already serving this port; reusing it.")
	} else {
		if process, err = box.launch(logPath); err != nil {
			return err
		}
		defer func() {
			if !*keep && process.Process != nil {
				_ = process.Process.Signal(syscall.SIGTERM)
			}
		}()
		fmt.Print("Waiting for Zotero… ")
		if err := waitForZotero(ctx, box.baseURL()); err != nil {
			return fmt.Errorf("%w (see %s)", err, logPath)
		}
		fmt.Println("up")
	}

	// Assert the isolation held before writing anything. The data directory is a
	// request until Zotero honours it, and the one time this went wrong the only
	// warning was a library open on screen.
	if err := assertIsolated(ctx, box); err != nil {
		return err
	}

	c := zotero.New(box.baseURL())
	c.SetWriteAuthorizer(zotero.AllowAllWrites())

	health := c.CheckHealth(ctx)
	writable := health.Supports(zotero.CapabilityWrite)
	fmt.Printf("Zotero %s, write API: %v\n", health.ZoteroVersion, writable)

	if writable {
		if key != "" {
			c.SetLocalKey(key)
		} else {
			fmt.Println("\nApprove the authorization prompt in Zotero (choose \"Always Allow\").")
			if _, err := c.Authorize(ctx, "zotgo seed-sandbox"); err != nil {
				return fmt.Errorf("authorize: %w", err)
			}
		}
	}

	fmt.Println("\nSeeding…")
	r, err := seed(ctx, c, zotero.UserLibrary(), box.baseURL(), writable)
	if err != nil {
		return err
	}

	fmt.Printf("\n  items:       %d\n", r.items)
	fmt.Printf("  collections: %d\n", r.collections)
	fmt.Printf("  attachments: %d\n", r.attachments)
	fmt.Printf("  annotations: %d\n", r.annotations)
	for _, skipped := range r.skipped {
		fmt.Printf("  skipped:     %s\n", skipped)
	}

	fmt.Printf("\nRun the live suite against it:\n\n    ZOTGO_BASE_URL=%s", box.baseURL())
	if key != "" {
		// A throwaway credential for a throwaway library, printed because the write
		// tests would otherwise block on a modal nobody is there to click. It
		// authorizes nothing outside this sandbox.
		fmt.Printf(" \\\n    ZOTGO_LOCAL_KEY=%s", key)
	}
	fmt.Printf(" \\\n    just test-live\n")
	if *keep && process != nil {
		fmt.Printf("\nZotero is still running (pid %d). Stop it when you are done.\n", process.Process.Pid)
	}
	return nil
}

func defaultDir() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "zotgo-sandbox")
	}
	return filepath.Join(cache, "zotgo-sandbox")
}

func waitForZotero(ctx context.Context, baseURL string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/connector/ping", nil)
		if err != nil {
			return err
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	//lint:ignore ST1005 "Zotero" is a proper noun.
	return errors.New("Zotero did not answer on the sandbox port within 90s")
}

// assertIsolated confirms the running Zotero opened the sandbox's data directory
// and not some other library. It resolves an attachment's path when there is one;
// on an empty corpus there is nothing to resolve yet, and the check is deferred
// rather than faked.
func assertIsolated(ctx context.Context, box *sandbox) error {
	c := zotero.New(box.baseURL())
	attachments, err := c.AllItems(ctx, zotero.UserLibrary(), zotero.ItemsOptions{ItemType: "attachment", Limit: 1})
	if err != nil || len(attachments) == 0 {
		return nil
	}
	path, err := c.AttachmentLocalPath(ctx, zotero.UserLibrary(), attachments[0].Key)
	if err != nil {
		return nil
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	root, err := filepath.Abs(box.dataDir)
	if err != nil {
		return nil
	}
	if !isWithin(resolved, root) {
		return fmt.Errorf("ABORT: Zotero is serving %s, which is outside the sandbox at %s", resolved, root)
	}
	return nil
}

func isWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && !hasDotDotPrefix(rel)
}

func hasDotDotPrefix(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}
