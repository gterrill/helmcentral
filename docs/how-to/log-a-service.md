# Log a service

Record a completed job, a repair or an improvement against a piece of gear,
whether it came from a scheduled rule or not.

## Complete a scheduled rule

1. From **Inventory → Maintenance**, find the rule and tap **Complete**.
   (A rule that's already acknowledged, or overdue, still completes the same
   way.)
2. The date defaults to today. When the item has a live gauge reading, the
   hours field fills in from it, and Helmcentral shows how long ago that figure
   came in. The field always wants the number on the physical gauge, never a
   running total you worked out yourself. For a rule that runs on hours the
   field is required, so if there's no live reading, read the hours off the
   gauge and type them in.
3. A certificate or expiry has a fixed date and no recurring monthly interval
   (a one-off renewal). For one of those, enter the new due date, because
   there's no interval to work it out from. A rule that renews every so many
   months shows the date it has calculated, with nothing for you to type in.
4. Add what was done, who did it, the cost and currency if you're tracking
   spend, and any parts used. Search inventory for a spare you fitted and set
   the quantity.
5. Tap **Complete**. The rule resets to count down again from today, and any
   acknowledgement on it clears.
6. If you like, add photos to the entry, such as a data plate, the old part or
   the finished job, then close.
7. If the rule has a linked procedure, a link to it is offered in case the
   steps need updating. It only opens if you tap it.

## Log a repair or improvement with no rule

Not every job is scheduled. A repair after a breakdown, or an upgrade, still
belongs in the item's history.

1. Open the item's own page from **Inventory → Equipment**.
2. In the **Maintenance** block, tap **Add log entry**.
3. Choose **Repair** or **Improvement**, then fill in the date, the hours off
   the gauge, the description, who did it, the cost and the parts, as they
   apply.
4. Save, then add photos if you like.

## Edit or delete a log entry

Open the entry from the item's own Maintenance block to change any of its
details, add or remove photos, or delete it. When you delete a log entry, any
photo it carries is deleted too, but only if nothing else is using that photo.
The photo strip works the same way as it does for an equipment item's own
photos.

## Export the service log

1. Open **Inventory → Maintenance**.
2. Tap **Export CSV** for the whole log. To export just one item's history,
   open the item's own page and export from there.
3. The file opens in any spreadsheet program. It has one row per entry, with
   the item, the rule it was against, what was done, who did it, the cost and
   any parts used. The hours column is the true running total (the gauge
   reading plus any meter replacement's offset), not the raw gauge figure
   typed in on the day.
