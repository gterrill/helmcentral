# ADR 0112: Displays Are Managed on a Page, Not in a Dialog

## Status
Accepted. Supersedes ADR 0110 section 8's management surface: the dialog and
the nested sidebar group. The rest of section 8 stands, including the reason
wall pages leave the Dashboard page list at all.

## Context

ADR 0110 put display management in a dialog opened from the sidebar, on the
reasoning that ADR 0074 keeps dialogs out of the URL. That rule is about
where transient state belongs; it is not an argument that a surface should
be transient. Applying it here decided the container before anyone had
counted what the container needed to hold.

What it needed to hold was seven controls for the screen itself (name, slug,
canvas width and height, magnification, rotation, and two behaviour flags)
and, once a display owns an ordered rotation, a list of its pages with a
dwell, a condition and a position each. The shipped result put the seven
controls on one inline row inside a table cell. On the author's own laptop
that row squeezed the slug input until it truncated its value mid-word, and
there was nowhere at all to put the page list, so ADR 0110 deferred
per-display ordering as "the one item safe to leave for last". It was not
safe to leave for last. It was the thing the surface existed to do, and the
container could not accommodate it.

Facts that shaped the decision:

- The operator reaches this surface from a nav-station laptop while setting a
  screen up, not from the helm while doing something else. It is a task
  someone sits down to.
- The Documents panel (ADR 0106) already establishes the shape this needs: a
  panel with an index, a drill-down, a breadcrumb back, and a table. It is
  also the only consumer of the table primitive, so following it costs no new
  vocabulary.
- A display's feed order is the global page order. Reordering within one
  display therefore has to swap a page with the previous page *on that
  display* and then rebuild the full list the reorder endpoint demands, which
  is a real operation needing a real affordance rather than a corner of a
  dialog.
- The sidebar group ADR 0110 added nested every display's pages beneath it.
  That group was itself a small version of the index this ADR builds, and
  keeping both would put the same information in two places while re-growing
  the sidebar that section 8 had just shortened.

## Decision

### 1. Two routes, gated on write

`/wall-displays` lists the configured displays. `/wall-displays/<slug>` edits
one. Both are gated on write access, not admin, which is the same reasoning
ADR 0110 used to keep this out of Settings: an operator who can author a wall
page must be able to create the screen to put it on. That rejection stands
unchanged.

The management route is deliberately **not** `/displays/<slug>`. That spelling
sits one character from `/display/<slug>`, the wall route an operator types
into a kiosk browser, and a typo would put the management interface on a
1920x360 strip instead of the rotation: a wall that looks broken rather than
an error anyone can act on. One hyphen removes the failure mode entirely, and
a comment in the parser says so, because the next person to read those two
branches will be tempted to tidy them into each other.

### 2. The index is a table, and its empty state teaches

One row per display: name linking to its editor, the `/display/<slug>`
address, the canvas and magnification, the rotation, how many pages are on
it, badges for the two behaviour flags when they are on, and a preview action
opening the wall itself in a new tab.

With no displays configured the surface explains what a wall display is and
that the canvas numbers come from running the probe on that screen, rather
than saying that there is nothing here. A first run is the only time anyone
needs that explanation and the only time the surface has room to give it.

### 3. The editor is a toolbar and two field groups

Preview and delete act on the display as a whole rather than on any one
field, so they sit in a toolbar under the breadcrumb, in the same control
vocabulary the dashboard's own layout toolbar uses. Stranding them inside
the settings card, which is where they first landed, put page-level actions
among field-level ones and left a band of dead space behind.

The rest is two field groups, named by their own legends: **Display
settings**, holding one row for the geometry and, below a separator, one row
for the two behaviour flags, which describe how the panel behaves rather
than how the board is laid out; and **Pages**, holding the add controls and
the table together, since the controls act on the table directly beneath
them. The legends use the existing field primitives rather than bespoke
headings, so the section names carry the same micro-label treatment as every
other label in the app.

The pages table lists the display's pages in feed order: position, the page
name linking to `/dashboard/<id>` where tiles are actually arranged, dwell,
condition, duplicate, and remove.

