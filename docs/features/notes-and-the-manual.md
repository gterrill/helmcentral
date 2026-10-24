# Notes and the manual

Boat knowledge arrives at the worst possible moment to write it down: wet, in
a locker, with a torch in one hand, or on a dock halfway through a phone
call. Capture puts it somewhere in one action, so the thing you worked out
about the shower drain pump is still there in two years when it matters, and
readable by whoever is aboard when you are not.

A note opens straight into the same editor used for headings, lists, tables
and links elsewhere in Helmcentral, not a plain box you tidy up later. There
is no title to fill in, no folder to choose and nothing to get right before
you can save. Type a plain sentence and it captures as is, or reach for the
toolbar when something needs real structure: a numbered start-up sequence, a
table of settings, a link to another note. Type it or dictate it, and it is
captured and searchable immediately, filed alongside everything else in
**Documents**.

## Capturing a note

Open **Documents** and choose **More actions → New note**. Pick a type from
the submenu if you already know it, or **Auto** if you don't. The submenu is
one extra hover.

Write the note and press Capture, or Ctrl+Enter (Cmd+Enter on a Mac). Plain
prose is enough for most notes. Whether or not you used the toolbar, you need
no title and no folder.

The microphone button dictates instead of typing, which is easier with wet
hands or while the boat is moving. It types wherever the cursor is, not just
at the end, so you can dictate a line, tap back into the middle to fix a
word, and carry on from there. Speech recognition is the browser's own, so it
needs no OpenRouter account. Most browsers send the audio to a speech service,
though, so it needs an internet connection. If the connection is down the
button says so. On a browser with no speech support the button is absent.

## Adding a photograph

A note can carry a photograph of the thing it describes: the valve, the
switch, the leak. Open the note editor and press **Take photo** to shoot it
there and have it drop into the note at the cursor, with no trip through the
document library first. **Add from library** does the same for a photo already
on the device. Pick several at once and they go into the note in the order you
picked them.

## What kind of note it is

Every note has a type, shown as an icon and a colour at the start of its row
in Documents, so you can pick one out of a long list without reading it:

| Type | What it is |
| --- | --- |
| Contact | A person and how to reach them |
| Procedure | Steps, or a checklist |
| Spec | A setting or a number worth remembering |
| Quirk | Something about this boat that will catch you out |
| Recipe | Galley |
| Note | Anything else |

The capture sheet's type field defaults to **Auto**, and that is usually the
right choice. Helmcentral works the type out from what you wrote, on the boat,
with no internet connection and no OpenRouter account. A phone number or an
email address makes it a contact. Numbered or ticked lines make it a
procedure. A figure with units in it, like a cruise RPM and a fuel burn, makes
it a spec.

It will sometimes be wrong. Pick a type yourself, either in the capture sheet
or afterwards by tapping the icon on its row. Once you've chosen, nothing
changes it back.

## Finding one again

The search box searches the whole document library, notes included, so a note
and the manufacturer's PDF it came from turn up together. Tag a note and it
filters by tag like anything else in the library. **Unfiled notes**, in the
folder pane, shows everything captured but not yet put anywhere.

With Mate switched on, search also matches on meaning, so a note about "the
head won't flush" is found by a search for the toilet. Without Mate, word
search works as it always has.

Mate can read your notes too, and answers questions from them in preference to
what it knows in general. A note you have pinned is put in front of Mate on
every question. That's worth doing for the handful of facts you keep asking
about: cruise RPM, the tender's engine oil, the combination on the aft locker.
**Pin for Mate**, in the viewer where you read the note, turns it on. The
button becomes **Unpin**, so taking a note back off is just as easy. Pin
sparingly. Only so many notes and so much text can ride along on every
question, and Helmcentral tells you when something doesn't fit instead of
quietly dropping it.

## Filing, and where notes go next

A captured note starts unfiled. **Unfiled notes** in the folder pane of
Documents shows how many, and switches the listing to just those. It's the
pile you are working through, not a place notes live permanently. Filing one
into a folder, with the same **Move…** you'd use on any document, takes it out
of that pile, so the count goes down as you file.

Folders are ordinary document folders, shared with the rest of the library. A
folder of contacts and a folder of recipes are worth having and need nothing
more. A folder can also be marked as a **manual**, which is how the boat's own
operations manual gets written.

## Manuals

