# ADR 0279: An Inbox for photos and files shared from a phone

## Status

Accepted (2026-11-12). Builds on [ADR 0204](0204-passage-replay-signs.md), which
made a sign a note with a photo pinned to a moment, [ADR 0248](0248-photo-moments-hold-the-replay.md),
which gave a photo sign its hold in the replay, and [ADR 0106](0106-documents-in-the-binary.md),
which keeps files in hash-named storage. It follows the staged-review habit of
[ADR 0150](0150-imports-are-staged-reviewed-and-remember-their-source.md). This is the first of four
steps: the store, the upload page, the Inbox view and its two destinations. The
share target on the phone is [ADR 0280](0280-sharing-to-helmcentral-from-an-android-phone.md);
the equipment and job destinations, and Mate's part, come later.

## Context

Photos of the passage live on a phone. Getting one onto a sign meant opening the
replay, choosing the passage, picking the file and typing a title, one photo at a
time, with the full-size original going up over the boat's connection. The same
phone holds receipts and shots of equipment that belong in Documents. The
operator wanted to share a camera roll's worth at once from the phone, throw away
the near-duplicates and the blurred ones, and sort the rest later on the Mac.

Four facts shaped the design.

- A shot that is about to be thrown away must not be indexed, charged for OCR or
  kept in the library. So a waiting item cannot be a Document.
- The phone knows things the box does not: when the photo was taken, how sharp it
  is, whether it repeats the one before. Working these out on the box would mean
  image code there. The phone can do it while it shrinks the photo anyway.
- The operator's phone, a Pixel, hands a browser-picked photo a GPS block that is
  present but zeroed, with empty reference letters. Read naively, every such photo
  was taken at 0, 0 facing due north. Its capture time and zone survive.
- A sign must sit inside a recorded passage, and a passage is recorded only once
  it ends. Photos taken on the way home arrive before the passage they belong to
  exists.

## Decision

**The Inbox is its own store, and an item is not a Document.** `inbox_items` is a
table in `helmcentral.sqlite`, created in place. The bytes live in a directory of
their own, `INBOX_DIR`, default `data/inbox`, named by sha256. A row holds the
file's name, type and size, the capture time, the facing and the phone's position
when real, the fingerprint, the sharpness score, the time it arrived, and whether
a preview is stored beside the file. The photo's own zone offset stays on the
phone, because only the phone needs it to turn a camera's clock into an instant.
An item stays until the operator files or deletes it, with no expiry. Deleting
removes the row and the file for good, after a confirmation; the original is on
the phone.

**The phone does the image work.** The upload page reads the EXIF block from the
original before anything else, because the re-encode that shrinks the photo
throws it away. It then decodes the photo once and makes three things from that
one bitmap: the photo shrunk to 2560 pixels on the long edge at JPEG quality 0.85
(converting HEIC on the way), a preview 400 pixels across at quality 0.7, and two
numbers from a 256 pixel grey copy, a 64-bit difference hash and the variance of
the Laplacian. The hash is sent as 16 hex characters. A PDF goes up untouched and
has no preview. `downscaleImage` already took the size as an argument, so its
decode and its encode were split rather than forked.

**The phone asks before it works.** Shrinking a 12 megapixel photo is the slow
part of a share, and most of a re-shared roll is already on the box. So the page
hashes every picked photo's original first, asks `POST /api/inbox/known` which of
those hashes the Inbox holds or has filed (500 at a time, a read call because it
changes nothing), and skips those photos before they are decoded, shrunk or sent.
They count as "already in Helmcentral". If the box cannot be asked, the batch
stops with the error and sends nothing, because sending everything blind would
bring the cost back. The upload still checks the same hashes on the box, so a
photo that arrives in the gap between the question and the send is caught there.

**A zeroed GPS block is missing.** `exif-gps.ts` counts a latitude or longitude
only when its reference letter is N, S, E or W, a bearing only when its reference
is T or M, and rejects 0, 0, a zero denominator and anything outside the globe or
the compass. A latitude without a longitude is dropped, not half used. The server
applies the same rule to the phone's position and stores both or neither.

