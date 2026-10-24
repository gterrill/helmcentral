# Inventory

Inventory records every system aboard, where it is stowed and what has been
done to it. The equipment registry holds one record per piece of gear, from a
bilge pump to a main engine, so "what generator is this and how do I start it"
has one answer instead of five places to look.

The **Inventory** panel in the sidebar has five sections in two groups.
Under **Inventory** are Equipment, Locations and Stocktake: what is aboard
and where it is stowed. Under **Servicing** are Maintenance and Profiles:
what keeps it running. Maintenance has its own page: see
[Maintenance](maintenance.md).

## Equipment

Every system aboard gets its own record. A record holds what you need when
you are standing in front of the thing, or ordering a part for it without
crawling into the compartment to read the data plate:

- **What it is.** Name, manufacturer, model, serial number, and how many of
  it there are.
- **System.** Propulsion, electrical, water, fuel, bilge, anchoring, safety,
  air conditioning, navigation, appliances, structure. This is how the list
  groups itself, so the electrical gear reads as one block.
- **Where it is.** A zone and, if it lives in one, a bin, plus a line of
  detail for the part a bin code cannot carry: "outboard side, behind the
  raw water strainer".
- **Status.** *Deployed* for gear in use, *stored* for a spare sitting in
  the lazarette. A spare impeller is a real record. Marking it stored keeps it
  out of maintenance reminders.
- **Spares stock.** A part number, how many are on board and how many you
  mean to carry. On board can be zero. A Racor seal kit you've used and not
  replaced is still a record, and it shows as below what you carry until
  you restock it.
- **SignalK source.** Optional. An engine, battery, alternator, solar
  controller, tank, generator, charger or inverter owns the one thing the boat
  reports for it, picked from the live instrument data your boat actually
  publishes. The item's name is then the name that tiles, alarms and Mate use.
  A battery item also holds its bank's cell count and capacity. Leave the
  field empty for gear the boat does not report on. See
  [Set up your vessel](../how-to/set-up-your-vessel.md).
- **Hour meter.** Not entered. An engine or a generator reads its runtime
  counter from its SignalK source, and the Maintenance section shows it read
  only, or **Not reporting**. Service rules counted in hours need that counter.
  Without it they can't count hours, and the Maintenance section says so.
- **Aliases.** What the crew actually calls it. "Genset" finds the
  generator.
- **Photos.** Any number, taken with the camera or picked from the library,
  one marked as the cover. See Tags and photos below.
- **Install date, notes, and whether you have verified it aboard.** The
  verified switch is there because a builder's handbook and the boat do not
  always agree, and an unverified figure should look unverified.

Records are created by hand, from the New item button, or from a bin's own
page: see Tags and photos.

Tap a column heading (Name, Status and so on) to sort the list by it, and
tap again to reverse the order. Each row's own menu opens the record or
removes it, the same choices as opening the record and using Delete there.
On a phone, the list shows as a stack of cards instead of a table.

### Documents

An equipment record links to anything already in the document library: the
operator manual, a parts catalogue, a schematic, the warranty certificate,
the invoice from the last service, a photo of the data plate. Add them from
the record's Documents list, which searches the whole library.

Each linked document shows as a line with a thumbnail (photos) or a file icon,
its title, the date it was added to the library and a type label such as PDF,
JPG or NOTE. Click a line, or choose **Open** from its menu, to read it in a
panel that slides in from the right without leaving the item. **Download**
saves the file to the device you are on. **Remove from item** takes the link
off this item only. The document stays in the library, and the change waits
for the save bar like any other edit.

Links point at documents, they don't copy them. What a document *is*
comes from its tags, so a manual tagged "manual" reads as one wherever you
meet it. Delete a document from the library and the link goes with it. A
record's own photos (above) appear here too. The Photos row is the
picture-sized view of the same list, so removing a photo here takes it off the
Photos row as well.

## Profiles

Equipment profiles are reference data about a make and model: its gauges,
its alarm bands and its manufacturer service intervals. They sit here, beside
the gear that uses them.

Pick a profile on an equipment record and the blank manufacturer and model
fields fill themselves in. The profile stays shared and unchanged, and the
record just points at it. Gear with no profile is normal, and most of a boat
will never have one.

For the profile format itself, see
[Equipment profiles](../reference/equipment-profiles.md).

## Locations

A location is an area of the boat: salon, flybridge, lazarette, engine room,
forepeak. A bin is a numbered container in one. Bin codes are short, meant to
be printed on a label and stuck to the bin: `LAZ-02` rather than something
generated.

Locations opens on a list of your locations with how many bins each holds.
Search narrows it by name, and the column headings sort it. Press **New
location**, give it a name, and its own page opens ready for bins. Tap any
location in the list to open that page. There you can:

- **Rename it.** Edit the name and press **Save** in the bar that appears at
  the top.
