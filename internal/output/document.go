// Package output defines zotgo's machine-readable output contract: stable DTOs
// that do not change when Zotero's own API shapes change, wrapped in a versioned
// envelope.
//
// The DTOs here are the contract. The Zotero envelopes in internal/zotero are
// not: they mirror whatever the Local API returns, and reach users only through
// `--raw`, which is deliberately outside this contract.
package output

// SchemaVersion identifies the DTO contract. Bump it when a field changes
// meaning or disappears; adding a field is not a breaking change, and consumers
// are expected to ignore fields they do not know.
const SchemaVersion = 1

// Kind discriminates a Document's payload.
type Kind string

const (
	KindItem        Kind = "item"
	KindItems       Kind = "items"
	KindCollection  Kind = "collection"
	KindCollections Kind = "collections"
	KindStats       Kind = "stats"
	KindHealth      Kind = "health"
)

// Document is the envelope wrapping every machine-readable response.
//
// Under --json a command emits exactly one Document whose Data holds the whole
// payload. Under --jsonl it emits one Document per record, each carrying its own
// Schema and Kind so that a single line, taken alone, remains interpretable
// after the stream is split, truncated, or concatenated with another.
type Document struct {
	Schema  int      `json:"schema"`
	Kind    Kind     `json:"kind"`
	Library *Library `json:"library,omitempty"`
	Data    any      `json:"data"`
	Meta    *Meta    `json:"meta,omitempty"`
}

// Meta reports how much of a result set the payload represents. It is absent
// when the notion does not apply.
type Meta struct {
	// Shown is the number of records in Data.
	Shown int `json:"shown"`
	// Total is how many exist in the library, which may exceed Shown when a
	// limit was applied. Zero when unknown.
	Total int `json:"total"`
}

// NewDocument builds an envelope at the current schema version.
func NewDocument(kind Kind, library *Library, data any) Document {
	return Document{Schema: SchemaVersion, Kind: kind, Library: library, Data: data}
}

// WithMeta attaches shown/total counts.
func (d Document) WithMeta(shown, total int) Document {
	d.Meta = &Meta{Shown: shown, Total: total}
	return d
}
