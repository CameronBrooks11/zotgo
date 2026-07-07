package render

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// CSV writes items as a spreadsheet-friendly table with a header row.
func CSV(w io.Writer, items []zotero.Envelope) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"key", "type", "title", "creators", "date", "tags"}); err != nil {
		return err
	}
	for _, it := range items {
		data, _ := it.ItemData()
		creators := it.CreatorSummary()
		if creators == "" {
			creators = creatorNames(data.Creators)
		}
		date := it.ParsedDate()
		if date == "" {
			date = data.Date
		}
		row := []string{it.Key, data.ItemType, data.Title, creators, date, tagNames(data.Tags)}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Markdown writes items as a bulleted reference list.
func Markdown(w io.Writer, items []zotero.Envelope) error {
	for _, it := range items {
		data, _ := it.ItemData()
		creators := it.CreatorSummary()
		if creators == "" {
			creators = creatorNames(data.Creators)
		}
		date := it.ParsedDate()
		if date == "" {
			date = data.Date
		}

		title := data.Title
		if title == "" {
			title = "(untitled)"
		}
		line := "- **" + title + "**"
		if creators != "" {
			line += " — " + creators
		}
		if date != "" {
			line += " (" + date + ")"
		}
		if _, err := fmt.Fprintf(w, "%s  \n  `%s` · %s\n", line, it.Key, data.ItemType); err != nil {
			return err
		}
	}
	return nil
}
