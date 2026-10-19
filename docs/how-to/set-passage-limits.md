# Set passage limits

When you are waiting on a weather window, Mate can check each day's forecast
against the conditions you are willing to go out in, and say which days are a
go. These are your limits. Helmcentral ships with none, because what is
comfortable depends on the boat, the crew and the passage. Until you set them,
Mate will not grade a passage.

The limits depend on where the wind is coming from. Twenty-five knots from
astern can be a fast, easy run. Fifteen knots on the nose is a wet, slow
beat. So you set them separately for three sectors of true wind angle off
your course:

| Sector | True wind angle off the bow |
| --- | --- |
| Ahead | 0–60° |
| Abeam | 60–120° |
| Astern | 120–180° |

Each sector has a **Marginal** and a **No-go** level for sustained wind,
gusts and significant wave height. Marginal means you would go, but with a
watchful eye. No-go means you would wait.

1. Open **Settings → Mate** and scroll to **Passage limits**.
2. For each sector, enter at least a **No-go** wind speed in knots. Mate
   will not grade a leg that falls in a sector without one.
3. Add **Marginal** levels, gust limits and wave limits where they matter to
   you. Any you leave blank are shown as not checked, never as passed.
   Marginal must be below no-go.
4. Under **Wind against tide**, set the wind speeds at which a stream running
   against the wind becomes marginal or no-go for you. This is checked only
   for legs where Mate knows which way the flood runs.
5. Turn on **Daylight arrival** if a leg that would arrive after sunset should
   count as no-go.
6. Choose **Save** in the bar at the bottom of the page.

Decimals can be typed with a point or a comma. A value Helmcentral cannot read
is marked on the field, and Save stays unavailable until you correct it.

While Mate is on, **Passage limits** appears in the Setup list under
**Settings → SignalK** as an optional step. It counts as done once every
sector has a no-go wind limit, and it never holds up the rest of setup.