A manual is a folder marked to be read front to back instead of browsed. The
mark gives it an order, section numbering and a reading view, in place of the
plain filed list an ordinary folder shows. Nothing about a note or file
underneath changes. The mark lives on the folder, not on what is inside it, so
nothing moves and nothing is lost if you take it back off.

A manual appears in Documents wherever its folder does, with a book icon in
place of the ordinary folder icon. Open it as you would any folder, by
choosing it in the folder pane. The listing becomes an ordered tree and a
reading pane instead of the table.

Creating one starts the same way as any other folder. **More actions → New
folder** offers a **Manual** checkbox when you are at the top level of the
library (a manual can't sit inside another folder). Check it, name it, and it
opens empty. You can turn an existing top-level folder into a manual, or back,
from its menu in the folder pane: **Treat as a manual** and **Stop treating as
manual**. Neither moves or deletes anything underneath.

A new manual is empty on purpose. It is meant to be built out of notes you've
already captured, not written from a blank page in one sitting.

Boats that keep more than one manual usually split them by reader. An
operations manual is for whoever is running the boat, including you at three
in the morning with something wrong. A crew training manual teaches someone
who doesn't yet know the boat the same systems, and explains what the
operations manual can safely assume. A folder of contacts or recipes is worth
having, but neither is a manual. A front-to-back reading order would mean
nothing for either, so they stay plain collections.

### Building a manual out of notes

A manual has no capture of its own. Every section started as an ordinary
captured note, filed into the manual the way any note is filed: **File…** on
its row (in the Unfiled notes view, or anywhere else it appears), choosing the
manual as the destination. The note leaves the unfiled pile and becomes a
section.

### Arranging sections

Open a manual and the left side is its whole table of contents: folders and
sections, in reading order. Turn on **Arrange** to move a section up or down
within its group. A manual can hold subfolders as well as sections, like any
folder, so a long manual can be split into chapters.

## Running a checklist

Any note written as a checklist, with ticked lines starting `- [ ]`, can be
run as well as read. **Start checklist** appears wherever that note is open,
in the Documents viewer or in a manual's reading pane. It swaps the page for
one large tick-off list: full-width rows with a big checkbox on each, sized
for a thumb rather than a cursor. Tap a row anywhere along its width to tick
it.

A run remembers where you are. Walk away from a genset shutdown at item four
and come back later, and **Start checklist** picks up there. It shows when you
started and how many are done, and lands you on the first thing still
unticked. Opening or closing the note doesn't start a run. Only tapping
**Start checklist** does.

A run is not a copy of the checklist. It is the current note plus which lines
you've ticked, so editing an unrelated step (fixing a typo, reordering the
list) never disturbs a tick already made. Only a step whose own wording
changes loses its tick. When that happens it is never silently dropped or
silently kept as done. It moves into its own **Re-check** group, showing the
wording it was ticked against, so you can see what changed and tick the new
version yourself. Bolding a word or fixing punctuation doesn't count as a
change. Only the words do.

**Complete checklist** closes out the run. **Abandon checklist** stops it
without finishing, for a run started by mistake or overtaken by events. Either
way, the record of what was ticked and when stays with the boat, and only
deleting the note clears it. Finishing or abandoning a run doesn't touch the
note. The next time anyone taps **Start checklist**, it opens fresh and
unticked.

## Catching up older notes

A note is typed the moment it is captured, on the boat. It needs no network
and no Mate, so there is never a backlog of untyped notes waiting on you.

Summarising a note and making it searchable by meaning is Mate's job, and it
happens on its own once Mate is switched on. Capture a note while Mate is
ready and it is queued at once. A note captured before you configured Mate
isn't left behind. When Mate becomes ready, at startup or the next time you
save a settings change that leaves it working, it sweeps the whole library for
anything still waiting. The same pass catches up the document library's
search-by-meaning index. There is nothing to click and no backlog count to
watch. Turning Mate on is the only consent it needs.

## What this is not

Notes and manuals are not the **Help** in the sidebar, which is Helmcentral's
own documentation and ships with the software.

They are also not the manuals already in your document library. The Cummins
manual describes every QSB 6.7 ever built. A note, and the manual you assemble
out of notes, describes this hull: which of the two fuel valves is the return,
what the previous owner did to the windlass wiring, and what to check first
when the genset won't start.