- **Add bins.** Type a code and, if you like, a name, then press **Add bin**.
- **Change or remove a bin.** Edit a bin's code or name in place, or press
  the trash button beside it to remove it.
- **Delete the location.** Use the menu at the top right, then confirm.

A bin belongs to one location, so naming the bin names the location. A
location or bin with things filed in it can't be deleted until you've moved
them, and the refusal tells you how many there are.

Tap a bin's code to open its own page: everything filed in it, with a photo of
each thing underneath. See Tags and photos.

### Deck plans

A list says the spare impeller is in bin `ENG-03` in the engine room. That is
enough for whoever set the bins up. A crew member, a delivery skipper or you,
six months on, still has to find the engine room hatch and then the third bin.
A deck plan shows where.

A deck plan is a picture of one deck, usually the builder's general arrangement
drawing, with your locations outlined on it and your bins pinned. You add the
picture, trace each location, and drop a pin where each bin sits. None of it is
required. A location that is not on a plan works as before, and you can plan
one deck and leave the rest.

The plan picture is a JPEG or PNG. A PDF drawing has to be exported to an image
first. Large pictures are shrunk to 3000 pixels on the long side, which
keeps small print on a drawing readable. The picture is kept in Documents like
any other file.

Where plans appear:

- **Locations, Plan view.** The switch at the top of Locations flips between
  the list (**Table**) and the plans (**Plan**). Plan shows one tab per deck.
  Tap a location's outline to open it, or a pin to open that bin.
- **A location's page.** An **On the plan** section shows a small plan with the
  location highlighted, and **Edit on plan** takes you to the deck to adjust
  it. A location that is not on a plan says so.
- **A bin's page.** If the bin has a pin, its page shows the plan with the
  location highlighted and the bin marked.
- **Decks.** Open it from the menu at the top right of Locations. It lists your
  decks, whether each has a plan picture, and how many locations are on it.
  This is where decks are added, renamed and deleted, and where plans are drawn.

A location is on one deck. If a space runs across two decks, make it two
locations. A bin can have a pin only when its location is on a plan, and
taking a location off a plan clears its bins' pins. A pin doesn't have to
fall inside its location's outline, since outlines are traced loosely and a
locker can sit on an edge.

Deleting a deck does not delete the locations or bins. Its locations lose their
outlines, their bins lose their pins, and the confirmation says how many
locations that is. The plan picture stays in Documents.

Replacing a deck's picture keeps every outline and pin where it was on the
image. If the new picture's shape differs from the old one by more than a
couple of percent, you get a warning: the outlines will no longer line up with
the drawing and need checking.

You draw by touch or mouse. There is no zoom yet, so the plan draws at the
width of the page. If bins sit too close together to place by finger on a large
boat, use a bigger screen for the drawing. The steps are in
[Draw your deck plans](../how-to/draw-deck-plans.md).

## Tags and photos

Stick an NFC tag on a bin, tap it with a phone, and the phone opens straight
to that bin's contents: a list of what's inside, with a photo of each item
big enough to read the packaging off. It answers "what's in this locker" while
you're standing at it, and lets you record what's in a bin the moment you're
looking into it instead of typing it up later at a desk.

- **Tap a bin to see what's in it.** Every bin has its own page, reachable
  by tapping its code in Locations or by tapping a tag stuck to the bin
  itself. The page lists the contents with each item's photos underneath,
  and lets you add a new item on the spot. A photo and a name is enough.
- **Writing a tag needs an Android phone running Chrome.** Nothing else can
  write a physical tag today. On any other phone or computer, the bin's page
  offers the address to copy, ready to paste into a separate NFC-writing app.
- **A printed QR label works on any phone.** Print label on a bin's page or
  an item's editor gives a label with a QR code and the bin code or item
  name in large type. Print bin labels in the Locations menu does every bin
  on one sheet. An iPhone camera reads it, with no tag writing needed. Like a
  tag, it opens the page only for a phone that can reach the boat.
- **On metal, use anti-metal tags, and seal tags in the engine room.** A
  standard sticker won't read reliably stuck straight to a metal surface.
  Anti-metal tags cost a little more and solve it. Heat, damp and diesel
  fumes are hard on an unsealed tag, so seal or laminate one before it goes
  near an engine.
- **Renaming a bin's code breaks tags already stuck to it.** The tag still
  works as a sticker but stops opening anything useful, because the tag's
  address is built from the code. The app warns you the moment you rename
  one, and you can write a fresh tag afterwards.

An item's record works the same way. Open it from its editor and write a tag
for it directly, and a tap anywhere opens straight to that item.

See [Tag bins and equipment](../how-to/tag-bins-and-equipment.md) for the
steps, and [Photograph your gear](../how-to/photograph-your-gear.md) for
adding photos.

## Still to come

