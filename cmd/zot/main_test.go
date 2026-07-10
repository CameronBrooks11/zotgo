package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/CameronBrooks11/zotgo/internal/output"
)

// fakeZotero serves the Local API and connector routes the read commands use.
func fakeZotero(localAPIEnabled bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connector/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zotero-Version", "9.9.9")
		_, _ = w.Write([]byte("Zotero is running"))
	})

	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !localAPIEnabled {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Local API is not enabled"))
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("GET /api/users/0/items/top", guard(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("format") {
		case "bibtex":
			w.Header().Set("Content-Type", "application/x-bibtex")
			_, _ = w.Write([]byte("@article{posten2009,\n\ttitle = {Algae paper}\n}\n"))
			return
		case "csljson":
			_, _ = w.Write([]byte(`[{"id":"AAAA1111","type":"article-journal"}]`))
			return
		case "csv":
			// Zotero's native CSV, distinct from zotgo's summary-csv.
			w.Header().Set("Content-Type", "text/csv")
			// Zotero's CSV translator prefixes every page with a UTF-8 BOM.
			_, _ = w.Write([]byte("\ufeff\"Key\",\"Item Type\",\"Title\"\n\"AAAA1111\",\"journalArticle\",\"Algae paper\"\n"))
			return
		case "ris":
			_, _ = w.Write([]byte("TY  - JOUR\nTI  - Algae paper\nER  -\n"))
			return
		case "mods":
			_, _ = w.Write([]byte(`<modsCollection><mods/></modsCollection>`))
			return
		}
		w.Header().Set("Total-Results", "2")
		w.Header().Set("Zotero-Schema-Version", "42")
		_, _ = w.Write([]byte(`[
			{"key":"AAAA1111","version":7,"data":{"key":"AAAA1111","version":7,"itemType":"journalArticle","title":"Algae paper"},"meta":{"creatorSummary":"Posten","parsedDate":"2009"}},
			{"key":"BBBB2222","version":8,"data":{"key":"BBBB2222","version":8,"itemType":"book","title":"A Book"},"meta":{"creatorSummary":"Author"}}
		]`))
	}))
	mux.HandleFunc("GET /api/users/0/items/{key}/children", guard(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"key":"CHILD001","data":{"itemType":"attachment","title":"Full Text PDF"}}]`))
	}))
	mux.HandleFunc("GET /api/users/0/items/{key}", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("key") != "AAAA1111" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"key":"AAAA1111","data":{"key":"AAAA1111","itemType":"journalArticle","title":"Algae paper","tags":[{"tag":"ml"}]},"meta":{"creatorSummary":"Posten","parsedDate":"2009"}}`))
	}))
	mux.HandleFunc("GET /api/users/0/items", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Total-Results", "42")
		_, _ = w.Write([]byte("[]"))
	}))
	mux.HandleFunc("GET /api/users/0/collections/{key}/items/top", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("key") != "ROOT0001" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Total-Results", "1")
		_, _ = w.Write([]byte(`[{"key":"INCOL001","data":{"key":"INCOL001","itemType":"book","title":"In Research"}}]`))
	}))
	mux.HandleFunc("GET /api/users/0/collections", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Total-Results", "2")
		_, _ = w.Write([]byte(`[
			{"key":"ROOT0001","data":{"key":"ROOT0001","name":"Research","parentCollection":false}},
			{"key":"CHILD001","data":{"key":"CHILD001","name":"Subtopic","parentCollection":"ROOT0001"}}
		]`))
	}))
	mux.HandleFunc("GET /api/users/0/tags", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Total-Results", "7")
		_, _ = w.Write([]byte("[]"))
	}))
	return httptest.NewServer(mux)
}

func runCLI(url string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	root := rootCommand()
	root.Writer = &stdout
	root.ErrWriter = &stderr
	full := append([]string{"zot", "--url", url}, args...)
	err := root.Run(context.Background(), full)
	return stdout.String(), stderr.String(), err
}

func TestDoctorReady(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "doctor")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Ready") {
		t.Errorf("output missing Ready:\n%s", out)
	}
}

