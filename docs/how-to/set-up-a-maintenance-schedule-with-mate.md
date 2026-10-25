# Set up a maintenance schedule with Mate

Have Mate work out a schedule for an item and put it on a card you can check
before anything changes. It saves typing when the manual has a page of
intervals.

Mate never changes the schedule itself. It writes a proposal, and the change is
made only when you tap **Apply**. You need write access to apply one. A
read-only session sees the card but has no buttons.

## Ask for a schedule

1. Open Mate from the sidebar, or from the sheet over any page.
2. Say which item and what you want. For example: "Set up the maintenance
   schedule for the generator from its manual". Or you could say: "The
   generator's oil change was 9 January 2025 at 239 hours, and it's due every
   250 hours or 12 months. Add that."
3. Mate reads the item and its current rules first, and checks the manual if
   you asked it to. Then it answers with a card under its reply.

Give hours as the number on the gauge at the time, the same as the forms in
Maintenance. If the item has had a meter replaced, Helmcentral keeps the
running total for you.

## Check the card

Each change is one line, with the fields it sets listed under it: what a field
is now and what it becomes. Read them the way you'd read the Maintenance list.
Here's what each kind of line means:

- **Add:** These lines are new rules, such as "Add Generator · Belts: every 12
  mo".
- **Change:** These lines show what the rule becomes, such as "Change
  Generator · Oil and filter: now every 300 h".
- **Date and "(meter)":** A line naming a date and "(meter)" sets what was last
  done, or logs a job as done, at that reading.
- **Acknowledge:** These lines carry your reason.
- **A Change line on a profile job:** If the job comes from the item's profile,
  the change applies to this item only, the same as editing it yourself. The
  profile and every other item using it stay as they were.

If a line is wrong, tap **Dismiss** and tell Mate what to change. Mate won't
offer the same proposal again unless you ask.

## Apply it

1. Tap **Apply**.
2. Every line on the card is made together. If anything can't be made, none of
   it is, and the card says why.
3. Once applied, each line links to **Inventory → Maintenance**, where the new
   rules and their status are.

If you tap Apply twice, or reload the page and tap it again, nothing is added
twice. An applied card stays applied.

## If Apply refuses

Apply refuses if the schedule no longer matches the card. That happens when a
rule was edited after Mate wrote the card, by you in Maintenance or from
another device. It also happens when you added the same entries by hand in the
meantime. Nothing is made. The card is marked **Out of date**, names the rule
that changed, and stays that way when you reload the page. If you ask Mate to
redo it, it reads the schedule again and writes a fresh card.

## What Mate can't do here

Mate can't delete a rule or a log entry, and it can't edit an entry already in
the service log. It can't add photos or parts to a job, record a meter
replacement or attach a procedure note either. Do those in **Inventory → Maintenance**. See
[Maintenance](../features/maintenance.md) for the whole feature, and
[Set up a maintenance schedule](set-up-a-maintenance-schedule.md) for doing it
by hand.
