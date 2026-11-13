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

## Change or delete a plan

1. Choose **Change plan** to edit it, or **Delete plan** to remove it.
2. A plan whose last day has passed moves under **Past**. It stays until you
   delete it.

Anyone using Helmcentral on the boat can see the plans. Only someone who's allowed
to make changes sees the buttons to add, change or delete one.

## Ask Mate

Mate can't make or change a plan yet. It can tell you the weather or the tides
"at the marina", using the stop your current plan has you at or heading for.