Locations, deck plans, photos, tags, a scan-based stocktake and maintenance
schedules are built, and Mate can already look up an item, its hour meter and
its jobs (see [What Mate can look up](../reference/mate-tools.md)). Not built
yet:

- **Cost across a system's whole life.** Maintenance records what each job
  cost, but nothing adds those up per system.
- **Minimum stock levels.** No warning yet when a consumable runs low.
- **Expiry dates.** Flares, EPIRB battery, liferaft service, extinguishers and
  medications have no due date to track, and no alarm when one lapses.
- **Inventory tiles.** Nothing from the inventory appears on a dashboard page.
- **Reading labels from a photo.** Type the manufacturer, model, serial and
  part number yourself, or read them off the photo.

## Scanning, or not

You can find items by NFC tap, barcode scan or manual entry, and mix them.
You don't need scanning hardware beyond a phone.

### NFC tags

See Tags and photos above for writing and using them. A tap opens a bin or
an item's own page directly, which is the fastest way from standing at a
locker to seeing what's supposed to be in it.

### Barcodes

A USB barcode scanner acts as a keyboard: it types what it reads and
presses Enter, with no setup beyond plugging one in. Print a bin's code as a
barcode and scanning it sets that bin as the one you're working in during a
stocktake, the same as typing the code in by hand.

### RFID tags

RFID identifies a specific item. Sixteen identical tins of tomatoes share a
barcode, but no two RFID tags match. RFID support (a reader, and tags matched
back to your records) is designed and not yet built, so don't buy tags or a
reader yet. NFC tags and barcodes both work today.

Passive UHF RFID will have real limits when it does arrive. Range depends
heavily on what is in the way, and a closed aluminium locker of tinned food
may read nothing at all. On-metal tags cost more than standard ones. Readers
are region-specific (Australia and New Zealand use 920 to 926 MHz, the EU 865
to 868, North America 902 to 928), though tags are mostly universal.

### Typing it in

You can record items without tags. Every field a scanner fills in can be typed
or corrected by hand, and an item doesn't need a scannable identifier.

## Browsing and searching

The default view shows locations, then the bins in a location, then each bin's
contents. A location shows how many bins it holds, and a bin shows its contents and
their photos.

Search is fuzzy and covers names, part numbers, manufacturers and serials. It
lists matching items and highlights the bins that hold them.

## Doing a stocktake

Open Stocktake, then scan. Tap a bin's NFC tag with your phone, scan a
barcode of a bin's code, or type a bin code into the scan field and press
Enter. Once a bin is set, tap an item's own tag (or type its address) to
work through its contents.

- Scanning a bin sets it as the one you're working in, and shows its photo
  grid underneath so you can check things off by eye as well as by scanning.
- Scanning an item already filed in that bin confirms it, and marks it
  verified aboard if it wasn't already.
- Scanning an item filed somewhere else, or nowhere, shows where it's
  recorded and offers a **Move** button. Nothing moves until you press it.
- Anything scanned that isn't a recognised bin or item is reported as
  unrecognised, never guessed at.

The bin's items not yet scanned this pass stay listed while you work in it, so
you can chase them down or account for them by eye. Nothing changes for them
on its own.

**Nothing you scan can mark an item lost, move it, or delete it.** A stocktake
confirms what it finds, and every move is a press you make yourself.

See [Run a stocktake](../how-to/run-a-stocktake.md) for the steps.

## Photos

Items and bins both carry photos, taken from the device camera or picked
from the library. They are downscaled on the way in so a bin of twenty items
loads quickly over a Tailscale connection. The first photo is the cover, and any
photo can be made the cover, and any photo can be removed.

A picture identical to a file already in Documents is linked to the item
rather than stored a second time. Removing a photo from an item only takes it
off that item. The picture stays in Documents unless you choose to delete it
too, and that choice is offered only for a picture no other item uses and that
isn't filed in Documents. See
[Photograph your gear](../how-to/photograph-your-gear.md).

## What this is not

- Not a shopping tool. No supplier catalogues, no price lookups, no ordering.
- Not provisioning or meal planning.
- Not fleet inventory. One boat.
- Not a warehouse system. No pick lists, purchase orders or receiving.
- No automatic deletion of records after a failed scan.

## Your data

Inventory lives in `data/helmcentral.sqlite`, the same file as the document
library it links to. Photos are ordinary image files next to it. Export to
JSON or CSV is part of the design and not yet built. Include the state
directory in your backups. Once you have a few hundred photos, they will be
the biggest part of it.

Copy `helmcentral.sqlite` with Helmcentral stopped, or take a live backup
with `sqlite3 data/helmcentral.sqlite ".backup /path/to/backup/helmcentral.sqlite"`
rather than copying the file while it's running. The full backup guidance is
in [Configuration](../reference/configuration.md).
