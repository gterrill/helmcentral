# Mate

Mate is a chat assistant that answers passage-planning questions using the
same live data the rest of the dashboard already has: your position, the
forecast providers you've configured, and the tide station you've picked.
Ask it something like:

> We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first
> over the next two days?

and it looks both places up, checks wind and tide for each, and comes back
with a short answer and a comparison table, in the units and time zone the
rest of the dashboard uses.

The answer appears as Mate writes it, rather than sitting on a blank panel
until the whole reply is ready. While it's still looking something up you
see what it's doing instead ("Looking up Tongue Bay…"); that swaps for the
words themselves the moment there's an answer to show.

Mate keeps writing the answer even if you switch to another panel, or the
sheet closes, while it's still working. Come back and you'll either see it
still arriving or already finished, footer and all, instead of finding it
gave up. Only pressing the stop button next to a question actually cancels
it; leaving the page never does. If you've moved on to something else by the
time it finishes, a toast tells you Mate answered (or couldn't), with a
button that takes you straight to that conversation.

## Asking Mate

There are two ways in. The sidebar's Mate panel is the full conversation
view: a list of past conversations down one side, the current thread on the
other, the same as any chat app. The header's sparkle button, on every panel
and every dashboard page, opens Mate as a sheet over whatever you're already
looking at instead, so asking a question about the forecast doesn't mean
leaving it. Both talk to the same conversations; the sheet is just the quick
channel for a question that doesn't need its own screen. The sheet keeps
appending to whatever conversation is already current, no matter how long
you leave it and come back, until you press its "New conversation" button.
Its "Open in Mate" button hands that same thread to the full panel and
takes you there, for when a question turns into a longer working session.

The panel lives at `/mate`. A specific conversation can be opened directly at
`/mate/<thread-id>`, for example `/mate/12345`.

## What it knows

Every question starts with your vessel's live position, heading, speed and
apparent wind, the current place name, and whatever marine warning is in
force for your zone, exactly as the dashboard shows them right now. If a
reading is unavailable it says so rather than guessing: an unknown wind
reads as "unknown", not as a number that happens to be wrong.

Beyond that, on request, it can look up wind, wave and tide forecasts for
any named place, not just where you are now. If the name doesn't resolve,
or a provider is down, or no tide provider is configured, it says which one
failed. It never invents a forecast for a place it couldn't find, or a tide
time from a provider that returned nothing.

When it plans a passage leg, it works out the wind and sea angle against
your planned course itself, rather than leaving a language model to
subtract bearings by eye: a following sea and a head sea get called
correctly instead of guessed at.

When InfluxDB is configured, it can also estimate how long a passage will
take and how much fuel it will burn, from your own boat's logged speed over
ground and fuel-rate history, not a manufacturer's polar or fuel curve.
Give it a distance and a planned speed and it reads a burn rate and rpm off
what this vessel has actually done at that speed over the last few months,
telling you plainly that this is observed data across whatever conditions
occurred, not a guarantee: a head sea will add time and fuel beyond what
the log shows. Without InfluxDB configured, it skips this and reasons about
the passage in general terms instead.

Mate can also tell you who else is around. Ask about a boat by name - a
neighbour anchored nearby, one crossing your path, a boat sharing your
marina - and it reports range, bearing, speed, and how long it has been
sitting there. That last figure is a floor, not a fact: Mate only knows a
boat has been within about five kilometres of you since it first showed up
on your own instruments, so a boat that arrived before you did reads as "in
range" for less time than it has actually been there. Ask "when did we last
see Solaris" and it also answers for a boat that has since moved on, from
the record of past encounters. If your boat's own data logger has been set
up to record other vessels' position history and not just your own - most
boats leave this off - Mate reads that vessel's own track and gives a
tighter answer for how long it has actually been sitting at its current
spot.

If a reading looks wrong - missing, stuck on one value, or just not there
where you'd expect it on a tile - ask Mate before you go hunting for it
yourself. It checks the live instrument connection directly: whether it's up
at all, which feeds are currently updating and which have gone quiet, and
the value and age of anything you name. Ask "why is the exhaust temperature
not showing" or "is the depth reading working" and it answers from that
check rather than guessing. With a history log configured for the boat, it
goes further: ask "when did the tank levels stop updating" and it finds the
last recorded time for each one, names the specific feed behind it if that
feed has gone quiet, and can pull up what a reading was doing in the run-up
to when it stopped - flat-lined, dropping in and out, or just gone. Without
a history log configured, Mate can still tell you what's live right now; it
just can't look further back than that.

Mate also knows two things about the app itself: what you're looking at when
you ask, and how Helmcentral's own features work. A question asked from the
sheet over the Forecast panel arrives with that noted, so "explain how the
upper atmosphere graph works" gets answered as a question about the panel
you're on, not a guess at what you might mean. And for a question about
Helmcentral itself, Mate reads the in-app help, the same pages the Help
button in the app opens, and answers from what the docs actually
say rather than from a general impression of what a 500mb chart usually
shows. Ask "what does the upper air chart on the forecast panel actually
show" and it reads the help's own [Forecast](forecast.md) page and quotes
it back.

See [What Mate can look up](../reference/mate-tools.md) for exactly which
provider each of these draws on, what counts as "nearby" for a place
search, and what a failed or unconfigured lookup looks like.

## Documents

Mate can read from the boat's own document library: manuals, receipts,
invoices, service logs, passage notes, photos of a part or a data plate.
There are two ways it draws on that: attach a file directly to a question -
the paperclip in the composer, or drop a file straight onto it - or let
Mate search the whole library on its own, the same way it looks up a
forecast or a tide station, with no attachment needed. See
[Documents](documents.md#attaching-a-document-to-mate-and-mate-searching-on-its-own)
for exactly how much of a document Mate reads and when.

When an answer draws on something specific it found this way, it cites the
source as a small icon next to the text instead of describing it in a
sentence - tap or click it to open that manual, receipt, or note directly,
and hover or press it to see its title. A citation to something that's
since been removed from the library shows as a faded, broken icon rather
than just vanishing.

That one search covers everything in the library at once: uploaded manuals,
your own captured notes, receipts, photos of a data plate. There's no
separate search that only looks inside your authored manuals, even though a
boat's own [operations manual](notes-and-the-manual.md) lives in that same
library. It's tempting to want one, but the question that sounds like it
needs it is the one it would get wrong: ask "what does the manual say about
the impeller" and on most boats that means the engine's own PDF, not the
operations manual you wrote by hand, so a search scoped to just your own
manual would answer confidently from the wrong book. Mate is told which
manuals exist and roughly what's in each (see below), and reaches for the
same library-wide search either way, narrowed to the right folder when a
question is obviously about one manual in particular.

See [Documents](documents.md) for what gets indexed, what it costs, file
types and limits, and how folders and tags work.

## Talking to Mate

Voice is push-to-talk: tap the microphone in the header, or press `Alt+M`
from anywhere in the app, and speak; the transcript lands in the composer as
you talk and sends the moment you stop. The composer has its own, separate
microphone too, and it does a different job: it dictates into whatever
question you're already typing, rather than sending on its own, for adding a
follow-up thought by voice without cutting off what you'd already typed. A
voice question can also be answered aloud, on top of the full written
answer, never instead of it.

There's also "Listen for Hey Mate", an experimental always-on mode: say "Hey
Mate" followed by your question, or say "Hey Mate" alone and Mate waits about
eight seconds for the question to follow. It's off by default, and it has
real costs worth knowing before switching it on: it keeps the microphone
open the whole time the app is on screen, it uses more battery than
push-to-talk, and on Chrome the audio stream goes to Google continuously
rather than only when you actually ask something. It also mishears ordinary
conversation as "Hey Mate" from time to time; that's an accepted cost of
always listening, not a bug. It pauses automatically when the browser tab is
hidden.

Voice needs the app opened over **https**, with microphone permission
granted, in **Safari** or **Chrome** (Firefox has no speech recognition to
use). See [Talk to Mate](../how-to/talk-to-mate.md) for how to turn each of
these on, reach that address, and what to do when the microphone button
won't light up.

## What leaves the boat

Every question sends your position, the question text itself, and whatever
forecast or tide data Mate decided to look up, to OpenRouter and from there
to whichever model you've chosen. This only happens when you ask something
and Mate is switched on; nothing is sent in the background.

A document you attach to a question, or one Mate reads through its own
library search, is sent the same way - as text, since it's already been
read once when it was indexed. Separately, uploading or reindexing a
document while Mate is switched on sends that document's own content for
OCR and summarising, whether or not you go on to ask a question about it;
see [Documents](documents.md) for what that costs and what leaves the boat
at that point, not just when you ask Mate about one.

A voice question additionally sends your speech to whichever engine your
browser uses to turn it into text: Apple's dictation for Safari (on-device on
recent hardware), Google's speech service for Chrome. That happens before
the question ever reaches Mate, and it happens for every voice question,
push-to-talk included, not only for "Hey Mate".

You bring your own OpenRouter account and your own key. You choose the
model. Different model providers have different data-retention and training
policies; check the one you pick if that matters to you.

Settings now supports two routing modes:

1. A fixed model id, chosen from a tool-capable model list fetched from
	OpenRouter.
2. OpenRouter Auto (`openrouter/auto`), with optional routing constraints:
	allowed model patterns, excluded model ids, and a cost tier cap (`low`,
	`medium`, `high`, `xhigh`, `max`).

Auto routing only applies when the model is set to OpenRouter Auto; for any
other model id, those routing constraints are ignored.

## Standing notes

Settings → Mate has a notes field that gets sent with every question, word
for word. This is where you put anything you know about your own cruising
ground that a general-purpose model wouldn't: which anchorage suits which
tide, which bay is a lee shore in a southerly, when the fish bite. For
example:

> Queenfish fish Hill Inlet on a rising tide.
> Snorkel Blue Pearl Bay in the last 1-2h of flood up to high slack.

Mate is told to prefer these notes over its own general knowledge about a
place. There's no separate rules screen or anchorage database to maintain:
if you know a piece of local knowledge well enough to write it as a
sentence, it goes here.

That field is one blob for the whole boat, sent whole every time. For a
single fact rather than a paragraph, pin a note instead (see [Notes and the
manual](notes-and-the-manual.md)): open it in Documents and use **Pin for
Mate**. A pinned note's title and full text ride along on every question
too, and get the same "prefer this over general knowledge" treatment the
settings field does, without editing a shared paragraph every time you want
to add or retire one fact. Pin sparingly - a handful of notes and a few
thousand characters between them actually reach Mate, and going over that is
loud rather than silent: the prompt says outright how many notes it left out
rather than truncating one mid-sentence.

Traffic runs the other way too. When an answer is worth keeping - a
procedure Mate has just walked you through, a figure it worked out - **Save
as note** under that answer keeps it, without retyping. Mate does not write
it: the note is created by your tap, lands unfiled like any other capture,
and you file or edit it afterwards. Mate has no tool that can write
anything, which is deliberate and explained below.

Mate is also always told which manuals exist and, for each, the names of its
top-level sections - "Operations Manual (Before Leaving, Getting Underway),
Crew Training (Watchkeeping)" - so it knows which book to point you at by
name. It's an index, not the manual's content: the actual reading still goes
through the same document search everything else in the library uses (see
[Documents](#documents) above for why there's no manual-specific search).

## Conversations and cost

Questions and answers are kept as conversations you can return to, list, or
delete, the same way a chat app works. Starting a new conversation clears
the context Mate carries forward, so a fresh question about a different part
of the coast doesn't get answered in light of yesterday's passage. The sheet
and the full panel share the same conversations, so a question asked from
the sheet is still there if you later open the full Mate panel.

Every reply's footer leads with what OpenRouter charged for that exact
reply, for example `$0.056`. A question that also had Mate read the help
shows a tool-round count alongside it (`$0.056 · 1 tool round`), since
that's one extra round trip to the model before the answer comes back.
There's no separate bill to check afterwards: the running cost of asking
questions is on the screen every time you ask one. Which model answered and
how many tokens the question and answer used between them are still there
if you want them, in the tooltip you get from hovering the footer (or a
long press on a touchscreen) - detail worth having on hand, not something
you need at a glance every time.

## What it is not

It is not a navigational authority. It reasons from the forecast and tide
providers you've configured and from what it can find about a place by
name, and it will state the assumptions it's making about shelter and
exposure out loud, for example "I am assuming Blue Pearl Bay is open to the
north-west; correct me if not." Check that assumption against your own
chart before you act on it. Weather models and tide predictions are both
already approximations before a chat model summarises them.

It needs internet. Without a connection, or with no key configured, or
switched off, it says so plainly rather than pretending to work.

It is read-only. It can look things up and it can explain how Helmcentral
works; it cannot start anything, change a setting, or steer anything. There
is nothing it can do to the boat, by typing or by voice.

Voice is a convenience on top of that, not a separate control path: Mate
still only answers, whether you typed the question or said it.

## When it can't run, or can't listen

If Mate can't answer at all, it tells you exactly why instead of failing
silently: switched off, no OpenRouter key configured, or no model chosen,
each naming Settings → Mate as the place to fix it. If the model you've
chosen doesn't support tool calling, a question comes back with an upstream
error saying no endpoint supports tool use; see [Set up
Mate](../how-to/set-up-the-assistant.md) for what to do about that.

Some models answer a question by writing out a tool call as ordinary text
rather than making one. Mate refuses that reply instead of showing it to
you, and the error names the model, because a page of markup where an
answer should be is worse than being told plainly that this model can't do
the job. Pick a different one in Settings → Mate.

A lookup that keeps failing is abandoned rather than retried forever.
Retrying forever is what used to happen: a model will happily spend every
round it's allowed on a server that has stopped responding, so no answer
ever arrives. See [What Mate can look up](../reference/mate-tools.md) for
exactly when that kicks in and what Mate says when it does.

If Mate can answer but voice specifically won't work, the header microphone
says why rather than sitting there unresponsive - unless the browser has no
speech recognition at all, in which case it isn't shown rather than shown
disabled with no way to act on it. Press `Alt+M` anyway and Mate names the
reason in a toast. The composer's own dictation mic follows the same rule.
See [Talk to Mate](../how-to/talk-to-mate.md) for what each message means
and how to fix it.
