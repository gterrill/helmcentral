# ADR 0280: Sharing to Helmcentral from an Android phone

## Status

Accepted (2026-11-12). Second of four steps after [ADR 0279](0279-an-inbox-for-photos-and-files-shared-from-a-phone.md).
It adds a way in beside the upload page and relaxes one line of
[ADR 0045](0045-web-push-secure-context-and-pwa-shell.md), which kept the service
worker free of `fetch` handling.

## Context

The upload page needs the operator to open Helmcentral, find the page and pick the
photos again. On a Pixel the natural move is the share sheet in Photos. Chrome on
Android offers a web app there when its manifest declares a `share_target`, but
only for an installed app, and the share arrives as a `POST` the page never sees.
Three facts shaped the design.

- A share is a one-shot `POST` with the files in it. If nothing keeps them, they
  are gone, and the phone shows a plain page rather than the app.
- A roll of photos is slow to send over the boat's link and the app can be closed
  half way. A share that starts the upload but remembers nothing cannot resume.
- iOS has no web share target. The upload page stays the way in there and on a Mac.

## Decision

**The manifest declares the share target.** `share_target` posts
`multipart/form-data` to `/share-target`, one field called `files`, accepting
`image/*`, `.heic`, `.heif` and `application/pdf`. Only an installed app appears
in the share sheet, and Chrome refreshes an installed app's manifest on its own
timetable, so an app installed before this change can take a day to offer the
share. Removing and reinstalling it is the quick way, and the help page says so.

**The service worker keeps the files and nothing else.** `sw.js` gains one `fetch`
handler that answers a `POST` to `/share-target` and returns without
`respondWith` for every other request, so the browser treats them exactly as
before. There is still no caching. The handler reads the form data, writes every
file to the waiting list in one IndexedDB transaction, posts `changed` on the
`helmcentral-inbox-queue` BroadcastChannel so other open windows read the list
again, and answers `303` to `/inbox/upload?shared=1`. If the share holds no files
it answers `303` to `/inbox/upload?share_error=no-files` and the page says what was
shared held no files Helmcentral can take. If the share cannot be read or the files
cannot be stored, it answers `303` to `/inbox/upload?share_error=store-failed` and
the page says the files could not be kept. Both log the real cause with
`console.error`, and an open that another window blocks logs too. If it is still
blocked after about five seconds, or the transaction cannot start, the share
answers `store-failed` rather than leaving the phone waiting. The worker never
drops a share quietly.

**If no worker is running, the server says so.** A share that reaches the server
(the app was never opened after install, so no worker is registered) hits `POST
/share-target`, which answers `303` to `/inbox/upload?share_error=no-worker`. The page
tells the operator the files did not reach the app. The files are lost at that
point, but the operator is told, which a bare 404 or 405 would not do.

**The worker is registered when the installed app starts.** Before, it registered
only when the operator turned on push, so a phone that never did had no worker to
catch a share. `main.tsx` now registers it at start-up when the page is a secure
context and `display-mode: standalone` matches, which is the installed app. A
browser tab and a kiosk that is not installed are left alone, which keeps the
spirit of ADR 0045. A registration that fails is logged. Push registers the same
file and path, so the two do not collide.

**The waiting list holds shares.** The list is an IndexedDB database,
`helmcentral-inbox-queue`, with two stores keyed by `id`. `meta` holds one small row per file: name, type,
size, last-modified time, when it was added and, if the last try failed, the
error. `blobs` holds the file data under the same id. The page lists and counts
from `meta` and never loads a Blob; the run reads one file's data at a time.
Removing a file deletes both rows in one transaction. `id` begins with the time added and the file's
place in its batch, so a plain sort gives the order they were added. The
worker is plain JavaScript served as it is and cannot import the app's code, so it
holds its own few lines of schema. `src/lib/inbox-queue.ts` defines the schema
and `src/test/service-worker-share.test.ts` loads the real `sw.js` against an
in-memory IndexedDB and reads what it wrote through the app's own store. The test
fails if the two drift. The app's store drops a connection the browser closed or
that another tab's upgrade asked it to release, and opens afresh on the next call.

Files picked on the upload page do not go on the list. They upload directly, as
before there was a list, so a pick works where IndexedDB is missing or blocked and
a large pick is not copied into storage that may be full. Resuming a pick means
picking again, and the box skips what it already holds. A share is different: it
cannot be picked again, so only shares need the list.

