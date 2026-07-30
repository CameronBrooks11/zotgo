package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

type fieldInfo struct {
	Field string `json:"field"`
}

type creatorTypeInfo struct {
	CreatorType string `json:"creatorType"`
}

// ItemTemplate returns a blank item of itemType, assembled from the Local API's
// itemTypeFields and itemTypeCreatorTypes endpoints — the Local API has no
// /items/new template route. Fields appear in Zotero's order with itemType
// first, plus one empty creator of the primary type and empty tags/collections/
// relations, ready to fill in and pass to CreateItems.
//
// Special item types (note, attachment) carry their content outside these
// fields, so their template will be sparse. An unknown type yields a clear error.
func (c *Client) ItemTemplate(ctx context.Context, itemType string) (json.RawMessage, error) {
	if itemType == "" {
		return nil, errors.New("item type is required")
	}
	q := url.Values{"itemType": {itemType}}

	var fields []fieldInfo
	if _, err := c.getJSON(ctx, "/api/itemTypeFields", q, &fields); err != nil {
		return nil, templateError(itemType, err)
	}
	var creators []creatorTypeInfo
	if _, err := c.getJSON(ctx, "/api/itemTypeCreatorTypes", q, &creators); err != nil {
		return nil, templateError(itemType, err)
	}
	return buildTemplate(itemType, fields, creators), nil
}

// templateError turns the endpoint's 400 for an unrecognized type into a clear
// message rather than a bare status.
func templateError(itemType string, err error) error {
	var se StatusError
	if errors.As(err, &se) && se.StatusCode == 400 {
		return fmt.Errorf("unknown item type %q", itemType)
	}
	return err
}

// buildTemplate assembles an ordered blank item object. Order is emitted by hand
// because Go marshals maps alphabetically, which would bury itemType among the
// fields and scramble Zotero's field order.
func buildTemplate(itemType string, fields []fieldInfo, creators []creatorTypeInfo) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	writeJSONPair(&b, "itemType", itemType)
	for _, f := range fields {
		b.WriteByte(',')
		writeJSONPair(&b, f.Field, "")
	}
	b.WriteString(`,"creators":[`)
	if len(creators) > 0 {
		fmt.Fprintf(&b, `{"creatorType":%s,"firstName":"","lastName":""}`, jsonString(creators[0].CreatorType))
	}
	b.WriteString(`],"tags":[],"collections":[],"relations":{}}`)
	return b.Bytes()
}

func writeJSONPair(b *bytes.Buffer, key, value string) {
	b.WriteString(jsonString(key))
	b.WriteByte(':')
	b.WriteString(jsonString(value))
}

func jsonString(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}
