# Set up a maintenance schedule

Get an item's service jobs into Helmcentral so the Maintenance list can
tell you what's due, whether the boat already has years of history behind
it or the item was fitted yesterday.

## Copy a schedule from an equipment profile

If the item was set up using a manufacturer's equipment profile, its
service intervals are already loaded and ready to copy in.

1. Open the item from **Inventory → Equipment**.
2. In the **Maintenance** block, tap **Use profile schedule**.
3. Every job the profile lists is added as a rule. A job the manufacturer
   names but gives no interval for (a "slot") is added too, showing
   **Interval not set** until you fill one in.
4. Tapping this again later only adds jobs you don't already have - it
   never duplicates one you've already copied in.

Rules copied this way are yours to edit afterward - change the interval,
add a due-soon window, or fill in a slot's frequency once you find it in
the manual.

## Add a rule by hand

For gear with no profile, or a job the profile doesn't cover:

1. From **Inventory → Maintenance**, tap **New rule** - or, from the
   item's own page, tap **Add rule** in its Maintenance block.
2. Pick the item, or leave it blank for something that isn't a piece of
   gear - flares, the EPIRB battery, insurance, registration.
3. Enter a description and at least one interval: hours, months, or a
   fixed date for a one-off expiry. A rule with both an hours and a months
   interval shows as due on whichever comes first.
4. Optionally set your own due-soon window (the default is 50 hours or one
   month before the interval is up).
5. Save.

## Record what's already been done

A brand new rule shows **Never recorded** until something is logged
against it. For a boat with existing history, record that history without
inventing a service that never happened through Helmcentral:

1. On a rule showing **Never recorded**, tap **Set last done**.
2. Enter the date it was last done, the hours off the gauge at the time
   (not a hand-calculated running total), or both.
3. Save. The rule now counts down from that baseline - no service log entry
   is written, since none actually happened through the app.

## Link a procedure

To attach the exact steps for a job on your own boat:

1. Open the rule and tap **Create procedure note**.
2. A note titled from the rule opens, ready to write in. Write it as a
   plain description, or as a checklist if the job has a fixed sequence of
   steps - a checklist note offers **Start checklist** wherever it's
   opened, including from a Maintenance rule.

## If the hour meter is ever replaced

A meter or gauge replacement doesn't reset an item's true running hours to
zero in Helmcentral - record the change and hours keep counting correctly.
Every hours figure typed in anywhere on this item (completing a rule,
setting what's already done, a log entry) is always the number on
whichever gauge is fitted at the time; Helmcentral keeps the combined
running total behind the scenes.

1. Open the item's own page.
2. In the **Maintenance** block's hour meter section, tap **Record meter
   replacement**.
3. Enter the old meter's final reading, the new meter's own reading (often
   0), and the date it was changed.
4. Save. Every hours-based rule on the item keeps counting from the true,
   combined total from here on.
