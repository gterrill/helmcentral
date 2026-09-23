# Inventory

Inventory records every system aboard and, in time, what it is made of, what
has been done to it and what that cost. It starts with the equipment
registry: one record per piece of gear, from a bilge pump to a main engine,
so that "what generator is this and how do I start it" has one answer
instead of five places to look.

The **Inventory** panel in the sidebar has three sections: Equipment,
Profiles and Locations.

## Equipment

Every system aboard gets its own record. A record holds what you need when
you are standing in front of the thing, or ordering a part for it without
crawling into the compartment to read the data plate:

- **What it is.** Name, manufacturer, model, serial number, and how many of
  it there are.
- **Category.** *Mechanical* for anything driven by running hours - engines,
  generators, transmissions, thrusters, stabilisers, watermakers. *General*
  for everything else: navigation electronics, safety gear, plumbing, galley
  appliances, deck hardware. The difference matters because mechanical gear
  is serviced on hours and everything else on the calendar.
- **System.** Propulsion, electrical, water, fuel, bilge, anchoring, safety,
  air conditioning, navigation, appliances, structure. This is how the list
  groups itself, so the electrical gear reads as one block.
- **Where it is.** A zone and, if it lives in one, a bin, plus a line of
  detail for the part a bin code cannot carry: "outboard side, behind the
  raw water strainer".
- **Status.** *Deployed* for gear in use, *stored* for a spare sitting in
  the lazarette. A spare impeller is a real record; marking it stored is
  what will keep it out of maintenance reminders when those arrive.
- **Hour meter.** For mechanical gear, the SignalK path its runtime is
  published on, picked from the paths your boat actually publishes. This is
  what future service schedules will count against.
- **Aliases.** What the crew actually calls it. "Genset" finds the
  generator.
- **Install date, notes, and whether you have verified it aboard.** The
  verified switch is there because a builder's handbook and the boat do not
  always agree, and an unverified figure should look unverified.

Records are created by hand, from the New item button. Nothing creates them
for you.

### Documents

An equipment record links to anything already in the document library: the
operator manual, a parts catalogue, a schematic, the warranty certificate,
the invoice from the last service, a photo of the data plate. Add them from
the record's Documents list; search runs over the whole library.

Links point at documents, they do not copy them. What a document *is*
already comes from its tags, so a manual tagged "manual" reads as one
wherever you meet it. Delete a document from the library and the link goes
with it.

## Profiles

Equipment profiles are reference data about a make and model: its gauges,
its alarm bands and its manufacturer service intervals. They used to live
in Settings and now sit here, beside the gear that uses them.

Pick a profile on an equipment record and the blank fields fill themselves
in - manufacturer, model, and whether it is mechanical. The profile stays
shared and unchanged; the record just points at it. Gear with no profile is
perfectly normal, and most of a boat will never have one.

For the profile format itself, see
[Equipment profiles](../reference/equipment-profiles.md).

## Locations

A zone is an area of the boat: salon, flybridge, lazarette, engine room,
forepeak. A bin is a numbered container in one. Bin codes are short and
meant to be printed on a label and stuck to the bin, so `LAZ-02` rather than
something generated.

A bin belongs to one zone, so naming the bin names the zone. A zone or bin
that something is filed in cannot be deleted until you have moved what is in
it, and the refusal tells you how many things that is.

## Still to come

The registry is the foundation for the rest, which is designed and not yet
built:

- **Service schedules and maintenance history.** Intervals by running hours,
  by calendar, or whichever comes first; a log per item of what was done and
  when; tasks that appear when an interval comes due.
- **Cost tracking.** What each system has cost over its life, so an
  expensive one cannot hide inside the boat's total.
- **Spares and stock.** Quantity items, minimum levels, and the stocktake
  described below.
- **Asking Mate.** Mate will read an equipment record when your question is
  about that gear, so it can answer from the record, its documents and its
  live readings rather than searching for mentions.

The rest of this page describes the stock and stocktake half of inventory as
designed. None of it is built.

## Scanning, or not

Items can be found by RFID, barcode or manual entry. These methods can be used
together, and scanning hardware is optional.

### RFID tags

EPC Gen 2 passive tags cost a few cents each and need no battery. They attach
to items and bins and are read by a USB reader in keyboard mode during a
stocktake.

RFID has several limitations aboard:

- **Range depends on what is in the way.** Passive UHF is absorbed by liquid and
  reflected by metal. A boat locker is mostly liquid and metal. A closed
  aluminium locker of tinned food may read nothing at all, and no setting fixes
  that.
- **On-metal tags exist and cost more.** Standard adhesive labels do not work
  stuck to a tool or an engine part. Budget for the metal-rated ones where you
  need them.
