# ADR 0096: In-App Manual

## Status
Accepted, extends ADR 0094.

## Context

ADR 0094 staged the operator manual (`docs/features`, `docs/how-to`,
`docs/reference`) into the binary for Mate's `read_manual` tool, but nothing
served it over HTTP and the app itself had no help affordance at all: no
`?` button, no docs link, no `/help` route. The only way to reach the docs
from inside the app was to ask Mate a question and hope it picked the right
page. That works for "explain the upper atmosphere graph" but not for
someone who just wants to read the alarms page start to finish, or who
doesn't know what to ask yet.

A hosted docs site would solve this on shore, but Helmcentral runs on a
boat's own LAN, often with no internet at all, and the whole point of
staging the manual into the binary was to make it available offline. A
link to `github.com` is dead the moment the boat loses its connection. So
the manual needed a way to reach the operator that works from the same
binary already serving the dashboard, over the same LAN, with nothing new
to stand up.

## Decision

**One endpoint.** `GET /api/manual/*` at `tierRead`, serving the same
`globalManual` pages `read_manual` already answers from. A wildcard route,
not a `:id` param, because a page id contains a slash
(`features/dashboard`). Page lookup is an exact string match over the
loaded slice, the same approach `executeReadManual` already uses, so there
is no filesystem path to traverse and no need to sanitise `..` specially:
an id that isn't in the list is just unknown. An empty manual (a build that
skipped `make manual-stage`) answers 503 with the same sentence
`executeReadManual` uses, "the manual is not embedded in this build (run
make manual-stage)", so the message is one the operator has seen before
regardless of which door they came through. An unknown id is 404.

**`docs/index.md` is staged as a manual page too.** It was always the
hand-written contents page for the Diátaxis split; staging it as
`backend/manual/index.md` alongside the three directories gives the sheet a
landing page for free, and gives Mate one more page it can quote from,
without writing a second contents page that could drift from the first.

**A right-hand sheet, opened two ways.** A `?` button in the header, beside
Ask Mate, opens the manual on the page and heading for whatever screen the
operator is looking at: the Forecast panel opens `features/forecast`, the
Alarms panel opens `features/alarms`, Settings → Assistant opens the
`how-to/set-up-the-assistant` page at its "Configure it in Helmcentral"
heading. The sidebar's **Manual** item opens the contents page instead, for
someone who wants to browse rather than jump straight to the current
screen. Every Settings section also carries a small Manual link, since a
setting often needs more explanation than the label next to it can carry.
The mapping from screen to page and heading is a lookup table, checked by a
test that reads the real doc files off disk and confirms every mapped
heading actually exists as a `##` or `###` line: a docs rename that breaks
the mapping fails the build instead of quietly landing the operator on the
top of the wrong page.

**Links inside the manual stay inside the sheet.** A link from one manual
page to another (`alarms.md`, `../how-to/talk-to-mate.md`,
`dashboard.md#widgets`) resolves to a page-and-heading target and
navigates within the sheet, keeping history so Back works. A link to
something the manual doesn't stage, an ADR, an example plugin's own
README, the release workflow file, opens on GitHub in a new tab instead. A
plain `http(s):` or `mailto:` link is left alone. This is the one place
this feature needs a connection: those links are rare (a handful across
the whole manual) and exist for a maintainer or a curious operator with
shore Wi-Fi, not for anything the boat depends on mid-passage.

**The sheet's visual world is the incumbent's.** Same Sheet chrome as
`mate-sheet.tsx`, same Geist Sans prose, same semantic tokens, same 40px
ghost icon buttons. This is a comprehension surface, not a new design
statement.

**The kiosk gets nothing.** No `?` button, no sidebar item, no manual
route reachable from the kiosk shell. Nobody stands at the wall display
asking it questions; it has no keyboard, no mouse and no one addressing
it.

### Rejected

**Linking to GitHub for every manual page, not just the ones outside it.**
The whole reason the manual is embedded in the binary is so it works with
no connection; sending every click to GitHub would throw that away for the
common case to save building a page-resolution rule for the rare one.

**A hosted docs site.** None exists, and standing one up just for this
would duplicate the manual that already ships in the binary, and stop
working exactly when the boat needs it least.

**Per-field help links.** A tooltip or link on every individual setting,
rather than one Manual link per settings section, was considered and set
aside: it is a much larger surface to build and keep in sync with the docs
for one pass, and a section-level link already gets the operator to the
right page.

**A keyboard shortcut.** `Alt+?` is layout-dependent (it isn't `?` on every
keyboard), and F1 already belongs to the browser. Neither is worth
fighting for a button that's one click away in the header.

**A slugging dependency (`rehype-slug`, `github-slugger`).** The heading-id
algorithm GitHub uses is small enough to write directly against the
headings this manual actually has, and the project already renders
markdown with `react-markdown` and `remark-gfm` alone. Adding a dependency
to save writing one function wasn't worth it.

## Consequences

- Renaming a `##` or `###` heading the lookup table points at now fails a
  frontend test instead of quietly sending the `?` button to the top of
  the wrong page. Anyone renaming a heading has to update the table in the
  same change.
- The handful of links that lead outside the manual need a connection to
  resolve. That's a real, if small, gap in the "everything works offshore"
  story, and it's a deliberate one: those links point at things that were
  never staged into the binary in the first place.
- `docs/index.md` now appears in Mate's manual index alongside every other
  page, since staging it for the sheet also staged it for `read_manual`.
  Asking Mate a general "what can Helmcentral do" question can now surface
  the contents page itself, not just a feature page.
- The header and every Settings section each gain one more small control.
  Kept to a single ghost icon button and a single text-and-icon link
  respectively, matching the density the rest of the header and Settings
  already use, so the manual doesn't crowd out what was already there.

## Related

- ADR 0093 (onboard assistant over OpenRouter): the assistant whose tool
  surface this ADR's endpoint parallels.
- ADR 0094 (Mate, voice and app-wide help): staged and embedded the manual
  this ADR serves over HTTP; this ADR adds `docs/index.md` to that staging.
- ADR 0074 (deep links): the `screen` shape (panel, settings section,
  dashboard page) this ADR's lookup table keys off, the same one the
  assistant's screen context already reads.
- ADR 0089 (kiosk feed is a page flag): the shell that gets no manual
  affordance, for the same reason it gets no voice listener.