**One file per request, and the server answers whether it was new.** `POST
/api/inbox` takes one multipart file, its fields and the preview (a JPEG of at
most 512 KB, stored beside the file and served at `GET /api/inbox/:id/thumb`)
through the same `receiveUploadedFile` intake as documents. It answers 201 `stored`, or 200
`already_uploaded` with `where` set to `inbox`, `documents` or `filed`. Two checks
decide it. The content hash of the stored bytes is compared with items and with
Documents. The hash of the original file as the phone held it, `source_sha256`
(a small SHA-256 written in the page's own code and fed in chunks, because Web Crypto is missing on a plain http address such as the boat's LAN one), is
compared with waiting items and with `inbox_filed`, a table of originals already
sent to a passage or Documents. The second check is what recognises a photo
shrunk twice by different browsers, or one already filed whose copy has left the
Inbox. The upload holds the lock of both hashes, in sorted order, so two shrinks
of one original cannot both be stored. A deleted photo is not remembered, so sharing it again brings it back. The
phone sends the files that remain in order, counts each answer, and shows "12
uploaded, 3 already in Helmcentral". It refreshes the Inbox list once the batch
ends, and the count every ten stored files, rather than after every file. A file
that fails is recorded and the rest go on. Only JPEG, PNG, GIF, WebP and PDF are accepted, so a HEIC that
reaches the server unconverted is refused loudly with a 415.

**The list says where each item belongs, by time alone.** `GET /api/inbox` returns
the items in capture order with a group:

- `no_time`: the file carried no capture time.
- `passage`: the capture time falls inside a visible passage.
- `underway`: the capture time is after the end of the last recorded passage, or
  there is none, and the boat's track shows it not at rest at that moment (speed
  at or above the detector's rest speed).
- `off_passage`: everything else with a time.

The rule for `underway` is the simplest that holds without asking the recorder
anything: a passage is written only when it ends, so a moving boat with no
passage around the moment is on one that has not been recorded. A short stop in
the middle of an unfinished passage reads as `off_passage` until the passage
closes. Underway items can be culled but not assigned, and they move into their
passage on the next read after it is recorded. Nothing is assigned automatically.

