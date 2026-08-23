package output

// This file defines the closed vocabularies of the machine-output write
// contract (schema 3): the status of a write, the operation it performed, and
// the reason it failed. Consumers may switch on these values; new values may be
// added in a later schema, so an unrecognized value should be tolerated rather
// than rejected.

// Status is the outcome — or, under --dry-run, the plan — for one write request
// in a mutation record or an attachment import.
type Status string

const (
	// StatusPlanned is a dry-run outcome: the write would be applied.
	StatusPlanned Status = "planned"
	// StatusUnchanged means the request was a no-op (nothing to do).
	StatusUnchanged Status = "unchanged"
	// StatusNotFound means the target key did not exist and was skipped.
	StatusNotFound Status = "notFound"
	// StatusFailed means the write was attempted and rejected; see Failure.
	StatusFailed Status = "failed"

	// Applied outcomes, one per write verb:
	StatusCreated  Status = "created"
	StatusPatched  Status = "patched"
	StatusReplaced Status = "replaced"
	StatusRenamed  Status = "renamed"
	StatusMoved    Status = "moved"
	StatusAdded    Status = "added"
	StatusRemoved  Status = "removed"
	StatusDeleted  Status = "deleted"

	// Attachment-import outcomes (in addition to planned/failed):
	StatusDuplicate Status = "duplicate"
	StatusImported  Status = "imported"
	StatusPartial   Status = "partial"
)

// Operation names the kind of write a mutation record describes. The library-wide
// tag purge reports OpDelete: purge is the command name, delete is the operation.
type Operation string

const (
	OpCreate  Operation = "create"
	OpPatch   Operation = "patch"
	OpReplace Operation = "replace"
	OpDelete  Operation = "delete"
	OpRename  Operation = "rename"
	OpMove    Operation = "move"
	OpAdd     Operation = "add"
	OpRemove  Operation = "remove"
)

// FailureCode is the stable, documented reason a write failed. The batch-mutation
// codes are categories derived from Zotero's HTTP status (the exact number is in
// Failure.HTTPStatus); the attachment-import codes name the phase that failed.
type FailureCode string

const (
	// Batch-mutation categories (item/collection/tag writes):
	CodeInvalid            FailureCode = "invalid"             // malformed or rejected data (400/422)
	CodeConflict           FailureCode = "conflict"            // a conflicting object exists (409)
	CodePreconditionFailed FailureCode = "precondition-failed" // the object changed concurrently (412)
	CodeTooLarge           FailureCode = "too-large"           // the request exceeded a size limit (413)
	CodeNotFound           FailureCode = "not-found"           // the target did not exist (404)
	CodeRateLimited        FailureCode = "rate-limited"        // Zotero throttled the request (429/503)
	CodeServerError        FailureCode = "server-error"        // Zotero failed internally (5xx)
	CodeUnknown            FailureCode = "unknown"             // no HTTP status, or an unmapped one

	// Attachment-import phases:
	CodeAuthorizationRequired FailureCode = "authorization-required"
	CodeStagedFileFailed      FailureCode = "staged-file-failed"
	CodeMetadataCreateFailed  FailureCode = "metadata-create-failed"
	CodeMetadataCreateUnknown FailureCode = "metadata-create-unknown"
	CodeUploadAuthorizeFailed FailureCode = "upload-authorize-failed"
	CodeUploadFailed          FailureCode = "upload-failed"
	CodeRegisterFailed        FailureCode = "register-failed"
	CodeVerificationFailed    FailureCode = "verification-failed"
)

// Failure is the stable, structured reason a write failed — shared by every
// mutation record and by attachment import. Code is a documented FailureCode;
// HTTPStatus carries Zotero's exact status when the failure came from an HTTP
// response and is omitted otherwise; Message is a human-readable explanation.
type Failure struct {
	Code       FailureCode `json:"code"`
	HTTPStatus int         `json:"httpStatus,omitempty"`
	Message    string      `json:"message"`
}

// FailureCodeForStatus maps a Zotero HTTP status to its FailureCode category.
// The exact status is preserved separately in Failure.HTTPStatus; this only
// classifies it so consumers can branch without hardcoding numbers.
func FailureCodeForStatus(status int) FailureCode {
	switch {
	case status == 400 || status == 422:
		return CodeInvalid
	case status == 404:
		return CodeNotFound
	case status == 409:
		return CodeConflict
	case status == 412:
		return CodePreconditionFailed
	case status == 413:
		return CodeTooLarge
	case status == 429 || status == 503:
		return CodeRateLimited
	case status >= 500:
		return CodeServerError
	default:
		return CodeUnknown
	}
}
