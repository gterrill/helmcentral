# Log a service

Record a completed job, a repair, or an improvement against a piece of
gear - whether it came from a scheduled rule or not.

## Complete a scheduled rule

1. From **Inventory → Maintenance**, find the rule and tap **Complete**.
   (A rule already acknowledged, or overdue, still completes the same way.)
2. The date defaults to today; the hours field fills in automatically from
   the live gauge reading when the item has one - the field always wants
   the number on the physical gauge, never a hand-calculated running
   total, and Helmcentral shows how long ago that live figure came in. For
   a rule that runs on hours, this field is required - if there's no live
   reading, read the current hours off the gauge and type them in.
3. For a certificate or expiry with a fixed date and no recurring monthly
   interval (a one-off renewal), enter the new due date - there's no
   interval to work it out from automatically. A rule that renews every so
   many months shows the date it's calculating instead, with nothing to
   type in.
4. Add what was done, who did it, the cost and currency if you're tracking
   spend, and any parts used - search inventory for a spare you fitted and
   set the quantity.
5. Tap **Complete**. The rule resets to count down again from today, and
   any acknowledgement on it clears.
6. Add photos to the entry if you like - a data plate, the old part, the
   finished job - then close.
7. If the rule has a linked procedure, a link to open it is offered in case
   anything about the steps needs updating. It only opens if you tap it.

## Log a repair or improvement with no rule

Not every job is scheduled. A repair after a breakdown, or an upgrade,
still belongs in the item's history:

1. Open the item's own page from **Inventory → Equipment**.
2. In the **Maintenance** block, tap **Add log entry**.
3. Choose **Repair** or **Improvement**, fill in the date, the hours off
   the gauge, description, who did it, cost and parts as it applies.
4. Save, then add photos if you like.

## Edit or delete a log entry

Open the entry from the item's own Maintenance block to change any of its
details, add or remove photos, or delete it. Deleting a log entry never
deletes a photo it carries unless nothing else is using it - the photo
strip works the same way it does for an equipment item's own photos.

## Export the service log

1. Open **Inventory → Maintenance**.
2. Tap **Export CSV** for the whole log, or open an item's own page for
   just its own history and export from there.
3. The file opens in any spreadsheet program - one row per entry, with the
   item, the rule it was against, what was done, who did it, the cost, and
   any parts used. The hours column is the true running total (any gauge
   reading plus a meter replacement's own offset), not the raw gauge
   figure that may have been typed in on the day.
