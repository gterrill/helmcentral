# Documents

The document library keeps the files that matter to the boat where you can find
them from the boat, not from an inbox ashore: manuals, receipts, invoices,
service logs, passage notes, a photo of a part number or a data plate. Upload
one and Helmcentral reads whatever text it can find and indexes it for search.
With Mate switched on, a scan or photo is also OCR'd properly, and the document
gets a short summary and a few suggested tags.

The sidebar's **Documents** panel is where this happens day to day. You browse
folders, upload by button or drag-and-drop, search, and open, download, rename,
move, reindex or edit the details of anything in the library. The page is
titled with the folder you are in. **Upload** is the button at the top right.
**New folder** and **New note** are under the menu beside it (**More
actions**). The list holds documents only, and each row has its own menu.
Folders live in the pane on the left, described below. You can also attach a
file straight from Mate's composer (see [Documents](assistant.md#documents) on
the Mate page), and a chip on a past message opens the same viewer this panel
uses.

Notes you capture yourself, and folders marked as a manual for reading front to
back, have their own page: [Notes and the manual](notes-and-the-manual.md).

## What gets indexed, and what it costs

Text files, and PDFs with a real text layer (the kind you can select and copy
text from), are read on the boat the moment they're uploaded. That needs no
internet connection and no OpenRouter account, and it happens whether or not
Mate is switched on.

With Mate on, anything uploaded (or reindexed, see below) also gets:

- OCR for a scanned PDF or a photo: the actual words on the page, not a guess
  from a thin or missing text layer.
- A short summary, two or three sentences.
- A handful of suggested tags.

This runs as soon as the document is uploaded. There's no button to press and
no per-document prompt. It costs real money, though the amounts are small. A
one-page scanned invoice cost $0.0023, the same document split across two pages
cost $0.0046, and a photographed receipt cost $0.0012, all measured against a
live OpenRouter account. Each document tracks what has been spent reading it,
added up across every reindex, not just the last pass. That figure is on the
document's Details page, under Indexing.

**What leaves the boat:** with Mate off, nothing does. With Mate on, an
uploaded document's content goes to a separate reading model set up for the
document library. For a scan that content is the scan itself, and for a
text-layer PDF it's up to 24,000 characters of its text. This model is apart
from the one that answers your questions and defaults to a fast, inexpensive
one. See [Configuration](../reference/configuration.md) for where it's set.

A document uploaded while Mate was off keeps whatever was read on the boat at
upload time. Turning Mate on later doesn't go back and OCR or summarise it, so
reindex it if you want that now. Its existing text does join search by meaning
automatically once Mate is ready (see Searching, below).

## The folder pane

The pane on the left shows every folder in the library as a tree, so you can
see where things are filed and jump to any of them without stepping through the
levels. **Documents** is at the top and shows the documents filed at the root
of the library. The folders sit below it, each with a count on the right of the
documents filed directly in it. A chevron opens or closes a folder that has
subfolders, and Helmcentral remembers which ones you left open on that device.
Open a document's folder from a link and its parents open themselves, with the
folder highlighted.

A folder marked as a manual shows a book icon. Its sections are read inside
the manual, so they are not listed as folders in the pane, and its count is
every document anywhere inside it.

Below a divider, **Unfiled notes** shows how many captured notes are not
filed anywhere yet, and switches the list to just those.

Every folder has a menu (the three dots at the right of its line, always
showing on a touch screen and appearing on hover or focus elsewhere) with
**Rename**, **Move…**, **Treat as a manual** or **Stop treating as manual**
(top-level folders only) and **Delete**. A folder has to be empty before you
can delete it. **New folder** in the page menu creates the folder inside the
one you are viewing.

To file documents quickly, drag a row from the list onto a folder in the pane.
The folder lights up as you pass over it. If several rows are ticked, dragging
any one of them moves all of them. Drop onto **Documents** to take them out of
their folder. **Move…** and **Move to…** do the same job without dragging,
which is the way to do it on a touch screen.

**Hide folders** gives the list the whole width, and **Show folders** brings
the pane back. On a phone the pane isn't on the page. Tap **Folders** to open
it, and it closes as soon as you pick a folder. In the pane the up and down
arrows move between folders, right opens a folder, left closes it, and Enter
opens the folder you are on.

## Folders, tags, notes and title

