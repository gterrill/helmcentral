# Pin an indicator ribbon

You need write access, and you do this in layout mode.

1. Toggle layout mode in the header (desktop widths only).
2. Choose **Ribbon** next to the page's hero and skin controls. The first time,
   with nothing pinned yet, the dialog opens with a lamp already added for each
   piece of mapped equipment that has a running signal. That means each engine's
   revolutions, the generator's state, the charger's shore current, the
   inverter's AC input and each alternator's charging mode. Each lamp is
   labelled with the equipment's own name. Nothing is pinned until you choose
   Save, so you can still change or clear any of this.
3. Add a lamp by hand. Under **Reads**, choose **Equipment** to pick a mapped
   engine, generator, charger, inverter or alternator and the reading to watch.
   Or choose **A path** for a single signal such as a bilge float. Give it a
   short label, 12 characters or fewer. The whole label is always shown. Use
   "Lit when off" for a signal whose healthy state is off, such as a bilge float
   or a fault line. For a CZone bilge circuit, the path is
   `electrical.switches.bank.<n>.<m>.state`, using the bank and circuit numbers
   CZone assigned it. Check "Lit when off" if the circuit reads off while the
   bilge is dry.
4. Choose **Suggest lamps** to add any mapped equipment that isn't already in
   the list, such as a second generator you've mapped since you last edited the
   ribbon. Equipment that isn't mapped to a source gets no lamp. Suggest only
   adds what's missing. A lamp already in the list is left alone, whether you
   added it by hand or it came from an earlier suggestion.
5. Optionally give a lamp a **Group**, such as Propulsion or Power (16
   characters or fewer). Neighbouring lamps with the same group share one
   heading. Choose an **Icon** for the lamp, or leave **Automatic** and one is
   picked from the reading.
6. Reorder lamps with the up and down arrows, or remove one with the trash
   icon.
7. Toggle **Show the CHK indicator** on or off. CHK takes its colour from the
   worst active alarm, shows how many are active along with the worst one's
   title, and opens the alarms panel when clicked.
8. Choose **Save**.

The ribbon now appears above the grid on every dashboard page, inside whatever
skin that page uses. It holds at most sixteen lamps and wraps onto more lines
when the screen is narrow. It needs at least one lamp or the CHK indicator. A
ribbon with neither is rejected, since it would draw nothing.

To edit it again, reopen layout mode and choose **Ribbon**. Whatever is pinned
loads back into the same dialog.

To remove it, open the ribbon dialog and choose **Remove ribbon**. The row
disappears from every page at once. The grid underneath is unaffected, since
the ribbon was never one of its tiles.

The ribbon is separate from a lamp strip tile placed on a page. Pinning or
removing the ribbon never touches a lamp strip tile, and the reverse is also
true.
