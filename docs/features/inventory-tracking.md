# Inventory tracking

Knowing what is aboard, and where it is, is a problem every boat has and almost
no boat solves. The parts inventory lives in someone's head, the spares live in
whichever locker had room at the time, and the answer to "do we have a spare
impeller for the genset" is found by opening lockers until one turns up or the
trip is over.

Inventory tracking gives that a home: storage zones and numbered bins that match
the boat, items you can search by name or part number, and a stocktake you do by
walking around with a scanner instead of emptying cupboards.

> **Status: not built yet.** This page describes the feature as designed. It is
> here so the shape can be argued with before it is code.

## Scanning, or not

Items are found three ways, and all three coexist. Nothing here requires
hardware you do not want to buy.

### RFID tags

The interesting one. Cheap EPC Gen 2 passive tags, a few cents each, no battery,
stuck on items and on the bins themselves. A USB reader in keyboard mode reads
them, so a stocktake becomes a walk through the boat rather than a morning
spent opening every locker.

What that actually gets you, stated honestly, because RFID is sold with numbers
it does not hit on a boat:

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

The consequence that shapes everything else: **not seeing a tag does not mean
the item is gone.** A stocktake confirms what it finds and never concludes
anything from silence. Nothing you scan can mark an item lost, move it
somewhere, or delete it. Items a pass did not see are listed as "not seen
since", with the date they were last confirmed, and you decide what that means.
An empty locker and a locker lined with foil look identical to a reader, and
only one of them is a problem.

### Barcodes and QR codes

The same readers and the same workflow. A barcode scanner in keyboard mode needs
no separate support, so a shop-bought USB scanner works out of the box.

Barcodes and RFID tags identify different things, and the app treats them
differently on purpose. A barcode on a tin of tomatoes identifies the *product*,
and sixteen identical tins share it. An RFID tag identifies *that one item*, and
no two tags match. So an item is either:

- **A single thing**: a spare raw water pump, an EPIRB, a laptop. It can hold a
  serial number and warranty date, and it is what RFID tags suit.
- **A quantity in a bin**: fuel filters, tinned food, impellers, fuses. Counted
  rather than tagged, and identified by its product code.

### Typing it in

Not a fallback. It is the normal way to record anything not worth a tag, and
every field a scanner fills in can be typed or corrected by hand. An item with
no scannable identifier at all is perfectly valid.

## Browsing and searching

The default view walks the boat the way you would: zones, then the bins in a
zone, then what is in a bin. A zone shows how many bins and items it holds; a
bin shows its contents and when they were last confirmed.

Search is fuzzy and runs across names, part numbers, manufacturers and serials.
It highlights the bins holding matches as well as listing the items, because on
a boat "which locker" is usually the question, not "do we own one".

## Doing a stocktake

Pick a bin, or scan the bin's own tag, then scan its contents. Each scan
confirms the item, and if it was recorded somewhere else, moves it and notes
when.

Scanning a tag the system does not know offers to add it there and then, against
a new item or an existing one. That is how a locker full of untagged spares
actually gets tagged: during a stocktake, not in a separate setup session.

At the end you get a summary: confirmed, moved, newly tagged, and the items
expected in those bins that did not turn up. That last list is information, not
an instruction, per the rule above.

## Photos and reading labels

Items can carry photos, taken from the device camera when you first record them
or uploaded later. They are downscaled on the way in and capped per item, so the
library does not quietly become the largest thing on the boat's disk.

Optionally, a photo of a label can be read by a vision model to pull out the
manufacturer, model, serial number and part number, so you do not have to type a
serial off a sticker behind an engine. This needs an OpenRouter account and is
configured in settings.

Four things about it are deliberate:

1. **It is off until you turn it on, and it runs when you ask.** No photo leaves
   the boat unless you press the button on that photo. Nothing scans your
   library in the background.
2. **It suggests, you confirm.** Extracted values appear in the form for you to
   accept or correct. Nothing is saved unchecked, because a wrong serial number
   that nobody looked at is worse than a blank field. You find out when the part
   arrives and does not fit.
3. **It needs internet, and says so when there is none.** A failed read tells
   you it failed and the item saves without it. It never blocks recording an
   item.
4. **The API key is stored encrypted** alongside the other credentials, not in a
   config file.

Everything else in inventory tracking works with no internet and no account.

## Settings

An **Inventory** section in settings holds:

- **Zones and bins.** A zone is an area of the boat: salon, flybridge,
  lazarette, engine room, forepeak. A bin is a numbered container in one. Bin
  codes are short and meant to be printed on a label and stuck to the bin, so
  `SAL-04` rather than something generated.
- **Floor plans.** A zone can carry a plan image with its bins pinned on it, so
  "where is `LAZ-02`" has a visual answer for anyone aboard who did not stow it.
  A rough sketch is the point; this is not a deck drawing tool.
- **Master data.** Manufacturers, suppliers and categories, kept as lists so
  they stay consistent and can be used to filter.
- **Label reading.** The OpenRouter key, the model, and the switch.

## On the dashboard

Three widgets, aimed at the things that have a deadline rather than at counts:

- **Expiring soon.** Flares, EPIRB battery, liferaft service, fire
  extinguishers, medications, first aid. Expiry dates are the part of a boat
  inventory that actually bites, usually during an inspection.
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
- Not something that deletes your records because a scanner had a bad day.

## Your data

Inventory lives in its own database file in Helmcentral's state directory,
alongside everything else it stores, and exports whole to JSON or CSV whenever
you want it. Photos are ordinary image files next to it. Include the state
directory in your backups; once you have a few hundred photos, it will be the
biggest part of it.
