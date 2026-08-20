package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// CollectionPaths writes requested collection paths in request order.
func CollectionPaths(w io.Writer, paths []zotero.CollectionPath) {
	tw := newTable(w)
	fmt.Fprintln(tw, "KEY\tPATH")
	for _, path := range paths {
		names := make([]string, 0, len(path.Segments))
		for _, segment := range path.Segments {
			names = append(names, segment.Name)
		}
		fmt.Fprintf(tw, "%s\t%s\n", path.Key, strings.Join(names, " / "))
	}
	tw.Flush()
	fmt.Fprintf(w, "\n%d collection paths\n", len(paths))
}
