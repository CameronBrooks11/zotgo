package zotero

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func rawAnnotation(key, parent, annotationType, sortIndex, text, comment string) json.RawMessage {
	data, _ := json.Marshal(map[string]any{
		"key": key, "itemType": "annotation", "parentItem": parent,
		"annotationType": annotationType, "annotationText": text,
		"annotationComment": comment, "annotationColor": "#ffd400",
		"annotationPageLabel": "12", "annotationSortIndex": sortIndex,
		"annotationPosition": `{"pageIndex":11}`, "futureData": true,
	})
	envelope, _ := json.Marshal(map[string]any{"key": key, "futureTop": map[string]any{"kept": true}, "data": json.RawMessage(data)})
	return envelope
}

func TestDecodeAnnotation(t *testing.T) {
	annotation, err := DecodeAnnotation(rawAnnotation(
		"ANN00001", "ATTACH01", "future-type", "00012|00001", "quoted text", "comment"))
	if err != nil {
		t.Fatalf("DecodeAnnotation: %v", err)
	}
	want := Annotation{
		Key: "ANN00001", AttachmentKey: "ATTACH01", Type: "future-type",
		PageLabel: "12", Color: "#ffd400", SortIndex: "00012|00001",
		HasText: true, HasComment: true,
	}
	if !reflect.DeepEqual(annotation, want) {
		t.Fatalf("annotation = %#v, want %#v", annotation, want)
	}
}

func TestDecodeAnnotationsDocumentOrder(t *testing.T) {
	raw := []json.RawMessage{
		rawAnnotation("ANN00003", "ATTACH01", "note", "", "", "note"),
		rawAnnotation("ANN00002", "ATTACH01", "highlight", "00002", "text", ""),
		rawAnnotation("ANN00001", "ATTACH01", "highlight", "00001", "text", ""),
		rawAnnotation("ANN00000", "ATTACH01", "ink", "00001", "", ""),
	}
	annotations, err := DecodeAnnotations(raw, "ATTACH01")
	if err != nil {
		t.Fatalf("DecodeAnnotations: %v", err)
	}
	var keys []string
	for _, annotation := range annotations {
		keys = append(keys, annotation.Key)
	}
	want := []string{"ANN00000", "ANN00001", "ANN00002", "ANN00003"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if empty, err := DecodeAnnotations(nil, "ATTACH01"); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, %v", empty, err)
	}
}

func TestDecodeAnnotationRejectsMalformedItems(t *testing.T) {
	validData := `{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight"}`
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "nonobject", raw: `[]`, want: "expected an annotation item object"},
		{name: "invalid envelope", raw: `{`, want: "unexpected end"},
		{name: "missing key", raw: `{"data":` + validData + `}`, want: "missing annotation key"},
		{name: "missing data", raw: `{"key":"ANN00001"}`, want: "no data object"},
		{name: "null data", raw: `{"key":"ANN00001","data":null}`, want: "no data object"},
		{name: "invalid data", raw: `{"key":"ANN00001","data":{}`, want: "unexpected end"},
		{name: "missing data key", raw: `{"key":"ANN00001","data":{"itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight"}}`, want: "has no key"},
		{name: "mismatched data key", raw: `{"key":"ANN00001","data":{"key":"OTHER001","itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight"}}`, want: `data has key "OTHER001"`},
		{name: "wrong type", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"note","parentItem":"ATTACH01","annotationType":"highlight"}}`, want: `type "note"`},
		{name: "missing parent", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","annotationType":"highlight"}}`, want: "has no parentItem"},
		{name: "empty parent", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":"","annotationType":"highlight"}}`, want: "has no parentItem"},
		{name: "null parent", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":null,"annotationType":"highlight"}}`, want: "parentItem must be a string, not null"},
		{name: "false parent", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":false,"annotationType":"highlight"}}`, want: "parentItem must be a string"},
		{name: "missing annotation type", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01"}}`, want: "has no annotationType"},
		{name: "null annotation type", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01","annotationType":null}}`, want: "annotationType must be a string, not null"},
		{name: "null text", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight","annotationText":null}}`, want: "annotationText must be a string, not null"},
		{name: "number comment", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight","annotationComment":7}}`, want: "annotationComment must be a string"},
		{name: "object sort index", raw: `{"key":"ANN00001","data":{"key":"ANN00001","itemType":"annotation","parentItem":"ATTACH01","annotationType":"highlight","annotationSortIndex":{}}}`, want: "annotationSortIndex must be a string"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeAnnotation(json.RawMessage(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestDecodeAnnotationsRejectsWrongParentWithoutMutatingInput(t *testing.T) {
	raw := []json.RawMessage{
		rawAnnotation("ANN00002", "ATTACH01", "highlight", "00002", "", ""),
		rawAnnotation("ANN00001", "OTHER001", "highlight", "00001", "", ""),
	}
	before := append([]json.RawMessage(nil), raw...)
	annotations, err := DecodeAnnotations(raw, "ATTACH01")
	if err == nil || !strings.Contains(err.Error(), `reports parent "OTHER001"`) || annotations != nil {
		t.Fatalf("annotations=%#v error=%v", annotations, err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Fatal("DecodeAnnotations mutated raw input")
	}
}
