# Documents

The document library keeps files that matter to the boat: manuals,
receipts, invoices, service logs, passage notes, a photo of a part number
or a data plate. Upload one and Helmcentral reads whatever text it can find
in it and indexes that for search. If Mate is switched on, it goes further:
a scan or a photo gets OCR'd properly, and the document gets a short
summary and a few suggested tags.

> **Status: not yet reachable from the dashboard.** The document store,
> search, OCR and Mate's two document tools are built and working. What's
> missing is the dashboard side: a Documents panel to browse, upload and
> search from, and an attach control in Mate's composer. This page
> describes the finished feature; today, getting a file into the library
> means calling the API directly rather than using a screen in Helmcentral.

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
reindexed, not just the last pass.

**What leaves the boat:** with Mate off, nothing does. Local extraction
never talks to the internet. With Mate on, an uploaded document's content
- the scan itself for OCR, or up to 24,000 characters of a text-layer PDF's
own text - goes to whichever model `assistant.document_model` names in
Settings → Assistant, separate from the model that answers your questions
and defaulting to a fast, inexpensive one. A document uploaded while Mate
was switched off is never sent anywhere later on its own: turning Mate on
afterwards doesn't reach back and enrich what's already in the library. Only
a document uploaded (or reindexed) while Mate is on ever leaves the boat.

## Folders, tags, notes and title

Folders group documents - `Receipts/2026`, `Manuals/Engine` - the way any
file manager's folders do. A document sits in exactly one folder, or none
(unfiled, at the root). Moving a document between folders only changes
where it's filed; the file itself is untouched, so refiling something
never means re-uploading it.

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

## File types and limits

An upload is capped at 100 MB. Supported types: PDF (text or scanned),
plain text, Markdown, CSV, JSON, and NMEA and GPX logs, plus JPEG, PNG,
GIF and WebP images. HEIC and HEIF photos - what an iPhone saves by
default - are accepted and stored, but not read; convert to JPEG first if
you want Mate to see what's in one. Any other file type is still accepted,
stored, and downloadable later; it's simply not indexed, since there's no
text in it to search and nothing for Mate to read.

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

## Attaching a document to Mate, and Mate searching on its own

Ask Mate a question with a document attached, and Mate is given a short
preamble ahead of your question: the filename, what kind of file it is,
how many pages, whether it's finished indexing yet, and - for the message
you just sent - up to around 4,000 characters of what's actually in it.
If the answer needs more than that, Mate reads further into the same
document itself rather than needing the whole thing handed over up front.

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
