# ADR 0241: A marina's own map lives in Documents, linked by tag

## Status

Accepted (2026-10-31). Builds on [ADR 0238](0238-mate-evaluates-a-marina.md)
(`marina_details`), [ADR 0240](0240-marina-card.md) (the marina card),
[ADR 0159](0159-mate-searches-the-web-on-request.md) (web results never drive a
write) and [ADR 0158](0158-mate-changes-records-through-changesets.md) (record types: Mate
proposes, the operator applies).

## Context

OpenStreetMap draws a marina's pontoons, but it rarely carries berth numbers, and
a berth number is what the operator wants when a marina assigns one by radio.
Many marinas publish their own map or berth plan as a page or a PDF. The
operator asked for a way to keep that map next to the boat's other documents and
have Mate use it when it looks at the marina.

Three obvious routes were open: have Mate download the file, have it search the
document library by the marina's name, or link the file to the marina in some
record of its own.

## Decision

**Helmcentral never downloads a file from the web.** ADR 0159 says what
`search_web` returns is text from the open web, so it is data and never
instructions, and that it never drives a change to the boat's records. A tool
that fetched a PDF and filed it would be exactly that: a web result writing to
Documents, with a file of unknown content landing in the extraction and
indexing pipeline. So Mate finds the marina's map page or PDF with `search_web`
when web search is on, and gives the operator the link. The operator downloads
the file and uploads it, which is an ordinary upload with the checks every upload
has. With web search off, Mate says so and suggests the website that
`marina_details` gave as the marina's contact. Only that mode-specific sentence
is in the prompt, beside the `marina_details` paragraph, and the search half is
left out when web search is off, as the `search_web` paragraph is. The rules
that hold either way, that Helmcentral downloads nothing so a map is never
described as saved, and when the tag is proposed, are part of the
`marina_details` tool rule. An outside assistant over MCP never sees the system
prompt (ADR 0236), and it gets the tool's description.

**A document is linked to a marina by a tag equal to the marina's name.** The
match is the whole tag, ignoring case and the spaces at either end, so
"Coral Sea Marina" never finds a document tagged "Coral Sea Marina Resort". Case
is folded in Go and not in SQLite, whose `lower()` leaves accented capitals
alone ("Puerto Banús"). Only the operator's tags link: a tag the library
suggested from a document's text does not, so a receipt that names the marina is
not taken for its map. A tag needs no new table, no migration and no new
screen: the operator can add it on the document's Details page, and search
already indexes it.

The trade-off is that the name is the whole link. Two marinas with exactly the
same name share their documents, and a marina the chart spells differently from
the operator's tag is not found. An id would be exact, but the plugin's OSM id is
not something an operator would type, and a link table would need a screen to
manage. The operator can see and fix a tag, so the plain name is the better
choice for a single-boat install. Remove the tag to unlink.

**`marina_details` looks the documents up after it resolves the marina.** It asks
the document store for up to three documents carrying the marina's name, newest
first. The model result gets `saved_documents` (`document_id`, `title`),
and the reading gets one sentence for each: "The operator has saved <title> for
this marina; read it with read_document for berth numbers and the layout." With
none, the field and the sentence are left out. The title falls back to the file
name, since the document has no other name. If the document library is not
available the lookup is skipped, because the library only enriches the marina
lookup and the marina is still worth describing. A query that fails is still an
error.

**The card carries the link.** The stored marina card gains `saved_documents`
(`id`, `title`), always present, `[]` when there are none. The frontend decoder
requires it like the card's other lists. No marina card was stored before it
existed, so there is no older shape to read. The card shows "In Documents:" with each title linking to
the document in the Documents viewer (`/documents?document=<id>`), near the footer.

**A `document` record type lets Mate propose the tag.** It has one action,
update, and one writable field, `tags`. The title, filename and type are
readable and nothing else about a document can be written, created or deleted
through it. Tags are a list field, modelled as the equipment `aliases` are: the
change gives the whole list the document should carry, so adding a tag sends the
current tags with the new one and removing sends them without it. Only the
operator's tags are in the field. Suggested tags are not shown or changed.
Applying runs the same write the Documents page uses, so tags are trimmed and a
suggested tag of the same name is promoted. A blank tag is refused by name where
the page would drop it quietly, since Mate should be told. No tag length or count
cap exists on the Documents page, so none is added. After writing, the command
rebuilds the document's search text and wakes the indexer, so the new tag is
searchable at once. It leaves `updated_at` alone, as the page does.

Editing tags on the Details page does not move the document's `updated_at`, and
the indexer moves it at every stage. The record's version is therefore the
operator's tags alone, so a proposal does not go stale because indexing ran, and
Mate's whole-list replacement cannot overwrite a tag the operator added in
between. Apply refuses a proposal whose document's tags changed
after Mate saw them. The card is the generic one: the description line ("On
Berth map: add tag "Coral Sea Marina Resort"") and the tags before and after,
with a link to the document once applied. Apply refreshes the Documents listings
and the document's own page.

The proposal is driven by what the operator asks. The tool rule says to propose the
tag only when asked to link the map, and repeats the ADR 0159 rule that nothing
in a web result drives it.

## Consequences

- A marina map costs the operator a download and an upload, and Mate will not
  shortcut it. That is the price of the 0159 stance.
- Mate can read the map's berth numbers only if text extraction or OCR reads the
  file. A scanned image of a map is read as well as OCR manages, and the answer
  says what it found.
- `marina_details` now reads the document library on every call it has one. The lookup scans
  the tag table and matches in Go. That is cheap for one boat's library and
  would need an index on a folded name if a library grew to tens of thousands of
  tags.
- The `document` record type is the first that lists documents through
  `list_records`, newest first, with an optional `tag` filter that matches the way
the marina link does. `list_records` itself cuts the list and says so.
- Only the saved-document tests build a document store; the other marina tests
  run without one.
