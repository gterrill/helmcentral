# Assistant

The assistant is a chat panel that answers passage-planning questions using
the same live data the rest of the dashboard already has: your position,
the forecast providers you've configured, and the tide station you've
picked. Ask it something like:

> We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first
> over the next two days?

and it looks both places up, checks wind and tide for each, and comes back
with a short answer and a comparison table, in the units and time zone the
rest of the dashboard uses.

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

## What leaves the boat

Every question sends your position, the question text itself, and whatever
forecast or tide data the assistant decided to look up, to OpenRouter and
from there to whichever model you've chosen. This only happens when you ask
something and the assistant is switched on; nothing is sent in the
background.

You bring your own OpenRouter account and your own key. You choose the
model. Different model providers have different data-retention and training
policies; check the one you pick if that matters to you.

## Standing notes

Settings → Assistant has a notes field that gets sent with every question,
word for word. This is where you put anything you know about your own
cruising ground that a general-purpose model wouldn't: which anchorage
suits which tide, which bay is a lee shore in a southerly, when the fish
bite. For example:

> Queenfish fish Hill Inlet on a rising tide.
> Snorkel Blue Pearl Bay in the last 1-2h of flood up to high slack.

The assistant is told to prefer these notes over its own general knowledge
about a place. There's no separate rules screen or anchorage database to
maintain: if you know a piece of local knowledge well enough to write it as
a sentence, it goes here.

## Conversations

Questions and answers are kept as conversations you can return to, list, or
delete, the same way a chat app works. Starting a new conversation clears
the context the assistant carries forward, so a fresh question about a
different part of the coast doesn't get answered in light of yesterday's
passage.

## What a reply costs

Every reply's footer shows which model answered, how many tokens the
question and answer used between them, and what OpenRouter charged for
that exact reply, for example `anthropic/claude-sonnet-4.5 · 3,214 tokens ·
$0.0184`. There's no separate bill to check afterwards: the running cost of
asking questions is on the screen every time you ask one.

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

It is read-only. It can look things up; it cannot start anything, change a
setting, or steer anything. There is nothing it can do to the boat.

## When it can't run

If the assistant can't answer, it tells you exactly why instead of failing
silently: switched off, no OpenRouter key configured, or no model chosen,
each naming Settings → Assistant as the place to fix it. If the model you've
chosen doesn't support tool calling, a question comes back with an upstream
error saying no endpoint supports tool use; see [Set up the
assistant](../how-to/set-up-the-assistant.md) for what to do about that.
