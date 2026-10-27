# ADR 0204: Signs mark places and photos on the passage replay

## Status

Accepted (2026-10-23). Fourth of four steps toward a cinematic passage replay,
after [ADR 0200](0200-passage-replay-in-3d.md) (3D),
[ADR 0202](0202-passage-replay-boat-model.md) (the boat model) and
[ADR 0206](0206-passage-replay-camera-shots.md) (camera shots).

## Context

A replay that flies over a coast invites a caption. The whale off the bow, the
anchorage with the good beach, the squall line seen at 14:10: the operator has
the words, and usually a photo taken on the phone at the time. They should be
able to pin those to the track, have them appear as the boat goes by, and not
have to say when the photo was taken by hand. As with camera shots, whatever is
written must survive editing the passages themselves (split, merge, trim,
ADR 0176).

## Decision

1. **A sign is a note pinned to a real instant.** A row in `replay_signs` in
   the passage store: `t_ms` (Unix milliseconds), `title` (required, at most 80
   characters), `note` (at most 280), `document_id` (nullable), `visibility`
   (`always`, `passing`, `after`), `show_s` (1 to 30, default 4), created and
   updated times. Its place on screen is wherever the track is at that time.
   It carries no passage id, so splitting or merging a passage leaves it where
   it was. A trim that cuts its time out hides it (the replay only places signs
   inside a replayed passage) and undoing the trim brings it back. Nothing
   deletes a sign implicitly; only the operator's Delete does.
2. **Validation is on the server.** A new sign's time must fall inside some
   passage's span, hidden or not, when it is created or moved. Every field is
   range-checked. When `document_id` is set it must be an existing document of
   type png, jpeg, gif or webp, the types the documents API takes. Routes sit
   in the passages table with their tiers (ADR 0177): `GET
   /api/passages/signs?from=&to=` is read; `POST /api/passages/signs`, `PATCH
   /api/passages/signs/:id` and `DELETE /api/passages/signs/:id` are write.
   `/signs` is a fixed path registered beside `/:id`; the router prefers the
   fixed path, and a test pins that, since a sign list answered by the passage
   handler would be a quiet, confusing failure. `PATCH` takes `"document_id":
   null` to remove the photo.
3. **The photo is returned only while its document exists.** The list and every
   write response carry `document_id` and `photo_url` (the document's content
   URL) only when the document is still in the library; otherwise both are
   null and the sign keeps its title and note. The stored link is not erased,
   because nothing here deletes implicitly. The check goes through the document
   store, not a SQL join, because the two stores hold separate connections and
   the check must work wherever they live. A photo the server still lists but
   the browser cannot load is not hidden: the card shows a muted icon and a
   "Photo not available" line, and the console hears of it once per sign. That
   is a different case from a deleted document, where the server sends no photo
   and the card shows nothing.
4. **Visibility is measured in watched time.** `always` is up for the whole
   replay. `passing` (the default) appears 0.5 s of watching before the boat
   reaches the sign and stays `show_s` seconds after she reaches it. `after`
   appears when the boat reaches it and stays for the rest of the replay. The
   times are wall-clock seconds, mapped from the timeline through the same
   piecewise clock the camera shots use (`lib/replay-clock.ts`), so a faster
   replay or a slowed shot does not change how long a sign is readable.
5. **At most two cards at once.** `visibleSigns` (a pure function in
   `lib/replay-signs.ts`) takes the placed signs and the watched time and
   replays the appearances up to that time in order. When a third sign would be
   up, the one that appeared earliest gives way, and it stays gone: it does not
   flicker back when a later sign expires. `always` signs are never the one that
   gives way, so two of them mean nothing else shows, and with more than two the
   earliest in time show. Because the answer is worked out from the signs alone,
   not from the previous frame, a seek or scrub shows exactly what continuous
   play would have.
6. **Cards are map markers.** A card (photo thumbnail, title, note) sits on a
   short leader line above its point, as a MapLibre marker, so it stays on the
   point through pitch and terrain with no per-frame projection of our own. The
   second card has a longer leader so two nearby points do not overlap. Cards
   use the card surface and a border so they read over satellite imagery in day
   and night. While the replay is paused a tap on a card opens the photo in a
   dialog (Escape closes it). While it plays the tap does nothing, so a stray
   touch does not hide the picture the operator is watching.
7. **Pins when not replaying.** At the start (clock 0) every sign is a small
   pin on its point and a tap opens its card, with a close button. The pins also
   show while the Signs editor is open, so the operator can see what they are
   editing. A pin that is open as a card is not drawn twice.
8. **Photos live in Documents.** An upload goes into a folder named "Passage
   photos" at the root of the document library, found by name and created
   through `POST /api/document-folders` on first use. The lookup reads the folder
   tree (folders only) and compares names without regard to capitals, as the
   server does when it refuses a duplicate, so an existing "Passage Photos" is
   reused. Two uploads at once share one lookup, and a name clash from another
   tab (409) falls back to a second lookup, so there is never a duplicate. Or
   the operator picks an existing image through the document link picker, which
   gained an optional type filter.
