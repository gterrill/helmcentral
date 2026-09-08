# Pin an indicator ribbon

With write access, in layout mode:

1. Toggle layout mode in the header (desktop widths only).
2. Choose **Ribbon** next to the page's hero and skin controls. The first
   time, with nothing pinned yet, the dialog opens already carrying a lamp
   for whatever it can find on the boat's own bus: an engine's revolutions, the
   generator's state, shore power, an alternator's charging mode. Nothing is
   pinned until you choose Save, so it's still fine to change or clear any of
   this before it's kept.
3. Add a lamp by hand: give it a SignalK path and a short label (12
   characters or fewer). Use "Lit when off" for a signal whose healthy state
   is off, such as a bilge float or a fault line. For a CZone bilge circuit,
   the path is `electrical.switches.bank.<n>.<m>.state`, using the bank and
   circuit numbers CZone assigned it, with "Lit when off" checked if the
   circuit reads off while the bilge is dry.
4. Choose **Suggest lamps** to add anything the boat now publishes that isn't
   already in the list, such as a second generator wired in since you last
   edited the ribbon, or an engine the vessel didn't have before. It only
   appends what's missing; a lamp already in the list, whether you added it
   by hand or it came from an earlier suggestion, is left alone.
5. Reorder lamps with the up and down arrows, or remove one with the trash
   icon.
6. Toggle **Show the CHK indicator** on or off. CHK colours from the worst
   currently active alarm and opens the alarms panel when clicked.
7. Choose **Save**.

The ribbon now appears above the grid on every dashboard page, inside
whatever skin that page uses, with sixteen lamps as the maximum and one lamp
or the CHK indicator required. A strip with neither is rejected: it would
draw nothing at all, which is a mistake, not a choice.

To edit it again, reopen layout mode and choose **Ribbon**. Whatever is
currently pinned loads back into the same dialog, ready to change.

To remove it entirely, open the ribbon dialog and choose **Remove ribbon**.
The row disappears from every page immediately; the grid underneath is
unaffected, since the ribbon was never one of its widgets.

The ribbon is separate from a per-page lamp strip widget. Pinning or removing
it never touches a lamp strip widget you have placed directly on a page, and
the reverse is also true.
