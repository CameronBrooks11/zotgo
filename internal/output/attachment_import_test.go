package output

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAttachmentImportStableFields(t *testing.T) {
	record := AttachmentImport{
		Status: "planned", Stage: "preflight", ParentKey: "PARENT01",
		Filename: "paper.pdf", ContentType: "application/pdf", Size: 123,
		MD5: "0123456789abcdef0123456789abcdef",
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, field := range []string{
		"status", "stage", "parentKey", "attachmentKey", "filename", "contentType",
		"size", "md5", "fileStatus", "verification", "failure",
	} {
		if _, ok := fields[field]; !ok {
			t.Errorf("attachment import omitted %q: %s", field, encoded)
		}
	}
	if len(fields) != 11 {
		t.Errorf("fields = %#v, want exactly 11", fields)
	}
	if fields["attachmentKey"] != nil || fields["fileStatus"] != nil || fields["verification"] != nil || fields["failure"] != nil {
		t.Errorf("planned nullable fields = %#v", fields)
	}
}

func TestAttachmentImportVerificationOK(t *testing.T) {
	verified := AttachmentImportVerification{
		Parent: true, ManagedStorage: true, Title: true, SourceURL: true,
		Filename: true, ContentType: true, Size: true, Checksum: true,
		ActualFilename: "Example Paper.pdf",
	}
	if !verified.OK() {
		t.Fatal("complete verification is not OK")
	}
	verified.Size = false
	if verified.OK() {
		t.Fatal("incomplete verification is OK")
	}
	encoded, err := json.Marshal(verified)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"actualFilename":"Example Paper.pdf"`) {
		t.Fatalf("verification omitted actual filename: %s", encoded)
	}
}
