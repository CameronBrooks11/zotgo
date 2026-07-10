package main

import (
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
)

// outputMode reads the three mutually exclusive output flags.
func outputMode(cmd *cli.Command) (output.Mode, error) {
	return output.ResolveMode(cmd.Bool("json"), cmd.Bool("jsonl"), cmd.Bool("raw"))
}

// emitSet writes a collection of records in the requested machine mode.
//
// plural names the whole set (--json), singular names one record (--jsonl).
// raw is the untouched Zotero payload; pass nil when the command derives its
// result and has none, and --raw will be refused rather than faked.
func emitSet[T any](w io.Writer, mode output.Mode, plural, singular output.Kind, lib *output.Library, records []T, shown, total int, raw any) error {
	switch mode {
	case output.ModeJSON:
		return output.WriteJSON(w, output.NewDocument(plural, lib, records).WithMeta(shown, total))
	case output.ModeJSONL:
		return output.WriteJSONL(w, singular, lib, records)
	case output.ModeRaw:
		if raw == nil {
			return output.ErrRawUnavailable
		}
		return output.WriteRaw(w, raw)
	default:
		return fmt.Errorf("emitSet called in %s mode", mode)
	}
}

// emitOne writes a single record in the requested machine mode. Under --jsonl
// that is one line, which keeps the format uniform for scripts that pipe several
// commands into the same consumer.
func emitOne[T any](w io.Writer, mode output.Mode, kind output.Kind, lib *output.Library, record T, raw any) error {
	switch mode {
	case output.ModeJSON:
		return output.WriteJSON(w, output.NewDocument(kind, lib, record))
	case output.ModeJSONL:
		return output.WriteJSONL(w, kind, lib, []T{record})
	case output.ModeRaw:
		if raw == nil {
			return output.ErrRawUnavailable
		}
		return output.WriteRaw(w, raw)
	default:
		return fmt.Errorf("emitOne called in %s mode", mode)
	}
}
