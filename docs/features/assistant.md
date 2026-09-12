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

## What it knows

Every question starts with your vessel's live position, heading, speed and
apparent wind, the current place name, and whatever marine warning is in
force for your zone, exactly as the dashboard shows them right now. If a
reading is unavailable it says so rather than guessing: an unknown wind
reads as "unknown", not as a number that happens to be wrong.

Beyond that, on request, it can look up wind, wave and tide forecasts for
any named place, not just where you are now. It resolves a place name to a
position first, searching your saved route waypoints and then OpenStreetMap
within about a hundred nautical miles of the boat, then fetches wind and
waves through the same providers the forecast panel uses, and tides through
the same tide provider and station catalog. If the name doesn't resolve, or
a provider is down, or no tide provider is configured, it says which one
failed. It never invents a forecast for a place it couldn't find, or a tide
time from a provider that returned nothing.

Place search queries the same Overpass server as the place name shown on
the position tile. The configuration reference explains how to point that
at a mirror if your boat's connection can't reach the default one.

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

Mate also knows two things about the app itself: what you're looking at when
you ask, and how Helmcentral's own features work. A question asked from the
sheet over the Forecast panel arrives with that noted, so "explain how the
upper atmosphere graph works" gets answered as a question about the panel
you're on, not a guess at what you might mean. And for a question about
Helmcentral itself, Mate reads the operator manual, the same pages this
documentation site is built from, and answers from what the docs actually
say rather than from a general impression of what a 500mb chart usually
shows. Ask "what does the upper air chart on the forecast panel actually
show" and it reads this manual's own [Forecast](forecast.md) page and quotes
it back.

## Talking to Mate

Voice is push-to-talk: tap the microphone in the header, or press `Alt+M`
from anywhere in the app, and speak. The transcript lands in the composer as
you talk and sends once you stop. Escape cancels a listening session.

A voice question can also be answered aloud: turn on "Read replies aloud" in
Settings → Mate → Voice and Mate speaks a short summary of its answer as soon
as it arrives, in addition to the full written answer, never instead of it.

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
use). See [Talk to Mate](../how-to/talk-to-mate.md) for exactly how to reach
that address and what to do when the microphone button won't light up.

## What leaves the boat

Every question sends your position, the question text itself, and whatever
forecast or tide data Mate decided to look up, to OpenRouter and from there
to whichever model you've chosen. This only happens when you ask something
and Mate is switched on; nothing is sent in the background.

A voice question additionally sends your speech to whichever engine your
browser uses to turn it into text: Apple's dictation for Safari (on-device on
recent hardware), Google's speech service for Chrome. That happens before
the question ever reaches Mate, and it happens for every voice question,
push-to-talk included, not only for "Hey Mate".

You bring your own OpenRouter account and your own key. You choose the
model. Different model providers have different data-retention and training
policies; check the one you pick if that matters to you.

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

## Conversations and cost

Questions and answers are kept as conversations you can return to, list, or
delete, the same way a chat app works. Starting a new conversation clears
the context Mate carries forward, so a fresh question about a different part
of the coast doesn't get answered in light of yesterday's passage. The sheet
and the full panel share the same conversations, so a question asked from
the sheet is still there if you later open the full Mate panel.

Every reply's footer leads with what OpenRouter charged for that exact
reply, for example `$0.056`. A question that also had Mate read the manual
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

If Mate can answer but voice specifically won't work, the microphone button
says why rather than sitting there unresponsive: no speech recognition in
this browser, or the app needs to be opened over https first. See [Talk to
Mate](../how-to/talk-to-mate.md) for what each message means and how to fix
it.
