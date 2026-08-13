package zotero

import "fmt"

// ManagedAttachmentLinkMode reports whether linkMode identifies storage owned by
// Zotero. Unknown modes fail closed so callers do not treat future managed modes
// as ordinary linked attachments.
func ManagedAttachmentLinkMode(linkMode string) (bool, error) {
	switch linkMode {
	case "imported_file", "imported_url", "embedded_image":
		return true, nil
	case "linked_file", "linked_url":
		return false, nil
	default:
		return false, fmt.Errorf("unknown attachment linkMode %q", linkMode)
	}
}
