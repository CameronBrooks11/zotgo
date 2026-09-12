package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// copyPlan is everything a copy will touch, resolved before anything is written.
//
// Building it up front is what lets the command say where it is writing and what
// it will and will not carry *before* asking for confirmation — the same rule the
// write-authority design applies to every other write, and the one this command
// most needs, since its failure mode is looking complete while leaving children
// behind.
type copyPlan struct {
	items []plannedItem
}

type plannedItem struct {
	sourceKey string
	itemType  string
	title     string
	data      json.RawMessage
	children  []plannedChild
}

type plannedChild struct {
	sourceKey string
	// kind is "note", "attachment" or "annotation".
	kind     string
	title    string
	data     json.RawMessage
	linkMode string
	// managed reports Zotero-owned bytes that have to be read and re-uploaded.
	managed bool
	// skipReason, when set, is why this child will not be copied — stated in the
	// user's terms, because it appears in the plan and in the result.
	skipReason string
	// children is the annotations on an attachment. Annotations hang off the
	// attachment, not the item, and an unfiltered children listing omits them
	// entirely (see docs/zotero-api.md), so they are fetched deliberately.
	children []plannedChild
}

// counts summarizes a plan for the confirmation prompt. Carried and skipped are
// reported separately: a total alone would let a copy that silently drops
// everything look identical to one that carries everything.
type copyCounts struct {
	items       int
	notes       int
	attachments int
	annotations int
	files       int
	skipped     int
}

func (p copyPlan) counts() copyCounts {
	var c copyCounts
	c.items = len(p.items)
	for _, item := range p.items {
		for _, child := range item.children {
			if child.skipReason != "" {
				c.skipped++
				continue
			}
			switch child.kind {
			case "note":
				c.notes++
			case "attachment":
				c.attachments++
				if child.managed {
					c.files++
				}
			}
			for _, annotation := range child.children {
				if annotation.skipReason != "" {
					c.skipped++
					continue
				}
				c.annotations++
			}
		}
	}
	return c
}

// planItemCopy resolves one source item and everything hanging off it.
func planItemCopy(ctx context.Context, c *zotero.Client, source zotero.LibraryRef, key string, withAttachments bool) (plannedItem, error) {
	raw, children, err := c.RawItemWithChildren(ctx, source, key)
	if err != nil {
		return plannedItem{}, fmt.Errorf("read source item %s: %w", key, err)
	}
	identity, err := zotero.DecodeItemIdentity(raw)
	if err != nil {
		return plannedItem{}, fmt.Errorf("decode source item %s: %w", key, err)
	}
	if identity.ItemType == "attachment" || identity.ItemType == "note" || identity.ItemType == "annotation" {
		// Copying a child on its own would need a parent in the target to attach
		// it to, which the command has no way to name. Refuse rather than invent.
		return plannedItem{}, fmt.Errorf("%s is a %s, not a top-level item; copy its parent instead", key, identity.ItemType)
	}

	data, err := itemDataOf(raw)
	if err != nil {
		return plannedItem{}, fmt.Errorf("source item %s: %w", key, err)
	}
	planned := plannedItem{
		sourceKey: key,
		itemType:  identity.ItemType,
		title:     titleOf(data),
		data:      data,
	}

	for _, childRaw := range children {
		child, err := planChild(ctx, c, source, childRaw, withAttachments)
		if err != nil {
			return plannedItem{}, err
		}
		planned.children = append(planned.children, child)
	}
	return planned, nil
}

func planChild(ctx context.Context, c *zotero.Client, source zotero.LibraryRef, raw json.RawMessage, withAttachments bool) (plannedChild, error) {
	identity, err := zotero.DecodeItemIdentity(raw)
	if err != nil {
		return plannedChild{}, fmt.Errorf("decode child: %w", err)
	}
	data, err := itemDataOf(raw)
	if err != nil {
		return plannedChild{}, fmt.Errorf("child %s: %w", identity.Key, err)
	}

	child := plannedChild{
		sourceKey: identity.Key,
		kind:      identity.ItemType,
		title:     titleOf(data),
		data:      data,
	}

	if identity.ItemType != "attachment" {
		return child, nil
	}

	attachment, err := zotero.DecodeAttachment(raw)
	if err != nil {
		return plannedChild{}, fmt.Errorf("decode attachment %s: %w", identity.Key, err)
	}
	child.linkMode = attachment.LinkMode
	managed, err := zotero.ManagedAttachmentLinkMode(attachment.LinkMode)
	if err != nil {
		// An unrecognized mode fails closed rather than being copied as though it
		// were a plain link, which would lose whatever it actually refers to.
		return plannedChild{}, fmt.Errorf("attachment %s: %w", identity.Key, err)
	}
	child.managed = managed && attachment.Enclosure != nil

	if !withAttachments {
		child.skipReason = "excluded by --no-attachments"
		return child, nil
	}
	if managed && attachment.Enclosure == nil {
		// The attachment claims managed storage but Zotero advertises no file, so
		// there are no bytes to carry. The metadata still copies.
		child.skipReason = "Zotero advertises no file for this attachment; metadata only"
	}

	annotations, err := c.AllRawAnnotations(ctx, source, identity.Key)
	if err != nil {
		return plannedChild{}, fmt.Errorf("read annotations of %s: %w", identity.Key, err)
	}
	for _, annotationRaw := range annotations {
		annotationIdentity, err := zotero.DecodeItemIdentity(annotationRaw)
		if err != nil {
			return plannedChild{}, fmt.Errorf("decode annotation: %w", err)
		}
		annotationData, err := itemDataOf(annotationRaw)
		if err != nil {
			return plannedChild{}, fmt.Errorf("annotation %s: %w", annotationIdentity.Key, err)
		}
		child.children = append(child.children, plannedChild{
			sourceKey: annotationIdentity.Key,
			kind:      "annotation",
			title:     annotationSummary(annotationData),
			data:      annotationData,
		})
	}
	return child, nil
}

// itemDataOf extracts Zotero's own data object from an item envelope. That object
// is the write vocabulary; the surrounding envelope is not.
func itemDataOf(raw json.RawMessage) (json.RawMessage, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	if len(envelope.Data) == 0 {
		return nil, fmt.Errorf("envelope has no data object")
	}
	return envelope.Data, nil
}

func titleOf(data json.RawMessage) string {
	var fields struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(data, &fields)
	return fields.Title
}

// annotationSummary gives an annotation something recognizable to be listed as.
// Annotations have no title; their text or comment is the only thing a reader can
// identify them by. The full value is kept — shortening is a presentation
// decision, and the machine output should carry what was actually there.
func annotationSummary(data json.RawMessage) string {
	var fields struct {
		Text    string `json:"annotationText"`
		Comment string `json:"annotationComment"`
		Type    string `json:"annotationType"`
	}
	_ = json.Unmarshal(data, &fields)
	switch {
	case fields.Text != "":
		return fields.Text
	case fields.Comment != "":
		return fields.Comment
	default:
		return fields.Type
	}
}
