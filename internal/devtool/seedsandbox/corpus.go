package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// corpusMarker tags every item this tool creates. Zotero's import silently
// creates duplicates with no dedup and nothing in the response marking a match,
// so "already seeded" has to be answered by looking, not by hoping.
const corpusMarker = "zotgo-sandbox-corpus"

// corpusSize is the number of bibliographic items seeded. It is over 100 on
// purpose: the two bugs this corpus exists to catch both hid below a threshold —
// a CSV encoding fault that only showed on real libraries, and six live tests
// that skipped whenever a library exceeded 30 items and had therefore never run
// once. A corpus that does not cross those boundaries cannot reproduce either.
const corpusSize = 120

// corpusAttachmentTitle identifies the one attachment the corpus creates, so a
// re-run recognizes it wherever it hangs.
const corpusAttachmentTitle = "Sandbox corpus PDF"

type report struct {
	items       int
	collections int
	attachments int
	annotations int
	skipped     []string
}

func (r *report) skip(reason string) { r.skipped = append(r.skipped, reason) }

// seed builds the corpus. Bibliographic items go in through the Connector, which
// works as far back as Zotero 7.0 and needs no write API at all — so the item
// half of the corpus exists at every version the matrix will test. Everything
// else needs the local write API and is skipped, loudly, where there is none.
func seed(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, baseURL string, writable bool) (report, error) {
	var r report

	existing, err := countCorpusItems(ctx, c, lib)
	if err != nil {
		return r, err
	}
	if existing >= corpusSize {
		r.items = existing
		r.skip(fmt.Sprintf("%d corpus items already present; not re-importing", existing))
	} else {
		imported, err := importCorpus(ctx, baseURL, existing)
		if err != nil {
			return r, err
		}
		r.items = existing + imported
	}

	if !writable {
		r.skip("no local write API on this Zotero: collections, attachments and annotations were not created")
		return r, nil
	}

	collections, err := seedCollections(ctx, c, lib)
	if err != nil {
		return r, err
	}
	r.collections = collections

	attachmentKey, err := seedAttachment(ctx, c, lib)
	if err != nil {
		return r, err
	}
	if attachmentKey == "" {
		r.skip("no item available to attach a file to")
		return r, nil
	}
	r.attachments = 1

	annotations, err := seedAnnotations(ctx, c, lib, attachmentKey)
	if err != nil {
		return r, err
	}
	r.annotations = annotations

	if err := seedRelations(ctx, c, lib); err != nil {
		return r, err
	}
	return r, nil
}

// seedRelations relates two corpus items to each other, so the relation mapping
// has something to check. Relations are a Zotero-URI graph rather than plain
// keys, and nothing else in the corpus produces one — without this the live
// relation check skips, and a skip is not a pass.
func seedRelations(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef) error {
	items, err := c.AllItems(ctx, lib, zotero.ItemsOptions{Tags: []string{corpusMarker}, Top: true, Limit: 2})
	if err != nil || len(items) < 2 {
		return err
	}

	// The URI form is Zotero's own: a relation names an item by URI, not by key,
	// and the local user library is users/0 on this route.
	uri := func(key string) string {
		return fmt.Sprintf("http://zotero.org/users/0/items/%s", key)
	}
	for i, item := range items {
		other := items[(i+1)%len(items)]
		patch, err := json.Marshal(map[string]any{
			"relations": map[string]any{"dc:relation": []string{uri(other.Key)}},
		})
		if err != nil {
			return err
		}
		if err := c.PatchItem(ctx, zotero.OpItemPatch, lib, item.Key, patch, item.Version); err != nil {
			return fmt.Errorf("relate %s to %s: %w", item.Key, other.Key, err)
		}
	}
	return nil
}

func countCorpusItems(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef) (int, error) {
	items, err := c.AllItems(ctx, lib, zotero.ItemsOptions{Tags: []string{corpusMarker}, Limit: 100})
	if err != nil {
		return 0, fmt.Errorf("count existing corpus items: %w", err)
	}
	return len(items), nil
}

// importCorpus posts generated BibTeX to the Connector in batches. Each call gets
// a fresh session id: the parameter is `session` on the query string, not a
// `sessionID` in the body as every other connector route uses, and reusing one
// answers 409.
func importCorpus(ctx context.Context, baseURL string, alreadyHave int) (int, error) {
	const batch = 30
	var imported int
	for start := alreadyHave; start < corpusSize; start += batch {
		end := start + batch
		if end > corpusSize {
			end = corpusSize
		}
		body := generateBibTeX(start, end)
		session := fmt.Sprintf("zotgo-seed-%d-%d", time.Now().UnixNano(), start)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			baseURL+"/connector/import?session="+session, strings.NewReader(body))
		if err != nil {
			return imported, err
		}
		// Required, and not authoritative: a missing Content-Type answers 500, but
		// the value does not decide the parser — Zotero sniffs the payload.
		req.Header.Set("Content-Type", "application/x-bibtex")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return imported, fmt.Errorf("connector import: %w", err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return imported, fmt.Errorf("connector import: HTTP %d (%s)", resp.StatusCode, strings.TrimSpace(string(payload)))
		}
		// The response under-reports: a BibTeX note field becomes a child item that
		// the returned array does not include. Count what we asked for and verify
		// against the library afterwards rather than trusting this number.
		imported += end - start
	}
	return imported, nil
}

