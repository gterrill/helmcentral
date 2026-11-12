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

- A phone records the direction it faced. The Inbox kept the number and dropped
  which north it was measured from.
- Filing to a passage copied the photo into the library, released the content lock,
  and only then made the sign. A failed sign deleted the copy, and a link another
  request had made to the same bytes in between went with it.
- The Replay sign editor uploaded the camera's file as it was, and refused HEIC.

## Decision

**Mate sorts on arrival, off the request.** After an item is stored the upload handler
nudges a small classifier and answers. The classifier reads every item with no kind
and no recorded failure, two at a time, and asks the document model (`document_model`,
Gemini 2.5 Flash by default) for `{"kind": "scenery" | "document" | "equipment"}`
through the client path the document indexer uses. A PDF is a document without a
call. A failed call is tried once more, and a second failure is written to the item's
`classify_error`. Nothing retries it after that, so a failure stays visible. The
sorter reads the 400 px preview the phone sent, about a tenth of the full photo,
because telling a receipt from a coastline needs no more. The columns are
`suggested_kind` (null until sorted) and `classify_error`, added by an idempotent
`ALTER TABLE` on open. A pass runs at boot, which also sorts what arrived while Mate
was off.

**Mate is a normal state to be off.** The classifier asks the same readiness check
the indexer uses (enabled, key, document model). If it fails, nothing is called and
the kind stays null. If the settings or secrets cannot be read, that is an error and
surfaces. The list answer carries `mate_sorting`, so the page knows an item without a
kind is waiting for Mate rather than never going to get one.

**Groups follow the suggestion; the time rule stays underneath.** With Mate set up, a
`document` or `equipment` item goes to Documents or Equipment wherever it was taken.
A `scenery` item takes the PR 1 time rule (a passage, Underway now, Not on a passage,
No time). An item with no kind that no passage claims goes to Unsorted, with the
reason beside it if Mate failed. The group sets the default only: every action stays
open on every item. With Mate off, every item takes the time rule.

**Titles with Mate use the stored photo, the heading and five features.** The
title request sends the 2560 px copy, not the thumbnail: an island or headland is a
few pixels at 400 px, and only photos the operator is putting on a passage are asked
about. The boat's heading is the track point's course over ground, used only while
the speed is above the resting threshold, since a course at rest is noise. The five
nearest named islands and bays within 10 nm follow, each as "name, distance, side of
the bow" ("Cape Capricorn, 3.1 nm, 40° to port"), and with a bearing on the photo the
ones in the sector are marked. The prompt is a constant, `inboxTitlePrompt`, with a
test pinning its key instructions: one title of at most 80 characters, a name only if
the photo shows it, otherwise the subject, no note. A failed call is shown against
the row. It is not replaced by the nearest place. Titles keep the four-at-a-time
workers and blank-on-timeout of the place lookups, with a longer deadline when Mate
writes them (30 s against 8 s), since a vision call takes seconds.

**The nearby features come from the place-names provider.** `inboxFeaturesNear`
resolves the configured place-names plugin and, since the Overpass plugin is both
place-names and POI provider, asks its `fetch_poi` for the `island` and `bay`
categories. The fixed category list has no headland or point, so those appear only
where mapped as an island or a bay. One fetch covers a 0.02° cell (about 1.2 nm) at
10 nm plus a 1 nm margin, shared by every photo taken in it within one request, and
the host ranks, measures and de-duplicates with the Nearby tile's own function. The
plugin caps each category, so a very crowded area can drop the nearest feature. The
plugin's truncation is logged and not hidden further, because the alternative is to
fail the feature where it is most used.

**The bearing sector is pure arithmetic.** `inSector` is true when the camera bearing
and a feature's true bearing differ by at most 25° (edges inclusive, wrapping at
360). The nearest named feature in the sector within 10 nm titles a photo that
carries a bearing, with no Mate. If there is none, the title is the nearest place.
A feature lookup that fails is an error against the row and does not drop silently
to the nearest place.

**The bearing reference is kept, and a magnetic bearing is read as true.** The
phone's EXIF names which north the direction is measured from (T or M). The upload
now sends it (`bearing_ref`) and the item stores it. `cameraBearingTrue` takes an
optional magnetic variation. Nothing on the box records variation: the track keeps
position, speed and course over ground, and SignalK is read for none. So the handlers
pass no variation and a magnetic bearing is treated as true. The error is the local
variation, a few degrees on this coast, inside the 25° half-width. A variation source
can be wired in later without touching the maths.

**Filing to a passage makes the sign inside the copy's link step.** As for equipment
and jobs, `inboxLibrary.copy` takes a link callback that runs under the content lock.
The sign is created there, after a check that no sign already shows the document, so
the earlier "a sign already uses this photo" retry still clears the item without a
second sign. If the sign fails, `copy` removes only a document it just made, in the
same locked step, and leaves one that was already there. The separate post-hoc
discard is gone. `openJobDestination.check` now reads the job's status alone
(`OpenJobStatus`) instead of loading its photos.

**The sign editor shrinks on the device and the box owns the folder.** An uploaded
photo goes through `downscaleImage` at the Inbox's 2560 px and default quality, with
the capture time read from the original first, as before. HEIC and HEIF are accepted
by type or name and come out as JPEG. A photo that cannot be shrunk is an error, and
the original is not sent in its place. The Passage photos folder comes from
`POST /api/passage-photos-folder`, which calls `FindOrCreateRootFolder`, the one rule
the Inbox already used. The browser no longer reads the folder tree or creates
folders. Photos picked from the library are used as they are.

## What leaves the boat

Only when Mate is set up: each arriving photo's preview goes to OpenRouter (the
document model) for sorting, and the stored photo goes again when titles are asked
for. A PDF is never sent. The prompt and the five features go with the title photo.
No cost is recorded for these calls; OpenRouter's own usage shows it.

## Consequences

- A photo costs one small call on arrival and one larger one if it is titled by Mate.
- A photo that fails to sort stays in Unsorted with the reason until the operator
  files or deletes it. Nothing re-runs it.
- Items that arrived before Mate was set up are sorted at the next upload or restart.
- The list response grows two fields per item and one flag, and the title answer
  gains `errors`. The phone shares `bearing_ref` with the bearing.
- An older `inbox_items` table gains its columns when the store opens.
- Mate does not choose an equipment item or a job. It only says a photo looks like gear.
