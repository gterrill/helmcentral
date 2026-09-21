# Notes and the manual

Boat knowledge arrives at the worst possible moment to write it down: wet, in
a locker, with a torch in one hand, or on a dock halfway through a phone
call. Capture puts it somewhere in one action, so that the thing you worked
out about the shower drain pump is still there in two years when it matters,
and is readable by whoever is aboard when you are not.

A note is just text. There is no title to fill in, no folder to choose and no
format to get right before you can save it. Type it, or dictate it, and it is
captured and searchable immediately, filed alongside everything else in
**Documents**.

## Capturing a note

**Capture a note** sits in the header, reachable from any screen, so writing
something down never means navigating away from whatever you were doing
first. Alt+N opens the same sheet from the keyboard. Inside Documents, **New
→ Note** opens the identical sheet, for anyone who goes looking for it there
instead.

Write the note and press Capture, or Ctrl+Enter (Cmd+Enter on a Mac). That is
the whole interaction: no title, no folder, nothing else required.

The microphone button dictates instead of typing, which is the easier option
with wet hands or while the boat is moving. Speech recognition is the
browser's own, so it needs no OpenRouter account, but most browsers send the
audio to a speech service to do the work, which means it needs an internet
connection. If the connection is down the button says so rather than
appearing to do nothing. On a browser with no speech support at all the
button is simply absent.

## What kind of note it is

Every note is given a type, shown as an icon and a colour at the start of its
row in Documents so you can pick the one you want out of a long list without
reading it:

| Type | What it is |
| --- | --- |
| Contact | A person and how to reach them |
| Procedure | Steps, or a checklist |
| Spec | A setting or a number worth remembering |
| Quirk | Something about this boat that will catch you out |
| Recipe | Galley |
| Note | Anything else |

The capture sheet's type field defaults to **Auto**, and that is usually the
right choice: Helmcentral works the type out from what you wrote, on the
boat, with no internet connection and no OpenRouter account. A phone number
or an email address makes it a contact. Numbered or ticked lines make it a
procedure. A figure with units in it, like a cruise RPM and a fuel burn,
makes it a spec.

It will sometimes be wrong. Pick a type yourself instead, either in the
capture sheet or afterwards by tapping the icon on its row, and once you have
chosen, nothing changes it back.

## Finding one again

The search box searches the whole document library, notes included, so a
note and the manufacturer's PDF it came from turn up together. Tag a note and
it filters by tag like anything else in the library, and Unfiled notes shows
everything captured but not yet put anywhere. With Mate switched on, search
also matches on meaning rather than
only on words, so a note about "the head won't flush" is found by a search
for the toilet. Without Mate, word search still works exactly as it always
has.

Mate can read your notes too, and answers questions from them in preference
to what it knows in general. A note you have pinned is put in front of Mate
on every question, which is worth doing for the handful of facts you find
yourself asking about constantly: cruise RPM, the tender's engine oil, the
combination on the aft locker. **Pin for Mate**, in the same viewer you read
the note in, turns this on; the button becomes **Unpin** once it is, so
taking a note back off is exactly as easy. Pin sparingly: there is a limit to
how many notes and how much text actually rides along on every question, and
Helmcentral says so outright rather than quietly dropping the ones that don't
fit.

## Filing, and where notes go next

A captured note starts unfiled. **Unfiled notes** in Documents' toolbar
shows how many, and switches the listing to just those - the pile you are
working through, not a place notes live permanently. Filing one into a
folder, the same **Move…** you would use on any document, takes it out of
that pile, so the count is a drain, not an archive that only grows.

Folders are ordinary document folders, shared with the rest of the library.
A folder of contacts and a folder of recipes are worth having and need
nothing further. A folder can also be marked as a **manual**, which is how
the boat's own operations manual gets written.

## Manuals

A manual is a folder marked to be read front to back instead of browsed.
Marking it this way gives it an order, section numbering and a reading view,
in place of the plain filed list an ordinary folder shows. Nothing about a
note or file underneath changes: the flag lives on the folder, not on what
is inside it, so nothing moves and nothing is lost if it is ever taken back
off.