Folders group documents (`Receipts/2026`, `Manuals/Engine`) the way folders do
in any file manager. A document sits in exactly one folder, or none (unfiled,
at the root). Moving a document only changes where it's filed. The file itself
is untouched, so refiling never means re-uploading.

**Move…** opens a folder picker you can browse one level at a time. The
destination doesn't have to exist yet. Type a name and click **Create folder**,
and the picker makes it inside the folder it's showing and drops straight into
it, so **Move here** means the folder you just made. Filing the first receipt
of a new year is one trip, not three.

Every document carries:

- A **title**, typed at upload or left blank and filled in the first time Mate
  reads it. A title you've typed yourself is never overwritten by a suggested
  one.
- **Tags**, typed by hand or suggested by Mate. A suggested tag stays marked as
  a suggestion until you keep it, and it never replaces or hides a tag you
  added.
- **Notes**, a free-text field for anything about the document that isn't in
  the file itself: where the paper original lives, who to call about it, why
  it's worth keeping.

Title, filename, folder, tags, summary and notes are all searched along with
the document's own text. A document titled "Impeller kit" turns up on the word
"impeller" even before its contents have been read.

### The Details page

**Details…** on a document's row menu, or the **Details** button in the viewer,
opens that document's own page. This is where you edit the title, tags and
notes after the fact. Fix a title the scan got wrong, write down where the
paper original is filed, add the tag you'd actually search for. Tags you add
show as your own. The ones Mate suggested sit beneath them under **Suggested by
Mate**, each with a **Keep** that adopts it as yours. **Rename** on the row
menu is still there for when the title is all you want to change.

Nothing is written until you press **Save**. As soon as you change anything, a
save bar appears at the top of the screen with **Save** and **Discard**, and
**Discard** puts the page back the way you found it. If you have unsaved edits,
the page asks before it lets you leave.

Across the top, a trail shows where the document is filed: **Documents**, then
each folder it sits in, then the document's name. Every folder in the trail
opens that folder in the Documents list. **Documents** takes you back to the
folder you were browsing, or to the new folder after a move.

**Move** at the top right files the document in another folder, using the same
picker as the list, including **Create folder**. The trail and the **Folder**
line under **About the file** update as soon as the move lands, and going back
to the list takes you to the new folder.

The column beside the form (below it on a phone) describes the document and is
read-only. It shows:

- the status and, if it failed, why
- the folder it's filed in
- the file's name, type, size and page count
- what read it, and which model if Mate did
- when it was uploaded and last indexed
- the summary
- the **Indexing cost**: what reading this document has cost so far, added up
  across every reindex

A document read on the boat with Mate off shows $0.0000, because nothing about
it left the boat.

## File types and limits

An upload is capped at 100 MB. Supported types are PDF (text or scanned), plain
text, Markdown, CSV, JSON, NMEA and GPX logs, and JPEG, PNG, GIF and WebP
images. HEIC and HEIF photos, which is what an iPhone saves by default, are
accepted and stored but not read. Convert them to JPEG first if you want Mate
to see what's in one. Any other file type is still accepted, stored and
downloadable later. It just isn't indexed, since there's no text in it to
search and nothing for Mate to read.

## Searching: words, and meaning

The **Search** field above the list opens a full-page search box over whatever
you're looking at. Click it, start typing in it, or press **⌘K** (**Ctrl+K** on
Windows or Linux) from anywhere on the page. With nothing typed yet it offers
your last five searches and your twelve most-used tags. Click a tag to filter
the library by it, or click a recent search to run it again. Type to search,
use the arrow keys to move between the results below the box, and press Enter
to open the highlighted one, or just click it. **Esc** or the close button
dismisses the overlay and clears the search.

The search box always searches words. Type a part number, a boat name or an
invoice number and it finds the documents containing it, instantly, with no
connection and no account. Titles, filenames, folder paths, tags, summaries and
notes are searched along with the document's own text, so a receipt filed under
`Receipts/2026` and tagged `engine` turns up on any of those.

A search only looks in the folder you're browsing unless you turn on the **All
folders** switch inside the search box, which widens it to the whole library.

With Mate on, search also works by meaning. Ask "how often should I service the
engine's cooling system impeller" and the manual page that says "raw water
pump: inspect the rubber vanes... replace every 500 hours" comes back, even
though the two share no useful word. Both searches run and the results are
merged, so a document both agree on ranks above one that only one side found.

