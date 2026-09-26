# Documents

The document library keeps files that matter to the boat, and keeps them
findable from the boat itself instead of an inbox ashore: manuals,
receipts, invoices, service logs, passage notes, a photo of a part number
or a data plate. Upload one and Helmcentral reads whatever text it can find
in it and indexes that for search. If Mate is switched on, it goes further:
a scan or a photo gets OCR'd properly, and the document gets a short
summary and a few suggested tags.

The sidebar's **Documents** panel is where this happens day to day: browse
folders, upload by button or drag-and-drop, search, and open, download,
rename, move, reindex or edit the details of anything already in the
library. A file can also be
attached straight from Mate's composer - see
[Documents](assistant.md#documents) on the Mate page - and a chip on a past
message opens the same viewer this panel uses.

Two rows in that same listing are worth their own page: notes you capture
yourself, and folders marked as a manual for reading front to back. See
[Notes and the manual](notes-and-the-manual.md).

## What gets indexed, and what it costs

Text files, and PDFs with a real text layer already in them (the kind
you can select and copy text from), are read locally the moment they're
uploaded. That needs no internet connection and no OpenRouter account, and
it happens whether or not Mate is switched on.

Switching Mate on adds three things automatically, for anything uploaded
(or explicitly reindexed - see below) while it's on:

- A scanned PDF or a photo gets properly OCR'd: the actual words on the
  page, not a guess from a thin or missing text layer.
- A short summary, two or three sentences.
- A handful of suggested tags.

This runs the moment a document is uploaded, with no button to press and
no per-document prompt, the same way a plugin already runs once you've
turned it on. It costs real money, and the real numbers are small: a
one-page scanned invoice cost $0.0023, the same document split across two
pages cost $0.0046, and a photographed receipt cost $0.0012, all measured
against a live OpenRouter account. Every document tracks what's actually
been spent reading it, added up across however many times it's been
reindexed, not just the last pass. That figure is on the document's own
Details page, under Indexing - see below.

**What leaves the boat:** with Mate off, nothing does. Local extraction
never talks to the internet. With Mate on, an uploaded document's content
- the scan itself for OCR, or up to 24,000 characters of a text-layer PDF's
own text - goes to a separate, faster reading model configured for the
document library, apart from the model that answers your questions and
defaulting to a fast, inexpensive one; see
[Configuration](../reference/configuration.md) for where that's set. A
document uploaded while Mate was switched off keeps whatever it read
locally at upload time; turning
Mate on afterwards doesn't reach back and OCR or summarise it - reindex it
if you want that read properly now. Its already-read text does join search
by meaning automatically, the moment Mate is ready, alongside everything
else in the library. See Searching, below.

## Folders, tags, notes and title

Folders group documents - `Receipts/2026`, `Manuals/Engine` - the way any
file manager's folders do. A document sits in exactly one folder, or none
(unfiled, at the root). Moving a document between folders only changes
where it's filed; the file itself is untouched, so refiling something
never means re-uploading it.

**Move…** opens a folder picker you can browse the same way you browse the
panel, and the destination doesn't have to exist yet: type a name, click
**Create folder**, and the picker makes it inside whatever folder it's
showing and drops straight into it, so **Move here** means the folder you
just made. Filing the first receipt of a new year is one trip, not three.

Every document carries:

- A **title**, typed at upload or left blank and filled in automatically
  the first time Mate reads it. A title you've typed yourself is never
  overwritten by a suggested one.
- **Tags**, typed by hand or suggested by Mate. A suggested tag is shown as
  a suggestion until you keep it, and it never replaces or hides a tag you
  added yourself.
- **Notes**, a free-text field for anything about the document that isn't
  in the file itself - where the original paper copy lives, who to call
  about it, why it's worth keeping.

Title, filename, folder, tags, summary and notes are all searchable
alongside the document's own text, not as a separate lookup: a document
titled "Impeller kit" turns up on the word "impeller" even before its
contents have ever been read.

### The Details page

**Details…** on a document's row menu - or the **Details** button in the
viewer - opens that document's own page, and it's where all three of those
are edited after the fact. Fix a title the scan got wrong, write down where
the paper original is filed, add the tag you'd actually search for. Tags
you add show as your own; the ones Mate suggested sit beneath them under
**Suggested by Mate**, each with a **Keep** that adopts it as yours.
Nothing is written until you press **Save**, and **Discard** puts the page
back the way you found it. **Rename** on the row menu is still there for
when the title is all you want to change.

The lower half of the page is the document itself, read-only: its status
and, if it failed, why; the file's own name, type, size and page count;
what read it and which model, if Mate did; when it was uploaded and last
indexed; the summary; and the **Indexing cost**, what reading this one
document has cost so far, added up across every reindex. A document read
on board with Mate switched off reads $0.0000, because nothing about it
ever left the boat.

## File types and limits

An upload is capped at 100 MB. Supported types: PDF (text or scanned),
plain text, Markdown, CSV, JSON, and NMEA and GPX logs, plus JPEG, PNG,
GIF and WebP images. HEIC and HEIF photos - what an iPhone saves by
default - are accepted and stored, but not read; convert to JPEG first if
you want Mate to see what's in one. Any other file type is still accepted,
stored, and downloadable later; it's simply not indexed, since there's no
text in it to search and nothing for Mate to read.

## Searching: words, and meaning

**Search**, in the toolbar, opens a full-page search box over whatever
you're looking at - press **⌘K** (**Ctrl+K** on Windows or Linux) from
anywhere on the page to open it just as fast. Open it with nothing typed
yet and it offers your last five searches and your twelve most-used tags;
click a tag to filter the library by it directly, or click a recent search
to run it again. Type to search, use the arrow keys to move between the
results shown below the box, and press Enter to open whichever one is
highlighted - or just click it. **Esc**, or the close button, dismisses the
overlay and clears the search.

The search box always searches words. Type a part number, a boat name or an
invoice number and it finds the documents containing it, instantly, with no
connection and no account. Titles, filenames, folder paths, tags, summaries
and notes are searched alongside the document's own text, so a receipt filed
under `Receipts/2026` and tagged `engine` is findable by any of those.

By default a search only looks in the folder you're currently browsing;
the **All folders** switch inside the search box widens it to the whole
library.

With Mate on, search also works by meaning. Ask "how often should I service
the engine's cooling system impeller" and the manual page that says "raw
water pump: inspect the rubber vanes... replace every 500 hours" comes back,
even though the two share no useful word. Both searches run, and the results
are merged: a document both agree on ranks above one only a single side
found.

This needs each document's text to have been turned into vectors first.
Turning Mate on, with an embedding model chosen, sweeps the whole library
for you automatically in the background - at startup, and again each time
you save a settings change that leaves Mate ready - so there's nothing to
click and nothing left waiting on you. Anything uploaded or captured
afterwards is embedded the same way as it's read in. Stopping partway
through (a restart, a connection drop) picks back up where it left off on
its own, so nothing is ever paid for twice.

The cost is genuinely small. Embeddings bill around $0.02 per million tokens,
which works out to fractions of a cent for a whole library, and a search's own
query costs about two hundredths of a cent - less if you have searched the
same thing recently, since queries are cached.

If the meaning half of a search can't run - no connection, OpenRouter down -
the search still answers with its word results and says **Showing keyword
results only** above them, with the reason. Nothing is silently missing. If
you have no embedding model configured at all, semantic search is simply off
and the panel says nothing about it. If the background sweep itself hits a
snag, the toolbar names it and links straight to Settings → Assistant so you
can fix it there.

This runs on its own embedding model, set alongside the document-reading
model above. Leaving it blank turns semantic search off entirely and
leaves word search as the library's only retriever. See
[Configuration](../reference/configuration.md).

## Status and Reindex

A document is one of three things:

- **Pending**: just uploaded, not read yet.
- **Indexed**: read and searchable, and - if Mate was on at the time -
  summarised and tagged. A PDF that read cleanly except for one or two
  broken pages still reaches indexed, with a note saying "N of M pages
  unreadable" so the gap is visible instead of silent.
- **Failed**: something went wrong, with the reason shown alongside it.

**Reindex** re-reads a document from the beginning: local extraction runs
again, and if Mate is switched on right now, OCR and enrichment run again
too, regardless of whether they ran - or succeeded - the first time.
Reindex is also how a scan longer than 200 pages gets past the automatic
cost cap: a document that size is turned away the first time, with the
page count and an estimated cost, and only proceeds once you've asked for
it explicitly through Reindex.

Select more than one document (the checkbox on each row) and the selection
bar offers **Reindex…** alongside Move to… and Delete, reindexing every
one you picked in one confirmation - useful after re-scanning a whole
folder of receipts, or once Mate is finally switched on for a batch that
was uploaded while it was off. The confirmation shows the combined page
count across everything selected and, if any of it is a scan or a photo,
the estimated OCR cost for just those.

## Attaching a document to Mate, and Mate searching on its own

Ask Mate a question with a document attached, and Mate is given a short
preamble ahead of your question: the filename, what kind of file it is,
how many pages, whether it's finished indexing yet, and - for the message
you just sent - up to around 4,000 characters of what's actually in it.
If the answer needs more than that, Mate reads further into the same
document itself rather than needing the whole thing handed over up front.
The attachment stays on the message afterwards, shown as a small chip that
reopens this panel's own viewer for that file.

Beyond a direct attachment, Mate can search the whole library on its own,
the same way it looks up a forecast or a tide station. Ask something like
"what's the part number for the raw water pump impeller", and if a receipt
or a manual in the library mentions it, Mate finds it and reads it without
first being handed the file.

## Backups

The library is two things, and both are needed together: the
`data/documents/` folder (every file's bytes, one flat folder, named by
content) and `data/documents.sqlite` (everything that describes them -
titles, folders, tags, the search index). The database on its own
describes files it can no longer produce; the folder on its own is just
anonymous names with nothing left to say what any of them are. Back both
up as one unit. See [Configuration](../reference/configuration.md) for the
exact paths and how to override them.
