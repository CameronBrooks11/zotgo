package output

// ItemCopy is the outcome, or dry-run plan, for copying one source item into
// another library. A copy is a tree, not a single write: the item, then its notes
// and attachments, then the annotations hanging off those attachments. Reporting
// it as one status would hide exactly the thing this command exists to stop
// hiding — a copy that looked complete while leaving children behind.
type ItemCopy struct {
	Index     int       `json:"index"`
	Operation Operation `json:"operation"`
	Status    Status    `json:"status"`
	// SourceKey is the item that was copied from; Key is the item created in the
	// target library, absent until it exists.
	SourceKey string `json:"sourceKey"`
	Key       string `json:"key,omitempty"`
	Type      string `json:"type,omitempty"`
	Title     string `json:"title,omitempty"`
	// Children is every note, attachment and annotation the copy considered,
	// including the ones it deliberately skipped. Always present, so a consumer
	// never has to distinguish "no children" from "children not reported".
	Children []CopiedChild `json:"children"`
	Failure  *Failure      `json:"failure,omitempty"`
}

// CopiedChild is one note, attachment, or annotation within a copy. Annotations
// appear under the attachment they belong to rather than at the top level,
// because that is where Zotero keeps them and where their keys must be rewritten
// to.
type CopiedChild struct {
	Index int `json:"index"`
	// Kind is the item type being copied: "note", "attachment", or "annotation".
	Kind      string `json:"kind"`
	Status    Status `json:"status"`
	SourceKey string `json:"sourceKey"`
	Key       string `json:"key,omitempty"`
	Title     string `json:"title,omitempty"`
	// Reason explains a skipped child in the user's terms — an excluding flag, or
	// an attachment that has no bytes to carry.
	Reason string `json:"reason,omitempty"`
	// File reports the managed bytes, when this child is an attachment that has
	// any. Absent for notes, annotations, and link-only attachments.
	File *CopiedFile `json:"file,omitempty"`
	// Children is the annotations on this attachment; empty for other kinds.
	Children []CopiedChild `json:"children,omitempty"`
	Failure  *Failure      `json:"failure,omitempty"`
}

// CopiedFile reports what happened to one attachment's managed bytes. Metadata
// landing while the upload fails is an ordinary outcome, not an edge case, so the
// file's fate is reported separately from the attachment item's.
type CopiedFile struct {
	Status Status `json:"status"`
	// Filename and Size describe the source file as staged.
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
	// Stage names the last phase completed when Status is failed or partial,
	// matching the vocabulary attachment import already reports.
	Stage   string   `json:"stage,omitempty"`
	Failure *Failure `json:"failure,omitempty"`
}
