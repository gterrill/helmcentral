# Make a passage plan

Use this when you know where you're going and when, and you want the weather
for every day of the run in one table. See [Passage plans](../features/forecast.md#passage-plans)
for what the table shows and where it stops.

## Make one

1. Open the Forecast page.
2. Choose **New plan**.
3. Type a **Name**, for example "Home run", and set the **First arrival**. That's
   the day you reach your first stop, local to that place.
4. Under **Add a stop**, type the name of a marina, harbour or bay and choose
   **Search**. Each result shows how far it is from the boat and in which
   direction. Choose the right one.
5. Set the **Nights** you'll stay. Choose **+1 night** to add one.
6. Repeat from step 4 for each place, in the order you'll visit them. Use the
   arrows on a stop to move it.
7. Choose **Save plan**.

Every date after the first follows from the nights before it. Add a night to
one stop and every later stop moves a day.

## Steer a leg

Helmcentral measures a leg by water, along the shortest route that clears the
shore. If that route isn't the one you'll sail, add a via place.

1. Choose **Change plan**.
2. On the stop the leg leaves from, type a place under **Via** and choose
   **Search**, then choose the right one. Add more in the order you'll pass
   them.
3. To leave at a fixed time, set **Leave at**.
4. Choose **Save plan**.

In the table, choose **Route** on a passage row to see the path on a small map.
If Helmcentral can't route a leg, the row says why and shows no distance. It
never draws a straight line. When the charted shoreline closes off the water
between two places, the row names them. Move a stop or add a via place.

## Read the table

1. The page opens on the plan whose dates include today, or the next one coming
   up. Choose another plan from the buttons above the table.
2. Each row is a day. A passage row shows the leg, its distance, when you leave
   and when you should arrive. Its wind, gusts and waves are read at the
   middle of the leg, with the wind marked ahead, abeam or astern of your
   course. A lay day shows the wind and waves from 6 PM that day to 6 AM the
   next morning. The last stop's final row says **leave**: it is the day you
   sail from it, and **+1 night** is still there.
   If the wind turns during a day, the Wind column shows the turn instead of an
   average, for example "NNW→SE 7 AM" with "astern → ahead" under it. The speed
   range above it has no single direction then, since the day had more than
   one. On a passage row only the turns between your exact departure and your
   arrival count, followed by the turns in the night you arrive, and a lay day
   shows the turns between 6 PM and 6 AM. It's amber when the new wind is at or
   past your marginal wind for that side of the leg. If a row says "changes not
   checked", for example because your passage limits aren't set, hover over it
   for the reason. Read [Wind changes](../features/wind-changes.md) for how
   it's judged.
3. A day the forecast doesn't reach yet says "No forecast yet". Nothing is
   guessed for it.
4. Choose the name in a row to see the full forecast for that place. For a
   passage row that's the middle of the leg, and the heading says so. A leg that
   couldn't be routed can't be opened. The heading says it isn't the boat's
   position. Choose **Boat** to go back.

## Read the verdict

The last column judges each passage against your own passage limits. Set those
first under **Settings → Mate → Passage limits**, as
[Set your passage limits](set-passage-limits.md) describes. Until you do, the
page says the limits aren't set and shows no verdicts.

1. Read the word first. **Go** means every hour of the leg is inside your limits.
   **Marginal** means some hour is past 70 percent of a limit. **No-Go** means
   some hour is past a limit, and the row names it, for example "No-Go: gusts 32
   kn ahead". **No data** means the forecast has a gap along the leg.
2. Look at **Depart → ETA**. A time in plain type is the one you set with
   **Leave at**. A time in italics, like "Suggested 6.40 AM", is Helmcentral's
   suggestion, and the verdict is judged at that time. It's the Go time with the
   fairest stream, or a Marginal one when no time is Go. When every time that
   day is a No-Go, it shows the least bad one, and the verdict says No-Go. To use
   another, choose **Change plan** and set **Leave at**. Helmcentral never moves
   a time you set. The arrival comes from the speed your boat made on the
   passages Helmcentral recorded over the last twelve months. With none recorded
   yet, the row says there's no ETA.
3. If the row says "No daylight window", no departure fits the leg's daylight.
   When your passage limits ask for a daylight arrival, Helmcentral looks from
   first light to the last departure that still arrives in daylight. When they
   don't, it looks to sunset where you leave. Set a **Leave at** if you want to
   see how the leg judges at a time you pick.
4. Look at **Current**. "0.8 kn hindering" means the modelled current runs
   against you at that time. A dash means the model has no figure for the leg,
   and hovering over it says why.
5. Read the line above the table. It says when the verdicts were worked out and
   when the forecast behind them was fetched. They update on their own each
   time the forecast refreshes. After you change the plan, the verdict and the
   current read "…" until the new answer arrives, and a check that fails says
   why.

A leg further out than the forecast says "No forecast yet" and has no verdict.
A leg that would run past a day says it isn't judged yet. On a lay day, the
verdict column shows an official warning that covers that night, and stays empty
when there isn't one.

## Set an arrive-by

Use this when you have to be somewhere by a certain time, such as a haul-out or
a booking, and you want to know how much the run can slip.

1. Choose **Change plan**.
2. On the stop you have to reach, set **Arrive by** to the date and time. Any
   stop can have one except the first, because the plan starts where the boat
   already is. If you move or remove stops so one with an arrive-by becomes the
   first, its row says the arrive-by will be removed when you save. Move the stop
   back to keep it.
3. Choose **Save plan**.

A line above the table now says "Slack 1 day", which is the whole days between
when the plan has you arriving and the day you set. If the plan already has you
arriving later, it says "Late by 1 day" in red. If you've fallen behind, the
line says that first, for example "Behind plan by 4 days. Late by 1 day". The
page then works out your arrival as if the rest of the plan starts from today,
not from the dates you saved, so a plan you haven't kept up with doesn't claim
slack you've already used. A long leg you left on time and are still sailing isn't
counted as late. Below the slack, the page lists
what slipping the plan would meet, one day at a time, and names the worst leg
for each, for example "Slip 2 days: Mooloolaba → Manly Thu meets wind 18 kn
ahead, No-Go (late by 1 day)". The slips take a little longer than the table, so
they turn up after it. To clear an arrive-by, empty the field and save.

## Follow the plan as you sail it

You don't mark anything off. Helmcentral reads the passages it records.

1. A leg reads **Done** once a recorded passage ends within one and a half
   nautical miles of its destination, any time after the plan's first day, so a
   leg you sail early counts. The passage has to start at the last stop you
   reached, and a passage still being recorded counts once it ends. If you sail past a stop and end at a later one, that stop and the legs into it read
   **Skipped**. Done and Skipped rows are dimmed and left out of the slips.
2. If it's a leg's sailing day, an hour has gone by since the time you set to
   leave, and the boat is still at the stop, the row says "Planned to leave
   today" and names the stop you're still at. Choose **Slip a day** to add a night at that stop and
   move every later date. If you'd rather stay as planned, ignore it. Nothing
   moves on its own.
3. When you open a plan, a leg whose verdict is different from last time reads
   "was Go, now Marginal" under its verdict. The marks stay until you leave the
   page, and then the next visit starts from what you saw this time.

## Change or delete a plan

1. Choose **Change plan** to edit it, or **Delete plan** to remove it.
2. A plan whose last day has passed moves under **Past**. It stays until you
   delete it.

Anyone using Helmcentral on the boat can see the plans. Only someone who's allowed
to make changes sees the buttons to add, change or delete one.

## Ask Mate

Mate can't make or change a plan yet. It can tell you the weather or the tides
"at the marina", using the stop your current plan has you at or heading for.