**A file leaves the list only when the box has answered for it.** `uploadFiles`
now reports how each file ended (`stored`, `already_held` or `failed`) as it ends,
and awaits the report. A stored or already-held file is removed. A failed file
stays with its error and is tried again at the next run. The operator can remove
one file or discard them all after a confirmation. A run that cannot ask the box
which hashes it holds fails whole, as before, and leaves the list as it was.
`inbox-queue-runner.ts` feeds the list to the existing pipeline in one pass, so
no second pipeline exists. It takes the ids once and `uploadFiles` reads each
file's data from the phone just before it hashes it, and again just before it
prepares it, letting it go in between, so the run holds one file at a time. That
reads each file twice, which was simpler than hashing in a first pass and
threading the hashes through the progress and failure handling a second time. A
file added while the run goes stays waiting; the banner and the upload page show
it, and the next run takes it.

**One run at a time.** A run belongs to the window, not the page: its progress and
outcome live in `inbox-queue-runner.ts`, so a page that is left and opened again
shows the run going and keeps Upload, Remove and Discard off until it ends, and
a second start in the same window does nothing, with or without Web Locks. When a
run stops part way the message says how many files the box had stored or already
held, and that the rest are still waiting. Across windows, the run takes a Web Lock,
`helmcentral-inbox-upload`, with `ifAvailable`, so a second window finds it taken
and reports that another is uploading, without sending anything. Web Locks reach
Baseline 2022, inside the 2024 target. They exist only on a secure address. On the
plain-http LAN address, which cannot install the app or receive a share, the run
goes ahead with no guard across runs. A second run there only repeats the first:
the box checks every file's hash and refuses a duplicate, so nothing is stored
twice.

**One tap, and nothing starts by itself.** A share lands on `/inbox/upload?shared=1`
with its photos listed and a button, "Upload 7 photos" (files, if a PDF is among
them). Nothing is sent until the operator taps it. The same goes for a resume: when
Helmcentral opens on a phone whose list is not empty, a banner reads "7 photos
waiting to upload" with **Upload now** and **Discard**, and **Upload now** only
opens the upload page, where the same button waits. **Discard** is off while a run
goes.

The button, and **Choose photos and files**, stay off with "Getting the boat's
time zone…" until the boat's time-zone source has settled, so a photo with no
offset of its own keeps its capture time. After about ten seconds they turn on
anyway, and a photo without a zone of its own gets the no-time warning, as it does
when the boat's zone is unknown. The zone is read as each file is prepared, so one
that arrives mid-run is used. When a run ends and the list still holds photos, such
as ones shared while it ran, the page says "2 more waiting" and offers the button
again. If another window of the app holds the upload, the page says files shared
now wait for the next upload.

An unreadable list is shown, not hidden: the banner and, in the installed app or on
a page a share opened, the upload page say the waiting list could not be read and
why. A run changes the list once per file; the snapshot is edited in place, and
the other windows are told once, when the run ends.

The banner appears only in the installed app, which is where shares land, so a
kiosk or a desktop tab loads neither it nor the list's database. The
router rewrites the address bar to its canonical form soon after start-up and drops
the query, so `src/lib/opened-search.ts` keeps the address as it was opened, and
`src/lib/inbox-launch.ts` hands the flags to the page once, to say a share just
arrived or failed. There is no automatic start. An earlier design started at once
after a share, and the operator chose one tap over it. There is no Background Sync and no
Background Fetch: they are Chromium-only, could not run the on-device shrink, and
would upload when the operator was not looking at a metered link. The upload runs
while the app is open.

**A shared photo takes the same EXIF read.** Shared files go through `prepareFile`
like picked ones, so the time, the facing and the phone's position are read from
the original before it is shrunk, and a zeroed GPS block is still missing. A
share may keep a location that the picker hides. Tests pin both cases through the
queue. Whether the Pixel really keeps them is for the first real share to show,
through the compass mark ADR 0279 added.

## Consequences

- A share needs the installed app on an https address (the Tailscale one). A plain
  LAN address cannot install, so the how-to page says to use the secure address.
- A phone holds full-size shared originals in IndexedDB until they upload. A big
  share can fill storage, and the phone is then told the files could not be kept.
- A share made before the worker was ever registered is lost, and the operator is
  told. Opening the installed app once after installing avoids it.
- The worker's `fetch` handler is small and sees every request's method and URL.
  It returns before touching any request but the one share.

## Rejected

- **Uploading from the worker.** It cannot run the page's EXIF, shrink and hash
  code, and a long upload in a worker the browser may stop is worse than one in the
  open app, where the operator can see it.
- **Background Sync or Background Fetch.** See above.
- **Opening the app with the files in memory.** A reload or a closed app would lose
  them. The list is what lets an interrupted batch resume.
- **Putting picked files on the list too.** Copying a large pick into IndexedDB
  fails on quota or in a private window and would block uploading altogether, to
  save a resume that picking again already gives.
- **Falling back to a direct upload when a share cannot be written.** The share's
  files are gone once the worker answers, so the phone is told they were not kept.
- **Registering the worker on every page load.** A kiosk or a browser tab has no use
  for it. Registering only the installed app keeps ADR 0045's care.
