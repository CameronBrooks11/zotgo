// Package render turns zotero SDK values into terminal output: aligned tables
// for humans and indented JSON for machines. It holds no I/O or network logic —
// every function writes to an io.Writer so output is easy to test and redirect.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// JSON writes v as indented JSON.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Items writes a one-line-per-item table: key, type, title, creator, date.
func Items(w io.Writer, items []zotero.Envelope) {
	tw := newTable(w)
	fmt.Fprintln(tw, "KEY\tTYPE\tTITLE\tBY\tDATE")
	for _, it := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			it.Key,
			it.ItemType(),
			truncate(it.Title(), 60),
			truncate(it.CreatorSummary(), 24),
			it.ParsedDate(),
		)
	}
	tw.Flush()
}

// Item writes a detailed view of a single item and its children.
func Item(w io.Writer, item zotero.Envelope, children []zotero.Envelope) {
	data, _ := item.ItemData()

	tw := newTable(w)
	field(tw, "Key", item.Key)
	field(tw, "Type", data.ItemType)
	field(tw, "Title", data.Title)
	if by := item.CreatorSummary(); by != "" {
		field(tw, "Authors", by)
	} else if names := creatorNames(data.Creators); names != "" {
		field(tw, "Authors", names)
	}
	if date := item.ParsedDate(); date != "" {
		field(tw, "Date", date)
	}
	if tags := tagNames(data.Tags); tags != "" {
		field(tw, "Tags", tags)
	}
	tw.Flush()

	if len(children) > 0 {
		fmt.Fprintf(w, "\nChildren (%d):\n", len(children))
		ctw := newTable(w)
		for _, ch := range children {
			cd, _ := ch.ItemData()
			fmt.Fprintf(ctw, "  %s\t%s\t%s\n", cd.ItemType, ch.Key, truncate(cd.Title, 60))
		}
		ctw.Flush()
	}
}

// Stats writes library-wide counts.
func Stats(w io.Writer, library string, s zotero.Stats) {
	tw := newTable(w)
	field(tw, "Library", library)
	field(tw, "Items", fmt.Sprintf("%d", s.Items))
	field(tw, "  Top-level", fmt.Sprintf("%d", s.TopItems))
	field(tw, "Collections", fmt.Sprintf("%d", s.Collections))
	field(tw, "Tags", fmt.Sprintf("%d", s.Tags))
	tw.Flush()
}

// Collections writes collections as an indented tree (by parent), or as a flat
// key/name table when flat is true.
func Collections(w io.Writer, cols []zotero.Envelope, flat bool) {
	type node struct {
		key, name, parent string
	}
	nodes := make([]node, 0, len(cols))
	for _, c := range cols {
		data, err := c.CollectionData()
		if err != nil {
			continue
		}
		nodes = append(nodes, node{key: c.Key, name: data.Name, parent: data.ParentKey()})
	}

	if flat {
		tw := newTable(w)
		fmt.Fprintln(tw, "KEY\tNAME")
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].name < nodes[j].name })
		for _, n := range nodes {
			fmt.Fprintf(tw, "%s\t%s\n", n.key, n.name)
		}
		tw.Flush()
		return
	}

	children := map[string][]node{}
	known := map[string]bool{}
	for _, n := range nodes {
		known[n.key] = true
	}
	for _, n := range nodes {
		// A collection whose parent is outside the returned set (or none) is a
		// root; this keeps the tree well-formed even for partial result sets.
		if n.parent != "" && known[n.parent] {
			children[n.parent] = append(children[n.parent], n)
		} else {
			children[""] = append(children[""], n)
		}
	}
	for key := range children {
		sort.Slice(children[key], func(i, j int) bool { return children[key][i].name < children[key][j].name })
	}

	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, n := range children[parent] {
			fmt.Fprintf(w, "%s%s  (%s)\n", strings.Repeat("  ", depth), n.name, n.key)
			walk(n.key, depth+1)
		}
	}
	walk("", 0)
}

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
}

func field(tw *tabwriter.Writer, label, value string) {
	fmt.Fprintf(tw, "%s\t%s\n", label, value)
}

func creatorNames(creators []zotero.Creator) string {
	parts := make([]string, 0, len(creators))
	for _, c := range creators {
		switch {
		case c.Name != "":
			parts = append(parts, c.Name)
		case c.LastName != "" && c.FirstName != "":
			parts = append(parts, c.LastName+", "+c.FirstName)
		case c.LastName != "":
			parts = append(parts, c.LastName)
		}
	}
	return strings.Join(parts, "; ")
}

func tagNames(tags []zotero.Tag) string {
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		parts = append(parts, t.Tag)
	}
	return strings.Join(parts, ", ")
}

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