**A phone clock that disagrees with the track is flagged.** When the phone sent a
real position and the track has a point within fifteen minutes before the capture
time, the item is flagged `gps_mismatch` if the two are farther apart than a
nautical mile plus how far the boat could have gone since that point (its speed
over ground times the point's age). The phone's position is never used to place anything. The track is the
boat's position; the phone's only job is to say the clock or zone may be wrong.

**Filing is per item and fails per item.** `POST /api/inbox/file-to-passage` takes
a title for each item and, where the photo's own time will not do, a `t_ms`. For
each item the server checks the title (1 to 80 characters), that the file is a
photo, and that the time falls inside a passage. It then copies the file into the
library's **Passage photos** folder (made if absent, matched by name without
regard to capitals, as the sign editor does) through the same `storeUploadedFile`
step an upload takes, creates the sign with the default visibility and show time
and the photo moment on, and finally removes the item. A failure at any step
leaves the item in the Inbox and is reported against its id; a library copy made
for a sign that then failed is deleted again. The other items still go through.
An item is claimed in its own row while it is filed (a `filing_ms` time set by a
conditional update, which changes a row for only one of two racing requests), so
two requests for it cannot both make a sign. A claim older than five minutes, left
by a crash, is taken over, and a request releases only the claim it took (the
release matches the claim's timestamp), so a slow request cannot clear its
successor's. Bin skips a claimed item and reports it as being filed; an item filed and removed in the meantime is left out of the answer. If the sign exists but clearing the item failed, a retry finds a sign
already using the photo, clears the item and reports it as filed, making no second sign. Errors that
carry paths or database detail are logged, and the operator sees a short message.
`POST /api/inbox/file-to-documents` does the same into a chosen folder, with an
optional title, and leaves the indexer to read the new Document. It refuses an
item whose bytes the library gained in the meantime, naming the existing
document, and keeps the item.

**A photo between passages is attached by the operator.** The call accepts a
`t_ms` per item. The assign dialog offers the start and end of each passage and
chooses the nearest to the capture time to begin with. A photo with no time has
no default and must be placed by hand.

**Titles without Mate are the nearest named place.** `GET
/api/inbox/suggested-titles` looks up the last track point at or before each
capture time, within fifteen minutes, and names it with the place-name resolver
the passage names use, cut to the sign title limit. Photos taken within about 100 metres of one another share one lookup, so a burst
at an anchorage asks once. Lookups run four at a time and the request stops
waiting after eight seconds; a lookup that finishes later writes nothing. It answers an empty string
where there is no time, no track point, no name or no answer in time. The dialog shows them in an
editable list; a sign is made only on confirm.

**Culling suggestions are computed in the browser and never act.** Frames within
six bits of one another, taken within a minute (by chain, so a slow pan is one
set), are marked as duplicates with the sharpest, or the earliest on a tie, as
the keeper. A frame scoring under 40 percent of the batch median sharpness, in a
batch of at least four scored frames, is marked soft. "Mark suggested deletions"
selects them for the operator to confirm; nothing is removed otherwise. A sea and
sky scores low honestly, which is why this only suggests.

**Routes and the page.** Reads are `tierRead` and every change is `tierWrite`. The
Inbox is a panel of its own with a row in the sidebar just before Passages, at
`/inbox`, and the page that shares to it is `/inbox/upload`, an address that fits
on a phone's Home Screen. The row carries the number waiting, hidden at zero. It
is not a view of Passages because it files into Passages, Documents and, in later
steps, Equipment and open jobs, so no one of them owns it. An earlier draft put it
under `/passages/inbox` with a button on the Passages page; that was never
released, so the old addresses are not redirected. The Documents folder picker
opens at the folder last sent to, remembered in the device's local storage and
forgotten without complaint when storage is blocked.

## Consequences

- A photo sits on the box twice for a while: once in the Inbox and, once filed,
  as a Document. Filing copies and then removes, because the two directories may
  be on different disks. The Inbox directory is part of the data to back up.
- The contact sheet and the assign dialog load the 400 pixel previews. The
  full-size file is served at `GET /api/inbox/:id/content` for anywhere a picture
  is shown large, though no view does yet. The box makes no previews, by design:
  it stores the one the phone sent and deletes it when the item is filed or
  deleted. A PDF has none and shows a file icon.
- Two Pixel behaviours still need a real run: whether a share-target share keeps
  the location and bearing, and how the GPS block looks then. The compass mark on
  an item records whether a bearing arrived, so the first real share answers it.
- Hashing every picked photo before anything is sent means a big roll shows
  "Checking" for a while with nothing sent, and the pure-TypeScript hash is slower
  than Web Crypto. That is the price of never shrinking a photo the box has.
- A photo that is in the library under other bytes is not a duplicate. Only an
  identical file is skipped. The fingerprint finds near-duplicates inside the
  Inbox, not against Documents.
- Notes with no folder keep their API parameter, `?filed=0`, and are called
  Unfiled in the interface so the word Inbox means one thing.

## Rejected

- **Staging items as Documents with a flag.** The indexer, the OCR charge and the
  library listing would all need to learn to skip them, and a binned shot would
  leave chunks and tags to clean up. A separate table costs one copy at filing.
- **Image work on the box.** A hash and a sharpness score need a decoder on a
  small machine that has none today, and the phone already holds the decoded
  pixels.
- **Falling back to the phone's position for placement.** A phone with a wrong
  clock would still place the sign somewhere plausible, which is worse than a
  warning. The track is the single source of position.
- **Treating zero as a real value.** A bearing of 0 and a position of 0, 0 are
  both possible on a map and both wrong here. The reference letter is the
  evidence that the phone wrote the value.
- **One batch call that succeeds or fails whole.** A single bad title or a time
  outside every passage would hold back the rest of a camera roll. Each item
  stands on its own and says why it did not go.
- **Assigning underway photos to the passage in progress.** The passage's edges
  are not known until it ends, and a sign outside every passage is refused.