func TestDoctorDisabledExitsNonZero(t *testing.T) {
	srv := fakeZotero(false)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "doctor")
	var coder cli.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != 1 {
		t.Fatalf("err = %v, want ExitCoder(1)", err)
	}
	if !strings.Contains(out, "Local API disabled") {
		t.Errorf("output missing disabled guidance:\n%s", out)
	}
}

func TestListTable(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"AAAA1111", "Algae paper", "journalArticle", "2 items"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestListJSON(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var doc struct {
		Schema  int    `json:"schema"`
		Kind    string `json:"kind"`
		Library struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"library"`
		Data []struct {
			Key   string `json:"key"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"data"`
		Meta struct {
			Shown int `json:"shown"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if doc.Schema != output.SchemaVersion || doc.Kind != "items" {
		t.Errorf("schema/kind = %d/%q", doc.Schema, doc.Kind)
	}
	if doc.Library.Type != "user" || doc.Library.Name != "My Library" {
		t.Errorf("library = %+v", doc.Library)
	}
	if len(doc.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(doc.Data))
	}
	if doc.Data[0].Key != "AAAA1111" || doc.Data[0].Type != "journalArticle" {
		t.Errorf("first item = %+v", doc.Data[0])
	}
	if doc.Meta.Shown != 2 || doc.Meta.Total != 2 {
		t.Errorf("meta = %+v", doc.Meta)
	}
}

// --json must emit zotgo DTOs, never Zotero's envelope/data/meta split.
func TestListJSON_DoesNotLeakZoteroEnvelope(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Zotero names the field itemType and attaches links to every record; the DTO
	// calls it type and carries no links. (The document's own "meta" is a zotgo
	// field, so it is not a leak marker.)
	//
	// "version" is a leak marker too: an object version is scoped to the endpoint
	// that issued it, so the DTO contract carries none.
	for _, leak := range []string{`"itemType"`, `"links"`, `"version"`} {
		if strings.Contains(out, leak) {
			t.Errorf("Zotero envelope field %s leaked into --json:\n%s", leak, out)
		}
	}
}

// --raw is the escape hatch, so Zotero's own version must survive there. Without
// this, dropping the field from the DTOs could be mistaken for dropping the data.
func TestListRaw_KeepsZoteroVersion(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--raw", "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, `"version": 7`) && !strings.Contains(out, `"version":7`) {
		t.Errorf("--raw dropped Zotero's version:\n%s", out)
	}
}

// --raw is the escape hatch: Zotero's own shape, untouched.
func TestListRaw(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--raw", "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var envelopes []map[string]any
	if err := json.Unmarshal([]byte(out), &envelopes); err != nil {
		t.Fatalf("--raw is not a bare Zotero array: %v\n%s", err, out)
	}
	if len(envelopes) != 2 {
		t.Fatalf("len = %d, want 2", len(envelopes))
	}
	if _, ok := envelopes[0]["data"]; !ok {
		t.Errorf("--raw lost Zotero's data field: %v", envelopes[0])
	}
}

