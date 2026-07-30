package main

import (
	"os"
	"path/filepath"
	"strings"
)

// configDir is zotgo's configuration directory: $ZOTGO_CONFIG_DIR when set,
// otherwise the platform user-config dir with a zotgo/ subdirectory.
func configDir() (string, error) {
	if d := os.Getenv("ZOTGO_CONFIG_DIR"); d != "" {
		return d, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zotgo"), nil
}

// localKeyPath is where zotgo persists an approved local API key. It is a
// local-only write credential (it authorizes writes to the Zotero on this
// machine), kept owner-readable only.
func localKeyPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "local-api-key"), nil
}

// loadLocalKey returns the persisted local API key, or "" if none is stored or
// it cannot be read.
func loadLocalKey() string {
	path, err := localKeyPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// saveLocalKey writes the key with owner-only permissions, creating the config
// directory if needed.
func saveLocalKey(key string) error {
	path, err := localKeyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(key+"\n"), 0o600)
}

// clearLocalKey removes the persisted key. A missing file is not an error.
func clearLocalKey() error {
	path, err := localKeyPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
