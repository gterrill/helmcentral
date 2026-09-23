# Tag bins and equipment

See [Inventory](../features/inventory-tracking.md#tags-and-photos) for what
tapping a tag does and why you'd bother. These are the steps for actually
writing one.

## What you need

NFC stickers (NTAG215 or similar) cost a few cents each from any NFC
supplier and need no battery. Buy anti-metal ones for anything going on or
near metal - a tool, an engine part, a steel locker door - and expect a
standard sticker to read poorly or not at all stuck straight onto one.

## Write a tag from an Android phone

1. Open the bin's page (tap its code in Locations, or create the bin first
   if it doesn't exist yet) or the item's own editor.
2. Find the tag row near the top and press **Write tag**.
3. Hold the tag against the back of the phone when prompted, and hold it
   still until the button changes to **Written**.
4. Stick the tag on the bin or the item. Tap it with any phone to confirm
   it opens the right page.

This only works in Chrome on an Android phone - there is no equivalent on
an iPhone or a computer.

## Write a tag from an iPhone or a computer

1. Open the same bin or item page and press **Copy** next to the address
   shown in the tag row.
2. Paste it into a separate NFC-writing app (several free ones exist on the
   App Store) and write the tag from there.
3. Stick the tag down and test it the same way.

## Tap a tag

Tap it with any NFC-capable phone, no app required. On Android it opens
straight into the app if it's already open, or into the browser if not. On
an iPhone it always opens in Safari, never inside the installed app icon -
that's a limit of how iPhones handle tags, not a bug. The first tap this
way on an iPhone asks you to sign in through Safari, separately from the
installed app; after that it stays signed in like any other website you
visit.

## When to lock a tag

Most NFC-writing apps offer a **lock** option that stops the tag ever being
rewritten again, by anyone. Lock one once you're confident the bin's code
or the item it's stuck to won't change - a locked tag can be tapped and
read by anyone aboard with no risk of it later being overwritten to point
somewhere else by mistake. Leave tags unlocked while you're still settling
a storage system, since a locked tag can't be corrected if you get the
bin's code wrong.

## If a code changes

Renaming a bin's code in Locations doesn't touch tags already stuck down -
they still work as physical stickers, they just stop opening anything
useful. The rename screen tells you this the moment you make the change.
Write a fresh tag for the new code and peel the old sticker off, or mark
it so nobody taps it expecting the old result.
