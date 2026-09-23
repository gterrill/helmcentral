# Inventory

Inventory records every system aboard and, in time, what it is made of, what
has been done to it and what that cost. It starts with the equipment
registry: one record per piece of gear, from a bilge pump to a main engine,
so that "what generator is this and how do I start it" has one answer
instead of five places to look.

The **Inventory** panel in the sidebar has four sections: Equipment,
Profiles, Locations and Stocktake.

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
- **Hour meter.** For mechanical gear, the engine-hours reading from the
  vessel telemetry that this item's runtime comes from, picked from the live
  instrument data your boat actually publishes. This is what future service
  schedules will count against.
- **Aliases.** What the crew actually calls it. "Genset" finds the
  generator.
- **Photos.** Any number, taken with the camera or picked from the library,
  one marked as the cover. See Tags and photos below.
- **Install date, notes, and whether you have verified it aboard.** The
  verified switch is there because a builder's handbook and the boat do not
  always agree, and an unverified figure should look unverified.

Records are created by hand, from the New item button, or from a bin's own
page - see Tags and photos.

### Documents

An equipment record links to anything already in the document library: the
operator manual, a parts catalogue, a schematic, the warranty certificate,
the invoice from the last service, a photo of the data plate. Add them from
the record's Documents list; search runs over the whole library.

Links point at documents, they do not copy them. What a document *is*
already comes from its tags, so a manual tagged "manual" reads as one
wherever you meet it. Delete a document from the library and the link goes
with it. A record's own photos (above) live in their own Photos row and
never show up in this list, so the two stay easy to tell apart.

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

Tap a bin's code to open its own page: everything filed in it, at a glance,
with a photo of each thing underneath. See Tags and photos.

## Tags and photos

Stick an NFC tag on a bin, tap it with a phone, and the phone opens straight
to that bin's contents - a list of what's inside, with a photo of each item
big enough to read the packaging off. This is the fast way to answer "what's
in this locker" standing at the helm, or to record what's in a bin the
moment you're looking into it rather than typing it up later at a desk.

- **Tap a bin to see what's in it.** Every bin has its own page, reachable
  by tapping its code in Locations or by tapping a tag stuck to the bin
  itself. The page lists the contents with each item's photos underneath,
  and lets you add a new item on the spot - a photo and a name is enough.
- **Writing a tag needs an Android phone running Chrome.** That is the only
  combination that can write a physical tag today. On any other phone or
  computer, the bin's page offers the address to copy instead, ready to
  paste into a separate NFC-writing app.
- **On metal, use anti-metal tags, and seal tags in the engine room.** A
  standard sticker will not read reliably stuck straight to a metal
  surface; anti-metal tags cost a little more and solve it. Heat, damp and
  diesel fumes are hard on an unsealed tag, so seal or laminate one before
  it goes anywhere near an engine.
- **Renaming a bin's code breaks tags already stuck to it.** The tag still
  works as a sticker, it just stops opening anything useful - the code is
  what the tag's own address is built from, so the app warns you the
  moment you rename one and lets you write a fresh tag afterwards.

An item's own record works the same way: open it from its editor and a tag
can be written for it directly, opening straight to that item wherever it's
tapped.

See [Tag bins and equipment](../how-to/tag-bins-and-equipment.md) for the
steps, and [Photograph your gear](../how-to/photograph-your-gear.md) for
adding photos.

## Still to come

The registry is the foundation for the rest. Locations, photos, tags and a
scan-based stocktake are built; what's still designed and not yet built:

- **Service schedules and maintenance history.** Intervals by running hours,
  by calendar, or whichever comes first; a log per item of what was done and
  when; tasks that appear when an interval comes due.
- **Cost tracking.** What each system has cost over its life, so an
  expensive one cannot hide inside the boat's total.
- **Minimum stock levels.** A warning once a consumable's quantity drops
  below a level you set.
- **Asking Mate.** Mate will read an equipment record when your question is
  about that gear, so it can answer from the record, its documents and its
  live readings rather than searching for mentions.

## Scanning, or not

Items can be found by an NFC tap, a barcode scan, or manual entry. These
methods can be used together, and scanning hardware beyond a phone is
optional.

### NFC tags

See Tags and photos above for writing and using them. A tap opens a bin or
an item's own page directly - the fastest route from standing at a locker
to seeing what's supposed to be in it.

### Barcodes

