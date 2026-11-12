# ADR 0281: Inbox photos go on equipment and open jobs

## Status

Accepted (2026-11-12). Third of four steps after [ADR 0279](0279-an-inbox-for-photos-and-files-shared-from-a-phone.md),
which named the Equipment and Open job destinations and left them for later.
Builds on [ADR 0127](0127-bins-open-from-a-tag-items-carry-photos.md), which made an
equipment photo an ordinary Document linked to the item, and
[ADR 0269](0269-open-jobs-hold-known-problems-until-they-are-fixed.md), which gave
a job the same strip.

## Context

A phone holds shots of a leaking hose or a pump's data plate that belong on the
equipment item or on the job that is tracking the fault. The Inbox could already
send a photo to a passage or to a Documents folder. The two missing destinations
raised three questions.

- Where do an equipment photo and a job photo live? The photo endpoints answer
  it: each stores a Document, at the top level of the library, and links it to the
  item or job in a table of its own.
- Who picks the target? The operator knows which pump is in the photo. Guessing
  from the picture is Mate's part, and that is out of scope for now.
- What of a PDF? Those two strips show pictures. A PDF about a pump belongs in
  Documents, where it can be linked to the item.

## Decision

**Filing reuses the photo endpoints' store calls, not their HTTP routes.** The
Inbox file is copied into the library with the step Documents filing already takes
(`inboxLibrary.copy`), which stores the bytes by hash and reuses a Document that
already holds them. Then `AddEquipmentPhoto` or `AddOpenJobPhoto` links it at the
end of the item's strip, the same call the upload endpoints make. The two filings
differ only in the target check, the link and the words for a refusal, so both run
through one handler and one function, `fileInboxItemAsPhoto`, inside the same
durable claim, original-hash memory and batch loop as the other destinations. The
thumbnail goes with the item.

**The target is checked once, before any item is claimed or copied.** An unknown
equipment item, or a job that is gone or closed, refuses every item in the request
in plain words and copies nothing. If the target goes missing or closes mid-batch,
the link refuses the item still in hand. One case is not refused: an item whose
photo an earlier filing already linked to the target, but which failed to clear
from the Inbox. It is looked up by its bytes before either refusal, so filing it
again clears it as filed, even if the target has closed since, as the passage path
does for a sign that already shows the photo.

**The link happens under the content lock, inside the copy.** Documents holds a
lock per file hash for every step that stores, links or deletes a Document, and
the photo endpoints link while holding it. `inboxLibrary.copy` takes a link
callback and runs it in the store step's created and already-there branches, under
that same lock. Linking afterwards would leave a gap in which a Documents delete of
the same bytes could remove the Document before the link landed, or a failed link
would need an undo that has to guess whether anyone else had begun to use the
copy. Under the lock neither can happen. If the link fails, the copy removes the
Document it just made, and its file, before it releases the lock, so nothing new
is left behind. A Document that was already in the library is never touched.

**Photos only, as the photo endpoints take them.** JPEG and PNG go through. A PDF
is refused with "only photos can go on equipment" (or "on a job") and stays in the
Inbox. The Inbox also accepts GIF and WebP for passage signs, but the photo
endpoints do not, so those two are refused with their own message instead of being
admitted to a strip the Equipment screen would not accept. The Inbox never holds
HEIC, so that case does not arise here. The phone applies the same rule before it
sends, so a GIF or WebP counts as not a photo there too.

**The operator searches and picks.** One dialog serves both pickers. The equipment
picker asks the box to search, so it finds what the Equipment screen finds (every
word must hit the name, make, model, an alias or the zone name), and
offers stored items too, since a spare is as likely to be photographed. The job
picker reads the open jobs and narrows them as the operator types, because the box
has no job search; it offers open jobs only, because a closed job takes no more
photos. Each remembers the last choice in the device's local storage, as a starting
highlight only, and drops it if the item or job has gone.

**A mixed selection files its photos and keeps the rest.** The buttons stay off
when no marked item is a JPEG or PNG. Otherwise the client sends the photos alone,
says how many files stayed, and leaves them marked so that Send to Documents is one
tap away.

## Consequences

- A photo that arrives twice, on two items, becomes one Document linked to both.
  The Inbox normally stops the second copy at upload, so this is the rare race.
- Filing refreshes the equipment lists, the item's own page, the job list and the
  Documents counts, since the filing made a Document.
- Mate does not pick the item. The agreed step four may suggest one, still for the
  operator to confirm.
