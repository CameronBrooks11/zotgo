# Reading your library

All read commands work against either endpoint (local by default, or the Web API
under `--web` — see [profiles](profiles.md)).

```bash
zot list                       # top-level items (default 25)
zot list -c "Smart Grid" -n 50 # items in a collection, by name or key
zot list --tag ml --tag review # items with all the given tags
zot search "state estimation"  # search by title/creator/year
zot search algae --everything  # include full text and notes
zot show HRAC4E44              # one item with its attachments and notes
zot collections               # collections as a tree (--flat for a list)
zot stats                     # library-wide counts
```

Global flags: `--library`/`-L` selects a group library (by name or id; default is
My Library), and `--url` overrides the endpoint address.

## Export

`export` hands off to Zotero's own translators, so no bibliography formatting is
reimplemented here:

```bash
zot export bibtex -c Polyhedra   # BibTeX (from Zotero), scoped to a collection
zot export csljson -o refs.json  # -o writes to a file (atomically) instead of stdout
zot export ris                   # ris, biblatex, csv, mods, tei, rdf_* …
zot export summary-md            # zotgo's own summary shapes: json, summary-csv, summary-md
```

The Zotero translators are `bibtex`, `biblatex`, `csljson`, `csv`, `mods`, `ris`,
`tei`, and the `rdf_*` variants. zotgo shapes only `json`, `summary-csv`, and
`summary-md` itself.

`mods`, `tei`, and `rdf_*` wrap each page of results in a single XML root element,
so zotgo exports them only when the result fits in one page rather than emitting a
document with two roots; narrow the query with `-c`/`-t` if you hit that.

For scripting the output of any read command, see
[machine-readable output](machine-output.md).