- **Buy the reader for your region.** Australia and New Zealand use 920 to
  926 MHz, the EU 865 to 868, North America 902 to 928. Tags are mostly
  universal, readers are not.
- **A scan tells you a tag is nearby, not how near.** So the reader alone cannot
  work out which locker something is in. Tagging the bins solves it: scan bin
  `SAL-04`, and everything you scan next is recorded as being in it.

**An unread tag does not establish that an item is missing.** A stocktake
confirms what it finds and makes no changes based on unread tags. Nothing you
scan can mark an item lost, move it
somewhere, or delete it. Items a pass did not see are listed as "not seen
since", with the date they were last confirmed, and you decide what that means.
A reader may receive no tags from either an empty locker or one lined with foil.

### Barcodes and QR codes

The same readers and the same workflow. A barcode scanner in keyboard mode needs
no separate support, so a shop-bought USB scanner works out of the box.

Barcodes and RFID tags identify different things. A barcode on a tin of
tomatoes identifies the *product*,
and sixteen identical tins share it. An RFID tag identifies *that one item*, and
no two tags match. So an item is either:

- **A single thing**: a spare raw water pump, an EPIRB, a laptop. It can hold a
  serial number and warranty date, and it is what RFID tags suit.
- **A quantity in a bin**: fuel filters, tinned food, impellers, fuses. Counted
  rather than tagged, and identified by its product code.

### Typing it in

Manual entry is a standard way to record items without tags. Every field a
scanner fills in can be typed or corrected by hand, and an item does not need
a scannable identifier.

## Browsing and searching

The default view shows zones, then the bins in a zone, then each bin's contents.
A zone shows how many bins and items it holds; a
bin shows its contents and when they were last confirmed.

Search is fuzzy and runs across names, part numbers, manufacturers and serials.
It lists matching items and highlights the bins that hold them.

## Doing a stocktake

Pick a bin, or scan the bin's own tag, then scan its contents. Each scan
confirms the item, and if it was recorded somewhere else, moves it and notes
when.

Scanning a tag the system does not know offers to add it there and then, against
a new item or an existing one. This lets you tag spares during a stocktake
without a separate setup session.

At the end you get a summary: confirmed, moved, newly tagged, and the items
expected in those bins that were not scanned. As described above, that last
list does not change the items' records.

## Photos and reading labels

Items can carry photos, taken from the device camera when you first record them
or uploaded later. They are downscaled on the way in and capped per item, so the
library's disk usage is limited.

Optionally, a photo of a label can be read by a vision model to pull out the
manufacturer, model, serial number and part number, so you do not have to type a
serial off a sticker behind an engine. This needs an OpenRouter account and is
configured in settings.

Label reading has four constraints:

1. **It is off until you turn it on, and it runs when you ask.** No photo leaves
   the boat unless you press the button on that photo. Nothing scans your
   library in the background.
2. **It suggests, you confirm.** Extracted values appear in the form for you to
  accept or correct before saving, to avoid recording incorrect details that
  could lead to ordering the wrong part.
3. **It needs internet, and says so when there is none.** A failed read tells
   you it failed and the item saves without it. It never blocks recording an
   item.
4. **The API key is stored encrypted** alongside the other credentials, not in a
   config file.

Everything else in inventory tracking works with no internet and no account.

## More settings

Beyond the zones and bins above, the design adds:

- **Floor plans.** A zone can carry a plan image with its bins pinned on it, so
  "where is `LAZ-02`" has a visual answer for anyone aboard who did not stow it.
  A rough sketch is sufficient; deck drawing tools are not included.
- **Master data.** Manufacturers, suppliers and categories, kept as lists so
  they stay consistent and can be used to filter.
- **Label reading.** The OpenRouter key, the model, and the switch.

## On the dashboard

Three tiles track expiry dates, stock levels and stocktakes:

- **Expiring soon.** Flares, EPIRB battery, liferaft service, fire
  extinguishers, medications, first aid. These dates help identify items that
  need attention before an inspection.
- **Low stock.** Quantity items that have dropped below the minimum you set.
- **Last stocktake.** When you last did one, and what it did not find.

Expiry is intended to raise an ordinary Helmcentral alarm, so an out-of-date
flare reaches your phone the same way a low battery does. That connection is
designed but not yet built.

## What this is not

- Not a shopping tool. No supplier catalogues, no price lookups, no ordering.
- Not provisioning or meal planning.
- Not fleet inventory. One boat.
- Not a warehouse system. No pick lists, purchase orders or receiving.
- No automatic deletion of records after a failed scan.

## Your data

Inventory lives in Helmcentral's own database in its state directory, beside
the document library it links to. Photos will be ordinary image files next to
it, and export to JSON or CSV is part of the design. Include the state
directory in your backups; once you have a few hundred photos, it will be the
biggest part of it.