Ordering uses up and down buttons rather than drag. The surface is reachable
from a helm touchscreen even though it is not designed for one, up and down
is the affordance the page switcher already uses, and a drag interaction
would have to be rebuilt for touch, pointer and keyboard separately to be
worth having.

### 4. The row action removes, and says so

Taking a page off a display unassigns it; the page continues to exist and
returns to the Dashboard list. The control says "remove from this display"
rather than "delete", because the two are one click apart in the same table
and only one of them is reversible. Deleting a display likewise names what
survives.

### 5. A screen can make its own pages, and says when one is empty

Adopting an existing page is only half the job. Setting a
screen up is exactly the moment its pages do not exist yet, so an adopt-only
picker sent the operator away to create them somewhere else and back again
to adopt them, once per page.

**New page** creates a blank page already on this display, with the dwell
the server requires, and leaves the operator on the rotation they are
composing rather than jumping into the new page. A screen is built by
roughing out its sequence and then filling each page in, so the list is the
place to stay; the page name in the table is the way into the tiles.
**Add page** keeps adopting an existing page, because moving a page that
already exists onto a screen is a different job from starting a new one.

This exposes an existing rule that had never been visible. `displayFeed`
skips a page with no tiles on purpose, because a blank slot held for a whole
dwell is a mistake to skip rather than a screen to render. A page created
here therefore has no effect on the wall until it has a tile on it, which
without a signal reads as the add having silently failed. Each such row is
marked as having no tiles yet, which both explains the absence and names the
next thing to do.

Rejected: creating the page and navigating straight into layout mode. It
reads as helpful for the first page and becomes a round trip for every page
after it, which is the cost the adopt-only picker already charged.

Rejected: suppressing the marker and letting an empty page occupy its slot
on the wall. That trades a visible explanation for a blank screen, which is
the opposite of the project's stance on failures being visible.

### 6. Assignment stays available from the page as well

The layout toolbar's display picker remains. A page-side control and a
display-side control are two views of one relation, and each is the natural
place from a different starting point: composing a rotation works from the
display, while taking the page you are looking at off a wall works from the
page.

Rejected: removing the toolbar control in the name of one way to do things.
An operator editing a wall page would then have to leave it, find the display
and scroll a table to change something about the page already on screen.

### 7. The sidebar drops to one item

"Wall displays" becomes an ordinary nav item like Documents or Forecast. The
nested tree is gone. Wall pages are reached through the display that owns
them, which is the same number of clicks and puts them behind the screen they
belong to rather than in a second list beside the one they were removed from.

## Consequences

- Per-display ordering exists, which is what a rotation actually needs and
  what the dialog could not hold.
- The surface has a URL, so it can be linked, bookmarked and returned to, and
  the browser's back button works across the index and the editor.
- A display's controls are legible at the width they need instead of being
  compressed into a table cell.
- `displays-dialog.tsx` and `display-sidebar-group.tsx` are deleted. Nothing
  else consumed them.
- There are now two places to assign a page to a display, and two ways to put
  one on a screen at all. Both are deliberate (decisions 5 and 6) and are the
  duplication this ADR accepts.
- A page can now exist that is on a screen and shows nothing there. That was
  always possible; this surface is the first to make it, and the first to say
  so on the row rather than leaving the operator to infer it from a wall.
- ADR 0110's reasoning that dialogs carry no URL is not wrong; it was applied
  to a surface that should not have been a dialog. The lesson worth keeping is
  that the container is chosen after counting what it holds, not before.

## Related

- ADR 0110 (wall displays are records), whose section 8 management surface
  this supersedes and whose page-list reasoning it keeps.
- ADR 0074 (hand-rolled path routing), which gains one parsing branch, and
  whose no-URL-for-dialogs rule is what was misapplied.
- ADR 0106 (documents in the binary), the index-and-drill-down panel this
  surface follows so it needs no new interface vocabulary.
- ADR 0107 (new page flow), whose create-and-name flow the add and duplicate
  actions reuse.
