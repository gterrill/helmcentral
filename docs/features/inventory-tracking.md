# Inventory tracking

Inventory tracking records what is aboard and where it is stored, so you can
check for a spare impeller for the genset without searching every locker. The
design uses storage zones and numbered bins that match the boat, items
searchable by name or part number, and scanner-assisted stocktakes.

> **Status: not built yet.** This page describes the feature as designed. It is
> open for review before implementation.

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

## Settings

An **Inventory** section in settings holds:

- **Zones and bins.** A zone is an area of the boat: salon, flybridge,
  lazarette, engine room, forepeak. A bin is a numbered container in one. Bin
  codes are short and meant to be printed on a label and stuck to the bin, so
  `SAL-04` rather than something generated.
- **Floor plans.** A zone can carry a plan image with its bins pinned on it, so
  "where is `LAZ-02`" has a visual answer for anyone aboard who did not stow it.
  A rough sketch is sufficient; deck drawing tools are not included.
- **Master data.** Manufacturers, suppliers and categories, kept as lists so
  they stay consistent and can be used to filter.
- **Label reading.** The OpenRouter key, the model, and the switch.

## On the dashboard

Three widgets track expiry dates, stock levels and stocktakes:

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

Inventory lives in its own database file in Helmcentral's state directory,
alongside everything else it stores, and exports whole to JSON or CSV whenever
you want it. Photos are ordinary image files next to it. Include the state
directory in your backups; once you have a few hundred photos, it will be the
biggest part of it.
