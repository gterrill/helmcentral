# Pin an indicator ribbon

With write access, in layout mode:

1. Toggle layout mode in the header (desktop widths only).
2. Choose **Ribbon** next to the page's hero and skin controls.
3. Add a lamp: give it a SignalK path and a short label (12 characters or
   fewer). Use "Lit when off" for a signal whose healthy state is off, such as
   a bilge float or a fault line.
4. Reorder lamps with the up and down arrows, or remove one with the trash
   icon.
5. Toggle **Show the CHK indicator** on or off. CHK colours from the worst
   currently active alarm and opens the alarms panel when clicked.
6. Choose **Save**.

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
