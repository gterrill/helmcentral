# Tiles

Field-level detail for what lands on a dashboard page: the built-in catalog,
the exact fields on the tiles dense enough to need them, and the rules
every tile follows for staleness and alert colour. See [The
dashboard](../features/dashboard.md) for what each of these is for and how
you put one on a page.

## Built-in tiles

Twenty-one: Vessel, Apparent Wind, Depth & Tide, Position, Today & Now,
Anchor Watch, Tanks, Route, Nearby Vessels, Radar Targets, Battery & Power,
Solar, Alternator, Generator, Switches, Hot Water, Autopilot, Clock, Current
Conditions, Forecast and Sea State. Each is one per page.

**Add Tile** groups all of these, plus the configurable types below, by what
they are for: Navigation, Weather, Situational, Power, Engine, Systems, At a
glance, and Custom.

Clock, Current Conditions, Forecast and Sea State were sized for a wall
display's fold first, the narrowest being a flybridge instrument strip.
Nothing stops placing them on an ordinary page too.

## Tiles you configure yourself

Six tile types take settings of your own and can appear on a page more than
once, unlike the built-ins above:

| Tile | What it's for |
| --- | --- |
| Gauge | Any single value the boat's instrument network publishes, as a number, radial dial, bar, lamp or trend. Coloured bands double as alarm rules. |
| Gauge group | Several gauges clustered under one title, for duplicating onto a second engine or generator. See [Duplicate a gauge group](../how-to/duplicate-a-gauge-group.md). |
| Engine cluster | A fixed layout of engine readings for one engine. |
| Indicators | A row of status lamps, either pinned as the vessel-wide ribbon or placed on one page. See [Pin an indicator ribbon](../how-to/pin-an-indicator-ribbon.md). |
| Embed | Any URL in the grid: a Grafana panel, a camera feed, a windrose. **Frameless** drops the title bar and padding so the embedded page fills the tile; layout mode still draws the frame so an embed is never impossible to reconfigure. |
| Nearby map | Points of interest around the vessel. See [Add a Nearby map](../how-to/add-a-nearby-map.md) and [POI categories](poi-categories.md). |

## Battery & Power fields

Four cards, plus a Shore line that appears once the shore charger reports
anything:

- **State**: the big number, state of charge, with a bar underneath and a
  line reading "To full", "To 20%" or "To empty" depending on whether the
  bank is charging, discharging above a threshold, or discharging with none
  set.
- **Net**, **Solar**, and the rate shown beside State: energy moving into or
  out of the bank.
- **Loads**: what the bank is feeding, shown as plain text since it is not a
  state of the bank itself.
- **Shore**: the charger's mode and AC input current. Absent, not blank,
  while nothing is plugged in. A charger error always shows, in red, even
  before anything else about the charger has reported.

The state-of-charge number and bar only turn amber or red once you have set
a matching alarm rule; see [Set battery state-of-charge
bands](../how-to/set-battery-state-of-charge-bands.md).

### The Dawn figure

The footer's right side projects state of charge at sunrise: `Dawn 6:08 ·
51% · 4 nights`. `6:08` is tomorrow's sunrise, `51%` the projected state of
charge then, and `4 nights` means enough history exists to base the figure
on the boat's own recent overnight draw rather than tonight's rate alone.

Helmcentral takes the median discharge rate from the most recent *clean*
nights it can find, up to seven, searching back as far as thirty, and
applies it to tonight after first running the current rate forward to
sunset. A clean night runs sunset to sunrise with no shore power and no
generator run, has enough samples to trust the slope, and did not somehow
see the bank rise; a night that fails any of those is skipped rather than
averaged in. Hovering the figure shows how many nights were skipped and why.

Short of two clean nights, the same spot reads `Dawn 6:08 · 61% · at current
rate` instead, holding whatever rate the bank is charging or discharging
right now all the way to sunrise. Read literally, this can project as high
as 100% in the early afternoon while the panels are charging; once the sun
sets, the current rate *is* the night's rate and the figure is accurate. The
switch from "at current rate" to a night count happens on its own once two
clean nights exist.

While on shore power tonight, the line reads `Dawn 6:08 · on shore power`
instead of a percentage, since the bank is being held rather than run down.
Without a position there is no sunrise to project and the segment drops
out; with a stale feed, or the live-rate fallback switched off and too
little history, it reads a dash. The projected percentage takes the alarm
band colours the same as the live figure does.

## Tanks footer fields

Once at least one fuel tank is configured, a footer under the tank bars adds
three figures:

- **Fuel aboard**: the summed level and capacity of every fuel tank
  reporting both, in litres. Keeps reporting with the engines off, since a
  tank's level does not depend on one running.
- **Range at current burn** and **Time to empty**: both computed from the
  boat's speed and its engines' burn rate as they stand this second, with no
  averaging, so slowing down moves the figures immediately. Both need the
  boat to be burning fuel, so both sit blank while stopped.

Any of the three blanks out with a small stale marker if the reading it
depends on stops updating, rather than showing a number built on a burn
rate or tank level that stopped being true.

## Wall display fields

| Field | What it is for |
| --- | --- |
| Name | What you call the screen. "Flybridge", "Saloon TV". |
| Address | The last part of the screen's web address, so `flybridge` gives `/display/flybridge`. |
| Screen size | The size the screen's own browser reports, which is not always the panel's actual size. |
| Magnification | How much larger to draw everything: more for a television read across a room, less for a strip read at arm's length. |
| Upside down | For a panel mounted inverted. |
| OLED panel | Shifts the image a few pixels on a slow cycle, so a board left up all season does not burn in. |
| Keep awake | Asks the screen not to sleep. Cannot override the set's own power-saving menu. |

Each page assigned to a display carries its own dwell, 5 to 3600 seconds,
and a condition: **Always**, or a vessel state such as **While anchored**. A
page whose condition stops being true finishes the slot it is already
showing before dropping out, rather than cutting off mid-dwell. A rotation
with nothing currently eligible checks again every 15 seconds rather than
sitting blank.

See [Set up a wall display](../how-to/set-up-a-wall-display.md) for the
device check and setup steps.

## Staleness

A source's last reading stays on screen after the source itself stops
sending. Past two minutes of silence, a tile dims, its header gains a
`STALE` marker with the age, and its readings go to `—`. Blanking the
number is deliberate: a stale reading shown as current is worse than none.

- Solar tracks this per controller as well as for the array as a whole; one
  MPPT dropping off marks only that row.
- A gauge group or engine cluster badges the one reading that stopped and
  keeps reporting the rest; the tile as a whole only goes stale once every
  reading on it has.
- A lamp goes dark with the same age marker rather than staying lit on an
  old value.
- A number worked out from more than one reading, fuel economy for example,
  goes stale with whichever input stopped first.

Ages are measured against the vessel clock at the moment the reading was
taken, so a tablet with a wrong clock, or one waking from sleep, will not
invent staleness. A tile reading `—` with no `STALE` marker means
something else: that reading is not being published at all, which is worth
checking on the source before assuming a dead link.

## Tile state colours

A tile's edge, and a dot beside its title, take the colour of the worst
alert level any of its readings currently sits in: sky for alert, amber for
warn, red for alarm, a deeper red wash for emergency. A tile with every
reading in its normal band, or a reading with no warn or alarm threshold set
for it, draws no attention at all. A stale tile shows its amber staleness
treatment instead, whatever band its last reading was in, since a value the
tile can no longer vouch for should not also claim a state.
