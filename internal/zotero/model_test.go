package zotero

import (
	"encoding/json"
	"testing"
)

func TestLinkAcceptsFalseHrefForUnregisteredManagedFile(t *testing.T) {
	var envelope Envelope
	if err := json.Unmarshal([]byte(`{
		"key":"ATTACH01",
		"links":{"enclosure":{"href":false,"type":"application/pdf","title":"","length":12}},
		"data":{"itemType":"attachment","linkMode":"imported_file","parentItem":"PARENT01"}
	}`), &envelope); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	link := envelope.Links["enclosure"]
	if link.Href != "" || link.Type != "application/pdf" || link.Length == nil || *link.Length != 12 {
		t.Fatalf("link = %#v", link)
	}
	for _, raw := range []string{`{"href":true}`, `{"href":42}`, `{"href":{}}`} {
		var link Link
		if err := json.Unmarshal([]byte(raw), &link); err == nil {
			t.Errorf("invalid link accepted: %s", raw)
		}
	}
}

func TestVersionNumberUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    versionNumber
		wantErr bool
	}{
		{name: "number", input: `3`, want: 3},
		{name: "zero", input: `0`, want: 0},
		{name: "empty string", input: `""`, want: 0},
		{name: "numeric string", input: `"12"`, wantErr: true},
		{name: "invalid string", input: `"invalid"`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got versionNumber
			err := json.Unmarshal([]byte(test.input), &got)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unmarshal version: %v", err)
			}
			if got != test.want {
				t.Fatalf("version = %d, want %d", got, test.want)
			}
		})
	}
}

func TestDecodeItemIdentity(t *testing.T) {
	t.Parallel()

	identity, err := DecodeItemIdentity(json.RawMessage(`{"key":"ITEM0001","future":true,"data":{"itemType":"book","future":7}}`))
	if err != nil {
		t.Fatalf("DecodeItemIdentity: %v", err)
	}
	if identity.Key != "ITEM0001" || identity.ItemType != "book" {
		t.Fatalf("identity = %#v", identity)
	}
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "array", raw: `[]`},
		{name: "null", raw: `null`},
		{name: "malformed", raw: `{`},
		{name: "missing key", raw: `{"data":{"itemType":"book"}}`},
		{name: "missing item type", raw: `{"key":"ITEM0001","data":{}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeItemIdentity(json.RawMessage(test.raw)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	if err := RequireItemType(json.RawMessage(`{"key":"ITEM0001","data":{"itemType":"book"}}`), "attachment"); err == nil {
		t.Fatal("expected item type rejection")
	}
}

func TestEnvelopeAcceptsMigratedEmptyVersions(t *testing.T) {
	t.Parallel()

	var envelope Envelope
	if err := json.Unmarshal([]byte(`{
		"key": "ABCD1234",
		"version": "",
		"data": {"key": "ABCD1234", "version": "", "itemType": "attachment"}
	}`), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.Version != 0 {
		t.Fatalf("envelope version = %d, want 0", envelope.Version)
	}
	item, err := envelope.ItemData()
	if err != nil {
		t.Fatalf("decode item data: %v", err)
	}
	if item.Version != 0 {
		t.Fatalf("item version = %d, want 0", item.Version)
	}
}
