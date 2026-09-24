# ADR 0127: Bins Open From A Tag, Items Carry Photos

## Status

Accepted (2026-09-24). Amends [ADR 0065](0065-inventory-records-in-the-binary.md)
§4 (bin tags, the stocktake's no-move-no-delete rule) with the URL and
photo mechanics that section left unspecified, and builds the stocktake
§4 described but deferred. Builds on the equipment registry
[ADR 0123](0123-inventory-begins-with-the-equipment-registry.md) shipped
(zones, bins, equipment, `equipment_documents`) and the document store
[ADR 0106](0106-documents-in-the-binary.md), which this reuses rather than
duplicates for photo storage.

## Context

The operator bought fifty NTAG215 stickers to label the boat's plastic
storage bins. The workflow they want, seen already working on a
commercial product: tap the sticker on a bin with a phone, and the phone
opens straight to that bin's contents - a list of what's inside, with a
photo of each item big enough to read the packaging. The bin contents are
mostly consumables and spares (tape, caulk, zip ties, scrapers), not
installed gear, so adding one has to be as quick as a photo and a name,
done standing at the open bin.

ADR 0123 already gives every equipment item a stable URL
(`/inventory/equipment/<uuid>`) that opens on any phone with no app
required, and the document store already accepts JPEG/PNG uploads with
tags and links them to equipment through `equipment_documents`. What was
missing was a URL for a *bin* rather than an item, a place for item
photos, a way to actually write the fifty tags, and the stocktake ADR
0065 §4 designed but never built.

## Decision

### 1. A tag holds a URL and nothing else

An NFC tag written by Helmcentral carries one NDEF URL record. No custom
record type, no tag-UID registry kept anywhere. The host is the page's
own origin - in practice the boat's tailnet address
(`https://<machine>.<tailnet>.ts.net`) - and there is no new
configuration key for it; the running page already knows its own origin.

Web NFC (the browser API that lets a page write a tag) requires a secure
context, so a tag written from inside the app always carries the https
tailnet address, never a bare local IP. That is also the address that
still works when the operator is off the boat.

### 2. A bin's identity is its code, not a database id

A bin's URL is `/inventory/bins/<code>` - `LAZ-02`, matching the printed
label on the bin, not the bin's internal id. This is deliberate: the
whole point of a printable code is that it can be read off the sticker
without a lookup, and a URL that used the id instead would make a tag
and its label two different, silently divergent names for the same bin.

The cost is that renaming a bin's code orphans any tag already written
for the old one - the tag still points at a URL, but that URL no longer
resolves to anything. The rename control says so. An unknown code never
redirects anywhere; it renders "No bin `LAZ-02`" and offers to create
that bin on the spot, which is what makes it possible to write and stick
a tag on a bin before its record exists in Helmcentral at all - print the
label, tap the blank tag, create the bin from the page that opens.

### 3. A photo is a linked document, not a new kind of record

ADR 0123 §3 already settled that a link between equipment and a document
carries no role column - what a document *is* comes from its tags. A
photo follows that rule instead of becoming a special case: it is an
ordinary document, uploaded through the same intake as any other file,
tagged `photo`, and linked to its item through the existing
`equipment_documents` table. That table gains one column, `sort_index`,
so a photo strip has an order and a cover (the first photo).

Nothing about this is a second storage path. It means Mate's document
reading indexes photos exactly like it indexes a manual or an invoice, so
a question like "where's the spare impeller" can be answered from the
picture as well as the name. It also means the existing rule keeps
working with no extra code: untag a photo in the document library and it
drops out of the item's photo strip and survives as an ordinary linked
document, because that is just what removing a tag from a linked document
already did.

One consequence worth stating plainly: the whole-set-replace write that
the equipment editor's Documents list already used (`PUT
.../equipment/:id/documents`) had to be narrowed to leave photo-tagged
links alone. Before this it deleted and reinserted every link on the
item; left as it was, saving an ordinary document edit would have wiped
the photo strip out from under whatever was managing it. Photo links are
now written only by the photo routes; the documents route manages
everything else, same as before.

Photos are downscaled on the phone before they leave it, so a bin of
twenty items uploads over a boat's tailscale connection in a reasonable
time rather than choking on full-resolution phone camera output.

### 4. Writing a tag needs Chrome on Android; everything else gets Copy

Web NFC - the API that lets a browser tab write to a physical tag - only
exists in Chrome on Android at the time of writing. There is no partial
substitute on iPhone or desktop Chrome: rather than pretend otherwise,
the tag control on those platforms shows the URL as plain text with a
Copy button and says plainly that writing needs Chrome on Android, so the
operator pastes the address into a separate NFC-writing app instead. It
never tries to degrade silently into a worse version of the same feature.

Tapping a written tag from an iPhone opens the URL in Safari rather than
inside Helmcentral's installed home-screen app (the two are different
security contexts to iOS, and there is no way around that from a web
app). That means Safari needs its own sign-in the first time, separate
from the installed app's. This is a known platform limit, not a bug, and
it is documented for the operator rather than worked around.

### 5. A scan still never moves, marks missing, or deletes anything

ADR 0065 §3's rule holds unchanged: nothing a tag scan or an NFC tap does
automatically relocates an item, marks it missing, or removes its
record. The stocktake built here (ADR 0065 §4) confirms what it finds and
offers a move as an explicit button press - never a side effect of the
scan itself. This matters more now that scanning is easier: the easier a
scan gets, the more tempting it is to let it also tidy up silently, and
that is exactly the shortcut ADR 0065 ruled out for RFID's unreliable
read coverage. NFC's coverage is much better than passive UHF, but the
same discipline applies for the same reason - an automatic action that
usually fires correctly still trains the operator to stop checking on the
one pass it does not.

The stocktake section accepts two independent inputs, neither a fallback
for the other: an NFC scan loop on Chrome/Android, and a focused text
field that accepts a keyboard-wedge scanner or a pasted URL, ending on
Enter. The second path is what ADR 0065's original USB-reader design
already described, and it is also what makes the whole section testable
without a phone.

## Consequences

- `equipment_documents` carries a column (`sort_index`) that means
  something only for photo-tagged links and nothing for any other kind
  of link - an ordinary linked manual or invoice just leaves it at its
  default. That is a smaller cost than a second link table would have
  been, and it keeps ADR 0123 §2's single-file, one-join reasoning intact.
- A bin's code is now load-bearing in a second way: it is part of a URL
  that may be printed, laminated, and stuck to a physical tag, not just a
  label in a list. Renaming a bin is cheap in the database and expensive
  on the boat, and the UI says so at the point of rename rather than
  leaving the operator to discover it later.
- Photo upload is a second write-tier file intake next to the general
  document upload, deliberately built to the same size limit and MIME
  rules rather than sharing the same handler function - the general
  upload handler is heavily tested and used for every document kind in
  the library, and duplicating its narrower photo-only path was judged
  lower risk than reshaping it to serve both.
- Tag writing is a single-platform feature (Chrome/Android) with an
  honest, undisguised fallback to copy-and-paste everywhere else. This is
  a real limitation, not a design choice hidden behind a friendlier
  label.

## Open questions

- Whether a bin ever needs more than one tag (a large locker with two
  access points) is not addressed - one code, one URL, any number of
  physical stickers pointing at it works today without any schema
  change, so this is left alone until it is asked for.
- Vision-based label reading (ADR 0065 §5) is unaffected by this ADR and
  remains a separate, later switch - photos land in the library and are
  indexed for search whether or not that switch is ever turned on.

## Amendment, 2026-09-24: a duplicate photo refuses onto a non-photo document

§3's sha256 dedupe originally had `uploadEquipmentPhotoHandler` retag any
matching document `photo` and link it, whichever way the match was found -
an existing row (`GetBySHA`) or one that won Insert's own dedupe race. That
silently repurposed whatever the operator had already filed under
Documents: a fuel receipt photographed once for the file and again, weeks
later, as an item's photo would end up tagged `photo` and sitting in that
item's strip, never asked for. A match that is already tagged `photo` is
still linked, exactly as before - that is the genuine shared-photo case §3
always meant to support, and `RemoveEquipmentPhoto`'s "delete the document
only when no link remains" already handles it correctly. A match that
is *not* tagged `photo` is refused with 409 instead, naming the document
("This image is already in Documents as \"…\"") so the operator can go
find it rather than silently gaining a second, unwanted purpose. The
retagging call this replaced is deleted outright, not kept behind a flag:
nothing else needs "add the photo tag to an arbitrary document."
