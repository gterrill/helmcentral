# Watches

A watch is a regular check of the boat. At the times you choose, Helmcentral
reads the boat, builds a short report, and tells you whether anything in it
needs a look. Mate then reads the report and adds what it makes of it. You find
the result on the **Watches** page in the sidebar, newest first, so a skipper
coming on deck at 06:00 can see what happened overnight without asking.

The name comes from the officer of the watch. It's a different thing from an
[anchor watch](anchor-watch.md), which watches the anchor, and from a timed
watch on a reading, which is covered at the end of this page.

## What a watch gives you

- **A report:** The same sections in the same order every time, with the figures
  in your units. It looks the same at 06:00 on Tuesday as it did on Monday, so
  you can read it at a glance.
- **Findings:** A short list of the things that need a look, decided by rules you
  can read and change. When nothing needs a look, the list is empty.
- **Mate's review:** A few sentences from Mate on what the report means. It can
  add a finding of its own. It can never remove one.

A watch reads the boat and never changes anything. It doesn't replace an alarm.
If something is wrong right now, an alarm is what wakes you.

## Setting one up

Open **Watches** and tap **New watch**. The page starts empty, with three
templates to begin from:

- **Morning report:** Every day at 06:00.
- **Overnight check:** Every hour from 22:00 to 06:00.
- **At anchor:** Every 2 hours.

A watch has a name, a schedule, the report sections it covers, the thresholds
for its findings, and a box called **What to look out for**. Write what matters
to you there in your own words, for example "the fridge and the port bilge".
Mate reads it with every report.

You can also ask Mate. Say "check the boat every hour overnight" and Mate looks
at the watches you already have, then shows a card for the new one. Nothing is
set until you tap **Apply**.

You can have up to 20 scheduled watches.

## The schedule

You pick a preset, and Helmcentral shows what it means in words and the next
three times it will run.

- **Every hour:** On the hour, every hour.
- **Every few hours:** Every 2, 3, 4, 6 or 12 hours, on the hour.
- **Daily at a time:** For example "Every day at 06:00 boat time".
- **Chosen weekdays at a time:** For example "Mondays, Wednesdays and Fridays at
  07:30".
- **Hours between two times:** For example "Every hour from 22:00 to 06:00".
- **Custom (cron):** Five cron fields (minute, hour, day of month, month, day of
  week) for anything else. When you open a watch whose schedule none of the
  presets can write, the page shows Custom with the fields as they are saved.

If the schedule can't run, for example because it's more often than every
15 minutes, the page says why and **Save** stays off until you fix it.

Here's how a schedule behaves:

- **Boat time:** Times are on the boat's clock, worked out from its position
  and following daylight saving. They're never read as UTC. Every time on the Watches page is on that clock, not
  the clock of the phone or screen you're looking at. When the boat's time wasn't
  known, a run shows its time as UTC and says "boat time not known".
- **No position:** If the position drops out, the boat keeps the time zone of
  its last good position and watches carry on. If the boat has never had a
  position, Helmcentral doesn't know what time it is on board, so it doesn't run
  the watch. It records a skipped run that says "No boat position, so the boat's
  time is not known", once for each missed time. The watch runs when the first
  position arrives.
- **After the box was off:** A watch that was due runs once. Missed runs aren't
  made up.
- **Changing a schedule:** The watch runs at its next time on the new schedule.
  Changing the time never makes it run twice in a day. Switching a watch back on
  works the same way. It waits for its next time and never runs for one that
  passed while it was off.
- **Not too often:** A schedule can't run more often than every 15 minutes.
  Mate reviews every run, and a run every minute would only cost you money.
- **Run now:** The watch's page has a **Run now** button, and you can ask Mate to
  do the same. The run is saved with the others.

## The report

You choose which sections a watch covers. Each one gives its figures, and says
why when it can't.

- **Power:** House bank charge, voltage and current, solar watts, and DC load.
  It also says whether the house bank drained faster overnight than it usually
  does.