// Every jsonl line must stand alone: valid JSON, with its own schema and kind.
func TestListJSONL(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--jsonl", "list")
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), out)
	}
	for i, line := range lines {
		var doc struct {
			Schema int    `json:"schema"`
			Kind   string `json:"kind"`
			Data   struct {
				Key string `json:"key"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line %d not valid JSON: %v\n%s", i, err, line)
		}
		if doc.Schema != output.SchemaVersion || doc.Kind != "item" {
			t.Errorf("line %d schema/kind = %d/%q", i, doc.Schema, doc.Kind)
		}
		if doc.Data.Key == "" {
			t.Errorf("line %d has no item key: %s", i, line)
		}
	}
}

func TestOutputModesAreMutuallyExclusive(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	for _, flags := range [][]string{
		{"--json", "--jsonl"},
		{"--json", "--raw"},
		{"--jsonl", "--raw"},
	} {
		args := append(append([]string{}, flags...), "list")
		if _, _, err := runCLI(srv.URL, args...); err == nil ||
			!strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("%v: err = %v, want a mutual-exclusion error", flags, err)
		}
	}
}

// stats is derived from response headers; there is no raw payload to show.
func TestStatsRawIsRefused(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "--raw", "stats")
	if err == nil || !strings.Contains(err.Error(), "no raw Zotero response") {
		t.Fatalf("err = %v, want ErrRawUnavailable", err)
	}
}

func TestStatsJSON(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "stats")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var doc struct {
		Kind string `json:"kind"`
		Data struct {
			Items int `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if doc.Kind != "stats" {
		t.Errorf("kind = %q", doc.Kind)
	}
}

// show nests children under the item, so kind:"item" means one shape everywhere.
func TestShowJSON(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "show", "AAAA1111")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var doc struct {
		Kind string `json:"kind"`
		Data struct {
			Key      string `json:"key"`
			Children []struct {
				Key  string `json:"key"`
				Type string `json:"type"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if doc.Kind != "item" {
		t.Errorf("kind = %q, want item", doc.Kind)
	}
	if doc.Data.Key != "AAAA1111" {
		t.Errorf("key = %q", doc.Data.Key)
	}
	if len(doc.Data.Children) != 1 || doc.Data.Children[0].Type != "attachment" {
		t.Errorf("children = %+v", doc.Data.Children)
	}
}

// doctor reports machine-readable health, and still exits non-zero when broken.
func TestDoctorJSON(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "doctor")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var doc struct {
		Kind string `json:"kind"`
		Data struct {
			Ready           bool `json:"ready"`
			LocalAPIEnabled bool `json:"localApiEnabled"`
			Endpoint        struct {
				Kind    string `json:"kind"`
				BaseURL string `json:"baseUrl"`
			} `json:"endpoint"`
			Capabilities []struct {
				Name      string `json:"name"`
				Supported bool   `json:"supported"`
				Reason    string `json:"reason"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if doc.Kind != "health" || !doc.Data.Ready || !doc.Data.LocalAPIEnabled {
		t.Fatalf("doc = %+v", doc)
	}
	if doc.Data.Endpoint.Kind != "local" || doc.Data.Endpoint.BaseURL != srv.URL {
		t.Errorf("endpoint = %+v", doc.Data.Endpoint)
	}

	caps := map[string]bool{}
	reasons := map[string]string{}
	for _, c := range doc.Data.Capabilities {
		caps[c.Name] = c.Supported
		reasons[c.Name] = c.Reason
	}
	for _, want := range []string{"read", "write", "connector-ingest", "local-file-access"} {
		if _, ok := caps[want]; !ok {
			t.Errorf("capability %q missing from --json", want)
		}
	}
	if !caps["read"] {
		t.Error("read should be supported against a healthy fake")
	}
	// The load-bearing claim: local writes do not exist, and we say why.
	if caps["write"] {
		t.Error("write must not be advertised")
	}
	if !strings.Contains(reasons["write"], "5015") {
		t.Errorf("write reason should cite the upstream issue, got %q", reasons["write"])
	}
	// A supported capability carries no reason.
	if reasons["read"] != "" {
		t.Errorf("supported capability carries a reason: %q", reasons["read"])
	}
}

// doctor's human output must name each capability and explain the missing ones.
func TestDoctorHumanListsCapabilities(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "doctor")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{
		"Capabilities:", "read", "write", "connector-ingest", "local-file-access",
		"zotero/zotero#5015", "local endpoint",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}

// With Zotero down, nothing is supported and every line explains itself.
func TestDoctorHumanCapabilitiesWhenDown(t *testing.T) {
	srv := fakeZotero(true)
	url := srv.URL
	srv.Close() // now refusing connections

	out, _, err := runCLI(url, "doctor")
	if err == nil {
		t.Fatal("expected a non-zero exit")
	}
	for _, want := range []string{"Zotero not running", "Capabilities:", "Start the Zotero"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "✓") {
		t.Errorf("a capability is marked supported while Zotero is down:\n%s", out)
	}
}

func TestDoctorJSONDisabledExitsNonZero(t *testing.T) {
	srv := fakeZotero(false)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "--json", "doctor")
	if err == nil {
		t.Fatal("expected a non-zero exit when the Local API is off")
	}
	// The payload must still be emitted, so a script can read why.
	var doc struct {
		Data struct {
			Ready bool `json:"ready"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("no JSON payload alongside the failure: %v\n%s", err, out)
	}
	if doc.Data.Ready {
		t.Error("ready = true despite a disabled Local API")
	}
}

func TestShowNotFound(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "show", "ZZZZ9999")
	if err == nil || !strings.Contains(err.Error(), "no item with key") {
		t.Fatalf("err = %v, want not-found message", err)
	}
}

func TestShowRendersChildren(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "show", "AAAA1111")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"Algae paper", "Children (1)", "Full Text PDF"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestStats(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "stats")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"My Library", "42", "7"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats output missing %q:\n%s", want, out)
		}
	}
}

func TestCollectionsTree(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "collections")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Research") || !strings.Contains(out, "Subtopic") {
		t.Fatalf("tree missing nodes:\n%s", out)
	}
}

func TestListByCollectionName(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	// "Research" must resolve to key ROOT0001 and route to its items.
	out, _, err := runCLI(srv.URL, "list", "-c", "Research")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "In Research") || !strings.Contains(out, "INCOL001") {
		t.Errorf("collection-name routing failed:\n%s", out)
	}
}

func TestListUnknownCollection(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "list", "-c", "Nonexistent")
	if err == nil || !strings.Contains(err.Error(), "no collection matching") {
		t.Fatalf("err = %v, want no-collection message", err)
	}
}

func TestExportBibtex(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "export", "bib")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "@article{posten2009") {
		t.Errorf("bibtex output unexpected:\n%s", out)
	}
}

// summary-csv is zotgo's own shape, built from item envelopes.
func TestExportSummaryCSV(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "export", "summary-csv")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(out, "key,type,title") || !strings.Contains(out, "AAAA1111") {
		t.Errorf("summary-csv output unexpected:\n%s", out)
	}
}

// Plain `csv` now means Zotero's own translator, not zotgo's summary.
func TestExportNativeCSV(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "export", "csv")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Zotero emits a UTF-8 BOM so spreadsheets detect the encoding; zotgo
	// rejoins pages without stripping it from the document.
	if !strings.HasPrefix(out, "\ufeff") {
		t.Errorf("native csv lost Zotero's UTF-8 BOM:\n%q", out)
	}
	if !strings.HasPrefix(strings.TrimPrefix(out, "\ufeff"), "Key,Item Type,Title") {
		t.Errorf("expected Zotero's native csv header, got:\n%s", out)
	}
}

// A translator zotgo never had a bespoke method for now works generically.
func TestExportRIS(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	out, _, err := runCLI(srv.URL, "export", "ris")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "TY  - JOUR") {
		t.Errorf("ris output unexpected:\n%s", out)
	}
}

// md and markdown still resolve, now to summary-md.
func TestExportMarkdownAlias(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	for _, alias := range []string{"md", "markdown", "summary-md"} {
		out, _, err := runCLI(srv.URL, "export", alias)
		if err != nil {
			t.Fatalf("%s: err = %v", alias, err)
		}
		if !strings.Contains(out, "Algae paper") {
			t.Errorf("%s output unexpected:\n%s", alias, out)
		}
	}
}

func TestExportUnknownFormat(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "export", "pdf")
	if err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("err = %v, want unknown-format error", err)
	}
}

func TestExportToFile(t *testing.T) {
	srv := fakeZotero(true)
	defer srv.Close()
	path := t.TempDir() + "/out.bib"
	_, errOut, err := runCLI(srv.URL, "export", "bibtex", "-o", path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("output file not written: %v", readErr)
	}
	if !strings.Contains(string(data), "@article{posten2009") {
		t.Errorf("file content unexpected:\n%s", data)
	}
	if !strings.Contains(errOut, "wrote") {
		t.Errorf("expected 'wrote' diagnostic on stderr, got %q", errOut)
	}
}

func TestZoteroDownFriendlyMessage(t *testing.T) {
	srv := fakeZotero(true)
	url := srv.URL
	srv.Close()
	_, _, err := runCLI(url, "list")
	if err == nil || !strings.Contains(err.Error(), "cannot reach Zotero") {
		t.Fatalf("err = %v, want friendly down message", err)
	}
}

func TestLocalAPIDisabledFriendlyMessage(t *testing.T) {
	srv := fakeZotero(false)
	defer srv.Close()
	_, _, err := runCLI(srv.URL, "stats")
	if err == nil || !strings.Contains(err.Error(), "Local API is disabled") {
		t.Fatalf("err = %v, want friendly disabled message", err)
	}
}
