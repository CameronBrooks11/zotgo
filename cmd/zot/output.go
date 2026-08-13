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

// machineWriteMode resolves the output mode for a write command and enforces the
// two rules shared by every machine write: --raw is refused (a write result is
// derived, not a raw Zotero response), and a non-dry-run machine write must pass
// --yes so an automation command never blocks on an interactive prompt. noun
// names the object kind for the error message ("item", "collection", "tag").
func machineWriteMode(cmd *cli.Command, noun string) (output.Mode, error) {
	mode, err := outputMode(cmd)
	if err != nil {
		return mode, err
	}
	if mode == output.ModeRaw {
		return mode, output.ErrRawUnavailable
	}
	if mode != output.ModeHuman && !cmd.Bool("dry-run") && !cmd.Bool("yes") {
		return mode, fmt.Errorf("%s %s writes require --yes (or --dry-run)", mode, noun)
	}
	return mode, nil
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

// emitMutations writes write results without list pagination metadata. JSON
// always carries the complete request as an array under the plural kind; JSONL
// carries one self-describing document per record under the singular kind. --raw
// is refused, because a mutation report is derived rather than a raw response.
func emitMutations[T any](w io.Writer, mode output.Mode, plural, singular output.Kind, lib *output.Library, records []T) error {
	switch mode {
	case output.ModeJSON:
		return output.WriteJSON(w, output.NewDocument(plural, lib, records))
	case output.ModeJSONL:
		return output.WriteJSONL(w, singular, lib, records)
	case output.ModeRaw:
		return output.ErrRawUnavailable
	default:
		return fmt.Errorf("emitMutations called in %s mode", mode)
	}
}

func emitItemMutations(w io.Writer, mode output.Mode, lib *output.Library, records []output.ItemMutation) error {
	return emitMutations(w, mode, output.KindItemMutations, output.KindItemMutation, lib, records)
}

func emitCollectionMutations(w io.Writer, mode output.Mode, lib *output.Library, records []output.CollectionMutation) error {
	return emitMutations(w, mode, output.KindCollectionMutations, output.KindCollectionMutation, lib, records)
}

func emitTagMutations(w io.Writer, mode output.Mode, lib *output.Library, records []output.TagMutation) error {
	return emitMutations(w, mode, output.KindTagMutations, output.KindTagMutation, lib, records)
}