- **Anchor:** Whether the boat is inside the anchor watch radius, and how far it
  is from the anchor. With no anchor watch set, it says "No anchor watch set".
- **Alarms:** The alarms active now, and the ones raised since the last report
  (or in the last 24 hours on a first report). Repeats are grouped by title with
  how many times, the first and last time, and "active now" when that alarm is
  still going. Times say "yesterday" or the day when they aren't today. The
  five most frequent titles are named and the rest are counted.
- **Tanks:** Each tank's level, and how much it has moved since the last report.
- **Weather:** Pressure, how it has moved over three hours, and the true wind.
  If you have a planned stay, it also shows the forecast wind there for your
  dates.
- **Instruments:** Any sensor that has frozen, gone out of range, or gone silent.

The report covers the time since the watch last ran. The first report covers the
last 24 hours and says so.

A section that can't report says why. Two examples are "pressure history still
filling" and "the alarm log is not open". It never fills a gap with an older
reading, and an unavailable section is not an all clear.

## Findings

Helmcentral decides what counts as a finding, using rules you can change on each
watch. These are the defaults:

- **Alarms:** Any alarm raised since the last report.
- **Instruments:** Any frozen, out of range or silent sensor.
- **Power:** The house bank draining overnight at least one and a half times
  faster than it usually does.
- **Anchor:** The boat outside the anchor watch radius.
- **Tanks:** A level that has changed by 10 percentage points or more since the
  last report.
- **Weather:** Pressure down 3 hPa or more in 3 hours, or the forecast wind at
  your planned stay reaching your lowest no-go wind from your
  [passage limits](../how-to/set-passage-limits.md). Without passage limits the
  forecast is shown and not judged.

On a watch you can change how many alarms or sensor problems count, how far a
tank has to move, and how far the pressure has to fall. None of the defaults
belong to a particular boat, so check them against yours.

## Mate's review

Whenever Mate is switched on, it reads every report. There's no separate switch
for watches and no spending limit. Turning Mate on is what allows it, the same as
for any question you ask.

Mate gets the report, the findings and your **What to look out for** text. Here
is what it does with them:

- **Judges in context:** It says what changed and what matters, in a few
  sentences.
- **Adds findings:** If it sees something the rules missed, it adds a finding.
  The page marks it as Mate's. It can't remove or soften a finding the rules
  reached.
- **Asks when it isn't sure:** It asks you rather than guessing, and it never
  names a cause it can't see in the figures.
- **Reads only:** It can look at the boat, the alarms, the history, the forecast
  and your logbook. It can't change anything or propose anything.

If Mate isn't ready, for example because no model is chosen, the report is still
recorded and the run says why there is no review.

Each run shows what its review cost. The page also shows the total for the last
24 hours. When a failed request leaves the cost unreadable, the run says "cost
not known". Nothing is paused or skipped because of a cost.

You can tap **Ask Mate about this run** on any run. It opens a conversation with
that run already in front of Mate.

## Notifications

A watch can send its report to your phone or your inbox, so you don't have to
open the page to find out what it saw. A report is not an alarm, so it travels
as its own kind of message. It never appears in the Alarms drawer or as an alarm
banner. It is also never published to SignalK, where it would look like an alarm
to every other device on the network.

Each watch has a **Notifications** section with two choices:

- **Channels:** Any of ntfy, Email, Webhook and Phone and browser push. They are
  the same channels your alarms use, switched on in **Settings** under
  **Alarms**. A new watch starts with every channel that is on. A channel that is
  off shows as off, with a reminder where to turn it on, and nothing is sent to it
  until you do. The SignalK channel isn't offered.
- **Send:** **Only when it finds something** is the default. **Every report**
  sends each report, including the ones with nothing to report.

Here is how **Only when it finds something** decides. A report goes out when it
has a finding that the report before it did not have. It also goes out when you
never got that finding, because the report before it didn't reach you.

- **A standing problem:** A bank that stays low all night tells you once, not
  every hour.