9. **EXIF parsing happens in the browser, in a small module of our own.** The
   file is already in the browser at upload, the time-zone rule needs the boat's
   zone (which the replay bar already derives from the first fix's longitude,
   `vesselZone`), and a picked document needs only its first 256 KB read. The
   backend has no EXIF reader and would need one plus a second round trip to
   learn the placement. We read only `DateTimeOriginal` and
   `OffsetTimeOriginal` from a JPEG (the only format phones and cameras write
   them in); a PNG, GIF or WebP, or a JPEG without a time, is placed at the
   cursor and the editor says so. A damaged block is reported ("Could not read
   when the photo was taken") rather than guessed at: a file that stops short
   (every read is bounds-checked), a date that does not exist (30 February,
   second 60, checked against the calendar), or an offset that is present but
   not a zone (`+10:00` and `+1000` are read, padding is tolerated, anything
   else is damaged rather than quietly read in boat time). A picture in the
   library that cannot be fetched is the same: the sign goes to the cursor and
   the note says why. There is no new
   dependency: a general EXIF library is several times the size for two tags,
   and this module loads only with the replay panel.
10. **The time-zone rule.** If the photo carries `OffsetTimeOriginal` that
    offset is used. Otherwise the capture time is read in the boat's zone, the
    whole-hour zone the replay bar labels its time with ("UTC+10"). The editor
    shows the time and zone it used. If that instant is inside a replayed
    passage the new sign is placed there and the replay seeks to it; otherwise
    it is placed at the current position and the editor says the photo was taken
    outside the replay, in plain words.
11. **The photo's GPS is ignored.** The track is the reliable source; a phone
    fix at the moment of a shot can be minutes stale or a coarse network fix.
12. **Editor.** A **Signs** button beside Camera opens a compact editor. "Add
    sign here" creates a note-only sign at the cursor and picks it. "Add photo
    sign" is one menu, "Upload photo" or "From Documents", and creates a sign
    for the photo with the placement above. A selected sign has one **Photo**
    control: its thumbnail, and a menu of Upload, From Documents and Remove.
    Each sign is a diamond tick on the slider, a different shape from the
    camera marks. Picking one seeks there and opens its title, note, when it
    shows (Always, While passing, After passing), seconds shown (While passing
    only), photo and Delete. Title and note save when the field is left, not on
    every key, and keep their typed text on show until the saved value returns.
    Nothing in the editor is disabled while a write runs, because the click
    that leaves a field would otherwise land on a button that has just gone
    dead. Instead the writes are queued and run one at a time in the order made,
    so a slow upload and a title saved a moment later cannot cross, and each
    failure is shown as its own (a later success does not wipe it). The note
    about where a photo went clears when another sign is picked, when a field
    changes, and when a photo add fails. A selected sign can be moved in time
    without deleting it. **Move sign here** sets its time to the cursor, and
    is off while the cursor is already on the sign. Four nudges (−1 min, −10 s,
    +10 s, +1 min) shift it and seek the replay to the new time. A move works out its
    target when its queued write runs, from the sign as the server last saved
    it (the write before it has re-read the signs), so quick clicks add up and
    a refused move leaves nothing behind. A move for a sign deleted meanwhile
    sends nothing, and a move to a moment the replay does not include is
    refused in the page without a request. **Back to photo time**, in the Photo menu of a sign that has a
    photo, reads the capture time again by the placement rules above and moves
    the sign if that lands inside a replayed passage. Otherwise the sign stays
    and the editor gives the same plain reason as the add flow. All three are
    ordinary queued `PATCH` writes of `t_ms`, and any refusal is shown as it is.
13. **Reduced motion.** Signs still appear and disappear; only the fade-in is
    dropped.

## Rejected

- **Placing by the photo's GPS.** It is less reliable than the track the boat
  recorded, which is the whole premise of the replay, and many photos carry
  none (a camera with no GPS, a phone with location off for the camera).
- **A sign belonging to a passage.** A split or merge would have to move, copy
  or drop it, and a trim would delete it. Time is the one thing those edits do
  not change.
- **Unlimited cards on screen.** A dozen signs near a harbour would bury the
  chart under cards. Two is what a watcher can read in the seconds each lasts.
- **A server-side EXIF reader.** Needs a dependency, and the zone rule needs the
  boat's longitude, which only the page has in hand.
- **Erasing the stored link when the document is deleted.** Nothing deletes
  implicitly; a document restored or re-uploaded at the same id would not
  come back, but the sign is never silently changed.

- **Dragging the pin on the map to move a sign.** Where a track doubles back,
  one point on the map is several instants, so the drop does not say which
  one is meant.
- **Dragging the sign's tick on the slider.** On a long passage a pixel is
  minutes, too coarse to land on the moment.

## Consequences

- A sign made for a passage stays with its time. If the passage is later
  re-imported with different times, it shows where the clock now says.
- A sign time is validated only when it is created or moved; a later trim may
  leave a sign outside every passage, where it is kept but not shown.
- Photos for signs are ordinary documents: they are searchable, indexed like any
  image, and deleting one in Documents takes it off the sign.
- Exports and the embedded player (ADR 0199) do not show signs yet.
