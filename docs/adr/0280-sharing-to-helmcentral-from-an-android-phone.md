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
file to the waiting list in one IndexedDB transaction, and answers `303` to
`/inbox/upload?shared=1`. If the share holds no files, cannot be read or cannot be
stored, it answers `303` to `/inbox/upload?share_error=1` and the page says the
files could not be kept. The worker never drops a share quietly.

**If no worker is running, the server says so.** A share that reaches the server
(the app was never opened after install, so no worker is registered) hits `POST
/share-target`, which answers `303` to `/inbox/upload?share_error=2`. The page
tells the operator the files did not reach the app. The files are lost at that
point, but the operator is told, which a bare 404 or 405 would not do.

**The worker is registered when the installed app starts.** Before, it registered
only when the operator turned on push, so a phone that never did had no worker to
catch a share. `main.tsx` now registers it at start-up when the page is a secure
context and `display-mode: standalone` matches, which is the installed app. A
browser tab and a kiosk that is not installed are left alone, which keeps the
spirit of ADR 0045. A registration that fails is logged. Push registers the same
file and path, so the two do not collide.

**One waiting list serves both ways in.** The list is an IndexedDB database,
`helmcentral-inbox-queue`, with one store, `pending`, keyed by `id`. A row holds the
file as a Blob with its name, type, last-modified time, when it was added and, if
the last try failed, the error. `id` begins with the time added and the file's
place in its batch, so a plain sort gives the order they were added. The
worker is plain JavaScript served as it is and cannot import the app's code, so it
holds its own few lines of schema. `src/lib/inbox-queue.ts` defines the schema
and `src/test/service-worker-share.test.ts` loads the real `sw.js` against an
in-memory IndexedDB and reads what it wrote through the app's own store. The test
fails if the two drift. Files picked on the upload page go on the same list before
anything is sent, so a batch resumes the same way whichever way it came in. If the
list cannot be written, nothing is uploaded: the page says nothing was added.

**A file leaves the list only when the box has answered for it.** `uploadFiles`
now reports how each file ended (`stored`, `already_held` or `failed`) as it ends,
and awaits the report. A stored or already-held file is removed. A failed file
stays with its error and is tried again at the next run. The operator can remove
one file or discard them all after a confirmation. A run that cannot ask the box
which hashes it holds fails whole, as before, and leaves the list as it was.
`inbox-queue-runner.ts` feeds the list to the existing pipeline, taking up files
added while it runs, so no second pipeline exists.

**One loop per device.** The run holds a Web Lock, `helmcentral-inbox-upload`, with
`ifAvailable`, so a second window finds it taken and reports that another is
uploading, without sending anything. Web Locks reach Baseline 2022, inside the
2024 target. They exist only on a secure address, so an in-window flag guards the
same run where they are missing, such as the plain-http LAN address. That
address cannot install the app or receive a share, so a second window there is the
only case the flag leaves open, and a second window means a second tab the operator
opened on purpose.

**Resume is a prompt, not a background job.** When Helmcentral opens on a phone
whose list is not empty, a banner reads "7 photos waiting to upload" (files, if a
PDF is among them) with **Upload now** and **Discard**. **Upload now** opens the
upload page and starts it. Opening `/inbox/upload?shared=1` starts at once. The
router rewrites the address bar to its canonical form soon after start-up and drops
the query, so `src/lib/inbox-launch.ts` reads the flags at import, before the
router, and hands them to the page once. There is no Background Sync and no
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
- A phone holds full-size originals in IndexedDB until they upload. A big roll can
  fill storage, and the page then reports that nothing was added and sends nothing.
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
- **A separate list for picked files.** Two lists would mean two ways to resume and
  two places a file could be stuck.
- **Falling back to a direct upload when the list cannot be written.** It would
  hide the failure and lose the resume the list exists for.
- **Registering the worker on every page load.** A kiosk or a browser tab has no use
  for it. Registering only the installed app keeps ADR 0045's care.
