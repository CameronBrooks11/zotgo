package render

import (
	"fmt"
	"io"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// Annotations writes compact annotation metadata without text or comments.
func Annotations(w io.Writer, attachmentKey string, annotations []zotero.Annotation) {
	if len(annotations) == 0 {
		fmt.Fprintf(w, "No annotations for attachment %s.\n", attachmentKey)
		return
	}
	tw := newTable(w)
	fmt.Fprintln(tw, "KEY\tPAGE\tSORT INDEX\tTYPE\tTEXT\tCOMMENT\tCOLOR")
	for _, annotation := range annotations {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			annotation.Key, annotation.PageLabel, annotation.SortIndex,
			annotation.Type, yesNo(annotation.HasText), yesNo(annotation.HasComment),
			annotation.Color)
	}
	tw.Flush()
	fmt.Fprintf(w, "\n%d annotations\n", len(annotations))
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
