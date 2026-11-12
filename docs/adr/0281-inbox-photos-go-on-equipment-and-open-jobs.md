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
differ only in the target check and the link, so both run through one function,
`fileInboxItemAsPhoto`, inside the same durable claim, original-hash memory and
batch loop as the other destinations. The thumbnail goes with the item.

**The target is checked before any file is copied.** An unknown equipment item, or
a job that is gone or closed, refuses that item in plain words and copies nothing.
If the link fails after a copy, the Document the copy made is deleted again, as the
passage filing does when its sign fails.

**Photos only, as the photo endpoints take them.** JPEG and PNG go through. A PDF
is refused with "only photos can go on equipment" (or "on a job") and stays in the
Inbox. The Inbox also accepts GIF and WebP for passage signs, but the photo
endpoints do not, so those two are refused with their own message instead of being
admitted to a strip the Equipment screen would not accept.

**The operator searches and picks.** Both pickers read the list the Equipment and
Open jobs screens already read and narrow it as the operator types. The equipment
picker offers stored items too, since a spare is as likely to be photographed. The
job picker offers open jobs only, because a closed job takes no more photos. Each
remembers the last choice in the device's local storage, as a starting highlight
only, and drops it if the item or job has gone.

**A mixed selection files its photos and keeps its PDFs.** The buttons stay off
when every marked item is a PDF. Otherwise the client sends the photos alone, says
how many PDFs stayed, and leaves them marked so that Send to Documents is one tap
away.

## Consequences

- A photo that arrives twice, on two items, becomes one Document linked to both.
  The Inbox normally stops the second copy at upload, so this is the rare race.
- Filing refreshes the equipment lists, the item's own page, the job list and the
  Documents counts, since the filing made a Document.
- Mate does not pick the item. The agreed step four may suggest one, still for the
  operator to confirm.
