# Set passage limits

When you're waiting on a weather window, Mate can check each day's forecast
against the conditions you're willing to go out in. Then it says which days
are a go. These are your limits. Helmcentral ships with none, because what's
comfortable depends on the boat, the crew and the passage. Until you set
them, Mate won't grade a passage.

The limits depend on where the wind is coming from. Twenty-five knots from
astern can be a fast, easy run. Fifteen knots on the nose is a wet, slow
beat. So you set them separately for three sectors of true wind angle off
your course:

| Sector | True wind angle off the bow |
| --- | --- |
| Ahead | 0 to 60° |
| Abeam | 60 to 120° |
| Astern | 120 to 180° |

You enter only the **No-go** levels. Those are the wind speed and, if you want
it checked, the wave height at which you would wait. Mate works out the rest.

- **Marginal** is 70% of no-go. With a no-go wind of 20 knots, 14 knots and
  above is marginal: you would go, but with a watchful eye.
- **Gusts** are judged against the sector's no-go wind plus 10 knots. With a
  no-go wind of 20 knots, gusts of 30 knots are no-go and gusts from 21 knots
  are marginal.

1. Open **Settings → Mate** and scroll to **Passage limits**.
2. For each sector, enter the **No-go wind** in knots, above 0. Mate won't
   grade a leg that falls in a sector without one.
3. Enter a **No-go wave height** in metres where it matters to you. A sector
   left blank is shown as not checked for waves, never as passed.
4. Under **Wind against tide**, enter how many knots to **Reduce wind limits
   by** while a stream runs against the wind, through the strongest part of
   the flood or ebb. It lowers the no-go wind only. Marginal follows at 70% of
   the lowered figure, and gust limits aren't changed. With a no-go wind of
   20 knots and a reduction of 5, wind against the stream is no-go from 15
   knots and marginal from 10.5. It must be more than 0 and less than every
   no-go wind. Leave it blank to skip the check. It's applied only for legs
   where Mate knows which way the flood runs.
5. Turn on **Daylight arrival** if a leg that would arrive in the dark, before
   sunrise or after sunset, should count as no-go.
6. Choose **Save** in the bar at the bottom of the page.

On the weather window card, a marginal cell says which figure made it so, for
example "Wind SE 12 kn ahead (marginal from 10.5)".

You can type decimals with a point or a comma. A value Helmcentral can't read
is marked on the field, and Save stays unavailable until you correct it.

While Mate is on, **Passage limits** appears in the Setup list under
**Settings → SignalK** as an optional step. It counts as done once every
sector has a no-go wind limit, and it never holds up the rest of setup.
