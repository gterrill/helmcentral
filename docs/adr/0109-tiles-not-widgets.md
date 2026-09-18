# ADR 0109: Tiles, Not Widgets

## Status

Accepted

## Context

Helmcentral had two words for one object and was shipping both at the same
time.

The split was not random. When the code talked about itself it said
"widget": `validDashboardWidgetIDs`, the persisted `widgets` array,
`dashboard-widgets.ts`, `BuiltinWidgetId`. When anyone described what the
operator actually looks at, they said "tile": twenty-eight `*-tile.tsx`
components, `components/ui/tile.tsx`, ADR 0081's own title, and every
behavioural passage of the manual. `tile-error-boundary.tsx` had both in one
line, rendering a `<Tile>` whose title came from `widgetDisplayName` off a
prop called `widget`.

The operator sat on the seam. A screen reader said "Remove Depth widget"
while the manual it was describing said "the tile dims". `docs/features/
dashboard.md` carried `## Widgets` and `## Tile state` as peer headings and
never said they were the same thing. `docs/how-to/create-a-dashboard-page.md`
managed both in one sentence: "Choose **Add Widget**. Tiles are grouped by
what they are for."

Two things made this worth fixing rather than tolerating.

The first is that it had already caused a defect. Commit 085a1d1 rewrote the
dashboard page for the operator and renamed its heading to `## Instrument
tiles`, which is the right word. Nothing updated the link in
`docs/features/forecast.md` pointing at `dashboard.md#widgets`, so the
manual shipped a dead anchor, and Mate's `read_manual` inherited it. The
frontend test covering link resolution stayed green throughout, because it
asserts on a fixture rather than on a target that exists. Terminology drift
is not only a matter of taste once it starts breaking links.

The second is the assistant. ADR 0093 gave Mate a `read_manual` tool over
these same pages. Mate generates its vocabulary fresh every turn, and a
model's prior for "dashboard component" is overwhelmingly "widget". Every
other channel is a string that can be changed once. That one regenerates,
which means a rename that stops at the codebase leaves the loudest
disagreement in the product still running.

The root cause was in this repository's own policy. `AGENTS.md`'s marine
terminology rule told doc authors to avoid "widgets / components" and then
offered four replacements: "gauges", "dials", "digital readouts",
"instrument displays". No default, four options, so every author picked
differently and the manual accumulated all of them.

The choice of word was derived rather than preferred. The deciding evidence
is that "tile" is what got written when nobody was making a naming decision:
in the component filenames, in the error boundary, in the prose describing
behaviour. "Widget" appears where the system addresses its own internals.
One is the word in use; the other is the word on reflection.

## Decision

### 1. The operator's word is "tile", everywhere

Every UI string, every `aria-label`, every page under `docs/features`,
`docs/how-to` and `docs/reference`, and Mate's answers. Nothing an operator
can read or hear says "widget".

Concretely: the toolbar button is **Add Tile**, the settings section is
**Tiles** (so navigation paths read **Settings → Tiles → Nearby**), controls
are "Remove <name> tile" and "Duplicate <name> tile", the drag handle names
the tile it moves, and the selector is "Hero tile for <page>". The settings
section id moved with its label, so the deep link is `/settings/tiles`: the
id is in the address bar, which makes it something the operator reads.

### 2. The code keeps both words, with a boundary

This is the part that is easy to get wrong by over-correcting. A **widget**
is the config record: an id, its geometry, its settings. It is what the
backend validates, what the `widgets` array persists, what
`dashboard-widgets.ts` types. It has no appearance. A **tile** is the
rendered surface the operator places, drags, resizes and removes.

So `widgetDisplayName(w)` feeding the string "Remove Depth tile" is correct.
A record has a display name; the surface it produces is a tile.

### 3. What does not change

The persisted `widgets` field, `validDashboardWidgetIDs` in
`dashboard_pages.go`, the frontend test asserting the two agree, the
`dashboard-widgets.ts` module, the config types, `widgetDisplayName`, props
named `onAddWidget`, and `data-testid="widget-*"` values. These are the
record layer. Renaming them buys nothing the operator can perceive and costs
a coordinated change against stored state.

### 4. The ADRs are not rewritten

Thirty-four ADRs say "widget", ADR 0081 and ADR 0107 among them. That was
the word at the time and these are the historical record. Rewriting them
would falsify it. `docs/adr/` is excluded from the rename and from the test
in Decision 6.

