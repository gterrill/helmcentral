# Maintenance

Maintenance tracks what every system aboard needs done, and when - so an
oil change doesn't slip past its interval because nobody wrote down when
the last one happened, and a flare kit doesn't go out of date unnoticed in
a locker. It turns the running hours and calendar dates you'd otherwise
keep in a logbook or your head into a single list of what's due, what's
overdue, and what's been done.

## What it tracks

A maintenance rule is one job on one piece of gear - "engine oil and
filter every 250 hours," "anode check every 6 months," "raw water impeller"
- or, for things that aren't a piece of gear at all, a calendar date on its
own: flares, the EPIRB battery, liferaft service, insurance, registration.
Each rule can run on running hours, on the calendar, or both, and shows as
due whichever comes first.

- **Overdue** and **Due soon** work exactly as they sound - due soon by
  default within 50 hours or one month of the interval, adjustable per
  rule.
- **Never recorded** is its own state for a rule that's never had a
  completion logged, rather than a wrong guess at how overdue it might be.
- **Interval not set** covers a job that's named but doesn't have a
  frequency yet - a manufacturer's schedule sometimes lists a service with
  no interval attached, and this shows that plainly instead of hiding it.
- **Hours unknown** appears when a rule runs on hours but the item's hours
  reading has never been received at all - the calendar side of a rule, if
  it has one, still shows correctly underneath.

## Where hours come from

For gear with a running-hours reading - an engine, a generator, a
watermaker - Helmcentral reads the current hours straight from the boat's
own instrument data, the same reading the equipment record's hour meter
already uses (an item with no hour meter set can't have its hours-based rules counted; its record shows a warning until you set one). An hour meter only changes while the machine it's on is
running, so the last reading it ever sent stays the current one for as
long as the engine is off - at anchor, or mid-service - rather than
showing as unknown; the Maintenance list and the Complete form show how
long ago that reading came in.

Wherever you type an hours figure by hand - completing a rule, setting
what was already done, or a standalone log entry - it's always the number
on the physical gauge. Helmcentral keeps the running total ("true hours")
itself, adding whatever a meter replacement changed underneath, so you
never have to do that arithmetic. If a meter or gauge is ever replaced,
record the change (the old reading, the new reading, and the date) from
the item's own page, and hours keep counting correctly from where the old
one left off - see
[Set up a maintenance schedule](../how-to/set-up-a-maintenance-schedule.md).

Gear with no hour reading at all - or a rule with no piece of gear behind
it - takes an hours figure by hand instead, entered when you complete the
work.

## Getting started on a boat with existing history

A rule you've just created shows **never recorded** until something is
logged against it, which is honest but not useful if the anode's already
six months into its life. **Set last done** records what was already true
- a date, an hours reading, or both - without pretending a service just
happened through Helmcentral. Completing the rule later works exactly the
same either way.

If an item was set up from an equipment profile, its manufacturer's
service schedule can be copied in with one action - see
[Set up a maintenance schedule](../how-to/set-up-a-maintenance-schedule.md).

## Completing a job

**Complete** on a rule opens a short form: the date, the hours off the
gauge (filled in automatically where a live reading exists), what was
done, who did it, what it cost, and any parts used from inventory. A rule
that runs on hours requires an hours figure to complete - type it in from
the gauge yourself if there's no live reading to fill it in for you. A certificate or expiry with a fixed
date and no recurring interval asks for the new due date, since there's
nothing to calculate it from automatically; one that renews every so many
months works it out for you. Saving it writes one line to the service log
and resets the rule so it starts counting down again from today. Photos can
be added to the entry afterward - a data plate, a worn part, the finished
job.

Repairs and improvements that aren't tied to a scheduled rule can be logged
the same way from an item's own page, at any time. See
[Log a service](../how-to/log-a-service.md).

## Acknowledging a job you can't do yet

A rule waiting on a part or a haul-out doesn't need to keep nagging.
**Acknowledge** it with a short reason - it stays overdue or due soon and
still counts, but shows that you already know, and sorts below the jobs
nobody's looked at yet. Completing the job clears the acknowledgement.

## Procedures

A rule can link to one written procedure - the exact steps for that job on
your boat. **Create procedure note** starts one titled from the rule, ready
to fill in; if it's written as a checklist, opening it offers to start a
run and tick items off exactly like any other checklist in Helmcentral. The
completion form links straight to it afterward, in case anything needs
updating.

## The list

**Inventory → Maintenance** shows every rule grouped by status - overdue
first, then due soon, then the rest - filterable by system, so the
electrical side of the boat can be checked without scrolling past the
rest. Each item's own page also shows its own rules and recent service
history, alongside where the hours come from and the option to copy in a
profile's schedule.

## Exporting the service log

The whole service log, or one item's own, exports as a spreadsheet-ready
file from the Maintenance list - useful for a broker's survey, an
insurance claim, or handing history to a new owner. A part named on a past
entry stays there by name even if that spare is later removed from
inventory - deleting the item never rewrites what was actually fitted on
the day.

## Asking Mate

Mate can read the schedule, so you can ask it what's overdue, what's coming
up on the generator, or when the raw water impeller was last done, and it
answers from this list rather than from memory. The status it quotes is the
one shown here, worked out against today's date at the helm.

Ask it to review a schedule and it compares your rules with the
manufacturer's recommended services on the item's profile and with the
manuals in your document library, then names the gaps: a service the
manufacturer lists that you have no rule for, an interval that differs from
the book, a rule that has never had a last-done date.

If an item's running hours aren't being read at the moment, Mate says the
hours are unknown, the same way the list does. It won't put a figure in
their place. A rule flagged that way can still read as fine on its calendar
side alone, so treat "hours unknown" as a reason to read the meter yourself.

Mate can't change anything. It will tell you what it would change, and you
make the edit here.

## What this is not

- Not an alarm. A rule going overdue doesn't raise a notification the way
  a system warning does - this cycle is the record and the list, not a
  reminder that follows you around the boat.
- Not stock control. Parts used against a job are recorded for the history;
  quantities on hand aren't adjusted.
- Not something Mate can change. Mate reads the schedule and the service
  log, but adding a rule, setting a baseline or marking a job done is still
  done here, in Inventory → Maintenance.
