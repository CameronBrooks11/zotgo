package output

import (
	"encoding/json"

	"github.com/CameronBrooks11/zotgo/internal/zotero"
)

// Library identifies the library a payload came from.
type Library struct {
	Type string `json:"type"`
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Item is one Zotero item, flattened from the API's envelope/data/meta split
// into the fields a script actually consumes.
//
// There is deliberately no version field. A Zotero object version is scoped to
// the endpoint that issued it, and the Local API's version has no durable
// meaning zotgo can promise:
//
//   - It is currently the *server* version, so it does not move when an item is
//     edited locally and not yet synced — change detection on it is unsound.
//   - Upstream zotero/zotero#5015 replaces it with a local clientVersion drawn
//     from an unrelated counter, silently changing what the number means.
//   - Feeding a local version to the Web API as a write precondition is a
//     data-integrity hazard the Zotero maintainers call out directly.
//
// Exposing a number with those properties invites exactly the misuse it cannot
// survive. `--raw` still carries Zotero's own version, unversioned and
// explicitly outside this contract. A properly endpoint-scoped version returns
// when writes do.
type Item struct {
	Key string `json:"key"`
	// Type is Zotero's itemType, e.g. "journalArticle".
	Type  string `json:"type"`
	Title string `json:"title"`
	// Date is the item's date field verbatim, in whatever form it was entered.
	Date string `json:"date,omitempty"`
	// ParsedDate is Zotero's normalized reading of Date, when it managed one.
	ParsedDate string `json:"parsedDate,omitempty"`
	// CreatorSummary is Zotero's short attribution, e.g. "Posten et al.".
	CreatorSummary string    `json:"creatorSummary,omitempty"`
	Creators       []Creator `json:"creators"`
	Tags           []Tag     `json:"tags"`
	// Collections holds the keys of the collections this item belongs to.
	Collections []string `json:"collections"`
	// NumChildren counts attachments and notes hanging off this item.
	NumChildren int `json:"numChildren"`
	// Children carries the attachments and notes themselves, but only where a
	// command fetched them (`show`). Absent elsewhere, so that `kind: "item"`
	// always denotes this one shape: a list omits it, a detail view fills it.
	Children []Item `json:"children,omitempty"`
}

// ItemMutation is the outcome, or dry-run plan, for one item write request.
// Index always refers to the caller's request order. Fields names the fields
// changed by a patch, without exposing the patch values themselves.
type ItemMutation struct {
	Index     int            `json:"index"`
	Operation string         `json:"operation"`
	Status    string         `json:"status"`
	Key       string         `json:"key,omitempty"`
	Type      string         `json:"type,omitempty"`
	Title     string         `json:"title,omitempty"`
	Fields    []string       `json:"fields,omitempty"`
	Failure   *MutationError `json:"failure,omitempty"`
}

// CollectionMutation is the outcome, or dry-run plan, for one collection write
// (create, rename, or delete). Index always refers to the caller's request
// order; for the single-collection create/rename it is 0.
type CollectionMutation struct {
	Index     int            `json:"index"`
	Operation string         `json:"operation"`
	Status    string         `json:"status"`
	Key       string         `json:"key,omitempty"`
	Name      string         `json:"name,omitempty"`
	ParentKey string         `json:"parentKey,omitempty"`
	Failure   *MutationError `json:"failure,omitempty"`
}

// TagMutation is the outcome, or dry-run plan, for one tag write. Index refers
// to the caller's request order among the tag names given. Item is the item a
// tag was added to or removed from; it is empty for the library-wide delete,
// which strips the tag from every item at once.
type TagMutation struct {
	Index     int            `json:"index"`
	Operation string         `json:"operation"`
	Status    string         `json:"status"`
	Tag       string         `json:"tag"`
	Item      string         `json:"item,omitempty"`
	Failure   *MutationError `json:"failure,omitempty"`
}

// MutationError is a structured rejection returned for a failed write.
type MutationError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

// Creator is one author, editor, or other contributor.
//
// Zotero stores a creator either as a first/last pair or as a single
// undifferentiated name, never both. Exactly one of the two forms is populated.
type Creator struct {
	Type      string `json:"type"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Name      string `json:"name,omitempty"`
}

// Tag is one tag. Automatic tags were added by Zotero (from an import or a
// translator); the rest were typed by a human.
type Tag struct {
	Name      string `json:"name"`
	Automatic bool   `json:"automatic"`
}

// Note is one note's stable metadata and exact Zotero rich-text HTML.
type Note struct {
	Key          string `json:"key"`
	ParentKey    string `json:"parentKey"`
	DateAdded    string `json:"dateAdded"`
	DateModified string `json:"dateModified"`
	Tags         []Tag  `json:"tags"`
	HTML         string `json:"html"`
}

// Attachment is one attachment's stable metadata.
type Attachment struct {
	Key          string               `json:"key"`
	ParentKey    string               `json:"parentKey"`
	Title        string               `json:"title"`
	LinkMode     string               `json:"linkMode"`
	ContentType  string               `json:"contentType"`
	Charset      string               `json:"charset"`
	Filename     string               `json:"filename"`
	URL          string               `json:"url"`
	AccessDate   string               `json:"accessDate"`
	DateAdded    string               `json:"dateAdded"`
	DateModified string               `json:"dateModified"`
	Tags         []Tag                `json:"tags"`
	MD5          *string              `json:"md5"`
	MTime        *int64               `json:"mtime"`
	Enclosure    *AttachmentEnclosure `json:"enclosure"`
}

// AttachmentEnclosure is the portable content-type and size metadata Zotero
// advertised for an attachment. The download href is deliberately excluded from
// the stable record: it is endpoint-scoped (the same attachment yields a
// localhost URL under the Local API and an api.zotero.org URL under --web), so
// it does not belong in a portable contract. Use --raw for Zotero's complete
// envelope, which still carries the href.
type AttachmentEnclosure struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Length *int64 `json:"length"`
}

// Collection is one collection, with its parent's key when nested. It carries no
// version, for the reasons given on Item.
type Collection struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	ParentKey string `json:"parentKey,omitempty"`
	NumItems  int    `json:"numItems"`
}

// Stats holds library-wide counts.
type Stats struct {
	Items       int `json:"items"`
	TopItems    int `json:"topItems"`
	Collections int `json:"collections"`
	Tags        int `json:"tags"`
}

// Endpoint identifies what zotgo is pointed at.
type Endpoint struct {
	// Kind is "local" for a running desktop Zotero.
	Kind    string `json:"kind"`
	BaseURL string `json:"baseUrl"`
}

// Capability is one thing the endpoint can or cannot do, with the reason when it
// cannot. It describes the endpoint, not what zotgo has implemented against it.
type Capability struct {
	Name      string `json:"name"`
	Supported bool   `json:"supported"`
	// Reason is present only when Supported is false.
	Reason string `json:"reason,omitempty"`
}

// Health reports what zotgo can reach, as `doctor` sees it. Endpoint.Kind fixes
// which of the endpoint-specific fields are present: a local probe carries
// zoteroRunning/localApiEnabled, a web probe carries keyValid/userId, and the
// other set is omitted rather than reported as a misleading false.
type Health struct {
	Endpoint Endpoint `json:"endpoint"`
	// Reachable is the shared liveness signal: the endpoint answered at all.
	Reachable bool `json:"reachable"`

	// Local endpoint only.
	ZoteroRunning   *bool  `json:"zoteroRunning,omitempty"`
	ZoteroVersion   string `json:"zoteroVersion,omitempty"`
	LocalAPIEnabled *bool  `json:"localApiEnabled,omitempty"`

	// Web endpoint only.
	KeyValid *bool `json:"keyValid,omitempty"`
	UserID   int64 `json:"userId,omitempty"`

	// SchemaVersion and APIVersion are Zotero's, not zotgo's.
	SchemaVersion string `json:"zoteroSchemaVersion,omitempty"`
	APIVersion    string `json:"zoteroApiVersion,omitempty"`
	// Ready is true when every surface zotgo needs for reads is usable.
	Ready bool `json:"ready"`
	// Capabilities always lists every known capability, supported or not, so a
	// script can test a field rather than probe for a key's absence.
	Capabilities []Capability `json:"capabilities"`
}

// zotero tag types: 0 is a manually entered tag, 1 one Zotero added itself.
const automaticTagType = 1

// NewLibrary converts a resolved library reference.
func NewLibrary(ref zotero.LibraryRef) *Library {
	return &Library{Type: ref.Kind, ID: ref.ID, Name: ref.Name}
}

// NewItem flattens a Zotero item envelope into an Item DTO. A malformed data
// payload yields the fields that did parse rather than an error: the envelope's
// key and version are always trustworthy, and a script is better served by a
// partial record than by a failed command.
func NewItem(e zotero.Envelope) Item {
	data, _ := e.ItemData()

	creators := make([]Creator, 0, len(data.Creators))
	for _, c := range data.Creators {
		creators = append(creators, Creator{
			Type:      c.CreatorType,
			FirstName: c.FirstName,
			LastName:  c.LastName,
			Name:      c.Name,
		})
	}

	tags := make([]Tag, 0, len(data.Tags))
	for _, t := range data.Tags {
		tags = append(tags, Tag{Name: t.Tag, Automatic: t.Type == automaticTagType})
	}

	collections := data.Collections
	if collections == nil {
		collections = []string{}
	}

	return Item{
		Key:            e.Key,
		Type:           data.ItemType,
		Title:          data.Title,
		Date:           data.Date,
		ParsedDate:     e.ParsedDate(),
		CreatorSummary: e.CreatorSummary(),
		Creators:       creators,
		Tags:           tags,
		Collections:    collections,
		NumChildren:    e.NumChildren(),
	}
}

// NewItemWithChildren flattens an item together with its attachments and notes.
func NewItemWithChildren(item zotero.Envelope, children []zotero.Envelope) Item {
	detail := NewItem(item)
	detail.Children = NewItems(children)
	return detail
}

// NewItems flattens a page of item envelopes.
func NewItems(envelopes []zotero.Envelope) []Item {
	items := make([]Item, 0, len(envelopes))
	for _, e := range envelopes {
		items = append(items, NewItem(e))
	}
	return items
}

// NewNote converts note metadata to the stable output contract.
func NewNote(note zotero.Note) Note {
	tags := make([]Tag, 0, len(note.Tags))
	for _, tag := range note.Tags {
		tags = append(tags, Tag{Name: tag.Tag, Automatic: tag.Type == automaticTagType})
	}
	return Note{
		Key:          note.Key,
		ParentKey:    note.ParentKey,
		DateAdded:    note.DateAdded,
		DateModified: note.DateModified,
		Tags:         tags,
		HTML:         note.HTML,
	}
}

// NewAttachment converts attachment metadata to the stable output contract.
func NewAttachment(attachment zotero.Attachment) Attachment {
	tags := make([]Tag, 0, len(attachment.Tags))
	for _, tag := range attachment.Tags {
		tags = append(tags, Tag{Name: tag.Tag, Automatic: tag.Type == automaticTagType})
	}
	var enclosure *AttachmentEnclosure
	if attachment.Enclosure != nil {
		enclosure = &AttachmentEnclosure{
			Type:   attachment.Enclosure.Type,
			Title:  attachment.Enclosure.Title,
			Length: attachment.Enclosure.Length,
		}
	}
	return Attachment{
		Key:          attachment.Key,
		ParentKey:    attachment.ParentKey,
		Title:        attachment.Title,
		LinkMode:     attachment.LinkMode,
		ContentType:  attachment.ContentType,
		Charset:      attachment.Charset,
		Filename:     attachment.Filename,
		URL:          attachment.URL,
		AccessDate:   attachment.AccessDate,
		DateAdded:    attachment.DateAdded,
		DateModified: attachment.DateModified,
		Tags:         tags,
		MD5:          attachment.MD5,
		MTime:        attachment.MTime,
		Enclosure:    enclosure,
	}
}

// NewCollection flattens a Zotero collection envelope.
func NewCollection(e zotero.Envelope) Collection {
	data, _ := e.CollectionData()
	return Collection{
		Key:       e.Key,
		Name:      data.Name,
		ParentKey: data.ParentKey(),
		NumItems:  metaInt(e.Meta["numItems"]),
	}
}

// NewCollections flattens a page of collection envelopes.
func NewCollections(envelopes []zotero.Envelope) []Collection {
	cols := make([]Collection, 0, len(envelopes))
	for _, e := range envelopes {
		cols = append(cols, NewCollection(e))
	}
	return cols
}

// NewStats converts library counts.
func NewStats(s zotero.Stats) Stats {
	return Stats{Items: s.Items, TopItems: s.TopItems, Collections: s.Collections, Tags: s.Tags}
}

// NewHealth converts a doctor probe.
func NewHealth(h zotero.Health) Health {
	statuses := h.Capabilities()
	caps := make([]Capability, 0, len(statuses))
	for _, s := range statuses {
		caps = append(caps, Capability{
			Name:      string(s.Name),
			Supported: s.Supported,
			Reason:    s.Reason,
		})
	}
	out := Health{
		Endpoint:      Endpoint{Kind: string(h.Endpoint.Kind), BaseURL: h.Endpoint.BaseURL},
		Reachable:     h.Reachable,
		SchemaVersion: h.SchemaVersion,
		APIVersion:    h.APIVersion,
		Ready:         h.Ready(),
		Capabilities:  caps,
	}
	if h.Endpoint.Kind == zotero.EndpointWeb {
		keyValid := h.KeyValid
		out.KeyValid = &keyValid
		out.UserID = h.WebUserID
	} else {
		running, enabled := h.ZoteroRunning, h.LocalAPIEnabled
		out.ZoteroRunning = &running
		out.LocalAPIEnabled = &enabled
		out.ZoteroVersion = h.ZoteroVersion
	}
	return out
}

func metaInt(raw json.RawMessage) int {
	var n int
	if len(raw) == 0 {
		return 0
	}
	_ = json.Unmarshal(raw, &n)
	return n
}
