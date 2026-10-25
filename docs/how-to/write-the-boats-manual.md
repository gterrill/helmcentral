# Write the boat's manual

This covers the mechanics: capturing a note, filing it into a manual, putting
the sections in order and adding a photograph. For what should go in a manual
in the first place, see [Start a ship's
manual](start-a-ships-manual.md).

You don't write a manual. You capture notes as things happen, and file them
into sections once there are enough to see the shape.

## 1. Capture as you go

Open **Documents** and choose **More actions → New note**. The note opens straight into
the full editor. Headings, lists, tables and links are all there in the
toolbar from the first word. Write the
note and press Ctrl+Enter, or Cmd+Enter on a Mac, or use the Capture button.

Nothing else is required. No title and no folder are needed. The type field
defaults to **Auto** and Helmcentral works it out from what you wrote. If you
are standing in the engine room with one hand free, use the microphone button
and dictate it. It types wherever the cursor is sitting, so you can dictate
a line, tap back in to fix a word, and carry on.

Write it as you would say it to someone. "Fuel return is the inboard valve,
the one with the scratched handle" is a finished note. Most of them are
plain sentences like this one, and the toolbar can sit there unused.

## 2. Start a manual

Open **Documents** and choose **More actions → New folder**. At the top level of the
library it offers a **Manual** checkbox. Check it, give the manual a name,
and it opens empty. That is deliberate: a manual is assembled from notes you
have already captured, not typed into a blank outline.

An existing top-level folder can become a manual the same way, from its menu
in the folder pane: **Treat as a manual**. Nothing inside it moves.

Most boats end up with more than one manual. An operations manual for
running the boat and a crew training manual for teaching someone are
different documents with different readers.

## 3. File notes into it

Every note's row carries a **File…** button. It's there whether the note is in
**Unfiled notes** or filed somewhere already. **Unfiled notes** is the filter
in the folder pane for notes still waiting to be dealt with. The button opens
the same Move dialog you'd use to move any document. Choose the manual, or a
folder inside it, creating one on the spot if it doesn't exist yet.

Filing removes the note from the unfiled count. The pile drains as the
manual fills, so what's left in it is always what you haven't dealt with
yet.

A note filed into a manual lands at the end of its order. If it belongs
somewhere else in the sequence, put it there with **Arrange** (next step).
Filing and ordering are two separate, deliberately small actions.

A folder that is not part of a manual works the same way and stays a plain
list. Contacts and recipes are worth filing and are not worth numbering.

## 4. Put the sections in order

Open the manual (a folder like any other, marked with a book icon) and press
**Arrange**. Each section gains move up and move down controls. Use them to
get the order right, then press **Done**.

Order matters more than it looks. A manual is read front to back by someone
who doesn't know the boat. So the sequence should follow how the boat is
actually operated: getting under way before anchoring, anchoring before
leaving her for a month.

## 5. Add a photograph

A photograph of the actual valve, in place, is worth more than a paragraph
describing which one it is.

Open the note and press **Edit**. This opens the note editor, with a normal
formatting toolbar and no Markdown syntax to remember.

Press the **image** button in the toolbar, then **Take photo**. Shoot the
valve on the spot and it uploads and drops straight into the note at the
cursor, with no trip to Documents first. Picking several photos at once with
**Add from library** files each one in, in the order you picked them. Type
a caption first if you want one. It applies to whatever you take or pick
next.

Already uploaded the photo through Documents? Paste its id from the address
bar into the same popover instead, and give it a caption. Either way the
editor inserts the reference and shows the photograph in place, exactly as
it will read once saved.

If you are editing the raw Markdown directly (the `</>` button in the
toolbar switches to it), a photograph is:

```
![Fuel return valve](hc-doc:PASTE-THE-ID-HERE)
```

and it works exactly the same as anything the toolbar button would have
written.

Photographs only display when they come from your own library. A note cannot
pull an image in from the internet, which keeps the boat from making outbound
requests because of something that got pasted into a note.

## 6. Link sections to each other

A section can link to another note, using its id the same way a photograph
does. In the note editor's toolbar, press the **link** button and paste the
other note's id (from its own address bar), or an ordinary web address.
`tel:` and `mailto:` links work too, which matters for a contact's phone
number.

The raw Markdown form, reachable the same way as a photograph's, is:

```
See [the fuel valve layout](hc-note:PASTE-THE-ID-HERE)
```

Use it where a procedure genuinely depends on another one, such as a start-up
checklist pointing at the fuel valve layout. Don't use it as a substitute for
saying the thing in place. A reader following a chain of links at 0300 is a
reader who hasn't been told what to do.

## 7. Run a checklist

Write a procedure as a checklist and it can be run, not just read. In the
note editor's toolbar, use the **Task list** button (or type `- [ ]` at the
start of a line if you are in the raw Markdown view) for each step:

```
- [ ] Close the raw water seacock
- [ ] Shut the fuel valve at the tank
- [ ] Flip the battery isolators off
```

Open the note anywhere it reads, in the Documents viewer or a manual's
reading pane, and **Start checklist** appears above it. It swaps the page
for a full-width tick-off list sized for a thumb, not a cursor. Tap a step
to tick it, in any order. **Complete checklist** closes the run out when
you are done, and **Abandon checklist** stops it early without finishing.

If you get interrupted partway through, the note holds your place. Tap **Start
checklist** again and it picks up exactly where you left off. It shows how many
steps are done and the first one still unticked, so you don't have to scan the
list to find it.

Editing the note while a run is under way doesn't disturb it, as long as
the step's own wording is unchanged. Reordering steps, fixing a typo
elsewhere or adding a new step above doesn't move or clear a tick.

If you reword a ticked step itself, that one tick moves into a **Re-check**
group, showing the old wording next to the new step. That way nothing gets
silently counted as done when it was written differently to what actually
got checked off.

Completing or abandoning a run doesn't touch the note either. The next **Start
checklist** begins a fresh, unticked run.

## Keeping it useful

Notes stay searchable wherever they are filed, so a manual section and the
manufacturer's PDF it came from turn up in the same search.

The sections worth writing first are the ones that are true of your hull and
no other. Anything that's true of every boat of the model is already in the
builder's handbook. Anything that's true of every engine of the type is already
in the engine manual. Your manual's value is the rest.
