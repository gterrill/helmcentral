# Set up a maintenance schedule

Get an item's service jobs into Helmcentral so the Maintenance list can
tell you what's due, whether the boat already has years of history behind
it or the item was fitted yesterday.

Prefer to describe it and check a card? Mate can propose the rules for you;
see [Set up a maintenance schedule with Mate](set-up-a-maintenance-schedule-with-mate.md).

## Use the equipment profile's schedule

An item that uses an equipment profile gets that profile's service jobs
automatically. There is nothing to copy: open the item from **Inventory →
Equipment** and its **Maintenance** block lists them under **From profile**.
Fix an interval on the profile and every item using it follows at once.

A job the profile names but gives no interval for shows **Interval not
set**. Fill it in on the profile when every unit of that model is serviced
the same way (from **Inventory → Profiles**), or on the item when it's this
one unit only.

## Change a profile job for this item only

Some units differ: an impeller in silty water wants changing sooner, or a
job the manufacturer lists doesn't apply to how this unit is fitted.

1. Open the job from the item's **Maintenance** block.
2. Each field shows the profile's value. Change the description, the hours
   interval or the months interval, or turn on **Not applicable to this
   item**.
3. Save. The changed value is marked on the item's page and in the
   Maintenance list, so you can see at a glance which jobs no longer follow
   the profile.
4. **Reset to profile** beside a changed field puts it back.

Due-soon windows and a fixed due date are set the same way and are always
this item's own.

## If the item changes profile or a job leaves the profile

Choosing a different profile for an item shows what happens first: which
jobs keep their history, which leave the schedule and which arrive new.
A job that leaves (because the item changed profile, or the job was taken
out of the profile) keeps its history. It moves to **No longer in the
profile** on the item's page, where you can read its log or delete it.

Saving a profile that drops a job some item has history for asks you to
confirm first, and names the items. A profile any item still uses can't be
deleted until those items use another one.

## Add a rule by hand

For gear with no profile, or a job the profile doesn't cover. These show
under **Added for this item**:

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
