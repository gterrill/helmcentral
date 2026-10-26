# Edit a passage

The passage finder gets most legs right, but not all. These steps correct the
ones it missed: a stop it split in two, a passage that ran on after you tied
up, or one you'd rather not keep. See [Passages](../features/passages.md) for
how a passage is found.

Open **Passages** and then the passage. Split and trim act on the point the
replay is showing, so drag the slider to the moment you mean first.

## Rename, change the ends, add notes

1. Change **Title**, **From**, **To** or **Notes**.
2. Press **Save**.

With no title, the list shows the two ends.

## Split a passage

1. Scrub the replay to where the passage should divide.
2. Under **Edit passage**, press **Split here**.

The passage becomes two. Each keeps its own figures.

If a named sign that starts a new leg is within a minute of the cut, the cut
moves onto that sign. Both halves take its name: the first ends there and the
second starts there. Helmcentral doesn't look the place up again. A cut further
away stays where you made it.

## Trim a passage

1. Scrub the replay to the new start, or the new end.
2. Press **Trim start to here** or **Trim end to here**.

Positions outside the new span stay in the log. They just stop being part of
this passage.

## Merge passages

Merging keeps each passage as its own leg, in its own colour, and writes a sign
where each pair met. The sign is named for the place the earlier leg arrived at,
or else the place the later leg left from. If neither leg has a name, the sign
waits for Helmcentral to look the place up from the track.

1. To merge one passage with its neighbour, open it and press **Merge with
   previous** or **Merge with next**.
2. To merge several at once, tick them in the list, or in the combined view
   from **View together**, and press **Merge**.

The passages must be neighbours with nothing in between, none of them hidden,
and all in the same cruise or in none. If they aren't, **Merge** is greyed out
and says why. The result is one passage with the earliest start and the latest
end, and a join at every place they met.

## Start a new leg anywhere

You can split the colours of a passage without merging anything.

1. Open the passage and press **Signs** on the replay bar.
2. Add a sign where the new leg starts, or pick one you already have.
3. Turn on **Starts a new leg**.

The track is drawn in a new colour from that moment on. Turn the switch off, or
delete the sign, to make it one colour again. If the sign has no name yet, it
shows "Awaiting a name" and turning the switch off asks you to type one first.

## Rate a passage

Rate a passage to keep a note of how it felt. See
[Passages](../features/passages.md) for where the rating shows up.

1. Open the passage and find **How it went** in the edit panel.
2. Press **Comfortable**, **Lively** or **Shouldn't have gone**.

It saves straight away, so you don't press **Save**. To clear it, press the
chosen one again. If the save fails, a message gives the reason and the rating
stays as it was.

## Correct what Mate recorded

When you sailed a passage from a Mate weather window, Helmcentral may have
guessed what you did with the plan. It shows "(inferred)" beside the guess.

1. Open the passage and find the **Planned with Mate** line under the route.
2. Press the choice next to it, which reads something like "Went · Fri 23 Oct
   (inferred)".
3. Pick **Went**, **Stayed**, **Just looking** or **Not set**.

Your choice replaces the guess and the "(inferred)" goes away. The same choice
is on the weather window card in the chat. You need write access to change it.

## Hide a passage

1. Press **Hide passage**.
2. Confirm.

The passage leaves the list and stays gone, even when Helmcentral finds the
same stretch again. The recorded positions stay in the
log.

## Group passages into a cruise

- **New cruise** in the left list makes an empty one. Select passages in the
  list and use **Move to cruise** to fill it.
- Select passages in the list and press **Save as cruise** to make the cruise
  and file them in one step. A passage already in another cruise moves out of
  it.
- From a single passage, **Move to cruise** moves it to a cruise or back to
  unfiled.

To rename a cruise, change its notes or delete it, open it from the list by
pressing **Cruise details**. Deleting a cruise keeps its passages and makes
them unfiled.

## Confirm where fresh water came from

On a cruise's page, each rise in the fresh water tanks is listed. Press
**Watermaker** or **Dock fill** to record which it was. Pressing the same button
again clears it. A suggested Dock fill is only a suggestion until you press it.

## View passages together

Select two or more passages in the list and press **View together**. They
replay one after another on a single map, each in its own colour, one for every leg. Nothing is
saved unless you press **Save as cruise**.
