# ADR 0282: Mate sorts and titles Inbox photos

## Status

Accepted (2026-11-12). Last of four steps after [ADR 0279](0279-an-inbox-for-photos-and-files-shared-from-a-phone.md),
which left Mate's part for later. Builds on [ADR 0281](0281-inbox-photos-go-on-equipment-and-open-jobs.md),
whose link-under-the-lock copy this ADR extends to passages.

## Context

A camera roll mixes scenery with a receipt, a pump's data plate and a PDF. The Inbox
sorted by time alone, so a receipt taken at sea sat under the passage. Titles came
from the nearest named place to the boat, which says where the boat was and not what
the photo shows. Three smaller problems rode along:

- A phone records the direction it faced, and the Inbox kept the number without
  using it. A title can now come from what lay in that direction.
- Filing to a passage copied the photo into the library, released the content lock,
  and only then made the sign. A failed sign deleted the copy, and a link another
  request had made to the same bytes in between went with it.
- The Replay sign editor uploaded the camera's file as it was, and refused HEIC.

## Decision

**Mate sorts on arrival, off the request.** After an item is stored the upload handler
nudges a small classifier and answers. The classifier reads every item with no kind
and no recorded failure (or one whose retry is due), two at a time, and asks
the document model (`document_model`, Gemini 2.5 Flash by default) for
`{"kind": "scenery" | "document" | "equipment"}` through the client path the document
indexer uses (`openrouter_image.go` holds the shared part: the data URL, the image
block, the call, the check for no choices and the fence-tolerant JSON decode). A PDF is
a document without a call. A call that fails for a reason the model might not repeat
(the network, a status) is tried once more after a short wait; an answer that cannot
be used is not, since it would cost the same again, and neither is a call that ran its
full time, since asking again could double what it cost. The classifier reads Mate's
settings before each item rather than once per pass, so switching Mate off ends the
pass and a changed model or key is used for the rest of it. Every failure after that is
treated alike, by the operator's decision of 2026-11-12: there is no split into
temporary and final. The item records the fixed line "Mate couldn't sort this yet" in
`classify_error` (never the provider's words, and never empty, which would read as no
failure), counts it in `classify_attempts` and sets `next_try_ms` to now plus 1 minute,
then 5, then 30, then an hour. `Run` wakes itself when the earliest `next_try_ms`
comes due, and the cause goes to the log. The list adds the time of the next try to
the line on the boat's clock, in the app's 12-hour form ("Mate couldn't sort this yet.
Trying again at 3.40 PM"), and sends `next_try_ms` so the page can look again then. The
promise is made only while the time is still ahead and Mate is on: a time that has
passed reads "Trying again shortly", and with Mate off the line is "Mate couldn't sort
this. Mate is off". The page checks the same two things against its own clock, since
the line it holds can be a minute old. Saving a change to what Mate depends on (the
assistant switch, `document_model`, or the OpenRouter key) requeues every failed item
(clears the error, the attempts and the wait) and wakes the classifier. The settings
and secrets save handlers compare before and after, and the requeue runs in the
background so a save does not wait on the Inbox's store. Any other save leaves the
waits alone. `POST /api/inbox/sort-again` (write tier) does the
same on demand, as a manual nudge, and the page shows one **Sort again** button when
any item has a failure. The sorter reads only the 400 px
preview the phone sent, about a tenth of the full photo, because telling a receipt from a
coastline needs no more. An image with no preview is not sent full size: it gets the
error "no preview to sort from". The columns are `suggested_kind` (null until sorted),
`classify_error`, `classify_attempts`, `next_try_ms` and `suggested_title`, added by
idempotent `ALTER TABLE`s on open, with a partial index on the items with no kind. The
classifier reads them 50 at a time, paging by the arrival cursor (creation time, then id)
rather than by skipping items it has seen, so a pass always moves on even when a result
cannot be saved. If saving fails, the pass finishes and returns the error, and `Run`
waits out the indexer's back-off. A pass runs at boot, which also sorts what arrived
while Mate was off. Three more things wake the classifier: reading the list when Mate is
set up and an item is waiting (cheap, since a wake before a pass starts makes one pass),
a save of what Mate depends on (above), and an upload. When a pass cannot start because the settings or the
store cannot be read, `Run` waits out the indexer's back-off (one minute, or a wake)
and tries again, rather than sitting until the next upload.

**One limit of three on Mate calls.** Sorting (two at a time) and titles (four at a
time) share a single process-wide limit of three calls in flight, so a dialog opened
while a roll is being sorted does not triple the load on the provider. Titles take
priority over sorting by construction: the sorter never holds more than two of the three
slots, so a title always has one free. A call's own timeout starts when it holds a
slot, not when it joins the queue. The reading of the Mate settings is kept for ten
seconds, so a list request does not read the settings file and the secrets store each
time. The read happens outside the lock, so a slow one does not hold up a save; two
callers may both read. Saving settings drops that reading and bumps a generation, so a
read that began before the save is not kept after it and a change takes effect at once.

**Mate is a normal state to be off.** The classifier asks the same readiness check
the indexer uses (enabled, key, document model). If it fails, nothing is called and
the kind stays null. If the settings or secrets cannot be read, that is an error: it is
logged and Run backs off, and the list does not fail with it. The list answer carries
`mate_sorting`, so the page knows an item without a kind is waiting for Mate rather
than never going to get one. When the settings cannot be read it carries
`mate_sorting: null` and a `mate_error` in the operator's words ("Mate's settings
couldn't be read; sorting is paused"). The page shows that one line and groups by the
time rule.

**Groups follow the suggestion; the time rule stays underneath.** With Mate set up, a
`document` or `equipment` item goes to Documents or Equipment wherever it was taken.
A `scenery` item takes the PR 1 time rule (a passage, Underway now, Not on a passage,
No time). An item with no kind that no passage claims goes to Unsorted, with the
reason beside it if Mate failed. The group sets the default only: every action stays
open on every item. With Mate off, every item takes the time rule.

**Titles with Mate use the photo, the course over ground and five features.** When
a named feature is in range, the title request sends the 2560 px stored copy: an island
or headland is a few pixels at 400 px. With none in range there is nothing to check a
name against, so the thumbnail is enough to say what the photo shows. (Shrinking the
2560 px copy on the box for this call was considered and deferred.) The boat's course
is the track point's course over ground, used only while the speed is above the resting
threshold, since a course at rest is noise. It is a course over ground, not a heading:
the track records no compass heading, and the prompt says so. The five nearest named
islands and bays within 10 nm follow, each as "name, distance, side of the course" ("Cape
Capricorn, 3.1 nm, 40° to port"), and with a bearing on the photo the ones in the
sector are marked. The five are chosen after the marking: the in-sector features (the
two nearest at most) plus the nearest others, five in all, so the feature the camera
faced is never cut for ranking sixth. Angles are normalised into 0 to 360 and rounded
(`bearingText`, using `normalizeDegrees`), so -10 reads 350°. The prompt is a constant, `inboxTitlePrompt`, with a test pinning its
key instructions: one title of at most 80 characters, a name only if the photo shows
it, otherwise the subject, no note. A failed call is shown against the row. It is not
replaced by the nearest place.

**Asking for titles is a write.** With Mate set up, titles make paid calls and store
`suggested_title`, so the route is `POST /api/inbox/suggested-titles` with `{"ids": [...]}`
at write tier, not a read-tier GET. If Mate's settings cannot be read, the request does
not fail: every row takes the no-Mate path (sector or place), and the answer carries a
top-level `mate_error` ("Mate's settings couldn't be read; these are place-name
titles") that the dialog shows once. The operator is told, so this is not a quiet
fallback.

**Each photo has its own time.** The title handler picks the way to title photos once
(Mate, the bearing sector, or the nearest place) and builds one task per photo, or per
100 m cell for place names. Four run at once. Each task has its own timeout: 25 s for
Mate, counted from the moment the call holds a Mate slot, since a vision call takes
seconds, and 8 s for a place lookup. A photo that runs out of time gets an `errors`
entry ("Mate didn't answer in time", or that the place names did not answer) and a
blank title. A Mate title's feature fetch is bounded by the same 8 s and, if it hangs,
is reported as the place-name server not answering, not as Mate being slow, and the others are not held to its clock. The whole request has a 90 s
deadline: a task not started by then gets the same entry and makes no call. One request
has Mate title at most 20 photos, in the order asked; the rest get "Mate titles up to 20
photos at a time; open again for the rest" and make no call. A photo that already has
its title stored costs no call and does not count towards the 20. A title Mate made is
stored on the item (`suggested_title`), so reopening the dialog, or restarting
Helmcentral, does not pay for it again. It goes with the row when the item is filed or
deleted, and a failure is never stored. Every failure reaches the page in the
operator's words ("The place-name server didn't answer, so no title was suggested.",
"Mate couldn't reach the model, so no title was suggested."); the plugin, network and
provider text is logged, not shown.

**The nearby features come from the place-names provider.** `inboxFeaturesNear`
resolves the configured place-names plugin and, since the Overpass plugin is both
place-names and POI provider, asks its `fetch_poi` for the `island` and `bay`
categories. The fixed category list has no headland or point, so those appear only
where mapped as an island or a bay. One fetch covers a 0.02° cell (about 1.2 nm) at
10 nm plus a 1 nm margin, shared by every photo taken in it and kept for ten minutes
across requests. A failed fetch is not kept across requests, but within one request
the first answer for a cell, failure included, serves the rest, so five photos in one
failing cell cause one fetch. The 0.02° grid is used because a
0.001° one would fetch again for nearly every photo on a moving boat; it is the same
cell type that place-name lookups use, at a finer grid. The host ranks, measures and
de-duplicates for each photo with the Nearby tile's own function, from the photo's
own position, because distances and sides shift by up to half a mile across a cell.
The plugin caps each category, so a very crowded area can drop the nearest feature.
That truncation is passed along and not hidden: without Mate, a bearing title for a
cut-short list is an error against the row ("too many named places nearby to pick one
reliably") and the title is left blank, because the feature picked might not be the
nearest. With Mate, the prompt says the list may be incomplete.

**The bearing sector is pure arithmetic.** `inSector` is true when the camera bearing
and a feature's true bearing differ by at most 25° (edges inclusive, wrapping at
360). The nearest named feature in the sector within 10 nm titles a photo that
carries a bearing, with no Mate: the first one in the sector of the list already
ranked nearest first. If there is none, the title is the nearest place. A feature
lookup that fails is an error against the row and does not drop silently to the
nearest place. The comparison stays in floating point (`angleDiffDeg`), because
`relativeAngleDeg` rounds to whole degrees and would let 25.4° pass the inclusive
25.0° edge.

**A magnetic bearing is read as true.** The phone's EXIF says whether the direction is
measured from true or magnetic north. Nothing on the box records magnetic variation:
the track keeps position, speed and course over ground, and SignalK is read for none.
So the bearing is stored and used as it came, and a magnetic one is treated as true
until a variation source exists. The error is the local variation, a few degrees on
this coast, inside the 25° half-width. The reference is not sent or stored (it would
be a no-op), and it comes back with the variation source.

**Filing to a passage makes the sign inside the copy's link step.** As for equipment
and jobs, `inboxLibrary.copy` takes a link callback that runs under the content lock.
The sign is created there, after a check that no sign already shows the document, so
the earlier "a sign already uses this photo" retry still clears the item without a
second sign. If the sign fails, `copy` removes only a document it just made, in the
same locked step, and leaves one that was already there. The separate post-hoc
discard is gone. `openJobDestination.check` now reads the job's status alone
(`OpenJobStatus`) instead of loading its photos.

**The sign editor shrinks on the device when it needs to, and the box owns the
folder.** A picked file is checked first: `isSignUploadFile` refuses anything that is
not a JPEG, PNG, GIF, WebP or HEIC/HEIF photo with "Pick a photo (JPEG, PNG, GIF, WebP
or HEIC)". An accepted photo goes through `downscaleForUpload` (2560 px on the long
edge, the same `UPLOAD_MAX_DIMENSION` the Inbox shrinks to, as a JPEG) only if it is HEIC
or HEIF, is over about 4 MB, is longer than 2560 px on its long edge (read from the
header, with no decode), or has a size the header does not give. Anything else goes as it
is, so a transparent PNG or an animated GIF is not flattened to a JPEG. The capture time
is read from the original first, as before. The Inbox itself does not call that helper,
since it makes its preview and fingerprint from the same decode. HEIC and HEIF are
accepted by type or name and come out as JPEG. A photo that needs shrinking and cannot be
shrunk is an error, and the original is not sent in its place. The Passage photos folder comes from
`POST /api/passage-photos-folder`, which calls `FindOrCreateRootFolder`, the one rule
the Inbox already used. The browser no longer reads the folder tree or creates
folders. Photos picked from the library are used as they are.

