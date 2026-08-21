package zotero

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeAttachment(t *testing.T) {
	raw := json.RawMessage(`{
		"key":"ATTACH01",
		"links":{
			"enclosure":{"href":"http://127.0.0.1/file","type":"application/pdf","title":"paper.pdf","length":1234},
			"alternate":{"length":{"future":"shape"}}
		},
		"data":{
			"itemType":"attachment","parentItem":"PARENT01","title":"Full Text PDF",
			"linkMode":"imported_file","contentType":"application/pdf","filename":"paper.pdf",
			"url":"https://example.com/paper.pdf","accessDate":"2026-01-01T00:00:00Z",
			"dateAdded":"2026-01-02T00:00:00Z","dateModified":"2026-01-03T00:00:00Z",
			"tags":[{"tag":"review","type":0}],"md5":"0123456789abcdef","mtime":1700000000000
		}
	}`)
	attachment, err := DecodeAttachment(raw)
	if err != nil {
		t.Fatalf("DecodeAttachment: %v", err)
	}
	if attachment.Key != "ATTACH01" || attachment.ParentKey != "PARENT01" || attachment.Filename != "paper.pdf" {
		t.Fatalf("attachment = %#v", attachment)
	}
	if attachment.MD5 == nil || *attachment.MD5 != "0123456789abcdef" || attachment.MTime == nil || *attachment.MTime != 1700000000000 {
		t.Fatalf("storage metadata = md5:%v mtime:%v", attachment.MD5, attachment.MTime)
	}
	if attachment.Enclosure == nil || attachment.Enclosure.Length == nil || *attachment.Enclosure.Length != 1234 {
		t.Fatalf("enclosure = %#v", attachment.Enclosure)
	}
	if !reflect.DeepEqual(attachment.Tags, []Tag{{Tag: "review", Type: 0}}) {
		t.Fatalf("tags = %#v", attachment.Tags)
	}
}

func TestAttachmentDecodeTreatsFalseEnclosureHrefAsUnavailable(t *testing.T) {
	const fixture = `{"key":"ATTACH01","links":{"enclosure":{"href":false,"type":"application/pdf","title":""}},"data":{"itemType":"attachment","parentItem":"PARENT01","linkMode":"imported_file","filename":""}}`
	attachment, err := DecodeAttachment(json.RawMessage(fixture))
	if err != nil {
		t.Fatalf("DecodeAttachment: %v", err)
	}
	if attachment.Key != "ATTACH01" || attachment.Enclosure != nil {
		t.Fatalf("attachment = %#v", attachment)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()
	attachment, err = New(srv.URL).Attachment(context.Background(), UserLibrary(), "ATTACH01")
	if err != nil || attachment.Enclosure != nil {
		t.Fatalf("Client.Attachment = %#v, %v", attachment, err)
	}
}

func TestAttachmentDataNullableShapesAndValidation(t *testing.T) {
	attachment, err := (Envelope{Key: "ATTACH01", Data: json.RawMessage(`{
		"itemType":"attachment","parentItem":false,"linkMode":"linked_url","tags":null,"md5":null,"mtime":null
	}`)}).AttachmentData()
	if err != nil {
		t.Fatalf("AttachmentData: %v", err)
	}
	if attachment.ParentKey != "" || attachment.MD5 != nil || attachment.MTime != nil || attachment.Tags == nil {
		t.Fatalf("nullable fields = parent:%q md5:%v mtime:%v tags:%v", attachment.ParentKey, attachment.MD5, attachment.MTime, attachment.Tags)
	}

	webAttachment, err := (Envelope{Key: "ATTACH02", Data: json.RawMessage(`{
		"itemType":"attachment","linkMode":"imported_file","mtime":"1331171741767"
	}`)}).AttachmentData()
	if err != nil {
		t.Fatalf("Web API string mtime: %v", err)
	}
	if webAttachment.MTime == nil || *webAttachment.MTime != 1331171741767 {
		t.Fatalf("Web API mtime = %v", webAttachment.MTime)
	}

	for _, tt := range []struct {
		name string
		item Envelope
		want string
	}{
		{name: "missing key", item: Envelope{Data: json.RawMessage(`{"itemType":"attachment"}`)}, want: "missing attachment key"},
		{name: "wrong type", item: Envelope{Key: "ATTACH01", Data: json.RawMessage(`{"itemType":"book"}`)}, want: `type "book"`},
		{name: "bad parent", item: Envelope{Key: "ATTACH01", Data: json.RawMessage(`{"itemType":"attachment","parentItem":{}}`)}, want: "parentItem"},
		{name: "bad md5", item: Envelope{Key: "ATTACH01", Data: json.RawMessage(`{"itemType":"attachment","md5":7}`)}, want: "md5"},
		{name: "bad mtime", item: Envelope{Key: "ATTACH01", Data: json.RawMessage(`{"itemType":"attachment","mtime":"later"}`)}, want: "mtime"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.item.AttachmentData()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestClientAttachmentUsesLosslessRawItem(t *testing.T) {
	const fixture = `{"key":"ATTACH01","version":"","futureTop":{"kept":true},"links":{"enclosure":{"href":"http://local/file","type":"application/pdf","title":"paper.pdf","length":1234,"futureLink":true}},"data":{"itemType":"attachment","linkMode":"imported_file","filename":"paper.pdf","futureData":7}}`
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/items/{key}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("key") {
		case "ATTACH01":
			_, _ = w.Write([]byte(fixture))
		case "WRONG001":
			_, _ = w.Write([]byte(`{"key":"OTHER001","data":{"itemType":"attachment"}}`))
		case "BOOK0001":
			_, _ = w.Write([]byte(`{"key":"BOOK0001","data":{"itemType":"book"}}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := New(srv.URL)
	attachment, err := client.Attachment(context.Background(), UserLibrary(), "ATTACH01")
	if err != nil {
		t.Fatalf("Attachment: %v", err)
	}
	if attachment.Key != "ATTACH01" || attachment.Filename != "paper.pdf" {
		t.Fatalf("attachment = %#v", attachment)
	}
	raw, err := client.RawItem(context.Background(), UserLibrary(), "ATTACH01")
	if err != nil {
		t.Fatalf("RawItem: %v", err)
	}
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if err := json.Unmarshal([]byte(fixture), &want); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("raw changed envelope: got %#v, want %#v", got, want)
	}

	raw, err = client.RawItem(context.Background(), UserLibrary(), "WRONG001")
	if err == nil || !strings.Contains(err.Error(), `response has key "OTHER001"`) {
		t.Fatalf("wrong-key error = %v", err)
	}
	if raw != nil {
		t.Fatalf("returned raw output after key validation error")
	}

	bookRaw, err := client.RawItem(context.Background(), UserLibrary(), "BOOK0001")
	if err != nil {
		t.Fatalf("RawItem book: %v", err)
	}
	if err := RequireItemType(bookRaw, "attachment"); err == nil || !strings.Contains(err.Error(), `type "book", not attachment`) {
		t.Fatalf("book type error = %v", err)
	}
	if _, err := client.Attachment(context.Background(), UserLibrary(), "BOOK0001"); err == nil || !strings.Contains(err.Error(), `type "book", not attachment`) {
		t.Fatalf("Attachment book error = %v", err)
	}
}
