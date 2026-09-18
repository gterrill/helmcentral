# Create a dashboard page

A page is a named arrangement of instrument tiles, for example "Anchored" or
"Underway". Most boats end up with a handful: one for the helm at a glance,
one for the engine watch, maybe one that only shows while the anchor is down.
This walks through building one from nothing.

With write access, on a screen at least 1024px wide:

1. Open the page switcher in the header (the current page's name, top right)
   and choose **New Page**. Helmcentral creates the page straight away, names
   it "Untitled page" for now, and turns layout mode on. No dialog asks for a
   name first; you name it in place, next.
2. The toolbar at the top of the grid opens with the name field already
   focused. Type a name and press Enter, or click elsewhere to save it. Click
   away without typing anything and the page keeps the name it has, since an
   empty name is never saved.
3. With nothing on the page yet, the grid shows a short prompt instead of
   sitting blank. Its link opens this same guide, in case you want it again.
4. Choose **Add Widget**. Tiles are grouped by what they are for: Navigation,
   Weather, Situational, Power, Engine, Systems, At a glance, and Custom. A
   tile already on the page greys out with an "On page" note. Gauge, Gauge
   Group, Engine Cluster, Indicators, Embed and Nearby map go on a page more
   than once, so those stay selectable regardless.
5. Pick a tile. It lands at a size chosen for what it shows rather than one
   blanket default: Battery & Power, for instance, arrives tall enough for
   its Shore line. Add as many as you want, repeating step 4 for each.
6. Drag a tile by its handle (top left corner) to move it, or drag its
   bottom-right corner to resize it. Let go and Helmcentral saves. There is
   no separate save step.
7. Remove a tile with the X on its top right corner. A gauge, gauge group,
   engine cluster, indicator strip, embed or Nearby map duplicates with the
   copy icon beside it. A built-in tile does not, since only one of each fits
   on a page.
8. Set the page's look with the rest of the toolbar. **Skin** switches
   between the app theme and the always-dark instrument skin. **Hero**
   promotes one placed tile to an enlarged, full-width row above the rest of
   the grid. **Kiosk** adds this page to the wall display's rotation, if you
   run one. See [The dashboard](../features/dashboard.md) for what each of
   these changes.
9. When the page looks right, press **Done**, or toggle layout mode off in
   the header, to leave editing. The toolbar and prompt disappear, and the
   page is what everyone connected to Helmcentral now sees when they open it.

## Renaming a page later

Reopen layout mode on the page you want to rename and edit the same name
field at the top of the toolbar. That field is the only place a page gets
renamed. The page switcher has no rename control.

## Deleting a page

Reopen layout mode on the page you want to delete. **Delete page** sits at
the far end of the toolbar, after Kiosk and set apart from the rest of the
row, because it is the one destructive control there. Confirm in the dialog
that follows. This removes the page and every tile on it, and you cannot undo
it.

The control does not appear at all with only one page, since a board always
keeps at least one and there is nothing to delete down to. Deleting, like
renaming, happens only from the toolbar.

## Below 1024px

The grid itself still shows and updates live on a phone or a narrow tablet,
but layout mode does not open there: at that width there is no room for a
drag target you could actually hit. Add, move, resize and name tiles on a
wider screen and they simply show up, already arranged, everywhere else.

Creating and deleting a page needs that same width, for the same reason. A
page made below 1024px would have no toolbar to name or remove it with, so
**New Page** stays hidden from the page switcher there. Picking an existing
page and reordering the list work at every width.

## Reordering and the rest

Page order, the indicator ribbon and the wall-display rotation are their own
topics. See [Reorder dashboard pages](reorder-dashboard-pages.md), [Pin an
indicator ribbon](pin-an-indicator-ribbon.md), and [Set up a wall
display](set-up-a-wall-display.md).
