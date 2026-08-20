package zotero

// Operation names a single mutating command for write-authorization scoping. The
// values are the stable, per-command vocabulary a write lease grants against, so
// least privilege can withhold, say, item.delete while allowing item.patch. They
// intentionally mirror the CLI's write commands one-to-one.
type Operation string

const (
	OpItemCreate       Operation = "item.create"
	OpItemPatch        Operation = "item.patch"
	OpItemReplace      Operation = "item.replace"
	OpItemDelete       Operation = "item.delete"
	OpCollectionCreate Operation = "collection.create"
	OpCollectionRename Operation = "collection.rename"
	OpCollectionMove   Operation = "collection.move"
	OpCollectionDelete Operation = "collection.delete"
	OpTagAdd           Operation = "tag.add"
	OpTagRemove        Operation = "tag.remove"
	OpTagDelete        Operation = "tag.delete"
	// OpAttachmentImport is reserved for the managed-file upload command (#52),
	// which conforms to the write-lease model once it lands.
	OpAttachmentImport Operation = "attachment.import"
)

// WriteAuthorizer decides whether one write may proceed. It is consulted at the
// write chokepoint before any mutating request reaches Zotero, so a denial costs
// no network I/O and fails closed. The returned error, if any, is surfaced to
// the caller unchanged, so an implementation can distinguish refusal reasons
// (missing authority, expired, out of scope) with its own sentinels.
type WriteAuthorizer interface {
	AuthorizeWrite(op Operation, lib LibraryRef) error
}

// allowAll authorizes every write. It is the permissive stance the CLI installs
// for interactive, human-confirmed writes; a write lease replaces it for
// non-interactive ones.
type allowAll struct{}

func (allowAll) AuthorizeWrite(Operation, LibraryRef) error { return nil }

// AllowAllWrites returns a WriteAuthorizer that permits every write. Callers use
// it when authority is established out of band — an interactive human answered a
// confirmation prompt — and tests use it to exercise the write mechanism itself.
func AllowAllWrites() WriteAuthorizer { return allowAll{} }

// SetWriteAuthorizer installs the authority policy consulted before every write.
// Installing nil restores deny-by-default: with no authorizer, every write is
// refused with ErrWriteNotAuthorized, so a write path that never established one
// cannot reach Zotero.
func (c *Client) SetWriteAuthorizer(a WriteAuthorizer) {
	c.mu.Lock()
	c.writeAuthorizer = a
	c.mu.Unlock()
}

// authorizeWrite consults the installed WriteAuthorizer, denying by default when
// none is set.
func (c *Client) authorizeWrite(op Operation, lib LibraryRef) error {
	c.mu.RLock()
	authz := c.writeAuthorizer
	c.mu.RUnlock()
	if authz == nil {
		return ErrWriteNotAuthorized
	}
	return authz.AuthorizeWrite(op, lib)
}
