# ADR 0179: Updates ask before reloading

## Status

Accepted (2026-10-15). Supersedes the automatic reload introduced in
4f8f9cc (focus-checked at most hourly) and the post-reload toast of 0b27414.
No earlier ADR recorded either; they lived in the code and its comments only.

## Context

Since v0.39.0 the app polled `/api/health` and called `window.location.reload()`
by itself when the build changed, unless a list of "unsaved work" said no. That
list was built page by page and missed the note editor (the viewer's Edit mode
and the manual folder view), the new-note sheet and the maintenance log
dialogs. A note draft was lost to a reload with nothing on screen to say why.

The "Helmcentral updated to vX" toast had the opposite problem. It fired only
from a `sessionStorage` marker that the old page wrote just before its own
automatic reload. A manual refresh, a new tab or a Home Screen relaunch never
produced it, and the wall display has no toaster. The boat went from v0.41.0
to v0.45.0 without it ever appearing; the first one arrived in v0.43.0 on a
page that happened to reload itself.

Detection was slow as well: the first hour after load was never checked, and
focus returns checked at most hourly.

Lazy chunks are served immutable from the embedded build, so after a deploy a
page still running the old bundle fails to load any chunk it had not fetched
yet. Nothing handled that.

## Decision

1. **Detect every five minutes.** While the page is visible the build is read
   every five minutes, and again on focus or visibility return (at most once a
   minute). The first successful read is the baseline. A failed or malformed
   read never counts as an update; it is logged.
2. **An attended screen never reloads by itself.** A changed version or
   revision raises an "Update available" toast with a Reload action (no
   auto-dismiss, once per detected build per tab) and turns the sidebar
   version line into "Update available · Reload". Reload asks "You have
   unsaved changes. Reload anyway?" when anything is unsaved.
3. **Unsaved work is reported by the screens that hold it.** The note editor,
   the new-note sheet and the maintenance log dialogs register a navigation
   guard that never holds a move (`useUnsavedWorkReport`), alongside Settings,
   Document Details and the Inventory panel, which already register real ones.
   "Any guard registered" is "unsaved work", with unsent Mate drafts counted
   on top. A screen that forgets to report still loses its draft only if the
   operator confirms a reload; nothing reloads behind their back.
4. **Unattended screens reload themselves.** The wall display and a dashboard
   running as a Home Screen app (`display-mode: standalone`) reload as soon as
   an update is seen, without waiting for alarms (the alarm is still there
   after the reload). A Home Screen app counts as unattended only while it is
   on a dashboard page: on Documents, Settings or any other panel someone is
   using it, so it prompts like a browser tab. A dashboard in the browser's
   Fullscreen API full screen is attended: a reload would drop it out of full
   screen, so it gets the prompt. An unattended screen holds the reload while
   unsaved work exists and retries after each later check.
5. **The "updated" message compares versions, not page lifetimes.** Each
   browser keeps the last version it showed in `localStorage`. After the first
   successful health read, a stored version that differs from the running one
   shows "Helmcentral updated to vX" (with a Release notes action for release
   tags) and is replaced. No stored version is stored silently. `dev` never
   announces, and neither does a revision-only change. If storage cannot be
   used the message is skipped and the reason logged. The wall display shows
   its own few-second, non-interactive notice inside the display shell.
6. **A lazy chunk that fails to load re-checks the build.** On
   `vite:preloadError` the event is handled (default prevented, logged) and
   `/api/health` is read again. A changed build shows "This screen needs the
   update. Reload to open it." with Reload; otherwise "Couldn't load this
   screen. Check the connection." with Retry, which checks again. Neither
   reloads an attended screen.

## Rejected

- **Reload automatically when nothing is at stake.** It needs a complete list
  of unsaved work, and the next form added breaks it silently. That is how the
  note drafts were lost.
- **Autosave drafts everywhere.** A large change to every editor to avoid one
  confirmation on a rare event.
- **Detect through SSE reconnects.** More server work for little gain over a
  five-minute poll of an endpoint that is already cheap.
- **Hold wall reloads for active alarms.** The alarm is still active after the
  reload, and a wall that stays on an old bundle for the length of an alarm is
  the worse outcome.

## Consequences

- An attended page can run an old bundle for as long as the operator leaves
  the toast alone. Settings and the rest keep working against the new backend
  as before; only chunks not yet fetched can fail, and that case is handled.
- `React.lazy` caches a rejected import, so Retry cannot re-run the same
  import in place. It re-checks the build and reports; the screen opens again
  after a reload or, for a transient failure, on the next attempt that mounts a
  fresh component.
- Operator-facing wording is in `docs/how-to/upgrading.md`.
