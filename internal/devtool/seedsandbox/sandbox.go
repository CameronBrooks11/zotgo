package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// sandbox is a throwaway Zotero: its own HOME, its own profile, its own data
// directory, its own port.
//
// The HOME override is the part that matters. Zotero's data directory is chosen
// by `extensions.zotero.dataDir`, but that pref is inert unless
// `extensions.zotero.useDataDir` is also true — set only the first and Zotero
// silently ignores it, opens the default ~/Zotero, and rewrites the pref file to
// record that it did. A pref is a request; an overridden HOME is a boundary,
// because the fallback then lands inside the sandbox instead of in someone's
// research library.
type sandbox struct {
	root    string
	home    string
	profile string
	dataDir string
	port    int
	binary  string
}

func newSandbox(root string, port int, binary string) *sandbox {
	return &sandbox{
		root:    root,
		home:    root,
		profile: filepath.Join(root, ".zotero", "zotero", "sandbox"),
		dataDir: filepath.Join(root, "Zotero"),
		port:    port,
		binary:  binary,
	}
}

func (s *sandbox) baseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", s.port) }

// wipe removes the sandbox entirely. It refuses anything that does not look like
// a sandbox this tool made, so a mistyped --dir cannot delete a real directory.
func (s *sandbox) wipe() error {
	if s.root == "" || s.root == "/" || s.root == filepath.Dir(s.root) {
		return fmt.Errorf("refusing to wipe %q: not a sandbox path", s.root)
	}
	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		return nil
	}
	marker := filepath.Join(s.root, sandboxMarker)
	if _, err := os.Stat(marker); err != nil {
		return fmt.Errorf("refusing to wipe %q: no %s marker, so this was not created by seed-sandbox", s.root, sandboxMarker)
	}
	return os.RemoveAll(s.root)
}

// sandboxMarker is written at creation and checked before any wipe. Deleting a
// directory tree on a path that came from a flag deserves more than a name check.
const sandboxMarker = ".zotgo-sandbox"

// create lays out the profile and data directory. unattendedKey, when non-empty,
// is registered as a remembered local API key so the seed can write without a
// human approving Zotero's modal.
func (s *sandbox) create(unattendedKey string) error {
	for _, dir := range []string{s.profile, s.dataDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(s.root, sandboxMarker), []byte("created by zotgo seed-sandbox\n"), 0o600); err != nil {
		return err
	}

	prefs := fmt.Sprintf(`user_pref("extensions.zotero.useDataDir", true);
user_pref("extensions.zotero.dataDir", %q);
user_pref("extensions.zotero.httpServer.enabled", true);
user_pref("extensions.zotero.httpServer.port", %d);
user_pref("extensions.zotero.httpServer.localAPI.enabled", true);
user_pref("extensions.zotero.firstRun2", false);
user_pref("extensions.zotero.automaticScraperUpdates", false);
user_pref("app.update.enabled", false);
user_pref("app.update.auto", false);
`, s.dataDir, s.port)
	if err := os.WriteFile(filepath.Join(s.profile, "prefs.js"), []byte(prefs), 0o600); err != nil {
		return err
	}

	profilesINI := "[Profile0]\nName=sandbox\nIsRelative=1\nPath=sandbox\nDefault=1\n\n[General]\nStartWithLastProfile=1\nVersion=2\n"
	if err := os.WriteFile(filepath.Join(s.root, ".zotero", "zotero", "profiles.ini"), []byte(profilesINI), 0o600); err != nil {
		return err
	}

	if unattendedKey != "" {
		return s.registerKey(unattendedKey)
	}
	return nil
}

// existingKey returns a remembered key already registered in this sandbox, if
// there is one.
//
// Re-running must reuse it rather than generate another. Zotero reads the key
// file once and caches it, so writing a fresh key underneath a running instance
// produces a key the server has never heard of — and the failure surfaces much
// later, as an unauthorized write, with nothing pointing at the cause.
func (s *sandbox) existingKey() string {
	raw, err := os.ReadFile(filepath.Join(s.profile, "localAPIKeys.json"))
	if err != nil {
		return ""
	}
	var stored struct {
		Keys []struct {
			Key      string `json:"key"`
			Remember bool   `json:"remember"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return ""
	}
	for _, entry := range stored.Keys {
		if entry.Remember && entry.Key != "" {
			return entry.Key
		}
	}
	return ""
}

// alreadyServing reports whether something is already answering on the sandbox's
// port. Launching a second Zotero against a port the first one holds gives a
// process that exits quietly while the tool talks to the original — so the seed
// appears to work and the new instance's state is never used.
func (s *sandbox) alreadyServing() bool {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(s.baseURL() + "/connector/ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// registerKey writes a remembered local API key straight into the profile.
//
// This is the one place the tool reaches into Zotero's own state rather than
// going through its API, and it is deliberate and narrow: the file belongs to a
// profile this tool just created, it is written once at setup, it is never read
// back, and no database is touched. It exists because authorizing through the
// documented route requires a human to click Zotero's modal, which CI cannot do —
// so without it the version matrix (#110) is not possible at all.
//
// The attended path does not call this. Run without --unattended and the key
// comes from POST /api/local/authorize like any other client's would.
func (s *sandbox) registerKey(key string) error {
	type localAPIKey struct {
		Key       string `json:"key"`
		AppName   string `json:"appName"`
		Remember  bool   `json:"remember"`
		CreatedAt string `json:"createdAt"`
	}
	payload := struct {
		Keys []localAPIKey `json:"keys"`
	}{Keys: []localAPIKey{{
		Key:       key,
		AppName:   "zotgo seed-sandbox (unattended)",
		Remember:  true,
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	}}}

	encoded, err := json.MarshalIndent(payload, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.profile, "localAPIKeys.json"), encoded, 0o600)
}

// launch starts Zotero against the sandbox and returns the process so a caller
// can stop it. -no-remote keeps it from handing off to an already-running Zotero,
// which would silently seed the wrong library.
func (s *sandbox) launch(logPath string) (*exec.Cmd, error) {
	log, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(s.binary, "-no-remote", "-profile", s.profile)
	cmd.Env = append(os.Environ(), "HOME="+s.home)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, fmt.Errorf("start Zotero: %w", err)
	}
	return cmd, nil
}

// findZotero locates a Zotero to run. ZOTERO_BIN wins, so the version matrix can
// point at a downloaded build rather than the installed one.
func findZotero() (string, error) {
	if fromEnv := os.Getenv("ZOTERO_BIN"); fromEnv != "" {
		if _, err := os.Stat(fromEnv); err != nil {
			return "", fmt.Errorf("ZOTERO_BIN=%s: %w", fromEnv, err)
		}
		return fromEnv, nil
	}
	if found, err := exec.LookPath("zotero"); err == nil {
		return found, nil
	}
	candidates := map[string][]string{
		"linux":   {"/usr/lib/zotero/zotero", "/opt/zotero/zotero"},
		"darwin":  {"/Applications/Zotero.app/Contents/MacOS/zotero"},
		"windows": {`C:\Program Files\Zotero\zotero.exe`},
	}
	for _, candidate := range candidates[runtime.GOOS] {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot find Zotero; set ZOTERO_BIN to its executable")
}

// randomKey matches the shape Zotero generates for a local API key.
func randomKey() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf), nil
}