For this, each document's text first has to be turned into vectors. Turning
Mate on with an embedding model chosen sweeps the whole library in the
background, at startup and again each time you save a settings change that
leaves Mate ready. There's nothing to click. Anything uploaded or captured
afterwards is embedded as it's read in. If the sweep is interrupted (a restart,
a dropped connection) it picks up where it left off, so nothing is paid for
twice.

The cost is small. Embeddings bill around $0.02 per million tokens, which is
fractions of a cent for a whole library. A search query costs about two
hundredths of a cent, and less if you've searched the same thing recently,
since queries are cached.

If the meaning half of a search can't run (no connection, OpenRouter down), the
search still answers with its word results and says **Showing keyword results
only** above them, with the reason. If no embedding model is configured at all,
semantic search is simply off and the panel says nothing about it. If the
background sweep hits a snag, the toolbar names it and links to Settings →
Assistant so you can fix it there.

The embedding model is set alongside the document-reading model above. Leave it
blank and semantic search is off, with word search as the only way to find
things. See [Configuration](../reference/configuration.md).

## The listing

Each row shows the document's name, its tags, when it was **Added** and its
size. Added is the day the file came into Helmcentral, shown as "12 Oct" for
this year and "3 Jan 2025" for earlier ones. Click **Name**, **Added** or
**Size** to sort by it, and click again to reverse. The list opens newest
first, and Helmcentral remembers your choice on that device. A row shows its
first three tags with a "+2" style count for the rest. Hover to see them all.

## Status and Reindex

In the listing, the document's icon carries its state. The normal grey icon
means indexed, amber means still being read (hover for "Reading…"), and red
means it failed (hover for "Failed"), with the reason on a line under its name.

A document is one of three things:

- **Pending**: just uploaded, not read yet.
- **Indexed**: read and searchable, and summarised and tagged if Mate was on at
  the time. A PDF that read cleanly except for one or two broken pages still
  reaches indexed, with a note saying "N of M pages unreadable" so the gap is
  visible.
- **Failed**: something went wrong, with the reason shown alongside.

**Reindex** re-reads a document from the beginning. Reading on the boat runs
again, and if Mate is on right now, OCR and enrichment run again too, whether
or not they ran, or succeeded, the first time. Reindex is also how a scan
longer than 200 pages gets past the automatic cost cap. A document that size is
turned away the first time, with its page count and an estimated cost, and only
goes ahead once you ask for it through Reindex.

Select more than one document (the checkbox on each row, or **Select all** in
the header for everything listed) and a bar appears above the list showing how
many are selected, with **Move to…**, **Reindex…**, **Delete** and **Clear
selection**. Reindex covers everything you picked in one confirmation. That's
useful after re-scanning a folder of receipts, or once Mate is switched on for
a batch uploaded while it was off. The confirmation shows the combined page
count and, if any of it is a scan or a photo, the estimated OCR cost for just
those.

## Attaching a document to Mate, and Mate searching on its own

When you ask Mate a question with a document attached, Mate gets a short
preamble ahead of your question: the filename, the kind of file, the page
count, whether indexing has finished, and up to around 4,000 characters of
what's in it. If the answer needs more, Mate reads further into the same
document itself. The attachment stays on the message as a small chip that
reopens this panel's viewer for that file.

Mate can also search the whole library on its own, the same way it looks up a
forecast or a tide station. Ask "what's the part number for the raw water pump
impeller", and if a receipt or a manual in the library mentions it, Mate finds
and reads it without being handed the file.

## Backups

The library is two things, and you need both: the `data/documents/` folder
(every file's bytes, in one flat folder, named by content) and
`data/helmcentral.sqlite` (everything that describes them: titles, folders,
tags and the search index, alongside the rest of your boat's records such as
inventory, equipment, maintenance, Mate's conversations and the nearby-vessel
history). The database alone describes files it can no longer produce. The
folder alone is just anonymous names. Back both up as one unit.

Copying `helmcentral.sqlite` while Helmcentral is running can miss changes
still waiting to be written. Stop Helmcentral first, or use the `sqlite3`
backup command instead of a plain file copy:

```sh
sqlite3 data/helmcentral.sqlite ".backup /path/to/backup/helmcentral.sqlite"
```

See [Configuration](../reference/configuration.md) for the exact paths and how
to override them.
