package main

import "testing"

// The A1 read aliases must be the top-level read commands verbatim: `item list`,
// `item show`, and `item search` mirror `list`/`show`/`search`, and
// `collection list` mirrors `collections`. Equivalence is asserted on identical
// output so the aliases cannot drift from their primaries.
func TestReadAliasesMirrorTopLevel(t *testing.T) {
	cases := []struct {
		name    string
		alias   []string
		primary []string
	}{
		{"item list", []string{"item", "list"}, []string{"list"}},
		{"item show", []string{"item", "show", "AAAA1111"}, []string{"show", "AAAA1111"}},
		{"item search", []string{"item", "search", "algae"}, []string{"search", "algae"}},
		{"collection list", []string{"collection", "list"}, []string{"collections"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeZotero(true)
			defer srv.Close()

			aliasOut, _, err := runCLI(srv.URL, append([]string{"--json"}, tc.alias...)...)
			if err != nil {
				t.Fatalf("alias %v: %v", tc.alias, err)
			}
			primaryOut, _, err := runCLI(srv.URL, append([]string{"--json"}, tc.primary...)...)
			if err != nil {
				t.Fatalf("primary %v: %v", tc.primary, err)
			}
			if aliasOut != primaryOut {
				t.Errorf("alias output differs from primary:\nalias:   %s\nprimary: %s", aliasOut, primaryOut)
			}
		})
	}
}
