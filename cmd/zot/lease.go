package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// A write lease is how a human grants an agent bounded, time-boxed, audited write
// access (see docs/design/write-authority.md). It is a single 0600 JSON file
// under the config dir; the CLI installs it as the client's WriteAuthorizer for
// non-interactive writes, so an expired or out-of-scope write fails closed and is
// recorded. It contains the write key it authorizes, so expiry and revocation
// actually remove write ability and a hand-written lease carries no usable key.
//
// The lease is NOT forgery-resistant against a same-user process — that is
// impossible under the single-user threat model and is not its job. It contains a
// rule-following agent and gives the human an audit trail.
type lease struct {
	ID       string     `json:"id"`
	Created  time.Time  `json:"created"`
	Expires  time.Time  `json:"expires"`
	Scope    leaseScope `json:"scope"`
	WriteKey string     `json:"writeKey"`
	Note     string     `json:"note,omitempty"`
}

type leaseScope struct {
	Libraries  []string `json:"libraries"`
	Operations []string `json:"operations"`
}

// MaxLeaseTTL bounds how long a lease may live, so an over-long grant cannot
// stand in for the removed all-or-nothing switch. The invariant it defends is
// that authority ends on a date the human set, whether or not anyone acts —
// not that the window is short. A month, rather than a day, lets a recurring
// job (a nightly sync) hold one lease across its whole cycle instead of failing
// closed daily until a human is at a terminal; see #93.
const MaxLeaseTTL = 30 * 24 * time.Hour

// LongLeaseTTL is the lifetime above which a grant counts as long-lived: it
// takes a second, explicit confirmation naming the end date, and `grant status`
// flags it. It is the previous maximum, so every lease mintable before is still
// mintable with exactly the friction it had.
const LongLeaseTTL = 24 * time.Hour

// DefaultLeaseTTL is the lease lifetime when --ttl is omitted.
const DefaultLeaseTTL = 30 * time.Minute

// validateTTL bounds a requested lease lifetime. It lives apart from grantAction
// so the bounds are testable without a terminal (minting demands a TTY, which a
// test does not have).
func validateTTL(ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("--ttl must be positive")
	}
	if ttl > MaxLeaseTTL {
		return fmt.Errorf("--ttl %s exceeds the maximum %s", ttl, MaxLeaseTTL)
	}
	return nil
}

// isLongLived reports whether the lease was granted for longer than
// LongLeaseTTL. It measures the granted span, not the time left, so a month-long
// lease stays flagged on its final hour.
func (l *lease) isLongLived() bool {
	return l.Expires.Sub(l.Created) > LongLeaseTTL
}

var (
	// ErrNoActiveLease means no lease file is present: a non-interactive write has
	// no authority. Its message names the remedy.
	ErrNoActiveLease = errors.New("no active write lease; run 'zot grant' to authorize non-interactive writes")
	// ErrLeaseExpired means the lease's expiry has passed.
	ErrLeaseExpired = errors.New("write lease expired")
	// ErrLeaseLibrary means the lease does not cover the target library.
	ErrLeaseLibrary = errors.New("write lease does not cover this library")
	// ErrLeaseOperation means the lease does not permit the requested operation.
	ErrLeaseOperation = errors.New("write lease does not permit this operation")
)

// libraryToken is the canonical scope identity of a library: its kind and the id
// used to route writes. Locally the user library is always "user:0" (the Local
// API's sentinel); groups use their real id. Minting and enforcement both derive
// it the same way, so they always agree.
func libraryToken(lib zotero.LibraryRef) string {
	return fmt.Sprintf("%s:%d", lib.Kind, lib.ID)
}

// authorize reports whether op may run against lib under this lease at now,
// returning a dimension-specific sentinel (expired, library, operation) with an
// actionable message otherwise.
func (l *lease) authorize(op zotero.Operation, lib zotero.LibraryRef, now time.Time) error {
	if now.After(l.Expires) {
		return fmt.Errorf("%w at %s; run 'zot grant' to renew", ErrLeaseExpired, l.Expires.Format(time.RFC3339))
	}
	token := libraryToken(lib)
	if !contains(l.Scope.Libraries, token) {
		return fmt.Errorf("%w (%s); it covers %s", ErrLeaseLibrary, token, strings.Join(l.Scope.Libraries, ", "))
	}
	if !contains(l.Scope.Operations, string(op)) {
		return fmt.Errorf("%w (%s); run 'zot grant' with that operation", ErrLeaseOperation, op)
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// leasePath is the single active lease's location under the config dir.
func leasePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lease.json"), nil
}

// loadActiveLease reads the active lease, returning ErrNoActiveLease when none is
// present. Any other read or parse error is returned so the caller can fail
// closed rather than treat a corrupt lease as absent.
func loadActiveLease() (*lease, error) {
	path, err := leasePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoActiveLease
		}
		return nil, err
	}
	var l lease
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("the write lease is unreadable: %w", err)
	}
	return &l, nil
}