A USB barcode scanner acts as a keyboard: it types what it reads and
presses Enter, with no setup beyond plugging one in. Print a bin's own code
as a barcode and scanning it sets that bin as the one you're working in
during a stocktake, exactly the same as typing the code in by hand.

### RFID tags

RFID identifies a specific item the way a barcode identifies a product -
sixteen identical tins of tomatoes share a barcode, but no two RFID tags
match. RFID hardware support - a reader, and tags matched back to your
records - is designed and not yet built. Buying tags or a reader ahead of
that is premature; NFC tags and barcodes both work today.

Passive UHF RFID, where it does arrive, will have real limits worth knowing
ahead of time: range depends heavily on what is in the way (a closed
aluminium locker of tinned food may read nothing at all), on-metal tags
cost more than standard ones, and readers are region-specific (Australia
and New Zealand use 920 to 926 MHz, the EU 865 to 868, North America 902 to
928) even though tags themselves are mostly universal.

### Typing it in

Manual entry is a standard way to record items without tags. Every field a
scanner fills in can be typed or corrected by hand, and an item does not need
a scannable identifier.

## Browsing and searching

The default view shows zones, then the bins in a zone, then each bin's contents.
A zone shows how many bins and items it holds; a
bin shows its contents and their photos.

Search is fuzzy and runs across names, part numbers, manufacturers and serials.
It lists matching items and highlights the bins that hold them.

## Doing a stocktake

Open Stocktake, then scan: tap a bin's NFC tag with your phone, scan a
barcode of a bin's code, or type a bin code into the scan field and press
Enter. Once a bin is set, tap an item's own tag (or type its address) to
work through its contents.

- Scanning a bin sets it as the one you're working in, and shows its photo
  grid underneath so you can check things off by eye as much as by
  scanning.
- Scanning an item already filed in that bin confirms it, and marks it
  verified aboard if it wasn't already.
- Scanning an item filed somewhere else, or nowhere, shows where it's
  recorded and offers a **Move** button - nothing moves until you press it.
- Anything scanned that isn't a recognised bin or item is reported as
  unrecognised, never guessed at.

The bin's own items not yet scanned this pass stay listed for as long as
you're working in it, so you can chase them down or account for them by
eye - nothing changes for them on its own.

**Nothing you scan can mark an item lost, move it, or delete it by
itself.** A stocktake confirms what it finds; every move is a press you
make yourself.

See [Run a stocktake](../how-to/run-a-stocktake.md) for the steps.

## Photos and reading labels

Items and bins both carry photos, taken from the device camera or picked
from the library, downscaled on the way in so a bin of twenty items loads
quickly over a tailscale connection. The first photo is the cover; any
photo can be made the cover, and any photo can be removed.

Reading a label with a vision model to pull the manufacturer, model, serial
and part number off it automatically is designed and not yet built. Typing
those fields by hand, or reading them off a photo yourself, works today.

## More settings

Beyond the zones and bins above, the design adds:

- **Floor plans.** A zone can carry a plan image with its bins pinned on it, so
  "where is `LAZ-02`" has a visual answer for anyone aboard who did not stow it.
  A rough sketch is sufficient; deck drawing tools are not included.
- **Master data.** Manufacturers, suppliers and categories, kept as lists so
  they stay consistent and can be used to filter.
- **Label reading.** The OpenRouter key, the model, and the switch.

## On the dashboard

Three tiles are designed, not yet built, to track expiry dates, stock
levels and stocktakes at a glance:

- **Expiring soon.** Flares, EPIRB battery, liferaft service, fire
  extinguishers, medications, first aid. These dates help identify items that
  need attention before an inspection.
- **Low stock.** Quantity items that have dropped below the minimum you set.
- **Last stocktake.** When you last did one, and what it did not find.

Expiry is intended to raise an ordinary Helmcentral alarm, so an out-of-date
flare reaches your phone the same way a low battery does. That connection,
like the tiles themselves, is designed but not yet built.

## What this is not

- Not a shopping tool. No supplier catalogues, no price lookups, no ordering.
- Not provisioning or meal planning.
- Not fleet inventory. One boat.
- Not a warehouse system. No pick lists, purchase orders or receiving.
- No automatic deletion of records after a failed scan.

## Your data

Inventory lives in Helmcentral's own database in its state directory, beside
the document library it links to. Photos are ordinary image files next to
it; export to JSON or CSV is part of the design and not yet built. Include
the state directory in your backups; once you have a few hundred photos, it
will be the biggest part of it.
