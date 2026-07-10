package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Mode selects how a command emits its result.
type Mode int

const (
	// ModeHuman renders tables and prose for a terminal.
	ModeHuman Mode = iota
	// ModeJSON emits one Document.
	ModeJSON
	// ModeJSONL emits one Document per record, newline-delimited.
	ModeJSONL
	// ModeRaw emits Zotero's own API payloads, unversioned and unshaped.
	ModeRaw
)

// ErrRawUnavailable means the command's result is derived by zotgo and has no
// underlying Zotero payload to pass through.
var ErrRawUnavailable = errors.New("no raw Zotero response for this command")

func (m Mode) String() string {
	switch m {
	case ModeJSON:
		return "--json"
	case ModeJSONL:
		return "--jsonl"
	case ModeRaw:
		return "--raw"
	default:
		return "human"
	}
}

// ResolveMode picks the output mode from the three mutually exclusive flags.
func ResolveMode(jsonFlag, jsonlFlag, rawFlag bool) (Mode, error) {
	var chosen []Mode
	if jsonFlag {
		chosen = append(chosen, ModeJSON)
	}
	if jsonlFlag {
		chosen = append(chosen, ModeJSONL)
	}
	if rawFlag {
		chosen = append(chosen, ModeRaw)
	}
	switch len(chosen) {
	case 0:
		return ModeHuman, nil
	case 1:
		return chosen[0], nil
	default:
		return ModeHuman, fmt.Errorf("%s and %s are mutually exclusive", chosen[0], chosen[1])
	}
}

// WriteJSON emits one Document, indented, with a trailing newline.
func WriteJSON(w io.Writer, doc Document) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// WriteJSONL emits one Document per record. Each line repeats the schema, kind,
// and library so that it stands alone once the stream is split or truncated.
//
// kind is the singular form: a line holds one record, not the set.
func WriteJSONL[T any](w io.Writer, kind Kind, library *Library, records []T) error {
	enc := json.NewEncoder(w) // No SetIndent: one record must be one line.
	for _, record := range records {
		if err := enc.Encode(NewDocument(kind, library, record)); err != nil {
			return err
		}
	}
	return nil
}

// WriteRaw emits a Zotero payload untouched apart from indentation. It is not
// covered by SchemaVersion: the shape is Zotero's and changes when Zotero's does.
func WriteRaw(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