- **A problem that clears and comes back:** It tells you again.
- **A report nobody received:** If every channel was off or failed, the finding
  still counts as new, so the next run tries again. The run says "Not sent" and
  why.
- **What counts as the same finding:** A finding from a rule is the same one
  whatever its figures. A finding that Mate added is the same one when Mate says
  it's the same issue, whatever its wording.
- **Nothing to report:** A report with no findings sends nothing. A run that was
  skipped, for example because the boat had no position, never sends anything.

What you receive depends on the channel:

- **The title:** The watch name, then "3 findings", "1 finding" or "nothing to
  report", for example "Overnight check: 2 findings".
- **ntfy and phone push:** The title, then up to three findings and the opening
  sentence of Mate's review. Tapping a phone push opens that run on the Watches
  page. They arrive at normal priority, so they don't break through
  do-not-disturb the way an alarm can.
- **Email:** The title is the subject, and the body is the whole report: every
  finding, Mate's review and every section's lines.
- **Webhook:** The report as data. See [Watch report webhook](../reference/webhook-payloads.md).

A link to the run needs to know the address you reach Helmcentral on from off the
boat, and Helmcentral can't tell. Set it in **Settings** under **Alarms**, in the
**Notifications** panel, as **Address for links**, for example
`http://boat.local:9091`. It's empty until you set it. With it set, ntfy opens
the run when you tap the message, and the email and the webhook carry the link.
Without it they carry none. The link opens the Watches page with that run at the
top, marked for a moment, even when it's an old run.

Each run on the Watches page says which channels it reached, for example
"Notified: ntfy, Email". If a channel can't be reached, the run says "Not
delivered yet to Email" and why. Helmcentral keeps trying, the same as for an
alarm, for up to 24 hours, and says so in its log each time it fails. A newer
report from the same watch replaces an older one still waiting, so you don't get
a stale report after a fresh one.

Two things to know:

- **One-off watches** don't send notifications. Their report already comes to
  the chat that asked for it.
- **Watches you made before this** send nothing until you pick channels on them.

## History

Every run is kept, newest first, with its report, its findings, Mate's review,
whether it ran or was skipped and why, and its cost.

- **A year:** A run is kept for a year. After that, runs that found nothing are
  dropped.
- **Findings stay:** A run with a finding is kept for good.
- **Delete by hand:** You can delete any run.
- **Space:** A run takes about 5 KB, so an hourly watch adds about 45 MB a year.
  The foot of the Watches page shows how much space your watches are using.

## One-off watches

When you ask Mate to "watch port and starboard engine load for ten minutes",
Mate starts a timed watch on those readings. It samples them once a second for
up to 30 minutes. Now it's a watch like the others. It shows on the Watches page
as "One-off · 10 minutes" while it runs, and then in the history.

- **The report:** When the time is up, the report goes into the conversation
  where you asked, and Mate explains it. It's also kept on the Watches page.
- **A restart:** If Helmcentral restarts during a one-off watch, it picks up
  where it left off. The time it was down shows as a gap in the report. If the
  watch ran out of time while Helmcentral was off, you get the report from the
  samples it had, and the report says so.
- **Settings:** A one-off watch's settings can't be changed. You can open it to
  read its report, and you can delete it.
- **Stop:** Stopping it from the chat ends it with no report. The history shows
  that it was stopped. Deleting it from the Watches page while it runs stops it
  too, and nothing goes to the chat.
- **Reported once:** A report goes to the conversation only once, even if
  Helmcentral restarts right afterwards.

See [Watching a reading](assistant.md#watching-a-reading) for what the report
contains and its limits.

## Limits

- A watch reads the boat and never changes anything.
- A watch doesn't replace an alarm. A finding is something to look at when you
  next look, and an alarm is what wakes you.
- A notification is a convenience. It can be late if a channel is down, and it doesn't wake you the way an alarm does.
- If Helmcentral can't save the history of a run, it stops all watches until you
  restart it. The Watches page says "Watches stopped" and why.
