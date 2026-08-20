package zotero

import "testing"

func TestManagedAttachmentLinkMode(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mode    string
		managed bool
		wantErr bool
	}{
		{mode: "imported_file", managed: true},
		{mode: "imported_url", managed: true},
		{mode: "embedded_image", managed: true},
		{mode: "linked_file"},
		{mode: "linked_url"},
		{mode: "future_mode", wantErr: true},
		{mode: "", wantErr: true},
	} {
		t.Run(test.mode, func(t *testing.T) {
			t.Parallel()
			managed, err := ManagedAttachmentLinkMode(test.mode)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("classify link mode: %v", err)
			}
			if managed != test.managed {
				t.Fatalf("managed = %t, want %t", managed, test.managed)
			}
		})
	}
}