A manual is a folder, so it appears in Documents wherever that folder does,
marked with a book icon in place of the ordinary folder icon. Open it the
same way you would open any folder, and the listing becomes the ordered
tree and reading pane instead of the table.

Creating one starts the same way as any other folder: **New → Folder**
offers a **Manual** checkbox when you are at the top level of the library
(a manual can't sit inside another folder). Check it, name it, and it opens
empty. An existing top-level folder can be turned into a manual, or back,
from its row menu: **Treat as a manual** and **Stop treating as manual**.
Neither moves or deletes anything underneath.

A brand new manual is empty on purpose: it is meant to be built out of notes
you have already captured, not written from a blank page in one sitting.

Boats that keep more than one manual usually split by who reads it. An
operations manual is for whoever is running the boat, including you at three
in the morning with something wrong. A crew training manual teaches someone
who does not yet know the boat the same systems, explaining what the
operations manual can safely assume. A folder of contacts or recipes is
worth having too, but neither is a manual: a front-to-back reading order
would mean nothing for either, so they stay plain collections.

### Building a manual out of notes

A manual has no capture of its own. Every section in it started as an
ordinary captured note, filed into the manual the same way any note is
filed: **File…** on its row (in the Unfiled notes view, or anywhere else it
appears), choosing the manual as the destination. The note leaves the
unfiled pile and becomes a section.

### Arranging sections

Open a manual and the left side is its whole table of contents: folders and
sections, in reading order. Turn on **Arrange** to move a section up or down
within the group it sits in. A manual can hold subfolders as well as
sections, the same way any folder can, so a long manual can be split into
chapters rather than left as one long list.

## Running a checklist

Any note written as a checklist, ticked lines starting `- [ ]`, can be run
rather than just read. **Start checklist** appears wherever that note is
open, in Documents' own viewer or inside a manual's reading pane, and swaps
the page for one large tick-off list: full-width rows, a big checkbox on
each one, sized for a thumb rather than a cursor. Tap a row anywhere along
its width to tick it, not just the box itself.

A run remembers where you are. Walk away from a genset shutdown at item
four and come back to it later, and **Start checklist** picks up exactly
there: it shows when you started and how many are done, and lands you back
on the first thing still unticked rather than making you scroll to find
your place. Nothing about opening or closing the note itself starts a run;
only tapping **Start checklist** does.

A run is not a copy of the checklist. It is the current note, plus which
lines you have ticked, so editing an unrelated step (fixing a typo,
reordering the list) never disturbs a tick already made. Only a step
whose own wording actually changes loses its tick, and when that happens
it is never silently dropped or silently carried over as still done: it
moves into its own **Re-check** group, showing the wording it was ticked
against, so you can see what changed and tick the new version yourself.
Bolding a word or fixing punctuation in a step does not count as a change
here; only the words themselves do.

**Complete checklist** closes out the run. **Abandon checklist** stops it
without finishing, for a run started by mistake or overtaken by events;
either way, the record of what was ticked and when stays with the boat, it
is only ever cleared by deleting the note itself. Finishing or abandoning a
run does not touch the note. The next time anyone taps **Start checklist**,
it opens fresh and unticked, ready to be run again.

## Catching up older notes

A note is typed the moment it is captured, by the same offline classifier
that decides Contact from Procedure from Spec - no network, no Mate, no
separate step, so there is never a backlog of untyped notes waiting on you.

Summarising a note and making it searchable by meaning is Mate's job, and
it happens on its own once Mate is switched on: capture a note while Mate
is ready and it is queued for that the same instant. A note captured before
you had Mate configured isn't left behind either. The moment Mate becomes
ready - at startup, or the next time you save a settings change that
leaves it working - it sweeps the whole library for anything still
waiting, the same pass that catches up the rest of the document library's
own search-by-meaning index. There is nothing to click and no backlog
count to watch; turning Mate on is the only consent this asks for.

## What this is not

Notes and manuals are not the **Help** in the sidebar, which is
Helmcentral's own documentation and ships with the software.

They are also not the same as the manuals already in your document library.
The Cummins manual describes every QSB 6.7 ever built. A note, and the
manual you assemble out of notes, describes this hull: which of the two fuel
valves is actually the return, what the previous owner did to the windlass
wiring, and what to check first when the genset will not start.
