# Maintenance

Maintenance tracks what every system aboard needs done and when. An oil change
doesn't slip past its interval because nobody wrote down the last one, and a
flare kit doesn't go out of date unnoticed in a locker. The running hours and
calendar dates you'd otherwise keep in a logbook or your head become one list:
what's due, what's overdue, what's been done.

## What it tracks

A maintenance rule is one job on one piece of gear: "engine oil and filter
every 250 hours," "anode check every 6 months," "raw water impeller." For things
that aren't gear at all, a rule can be a calendar date on its own: flares, the
EPIRB battery, liferaft service, insurance, registration. Each rule can run on
running hours, on the calendar, or both, and it shows as due whichever comes
first.

Each rule shows one of these states:

- **Overdue** and **Due soon:** These mean what they say. Due soon is by default
  within 50 hours or one month of the interval, and you can adjust it per rule.
- **Never recorded:** This is its own state for a rule that has never had a
  completion logged. It's better than a wrong guess at how overdue the job
  might be.
- **Interval not set:** This covers a job that's named but has no frequency
  yet. A manufacturer's schedule sometimes lists a service with no interval,
  and this shows that plainly instead of hiding it.
- **Hours unknown:** This appears when a rule runs on hours but the item's hours
  reading has never been received. The calendar side of the rule, if it has
  one, still shows correctly underneath.

## Where hours come from

For gear with a running-hours reading, such as an engine, a generator or a
watermaker, Helmcentral reads the current hours straight from the boat's own
instrument data. It's the same reading the equipment record's hour meter
already uses. If an item has no hour meter set, its hours-based rules can't be
counted, and its record shows a warning until you set one.

An hour meter only changes while the machine is running. The last reading it
sent stays current for as long as the engine is off, whether you're at anchor
or mid-service, so it doesn't show as unknown. The Maintenance list and the
Complete form show how long ago that reading came in.

Whenever you type an hours figure by hand, it's the number on the physical
gauge. That applies when you complete a rule, set what was already done or make
a standalone log entry. Helmcentral keeps the running total ("true hours")
itself and adds whatever a meter replacement changed underneath, so you never
do that arithmetic. If a meter or gauge is replaced, record the change from the
item's own page: the old reading, the new reading and the date. Hours keep
counting from where the old one left off. See
[Set up a maintenance schedule](../how-to/set-up-a-maintenance-schedule.md).

Some gear has no hour reading at all, and some rules have no piece of gear
behind them. For those, you type in an hours figure by hand when you complete
the work.

## Getting started on a boat with existing history

A rule you've just created shows **never recorded** until something is logged
against it. That's honest, but it's not much use if the anode is already six
months into its life. **Set last done** records what was already true (a date,
an hours reading, or both) without pretending a service just happened through
Helmcentral. Completing the rule later works the same either way.

An item that uses an equipment profile follows that profile's service schedule
live, so if you correct an interval on the profile, every item using it
follows.

- **A unit that differs:** It can change a job for itself only. The change is
  marked as such, and you can reset it.
- **A job that doesn't apply:** You can set it to not applicable.
- **A missing or damaged profile:** If an item's profile is missing or damaged,
  its profile jobs aren't shown at all. The Maintenance list says which item
  and why, because Helmcentral won't count down from numbers that may no longer
  be right.

See [Set up a maintenance schedule](../how-to/set-up-a-maintenance-schedule.md).

## Completing a job

**Complete** on a rule opens a short form. It asks for the date, the hours off
the gauge, what was done, who did it, what it cost, and any parts used from
inventory. The hours are filled in automatically where a live reading exists.
A rule that runs on hours needs an hours figure to complete, so type it in from
the gauge if there's no live reading to fill it in.

A certificate or expiry with a fixed date and no recurring interval asks for
the new due date, since there's nothing to calculate it from. One that renews
every so many months works the date out for you. Saving writes one line to the
service log and resets the rule so it counts down again from today. Afterward
you can add photos to the entry: a data plate, a worn part, the finished job.

You can log repairs and improvements that aren't tied to a scheduled rule the
same way, from an item's own page, at any time. See
[Log a service](../how-to/log-a-service.md).

## Acknowledging a job you can't do yet

A rule waiting on a part or a haul-out doesn't need to keep nagging.
**Acknowledge** it with a short reason. It stays overdue or due soon and still
counts, but it shows that you already know, and it sorts below the jobs nobody
has looked at yet. Completing the job clears the acknowledgement.

## Procedures

A rule can link to one written procedure, which holds the exact steps for that
job on your boat. **Create procedure note** starts one titled from the rule,
ready to fill in. If it's written as a checklist, opening it offers to start a
run and tick items off like any other checklist in Helmcentral. Afterward, the
completion form links straight to it, in case anything needs updating.

## The list

**Inventory → Maintenance** shows every rule grouped by status: overdue first,
then due soon, then the rest. You can filter by system, so you can check the
electrical side of the boat without scrolling past the rest. Each item's own
page also shows its rules and recent service history, alongside where the hours
come from. Jobs from the item's profile and jobs added for that item alone are
listed separately.

## Exporting the service log

You can export the whole service log, or one item's own, as a spreadsheet-ready
file from the Maintenance list. It's useful for a broker's survey, an insurance
claim, or handing the history to a new owner. A part named on a past entry
stays there by name, even if that spare is later removed from inventory.
Deleting the item never rewrites what was actually fitted on the day.

## Asking Mate

Mate can read the schedule. Ask it what's overdue, what's coming up on the
generator, or when the raw water impeller was last done. It answers from this
list rather than from memory. The status it quotes is the one shown here,
worked out against today's date at the helm.

If you ask it to review a schedule, it compares your rules with two things:
the manufacturer's recommended services on the item's profile, and the manuals
in your document library. Then it names the gaps:

- **Missing rule:** A service the manufacturer lists that you have no rule for.
- **Different interval:** An interval that differs from the book.
- **No last-done date:** A rule that has never had a last-done date.

If an item's running hours aren't being read at the moment, Mate says the hours
are unknown, the same way the list does. It won't put a figure in their place.
A rule flagged that way can still read as fine on its calendar side alone, so
treat "hours unknown" as a reason to read the meter yourself.

Mate can also propose changes to the schedule. You can ask it to add a rule,
change an interval, set what was last done, log a job as done or acknowledge a
rule. It answers with a card listing each change in one line, such as
"Generator · Oil and filter: every 250 h or 12 mo, last done 9 Jan 2025 at
239 h (meter)". Nothing has changed at that point. Here is what you can do with the card:

- **Apply:** Tap **Apply** and every change on the card is made together, or
  none of them is.
- **Dismiss:** Tap **Dismiss** to drop the card, then ask again with what you
  want different.
- **A rule edited since:** If a rule was edited after Mate wrote the card,
  Apply refuses and says so, and you should ask Mate to redo it.

Hours on the card are what the gauge shows, the same as the forms here. See
[Set up a maintenance schedule with Mate](../how-to/set-up-a-maintenance-schedule-with-mate.md).

## What this is not

- **Not an alarm:** A rule going overdue doesn't raise a notification the way a
  system warning does. Maintenance is the record and the list, not a reminder
  that follows you around the boat.
- **Not stock control:** Parts used against a job are recorded for the history,
  and quantities on hand aren't adjusted.
- **Not something Mate changes on its own:** Mate can propose additions and
  edits, but only your tap on **Apply** makes them. It can't delete a rule or a
  log entry, edit an existing log entry, add photos or parts, record a meter
  replacement or attach a procedure note. You do those here, in Inventory →
  Maintenance.