// saveLease writes the lease with owner-only permissions, creating the config
// directory if needed. It mirrors saveLocalKey's discipline (0700 dir, 0600 file).
func saveLease(l *lease) error {
	path, err := leasePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// revokeLease removes the active lease. A missing lease is not an error.
func revokeLease() error {
	path, err := leasePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// auditRecord is one line of a lease's audit log: an authorization decision, so a
// human can see what an agent attempted and whether it was allowed or refused.
type auditRecord struct {
	Time      time.Time `json:"time"`
	Operation string    `json:"operation"`
	Library   string    `json:"library"`
	Decision  string    `json:"decision"` // "allowed" or "refused"
	Reason    string    `json:"reason,omitempty"`
}

// auditPathFor derives a lease's audit log path from its id, so there is one log
// per lease and nothing extra to store in the record.
func auditPathFor(id string) (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "audit", id+".jsonl"), nil
}

// appendAudit writes one decision to the lease's append-only log. A failure to
// audit must not block or reverse the write decision, so it is reported to stderr
// rather than returned.
func appendAudit(id string, rec auditRecord) {
	path, err := auditPathFor(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zot: warning: could not resolve audit log: %v\n", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "zot: warning: could not create audit log: %v\n", err)
		return
	}
	line, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zot: warning: could not encode audit record: %v\n", err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zot: warning: could not open audit log: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "zot: warning: could not append to audit log: %v\n", err)
	}
}

// leaseAuthorizer enforces a write lease at the client's write chokepoint and
// records every decision to the lease's audit log.
type leaseAuthorizer struct {
	lease *lease
	now   func() time.Time // injectable for tests; defaults to time.Now
}

func newLeaseAuthorizer(l *lease) *leaseAuthorizer {
	return &leaseAuthorizer{lease: l, now: time.Now}
}

// AuthorizeWrite implements zotero.WriteAuthorizer: it applies the lease's scope
// and expiry, records the decision, and returns the dimension-specific refusal
// (which writeFriendly renders) or nil.
func (a *leaseAuthorizer) AuthorizeWrite(op zotero.Operation, lib zotero.LibraryRef) error {
	now := a.now()
	err := a.lease.authorize(op, lib, now)
	rec := auditRecord{
		Time:      now,
		Operation: string(op),
		Library:   libraryToken(lib),
		Decision:  "allowed",
	}
	if err != nil {
		rec.Decision = "refused"
		rec.Reason = err.Error()
	}
	appendAudit(a.lease.ID, rec)
	return err
}

// writeOperations is the closed vocabulary a lease may grant, in a stable order
// for display and for validating --operations.
var writeOperations = []zotero.Operation{
	zotero.OpItemCreate, zotero.OpItemPatch, zotero.OpItemReplace, zotero.OpItemDelete,
	zotero.OpCollectionCreate, zotero.OpCollectionRename, zotero.OpCollectionMove, zotero.OpCollectionDelete,
	zotero.OpTagAdd, zotero.OpTagRemove, zotero.OpTagDelete,
	zotero.OpAttachmentImport,
}

// destructiveOperations are the writes that can lose data; they are excluded from
// the default grant so an omitted --operations cannot authorize a deletion.
var destructiveOperations = map[zotero.Operation]bool{
	zotero.OpItemDelete:       true,
	zotero.OpCollectionDelete: true,
	zotero.OpTagDelete:        true,
}

// defaultGrantOperations is the scope when --operations is omitted: every
// non-destructive write. It is a usable middle ground — it covers create, patch,
// replace, tag add/remove, and attachment import — while withholding the delete
// operations, which must be named explicitly.
func defaultGrantOperations() []string {
	var ops []string
	for _, op := range writeOperations {
		if !destructiveOperations[op] {
			ops = append(ops, string(op))
		}
	}
	return ops
}

// resolveGrantOperations validates and normalizes the requested operation names,
// or returns the non-destructive default when none are given. Unknown names are
// rejected with the full vocabulary, so a typo cannot silently narrow a grant.
func resolveGrantOperations(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return defaultGrantOperations(), nil
	}
	valid := make(map[string]bool, len(writeOperations))
	for _, op := range writeOperations {
		valid[string(op)] = true
	}
	seen := make(map[string]bool, len(requested))
	var ops []string
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if !valid[name] {
			return nil, fmt.Errorf("unknown operation %q; valid operations are %s", name, strings.Join(operationNames(), ", "))
		}
		if !seen[name] {
			seen[name] = true
			ops = append(ops, name)
		}
	}
	sort.Strings(ops)
	return ops, nil
}

func operationNames() []string {
	names := make([]string, len(writeOperations))
	for i, op := range writeOperations {
		names[i] = string(op)
	}
	return names
}
