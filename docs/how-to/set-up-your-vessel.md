# Set up your vessel

Helmcentral needs to know which engines, battery bank, generator, charger and
inverter are yours before it can show or watch them. The Alternators tile, the
Battery & Power tile (including its dawn projection and state-of-charge bands),
the Generator, Solar and Tanks tiles, anchor auto-raise, Mate's passage
estimates and anomaly detection (frozen and impossible sensor readings,
charging into a full house bank, twin engines pulling apart) all read what you
set up here. Nothing is pre-filled for your boat: until you set it up, those
features show no data or stay off, rather than guessing from whatever the boat
reports.

The idea is simple. Each engine, battery, alternator, solar controller, tank,
generator, charger and inverter is an item in your equipment registry, and each
such item owns the one thing the boat reports for it, called its SignalK source
(for example `propulsion.port` or `electrical.batteries.1`). The item's name is
the name you see on tiles and alarms. A few single-choice parts, such as which
battery is the house bank, are then picked from those items under **Roles**.

## Follow the Setup list

On a fresh install, **Settings → SignalK** opens with a **Setup** list, and the
dashboard shows one quiet line under the header: "Setup: 2 of 4 done · Next:
House bank". Choose that line, or the button under the next step in the list, and
Helmcentral takes you to the exact control and puts the cursor on it. The steps,
in order:

1. **Connect to SignalK.** Enter the address and port, choose **Test
   Connection**, then **Save**.
2. **Engines.** In **Discovery**, create an item for each engine the boat reports.
   An engine item counts only if its source has reported RPM; one set to a source
   that never has stays open, and the step names it.
3. **House bank.** Under **Roles**, choose the house bank.
4. **Charging and power sources.** Under **Roles**, answer the port and
   starboard alternators, the shore charger, the inverter, the generator and the
   generator AC input. Each takes an item, or **This boat has none**. The step
   names the ones still open.
   Setting the generator to **This boat has none** also clears the generator AC
   input, which the generator no longer needs.

A step counts when it is saved. Until SignalK is connected on a new install, the
other steps wait for it. Once you have set things up, a dropped connection does
not reopen the list; the tiles' stale indicators say so instead. The list and the dashboard line work out where you are every time you
look, so if an instrument is renumbered, an item is deleted or a role is
cleared, the step comes back by itself. Both go away when the last step is done.

## Assign each source to an item

1. Open **Settings → SignalK**. Once Helmcentral is connected, **Discovery**
   lists what the boat reports, grouped by kind: engines, batteries,
   alternators, solar controllers, tanks, generators, chargers and inverters.
   Each row shows its live readings so you can tell one from another, for
   example "RPM 1800 rpm · Coolant 77 °C" for an engine.
2. On a row with no item yet, choose **Assign to item** and pick one of your
   existing items that does not own a source yet, or choose **Create item**,
   type a name such as "Port engine" and choose **Create**. A new item starts
   with just that name and the right system; fill in the rest on its own page.
3. A row that names an item is done. Choose **Open** to go to the item.

You do not have to visit Discovery first for the Roles. Each dropdown under
**Roles** lists your items for that kind, then a **From the boat** group of
sources the boat reports that no item owns, with a live reading, for example
"electrical.batteries.512 · 13.24 V · 87%". Choosing one creates an item named
after the role, such as "House bank", and picks it. If the item cannot be made,
you are told why and the role stays as it was. When the boat reports nothing of
that kind, the dropdown says so ("The boat isn't reporting any batteries.") or
says SignalK isn't connected.

A source belongs to one item. If another item already has it, Helmcentral says
which one, and you free it there first. A source an item still owns that the
boat has stopped reporting stays in the list and says **Not reporting now**.

You can also set a source from the item itself: open the item in Inventory and
use **Source** under **SignalK source**. The list is the same one Discovery and
Roles use, so there is nothing to type.

## Only real sources are offered

The bus sometimes publishes a numbered source that carries one stray reading
beside the source that carries the rest. On a twin-engine boat, for example,
`propulsion.0` and `propulsion.1` may carry only exhaust temperature while
`propulsion.port` and `propulsion.starboard` carry RPM, fuel rate and engine
hours. Helmcentral offers a source only when it reports what that kind of
equipment needs:

| Kind | Must report |
| --- | --- |
| Engine | RPM |
| Battery | voltage |
| Alternator | output current |
| Solar controller | panel power |
| Tank | level |
| Generator | state or run time |
| Charger | output current |
| Inverter | AC output power |