### 5. Mate's prompt pins the term

`buildAssistantSystemPrompt` now carries a product vocabulary line naming
"tile" and stating that "widget" is not a term this product uses. Removing
the word from the manual takes away the model's in-context anchor; the
prompt line handles the training prior that would otherwise put it back.

### 6. A test holds the line

`docs-vocabulary.test.ts` fails if "widget" appears anywhere under
`docs/features`, `docs/how-to`, `docs/reference` or `docs/tutorials`, in
`docs/index.md`, or in `README.md`. Without it this ADR is a one-time cleanup
that drifts again the first time someone writes a page from memory, which is
exactly how the two headings ended up as peers.

The README is in that list because it was not in the first version of it, and
the review caught the front page still describing "built-in widgets, plus
gauge widgets ... and embed tiles" in one sentence while every page it links
to had already stopped. An enforcement rule is worth only the surfaces it
actually covers.

`AGENTS.md`'s terminology rule now names one word instead of offering four.

### 7. The hero and the kiosk keep the noun

"Tile" is a grid word, and the hero renders above the grid while the kiosk
takes the whole screen. The noun stays constant and the position carries the
distinction: "the hero tile", "a tile shown full-screen". A second noun for
the two non-grid presentations would reintroduce the problem this ADR
closes.

## Consequences

- An operator reading the manual, hearing a screen reader, and asking Mate
  now gets one word for one object across all three.
- `docs/features/forecast.md` links to `#instrument-tiles`, which exists.
- The code still contains "widget" in quantity, deliberately. A future
  reader finding `widgetDisplayName` inside a tile label should read Decision
  2 before "fixing" it.
- The manual's link-resolution tests still assert on fixtures rather than on
  live anchors, so a renamed heading can still orphan a link without failing
  a test. Decision 6 does not cover this. A test that resolves every anchor
  link in the docs tree against the headings that exist is the obvious next
  step and is not in this ADR.
- The settings section id changed with its label, so the deep link is now
  `/settings/tiles`. The id reaches the address bar, which makes it a surface
  the operator reads, unlike the persisted `widgets` field. An old bookmark
  stops resolving. With one operator and no installed base that is a fair
  price for not shipping a URL that says "widgets" above a panel that says
  "Tiles".
- Decision 6 guards the docs and Mate. Nothing guards the frontend: a new
  `aria-label` saying "widget" would pass every test in the repository. Three
  operator-visible strings in `empty-page-prompt.tsx` and the settings nav
  label survived the first pass of this work precisely because no test looks
  for them. A lint rule over user-visible strings is the missing third guard
  and is not in this ADR.
- ADR 0108 is taken on the in-flight documents branch, which is why this is
  0109.

## Verification

`npx vitest run` in `frontend/`: 240 files, 2755 tests, all passing.
`npx tsc --noEmit` clean. `npm run lint` reports 0 errors and 19 warnings,
all of them pre-existing in files this change never touched.
`go test -short ./...` in `backend/` passes; `-short` is what skips the two
live-network BOM FTP tests. `gofmt -l` is clean on both touched Go files.
Two other files in the package are unformatted on `main` already and were
left alone.

Both new tests were checked in the failing direction, not just the passing
one. `docs-vocabulary.test.ts` was run against a deliberately planted
"A widget goes here." in `docs/reference/plugins.md`, and again against one
planted in `README.md` after the README joined its scope. Each failed naming
the file and line; each line was then removed and it passed. The prompt test was
run against a second, ordinary use of the word injected into the rendered
prompt and failed reporting a count of 2; the file was restored and verified
byte-identical afterwards.

The prompt test asserts a count rather than an absence. The instruction has
to name the word in order to rule it out, so a plain "does not contain"
assertion is either impossible or passes on a capitalisation technicality.
Exactly one mention, inside the sentence forbidding it, is the property
worth holding.

Checked by hand afterwards: every `aria-label` on the grid, the settings
nav label against the panel legend it opens, and that
`docs/features/forecast.md`'s anchor resolves to a heading that exists.

## Related

- ADR 0081 (tile state), ADR 0107 (new page flow): the two ADRs that had
  already settled on "tile" for the surface while the picker that places one
  still said "widget".
- ADR 0093 (onboard assistant), ADR 0096 (in-app manual): the pair that make
  Mate a vocabulary channel rather than a passive reader, and the reason
  Decision 5 exists.
