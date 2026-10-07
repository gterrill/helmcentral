# ADR 0164: Mate fills in paperwork forms

## Status

Accepted (2026-10-07). Builds on [ADR 0158](0158-mate-changes-records-through-changesets.md) (Mate
proposes record changes, the operator applies), [ADR 0162](0162-mate-summarises-a-conversation-into-a-note.md)
(Mate drafts, the operator reviews and saves) and [ADR 0106](0106-documents-in-the-binary.md) (documents, and
why the build has no CGO on any target, armv7 included).

## Context

Insurers and marinas keep sending the same form. The Pantaenius named-storm declaration asks for the
owner's name, the boat's type, length and age, the policy number, a tick against the marina the boat
sits in, who prepares the boat when the owner is away, and a signature and date. The next marina's
berth application asks for the same facts in a different order. The skipper types them in each time.

Mate can already take the PDF: the composer uploads it as a document and Mate reads its extracted text.
That is not enough to fill it in. The text says "Crystalbrook Marina" and nothing about where the box
beside it sits. The test form is also flat. It has no form fields at all. The tick boxes are Wingdings
glyphs set in the page text, and the blanks are rules drawn under the question. Plenty of forms the
operator will meet are the same, and some are properly fillable, so both have to work.

Mate also does not know the facts. Helmcentral stored hull details but nothing about the owner, the
insurer, the policy or the berth, and Mate had no tool to read what it did store.

## Decision

**Store what forms ask for.** Vessel particulars gain owner name, phone and email, insurer, policy
number, home marina, berth, storm delegate (the person who prepares the boat, free text), and stored
length overall and beam. They are on Settings, Vessel, in a new Owner & insurance section, and save
through the settings Save bar like the rest. No values are pre-filled. Length overall is also published
by the boat's instruments when they carry it, so the stored one is for forms filled in with nothing
connected; when both exist and differ, Mate is told and asks which the form should carry.

**Particulars are a registered record type, as a singleton.** `vessel_particulars` registers with the
changeset layer ([ADR 0158](0158-mate-changes-records-through-changesets.md)) with one fixed id,
`vessel`, and `update` as its only action. The registry could express that without change: a type
declares its actions, `Get` answers one id and `List` returns one row. A create or a delete is refused
by the existing action check, and any other id is "no vessel details with that id". An update takes the
same validation and the same write the Settings page uses. The answers an operator gives Mate in chat
therefore reach the record through the ordinary Apply card, with the same stale check. An empty record
has the version `unset`, so a proposal made against it still goes stale if someone saves in between.

**Mate reads them with `get_vessel_particulars`.** Each value carries its source: stored in Helmcentral
or live instrument data. Anything not on record is listed as absent, never guessed. A failed read of the
live feed is reported in the result, not hidden.

**Two tools for the form.**

- `inspect_form(document_id, page?)` says where things are. A form with fields returns each field's
  name, kind, options, current value and tooltip. A flat form returns, per page, its lines of text and
  the tick boxes found, with positions in PDF points from the lower left of the page (`y` is the text
  baseline, and the bottom of a box). Each box carries the text beside it as its label. A long form
  comes back a page at a time (the tool result is capped), with `next_page` set.
- `fill_form(document_id, entries, title?, folder?)` takes entries of three shapes: `{field, value}`
  for a form field, `{page, anchor, value}` to write after a line of text, and `{page, box}` to tick the
  box with that label. Anchor and box names are exactly what `inspect_form` returned. An optional
  `occurrence` picks one when the same label appears twice. Every entry is checked before anything is
  written; an unknown field, anchor or box, an option the field does not offer, a value too long for the
  space, a character plain Latin text cannot carry, or a signature all fail the whole call and name the
  entry. A signature is refused: a field of signature type is never listed or filled, and a flat entry
  whose anchor reads as a signature line is refused.

**The library is pdfcpu.** `github.com/pdfcpu/pdfcpu` is pure Go, Apache-2.0, and fills AcroForm fields
(text, date, check box, radio group, combo and list box), which is the half that a PDF writer has to
get exactly right: appearance streams, so the value shows in every viewer and not just the ones that
regenerate them. It builds with `CGO_ENABLED=0` for linux/arm (GOARM=7), arm64 and amd64.

**Reading a flat page.** The vendored read-only PDF reader supplies the content-stream lexer
(`backend/third_party/ledongthuc-pdf`). On top of that a small interpreter in `pdf_form_layout.go`
follows the text state (position, word and character spacing, horizontal scale, rise, the current
transformation, form XObjects) so the text runs land where they are drawn, with widths from the font,
including two-byte fonts and the built-in standard fonts that carry no width table. Text on one
baseline is cut into a line at a gap wider than a font size, or where a tick box sits.

Tick boxes are found two ways. A glyph box is a character that is a box: the Unicode empty squares, or
Wingdings' empty squares in its private-use range. Its size is read from the glyph's own bounding box
in the embedded TrueType program, so it is the box the eye sees and not a guess from the font size.
When the outline cannot be read the box is estimated from the font size and flagged `approximate`, and
Mate is told. A drawn box is a small, roughly square rectangle that is stroked; filled-only rectangles
are cell shading and are ignored. On the Pantaenius form this finds all 34 boxes on page 1, each
labelled with the marina beside it (the label is the line ending at the box's left edge, or starting at
its right edge, whichever is nearer, within 24 points and with no other box between). If a page yields
no boxes at all, `inspect_form` says so in its notes, and the prompt tells Mate to ask the operator what
to tick and not to guess.