func seedCollections(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef) (int, error) {
	// Three levels, because collection paths are rendered by walking
	// parentCollection upward and a two-level tree cannot tell a correct walk from
	// one that stops early.
	root, err := createCollection(ctx, c, lib, "Sandbox Corpus", "")
	if err != nil {
		return 0, err
	}
	mid, err := createCollection(ctx, c, lib, "Reactor Design", root)
	if err != nil {
		return 1, err
	}
	if _, err := createCollection(ctx, c, lib, "Photobioreactors — scale-up", mid); err != nil {
		return 2, err
	}
	return 3, nil
}

func createCollection(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, name, parent string) (string, error) {
	// Look first. Zotero happily creates a second collection with the same name
	// and parent, so a seeder that creates unconditionally doubles the tree on
	// every run — and the duplicate is invisible until something counts.
	if existing, err := findCollectionKey(ctx, c, lib, name); err == nil && existing != "" {
		return existing, nil
	}

	payload := map[string]any{"name": name}
	if parent != "" {
		payload["parentCollection"] = parent
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if _, err := c.CreateCollections(ctx, zotero.OpCollectionCreate, lib, []json.RawMessage{encoded}); err != nil {
		return "", fmt.Errorf("create collection %q: %w", name, err)
	}
	return findCollectionKey(ctx, c, lib, name)
}

// findCollectionKey looks the collection back up by name, because the batch
// create reports success without returning keys and a child collection needs its
// parent's.
func findCollectionKey(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, name string) (string, error) {
	collections, err := c.AllCollections(ctx, lib, zotero.CollectionsOptions{})
	if err != nil {
		return "", err
	}
	for _, collection := range collections {
		var data struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(collection.Data, &data); err != nil {
			continue
		}
		if data.Name == name {
			return collection.Key, nil
		}
	}
	return "", fmt.Errorf("collection %q was created but cannot be found", name)
}

// seedAttachment attaches a real PDF to a corpus item, so the live suite has
// something with bytes to resolve, download and annotate.
func seedAttachment(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef) (string, error) {
	// Look for the corpus attachment across the whole library, by its title.
	// Asking "does the first corpus item have an attachment" is not stable: the
	// item list has no guaranteed order, so a second run can pick a different
	// first item, find nothing, and attach a duplicate — with its own annotations
	// underneath.
	attachments, err := c.AllItems(ctx, lib, zotero.ItemsOptions{ItemType: "attachment", Limit: 100})
	if err != nil {
		return "", err
	}
	for _, envelope := range attachments {
		var data struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err == nil && data.Title == corpusAttachmentTitle {
			return envelope.Key, nil
		}
	}

	items, err := c.AllItems(ctx, lib, zotero.ItemsOptions{Tags: []string{corpusMarker}, Top: true, Limit: 100})
	if err != nil || len(items) == 0 {
		return "", err
	}
	// Deterministic choice, so the corpus is the same every time it is built.
	parent := items[0].Key
	for _, item := range items {
		if item.Key < parent {
			parent = item.Key
		}
	}

	metadata, err := json.Marshal(map[string]any{
		"itemType": "attachment", "linkMode": "imported_file", "parentItem": parent,
		"title": corpusAttachmentTitle, "contentType": "application/pdf",
	})
	if err != nil {
		return "", err
	}
	attachmentKey, err := createOneChecked(ctx, c, lib, zotero.OpAttachmentImport, metadata)
	if err != nil {
		return "", fmt.Errorf("create attachment metadata: %w", err)
	}

	if err := uploadCorpusPDF(ctx, c, lib, attachmentKey); err != nil {
		return "", err
	}
	return attachmentKey, nil
}

// seedAnnotations puts a highlight and a note on the corpus PDF, covering both
// annotation bodies. annotationPosition is required — omit it and Zotero answers
// 400 from a NOT NULL constraint deep in its own schema.
func seedAnnotations(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, attachmentKey string) (int, error) {
	existing, err := c.AllRawAnnotations(ctx, lib, attachmentKey)
	if err != nil {
		return 0, err
	}
	if len(existing) >= 2 {
		return len(existing), nil
	}

	annotations := []annotationPayload{
		{
			ItemType: "annotation", ParentItem: attachmentKey,
			AnnotationType: "highlight", Text: "Scale-up of photobioreactors",
			Color: "#ffd400", PageLabel: "1",
			SortIndex: "00000|000100|00100",
			Position:  `{"pageIndex":0,"rects":[[72,700,300,714]]}`,
		},
		{
			ItemType: "annotation", ParentItem: attachmentKey,
			AnnotationType: "note", Comment: "A note body, so both bodies are covered",
			Color: "#a28ae5", PageLabel: "1",
			SortIndex: "00000|000200|00200",
			Position:  `{"pageIndex":0,"rects":[[72,650,300,664]]}`,
		},
	}

	var created int
	for _, annotation := range annotations {
		encoded, err := json.Marshal(annotation)
		if err != nil {
			return created, err
		}
		if _, err := createOneChecked(ctx, c, lib, zotero.OpItemCreate, encoded); err != nil {
			return created, fmt.Errorf("create annotation: %w", err)
		}
		created++
	}
	return created, nil
}

// corpusPDF is a minimal one-page PDF. Written by hand rather than pulled from
// anywhere, so the corpus has no external dependency and no licensing question.
func corpusPDF() []byte {
	var buf bytes.Buffer
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		"<< /Length 68 >>\nstream\nBT /F1 18 Tf 72 700 Td (Scale-up of photobioreactors) Tj ET\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	offsets := make([]int, len(objects)+1)
	buf.WriteString("%PDF-1.4\n")
	for i, object := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// uploadCorpusPDF runs the three-phase managed upload: authorize with the file's
// metadata, send the bytes to the returned single-use URL, then register the
// upload key. Zotero deduplicates on MD5, so a re-run authorizes and finds the
// bytes already present rather than sending them twice.
func uploadCorpusPDF(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, attachmentKey string) error {
	pdf := corpusPDF()
	sum := md5.Sum(pdf) // #nosec G401 -- Zotero's upload protocol requires MD5.

	authorization, err := c.AuthorizeAttachmentUpload(ctx, lib, attachmentKey, zotero.AttachmentUploadMetadata{
		MD5:         hex.EncodeToString(sum[:]),
		Filename:    "sandbox-corpus.pdf",
		Size:        int64(len(pdf)),
		MTime:       time.Now().UnixMilli(),
		ContentType: "application/pdf",
	})
	if err != nil {
		return fmt.Errorf("authorize attachment upload: %w", err)
	}
	if authorization.Exists {
		return nil
	}
	if err := c.UploadAuthorizedAttachment(ctx, authorization, bytes.NewReader(pdf), int64(len(pdf))); err != nil {
		return fmt.Errorf("upload attachment bytes: %w", err)
	}
	return c.RegisterAttachmentUpload(ctx, lib, attachmentKey, authorization.UploadKey)
}

// createOneChecked writes one object and insists it actually landed.
//
// A batch write reports per-object failures *inside* its result, not as an
// error: Zotero answers 200 with the rejection in the body. Treating a nil error
// as success is how a seeder reports creating things it did not create — and a
// corpus that lies about its own contents is worse than no corpus, because the
// tests it feeds then pass for the wrong reason.
func createOneChecked(ctx context.Context, c *zotero.Client, lib zotero.LibraryRef, op zotero.Operation, payload json.RawMessage) (string, error) {
	result, err := c.CreateItemsReturningKeys(ctx, op, lib, []json.RawMessage{payload})
	if err != nil {
		return "", err
	}
	for _, failure := range result.Failed {
		//lint:ignore ST1005 "Zotero" is a proper noun.
		return "", fmt.Errorf("Zotero rejected it (HTTP %d): %s", failure.Code, failure.Message)
	}
	for _, key := range result.Successful {
		return key, nil
	}
	return "", fmt.Errorf("accepted but no key came back")
}

// annotationPayload exists because Zotero validates annotation fields in the
// order they arrive, and rejects the write with
//
//	400 annotationType must be set before other annotation properties
//
// if anything else comes first. A map cannot satisfy that: encoding/json sorts
// map keys, which puts annotationType last of all the annotation* fields. A
// struct encodes in declaration order, so the order is part of the type.
//
// annotationPosition is required — omitting it fails a NOT NULL constraint deep
// in Zotero's own schema, and the error arrives as raw SQL.
type annotationPayload struct {
	ItemType       string `json:"itemType"`
	ParentItem     string `json:"parentItem"`
	AnnotationType string `json:"annotationType"`
	Text           string `json:"annotationText,omitempty"`
	Comment        string `json:"annotationComment,omitempty"`
	Color          string `json:"annotationColor"`
	PageLabel      string `json:"annotationPageLabel"`
	SortIndex      string `json:"annotationSortIndex"`
	Position       string `json:"annotationPosition"`
}
