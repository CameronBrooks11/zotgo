package zotero

import (
	"encoding/json"
	"testing"
)

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
