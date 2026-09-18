# Create a dashboard page

A page is a named arrangement of widgets, for example "Anchored" or
"Underway". Most boats end up with a handful: one for the helm at a glance,
one for engine watch, maybe one that only shows while anchored. This walks
through building one from nothing.

With write access, at a screen at least 1024px wide:

1. Open the page switcher in the header (the current page's name, top
   right) and choose **New Page**. The page is created straight away, named
   "Untitled page" for now, and layout mode turns on automatically. You do
   not name it in a dialog first; you name it in place, next.
2. The toolbar at the top of the grid opens with the name field already
   focused. Type a name and press Enter, or click elsewhere to save it. An
   empty name is left alone rather than saved, so the page keeps whatever
   name it already has if you click away without typing one.
3. With nothing on the page yet, the grid shows a short prompt instead of
   sitting blank. Its link opens this same guide, in case you want it again.
4. Choose **Add Widget**. Widgets are grouped by what they're for:
   Navigation, Weather, Situational, Power, Engine, Systems, At a glance,
   and Custom. A widget already on the page shows greyed out with an "On
   page" note; Gauge, Gauge Group, Engine Cluster, Indicators, Embed and
   Nearby map can be added more than once, so those stay selectable
   regardless.
5. Pick a widget. It lands at a size chosen for what it actually shows, not
   one blanket default: Battery & Power, for example, arrives tall enough
   for its Shore line. Add as many as you want; repeat step 4 for each.
6. Drag a widget by its handle (top left corner) to move it, or drag its
   bottom-right corner to resize it. Changes save automatically as you let
   go, no separate save step.
7. Remove a widget with the X on its top right corner. A gauge, gauge
   group, engine cluster, indicator strip, embed or Nearby map can be
   duplicated with the copy icon next to it; a built-in widget cannot, since
   only one of each fits on a page.
8. Set the page's look with the rest of the toolbar: **Skin** switches
   between the app theme and the always-dark instrument skin; **Hero**
   promotes one placed widget to an enlarged, full-width row above the rest
   of the grid; **Kiosk** adds this page to the wall display's rotation, if
   you have one running. See [The dashboard](../features/dashboard.md) for
   what each of these actually changes.
9. When the page looks right, press **Done** (or toggle layout mode off in
   the header) to leave editing. The toolbar and prompt disappear; the page
   is what everyone connected to Helmcentral now sees when they open it.

## Renaming a page later

Reopen layout mode on the page you want to rename and edit the same name
field at the top of the toolbar. That field is the only place a page gets
renamed; there is no separate rename control in the page switcher.

## Deleting a page

Reopen layout mode on the page you want to delete. **Delete page** sits at
the far end of the toolbar, after Kiosk and set apart from the rest of the
row: it is the one destructive control there. Confirm in the dialog that
follows; this removes the page and every tile on it, and can't be undone.

The control does not appear at all with only one page: a board always keeps
at least one page, so there is nothing to delete down to. There is no delete
control in the page switcher; deleting, like renaming, only happens from the
toolbar.

## Below 1024px

The grid itself still shows and updates live on a phone or a narrow tablet,
but layout mode does not open there: there is no room for a usable drag
target at that width. Widgets are added, moved, resized and named on a
wider screen and then simply show up, already arranged, everywhere else.

Creating and deleting a page needs that same width, for the same reason: a
page made below 1024px would have no toolbar to name or remove it with, so
**New Page** is hidden from the page switcher there. Picking an existing
page and reordering the list aren't affected — both stay in the switcher at
every width.

## Reordering and the rest

Page order, the indicator ribbon and the wall-display rotation are their own
topics. See [Reorder dashboard pages](reorder-dashboard-pages.md), [Pin an
indicator ribbon](pin-an-indicator-ribbon.md), and [Set up a wall
display](set-up-a-wall-display.md).