## What leaves the boat

Only when Mate is set up: each arriving photo's preview goes to OpenRouter (the
document model) for sorting, and the stored photo (or its preview, when no named
feature is in range) goes again when titles are asked for. A PDF is never sent. The
prompt and the five features go with the title photo.
No cost is recorded for these calls; OpenRouter's own usage shows it.

## Consequences

- A photo costs one small call on arrival and one larger one if it is titled by Mate.
- A photo whose sort failed, for any reason, stays in Unsorted with one plain line
  and the time of the next try, and is tried again by itself after 1 minute, 5, 30,
  then hourly, until it works or the operator files or deletes it. A settings save or
  **Sort again** tries it at once. A failure that never clears (a photo with no
  preview) costs one log line an hour.
- Items that arrived before Mate was set up are sorted when settings are saved, when the
  list is read with Mate on, at the next upload or at restart.
- The list response grows two fields per item (`classify_error` and `next_try_ms`)
  and one flag, plus `mate_error` when the settings cannot be read, and the title
  answer gains `errors` and, when the settings cannot be read, `mate_error`.
- While Mate sorts, the page re-reads the list after 4 s, then 8 s, then every 15 s,
  and stops after ten minutes with nothing sorted and nothing new. Reopening the Inbox
  starts it again. While any item waits on a retry it also reads about every 60 s with
  no give-up, and once more just after the earliest `next_try_ms`.
- An older `inbox_items` table gains its columns when the store opens, and its old
  pending-items index is replaced by one on the items with no kind.
- Mate does not choose an equipment item or a job. It only says a photo looks like gear.