**Writing onto a flat page.** The text is plain Helvetica, WinAnsi encoded, appended to the page's
content as one extra stream inside its own `q` and `Q`, with the font added to the page's resources.
The existing content is untouched. It starts four points after the end of the anchor line, on its
baseline lifted one point so it clears a rule drawn beneath, in the line's own font size (clamped to 7
to 12 points) and shrunk to fit before the next text or box on the line or the page margin. A tick is
a vector check mark drawn inside the box. A rotated page is refused. Anything outside Latin text is
refused with the character named; the alternative, embedding a Unicode font, costs several megabytes in
a binary that is already the size of the boat's whole update.

**A filled form is a draft, not a document.** `fill_form` stores the PDF in the assistant's own tables
as a form draft: the source document, a suggested title and folder, the page count, and the bytes. It
belongs to no message until the run saves its reply, and then rides on that message as a card, as
proposals do. Unattached drafts older than a day are swept. Nothing reaches the document library until
the operator presses Save on the card.

The card offers: open the PDF, download it, edit the title and the folder (default from what Mate
suggested, such as `Insurance/2026`), **Save to Documents**, and **Dismiss**. Three routes serve it:
`GET /api/assistant/form-drafts/:id/content` (read tier), `POST .../save` with `{title, folder}` and
`POST .../dismiss` (write tier). Save creates the folder path if it is missing and stores the PDF
through the same file store, record and indexing as an upload, so the saved form is found by search
like any other document. Saving twice returns the same document and makes no second one; the draft
records the document and the title it was saved under. A dismissed draft's bytes are deleted. A
saved draft cannot be dismissed, since it is a document now, and a dismissed one cannot be saved.
Conversations delete their drafts with them. On the next turn Mate is told which drafts were saved or
dismissed, as it is for proposals.

**The prompt sets the order.** Read the layout; read what is on record; work out every entry; ask for
everything missing in one message; offer to save the operator's answers to the vessel record with
`propose_changes` (a separate Apply card); then fill once. A signature is always left to the operator,
and a date unless they say to use today's. Mate says the form is not saved until the operator saves it
from the card.

## Rejected

- **A vision model reading the page image.** The text and its positions are already in the file and
  exact; a picture would be an estimate of them, with a per-page cost and Mate's own admission that it
  cannot see. It would be the only way to do a scanned form, and a scanned form has no text to anchor
  to, so it is out of scope: `inspect_form` finds no lines and says so.
- **Mate giving raw coordinates.** Mate would be reading numbers off a list and doing page geometry in
  its head. Naming the label and letting the server place the text keeps the model on the question it
  is good at (which blank belongs to which fact) and makes a wrong guess an error that names the entry
  instead of text in the wrong place.
- **Saving the filled form straight to Documents.** A form filled wrongly would sit in the library, be
  indexed and be cited by Mate later. ADR 0162 already decided that what Mate drafts, the operator
  reviews first.
- **Storing the draft as a hidden document.** It would need a state that every document query then
  has to exclude, and its bytes would be content-addressed beside real documents. A table holding the
  bytes is simpler and a dismissed draft is just deleted.
- **Shelling out to qpdf, pdftk or poppler.** None of them is on the boat's image, and the update
  script and container are built to carry one static binary for linux/arm, arm64 and amd64.
- **unipdf, and other commercial libraries.** The licence rules them out for a public repository.
- **pdf-lib in the browser.** Mate runs on the server and the operator may be on a phone; the filled
  form would have to travel back out of the page to be saved, and a wall display has no business
  running it.
- **Writing the incremental update by hand.** Appearance streams for combo boxes, radio groups and
  check boxes are the part of the format that goes wrong silently. pdfcpu has the tests.
- **A one-line "fill from particulars" button.** It cannot pick between two berths, answer the question
  "who prepares the boat" for a form that asks it differently, or leave the policy number blank when
  nobody has entered it.

## Consequences

- The binary grows. Measured for linux/arm GOARM=7, stripped as the Dockerfile builds it: 27.1 MB
  before, 33.2 MB after, about 6 MB more. pdfcpu is imported through its `api` package, which pulls in
  its image, font and encryption code whether or not forms use it. Taking only the lower-level
  packages would claw some back and was left until it matters. Adding the module also moved
  `golang.org/x/crypto`, `net`, `sync`, `sys` and `text` forward a few minor versions, as it requires.
- pdfcpu would write a config file under the user config directory by default. The form code sets its
  config path to "disable" so a service on a boat writes nothing there.
- Flat forms are filled in one font. A form whose blank is a row of small boxes (one per letter), a
  table cell with no label on its line, or a blank above its question has no label the layout can name;
  Mate sees the lines and says what it can and cannot do, and the operator fills in the rest by hand.
- A drawn tick box is found only as a stroked, roughly square rectangle of 4 to 22 points. A box drawn
  as four separate lines is not found, and neither is a circle. A glyph box is found only for the
  characters listed; another symbol font's box would need adding.
- Mate cannot fill a form it cannot read at all: a scan with no text, an encrypted file, a page with
  the content drawn rotated. It says so.
- Fixtures for the tests are built in the tests with pdfcpu (one fillable form, one flat form with
  stroked tick boxes). The real Pantaenius form was used by hand only and is not in the repository.
  The glyph-box path, which the synthetic flat form does not exercise, was checked against it.