Reporting now is not required. Helmcentral also looks at what the boat has
logged over the last 90 days, so an engine that is switched off is still
offered, shown as **Not reporting now · last logged 16 Oct**. If the history
cannot be read, Settings and the item's page say so and list only the sources
reporting now; an engine that is off will not appear until it is running.

If an item is already set to a source that is active in the logged history but
has never reported its reading, Helmcentral says so in red on that source (in Discovery, under
the role, and on the item's page), and the Setup step stays open with the item's
name and the source. Open the item and choose the source that does report it.
When the history cannot be read, or the source has logged nothing at all (an
engine laid up, or a short log), Helmcentral does not guess: the step stays
done and notes that it could not check.

Names come only from items. There are no separate battery names, solar names,
engine names or tank labels any more. A tank shows on the Tanks tile once an
item owns it. A battery or solar controller no item owns shows under its
number. If you upgraded from the old Vessel settings, an item was made for each
battery, solar controller, tank and engine you had named, so you can delete the
ones you do not want.

## Engines

An engine is an equipment item mapped to a `propulsion.` source. Map one for
each engine the boat has.

- Give the item an engine profile and the engine's tiles take that profile's
  ranges and alarm limits. Add an Engine Cluster tile and choose the engine
  from the list; see [Show equipment on a tile](show-equipment-on-a-tile.md).
  There is nothing to copy across, and no per-tile limits to keep in step.
- The Alternators tile appears when a mapped engine is turning, and anchor
  auto-raise only acts while one is. With no engine mapped, or none reporting,
  the tile stays hidden and auto-raise does nothing.
- Mate's passage estimates use these engines for fuel history, and say engines
  are not set up if you have mapped none.
- The frozen-sensor check is ready once one engine is mapped. The
  engine-differential check (comparing engines against each other) needs two.

## House bank

1. Map the bank's battery source to an item as above, for example "House bank".
2. Open the item. Under **SignalK source**, enter its **Cells** and **Capacity**.
   Leave **Warn override** and **High override** blank to use the profile's own
   thresholds, multiplied out by your cell count. Fill either in only if your
   bank needs a different number than the profile gives.
3. Under **Profile**, pick the battery profile: the chemistry and charge
   thresholds. Save the item.
4. Open **Settings → SignalK**, find **Roles**, and set **House bank** to that
   item. Choose **Save** in the bar across the top of the screen.

This is the bank the Battery & Power tile reads for state of charge, voltage,
current and power. If the bank reports its own nominal capacity, that figure is
used; otherwise the capacity on the item is. The overnight dawn projection and
the state-of-charge bands follow the same bank. With no house bank chosen, the
tile says "No house bank chosen" and links to **Settings, SignalK, Roles**, and
the bands and dawn figure are absent. Solar, Loads, Shore and the voltage of the
readout battery still show. A bank that is chosen but not reporting shows a dash
(—). See [Set battery state-of-charge
bands](set-battery-state-of-charge-bands.md).

If you later move the house bank, either by choosing another item or by giving
the item another source, the state-of-charge bands move with it.

## Alternators

The Alternators tile shows what each engine's alternator is putting out. The
boat's instruments number the alternators in their own order, and that order can
change when equipment is reinstalled, so Helmcentral does not assume which
number is port and which is starboard.

1. Map each alternator to an item ("Port alternator", "Starboard alternator").
   With the engines running, the one that is charging shows a current in
   Discovery.
2. Under **Roles**, set **Port alternator** and **Starboard alternator**. An
   item chosen for one side cannot be chosen for the other.
3. Choose **This boat has none** if the boat has no alternator on that side. With
   only one side set, the tile shows a single alternator.
4. Choose **Save** in the bar across the top of the screen.

A side that is not set shows no reading on the Alternators tile. Until you set
at least one side, the tile is empty.

## Electrical

The boat's instruments number the generator, the AC meters, the chargers and
the inverters in their own order, and that order can change when equipment is
reinstalled, so Helmcentral does not assume which number is which.

1. Map the generator, the shore charger and the inverter to items, and any
   battery you want as the voltage readout.
2. Under **Roles**, set **Generator**, **Shore charger**, **Inverter** and
   **Voltage readout battery** to those items.
3. Set **Generator AC input** to the AC circuit the generator feeds. This one
   is not an item: AC meters are not equipment, so the list shows each circuit
   the boat reports with its live power. Start the generator and pick the one
   that reads load.
4. Choose **This boat has none** for any the boat lacks. That is an answer, and
   it finishes the setup step; **Not set** means you have not answered yet. Both
   show no figures. Choose **Save** in the bar across the top of the screen.

What each one does:

- **Generator.** The Generator tile reads its state and runtime from it, and its
  Start and Stop buttons control it. With none chosen the tile says the generator
  is not set up, links to **Settings, SignalK, Roles**, and offers no Start or
  Stop.
- **Generator AC input** is the Generator tile's Power card. Not set shows `--`.
- **Shore charger** is the Shore line on the Battery & Power tile.
- **Inverter** is the AC part of Loads on the Battery & Power tile. Not set
  leaves it blank, and Loads reads `--` because the total would otherwise leave
  out the AC draw.
- **Voltage readout battery** is the voltage in the tile's footer, labelled with
  the item's name. Not set, or a battery that is not reporting, leaves the
  voltage out.

The shore charger and the generator also decide which nights the dawn projection
trusts: a night when shore power was connected or the generator ran is skipped
because the bank did not discharge naturally. A night is only screened for the
ones you chose.

## Hour meters

An engine or a generator has an hour meter, read from the source you mapped it
to. You do not enter a path. Open the item and **Maintenance** shows the hour
meter, read only. If it says **Not reporting**, the item has no engine or
generator source mapped, or the source does not publish a runtime counter, and
service rules counted in hours cannot count hours for it.

## Saving, and what each check needs

Assigning a source or creating an item takes effect at once, like any change on
an item's own page. The roles save together with the rest of Settings. The bar
appears as soon as you change one, **Discard** puts it back, and if you try to
leave Settings with changes unsaved you are asked first.

The house bank and the voltage readout battery do not offer **This boat has
none**: the bank is needed for most of the electrical tiles.

Under **Roles**, each check shows whether it is ready, or what is still
missing, so you always know what saving will turn on.

If an item loses its source while a role still points at it, the role's figure
shows no data and the log says why; the role list shows **Item no longer
assigned** until you choose again. If the item is deleted, the role is cleared
and shows **Not set**. Other settings still save.

## Before you clear a source or delete an item

Clearing an item's source, moving it to another, or deleting it affects
everything that uses it. When something does, Helmcentral lists what first and
asks you to confirm, for example:

> Used by 2 tiles (Underway: Port, Wall: Engines: Port), anchor auto-raise, the
> engine checks, the House bank role, 3 state-of-charge alarm rules.

Tiles are named by their page and title. The list also covers the features that
read an engine, battery or tank, the roles the item plays, and the alarm rules
that watch its readings, including limits set on a gauge.

Nothing is removed behind your back:

- **Clearing the source** leaves every tile in place, showing **Not mapped**
  with a link to the item. The roles stay on the item, and the figures and alarm
  rules that depend on its source show no data until you map it again.
- **Moving the source** takes everything that uses the item to the new source.
- **Deleting the item** leaves its tiles showing **Equipment removed**, clears
  the roles it played, and leaves the state-of-charge rules in place watching
  nothing until you choose a new house bank.

If nothing uses the item, you are asked nothing more than before. If Helmcentral
cannot work out what uses it, the confirmation stays switched off and says why.

## Excluding a sensor you know is dead

A sender that has failed outright (a stuck or open-circuit reading) can
trip the frozen or impossible-reading check forever. Rather than a
settings list, this is handled from the alarm itself: open the alarm card
for that check and choose **Ignore** followed by the sensor's name (for example
**Ignore Port engine exhaust temperature**) for the reading you want
excluded. To see everything currently ignored, or to bring one back,
open **Settings → Alarms** and look under **Ignored sensors**.

## Owner and insurance

Mate fills in insurance declarations and marina applications from these, so
they are worth entering once. None of them is needed for anomaly detection.

1. Open **Settings → Vessel** and find **Owner & insurance**.
2. Enter the owner's name, phone and email, the insurer and policy number, the
   home marina and berth, and the storm delegate: whoever prepares the boat
   when you are away, with how to reach them.
3. Under **Particulars**, enter the length overall and beam in metres. The
   boat's live instruments supply a length too when they publish one, shown
   at the top of the section; this figure is the one kept for forms.
4. Choose **Save** in the bar across the top of the screen.

Leave a field blank if you do not want Mate to use it. It asks instead. See
[Fill in a form with Mate](fill-in-a-form-with-mate.md).
