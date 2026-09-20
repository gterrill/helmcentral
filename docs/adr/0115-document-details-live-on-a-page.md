# ADR 0115: A Document's Details Live on a Page, and Cost Lives With Them

## Status
Accepted. Amends ADR 0106's Documents panel (F1) and the wire shape its
handlers return. Nothing in ADR 0106 or ADR 0108 is reversed.

## Context

ADR 0106 shipped the document library with a listing table of seven columns,
one of them Cost: what reading that document has cost in OpenRouter charges,
added up across every reindex. Two things about that column were wrong.

It was in the wrong place. Cost is a fact about one document, consulted when
someone asks what the library is costing or why a particular scan was
expensive. It is not a fact anyone browses on: no one picks which manual to
open by what it cost to read. Meanwhile it took a column's width away from a
table that also has to carry a name, a status, a tag list and a size, on a
nav-station laptop.

It was also rounded past usefulness. The column printed three decimal places,
and the real figures ADR 0106 measured are fractions of a cent: a photographed
receipt cost $0.0012, which the column showed as $0.001. A number that has to
round its own typical value to one significant figure is decoration.

At the same time the panel had no place to edit anything a document carries.
ADR 0106 gave a document a title, notes and operator tags, and the store has
always accepted all three in one `PATCH /api/documents/:id`. The panel exposed
exactly one of them, as Rename, which patches `title` alone. Notes could be
written at upload and never changed. Tags could be typed into the upload form
and never corrected. Mate's suggested tags could be looked at and never kept.
The API was complete and the interface reached about a third of it.

The Move dialog had a smaller version of the same gap. Its folder picker
browses to a destination, and there is no destination until a folder exists,
so filing the first receipt of a new year meant cancelling the move, creating
`2026` from the toolbar, finding the document again and moving it. The picker
already holds its own `useDocuments` instance, so it already had everything it
needed to create one.

## Decision

### 1. Cost comes off the listing

The Cost column is removed outright, not narrowed or moved. The listing is for
browsing; the figure belongs to the document, and the document now has a page.

### 2. `/documents/<id>` is that page

A document's Details page is a route, not a dialog: it can be linked, it
survives Back and Forward, and it is a place someone goes rather than a thing
that appears over what they were doing. This follows ADR 0112's index/editor
split exactly - `/wall-displays` and `/wall-displays/<slug>` - with App.tsx
resolving which of the two renders and the page resolving its own document by
id.

The current folder rides along as `?folder=<id>`. Without it, Back out of the
page would return the listing to the root rather than the folder the operator
opened the document from.

The route deliberately uses a path segment where the existing viewer deep link
(`?document=<id>`, how a Mate attachment chip opens a file) uses a query
parameter. The two are different kinds of thing and the URLs say so: the
viewer is a dialog over the listing, which ADR 0074 keeps out of the path, and
Details is a navigation.

### 3. Title, notes and tags are edited together, and saved together

The page holds one draft of all three and sends them in one patch. It is not a
set of fields that each commit on blur, the way ADR 0112's display editor
commits, because a tag list has no blur worth committing on and because an
operator correcting a mis-OCR'd title while adding two tags means both or
neither, not a partial write if they navigate away mid-edit.

Rename stays on the row menu. It is the fast path for a title-only fix from
the listing, and losing it would make the common correction the slowest one.

### 4. A suggested tag is kept, not edited

Mate's suggested tags are shown beneath the operator's own, each with a Keep
action that moves its name into the operator set. Saving sends the operator
set, which is exactly what `UpdateMeta` already treats as the whole of it, and
the store promotes a name that was previously a suggestion rather than
inserting a duplicate row. There is no "delete this suggestion" action: a
suggestion is what the model said, and the honest way to be rid of one is to
reindex or to ignore it.

### 5. Cost is shown to four places, with what it covers

`$0.0012`, not `$0.001`, and a line under it saying the figure is what reading
the document has cost so far, added up across every reindex. The figure needs
that sentence: a document reindexed three times carries all three passes, and
a number with no stated scope invites the wrong reading.

### 6. The Move picker creates folders

The picker gains a name field and a Create folder action that creates in the
folder it is currently showing, then descends into the new folder so Move here
means the one just made. A rejected create (the 409 for a name already in use)
shows the server's own message and leaves the picker where it was: the
operator asked to create a folder, not to go anywhere.

### 7. A zero is a value, and the wire says so

`documentJSON` tagged its scalars `,omitempty`, so a document read locally -
no Mate, no OpenRouter, cost exactly zero - omitted `index_cost_usd`
altogether, and a document with no notes omitted `notes`. The TypeScript
`DocumentRecord` has always declared both as present. The listing happened to
survive the mismatch because it tested `index_cost_usd > 0` before formatting;
the Details page formats the figure unconditionally, and would have failed on
the most ordinary document in the library.

The tags come off the fields a client renders, rather than the client
defaulting the missing ones. A zero cost, an empty note and an unfiled
document are real values, and an absent key is not the same statement as
zero.

## What was rejected

- **A dialog for the same fields.** It would have fitted, and it would have
  been the third modal over a listing that already has a rename dialog, a move
  dialog and a viewer sheet. The read-only indexing facts have nowhere to live
  in a dialog sized for three inputs.
- **Keeping Cost as a narrower column.** Four decimals in a column is worse
  than three, not better, and the column's problem was never its width.
- **A per-pass cost breakdown.** The store keeps one running total by design
  (ADR 0106), and reconstructing per-pass figures would mean a schema change
  for a number nobody has asked for.
- **Editing tags inline in the listing.** A tag editor in a table cell is a
  worse version of the same editor with less room, and it would have put the
  one destructive-ish edit (removing a tag) one mis-click from a row the
  operator was only scanning.
- **`?edit=<id>` instead of a path segment.** It parses, but it makes Details
  look like a variant of the listing rather than a place, and it would have
  left two query parameters (`document`, `edit`) meaning two different ways of
  opening the same file.

## Consequences

- The listing is six columns. A future column has room without another fight.
- Every field the document API accepts is now reachable from the interface.
- The Details page is the natural home for anything per-document that arrives
  later: a per-document reindex history, an OCR confidence report, a "used by
  Mate in these answers" list. None of that is built.
- `documentJSON` now emits its zero values. Responses grow by a few bytes per
  document, which is nothing against the summaries they already carry.

## Related

- ADR 0106: the document library, the panel, and the cost figures quoted here.
- ADR 0108: semantic search, which the Details page reports nothing about.
- ADR 0112: the index/editor route split this follows.
- ADR 0074: path URLs, and why a dialog stays out of one.
