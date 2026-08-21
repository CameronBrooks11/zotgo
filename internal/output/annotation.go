package output

import "github.com/CameronBrooks11/zotgo/internal/zotero"

// Annotation is privacy-preserving metadata for one attachment annotation.
// Text, comments, position data, and image bytes require explicit raw output.
type Annotation struct {
	Key           string `json:"key"`
	AttachmentKey string `json:"attachmentKey"`
	Type          string `json:"type"`
	PageLabel     string `json:"pageLabel"`
	Color         string `json:"color"`
	SortIndex     string `json:"sortIndex"`
	HasText       bool   `json:"hasText"`
	HasComment    bool   `json:"hasComment"`
}

// NewAnnotations converts compact annotations to stable DTOs.
func NewAnnotations(annotations []zotero.Annotation) []Annotation {
	records := make([]Annotation, 0, len(annotations))
	for _, annotation := range annotations {
		records = append(records, Annotation{
			Key: annotation.Key, AttachmentKey: annotation.AttachmentKey,
			Type: annotation.Type, PageLabel: annotation.PageLabel,
			Color: annotation.Color, SortIndex: annotation.SortIndex,
			HasText: annotation.HasText, HasComment: annotation.HasComment,
		})
	}
	return records
}
