package output

// AttachmentImport is the stable outcome of one managed-file import.
type AttachmentImport struct {
	Status        string                        `json:"status"`
	Stage         string                        `json:"stage"`
	ParentKey     string                        `json:"parentKey"`
	AttachmentKey *string                       `json:"attachmentKey"`
	Filename      string                        `json:"filename"`
	ContentType   string                        `json:"contentType"`
	Size          int64                         `json:"size"`
	MD5           string                        `json:"md5"`
	FileStatus    *AttachmentImportFileStatus   `json:"fileStatus"`
	Verification  *AttachmentImportVerification `json:"verification"`
	Failure       *AttachmentImportFailure      `json:"failure"`
}

// AttachmentImportFileStatus conservatively summarizes advertised file metadata.
type AttachmentImportFileStatus struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// AttachmentImportVerification records focused post-registration invariants.
type AttachmentImportVerification struct {
	Parent         bool   `json:"parent"`
	ManagedStorage bool   `json:"managedStorage"`
	Title          bool   `json:"title"`
	SourceURL      bool   `json:"sourceUrl"`
	Filename       bool   `json:"filename"`
	ContentType    bool   `json:"contentType"`
	Size           bool   `json:"size"`
	Checksum       bool   `json:"checksum"`
	ActualFilename string `json:"actualFilename"`
}

// OK reports whether every managed-file invariant was verified.
func (v AttachmentImportVerification) OK() bool {
	return v.Parent && v.ManagedStorage && v.Title && v.SourceURL &&
		v.Filename && v.ContentType && v.Size && v.Checksum
}

// AttachmentImportFailure is a bounded structured failure for partial imports.
type AttachmentImportFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
