# Mate

Mate is a chat assistant that answers passage-planning questions. It uses the
same live data the rest of the dashboard already has: your position, the
forecast providers you've configured, and the tide station you've picked.
Ask it something like:

> We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first
> over the next two days?

and it looks both places up and checks wind and tide for each. Then it comes
back with a short answer and a comparison table, in the units and time zone the
rest of the dashboard uses.

The answer appears as Mate writes it, rather than after the whole reply is
ready. While it's still looking something up, you see what it's doing
("Looking up Tongue Bay…"). That gives way to the words themselves as soon as
there's an answer to show. While Mate is simply thinking, with nothing to
look up, the line shows a nautical phrase ("Coming about…", "Trimming the
sails…") that changes every few seconds.

A new conversation opens with a short list of what Mate can check, so you can
see what to ask about. The list covers position and wind, instrument history,
watching a reading for a few minutes, nearby vessels, forecasts and tides,
passage estimates, your passages and season, places, equipment and maintenance, and your documents.

Mate answers one question at a time in each conversation. Here is how that
works:

- **Send is greyed out:** It stays greyed out until the answer is in or you
  stop it.
- **A question that arrives mid-answer:** Suppose a question arrives by voice,
  from Help or from a selection while the sheet is still answering. It waits in
  the box, with "Mate is still answering the last question." above it, and you
  send it once the answer is in.
- **A question from another device:** One that reaches Mate from another device
  gets the same sentence, rather than a second answer.

Mate keeps writing the answer even if you switch to another panel or the sheet
closes. When you come back, you'll find it still arriving or already finished,
footer and all. Only the stop button next to a question cancels it, and leaving
the page never does. If you've moved on by the time it finishes, a toast tells
you Mate answered (or couldn't), with a button that takes you straight to that
conversation.

## Asking Mate

There are two ways in:

- **The Mate panel:** The sidebar's Mate panel is the full conversation view.
  It has a list of past conversations down one side and the current thread on
  the other, like any chat app.
- **The sheet:** The header's sparkle button, on every panel and every
  dashboard page, opens Mate as a sheet over whatever you're looking at. That
  way, asking a question about the forecast doesn't mean leaving it.

Both talk to the same conversations. The sheet is the quick channel for a
question that doesn't need its own screen. It keeps adding to the current
conversation, no matter how long you leave it, until you press its "New
conversation" button. Its "Open in Mate" button hands that thread to the full
panel and takes you there, for when a question turns into a longer working
session.

Both always open on a fresh, blank conversation. Neither resumes the thread
that was last active, and nothing is saved until you send a question, so
opening Mate and changing your mind leaves nothing behind. The one exception
is a reply that's already being written when you close and reopen the sheet
mid-answer, which keeps arriving as it was.

To pick up an earlier conversation, choose it from the panel's list, or use the
sheet's search. The search icon in its header opens a quick list of your 8 most
recent conversations. You can also type to find a conversation by any word in
it, not just its title. A match inside a conversation shows a line of the
matching text under the title.

On a phone the panel has no list down the side. Press **Chats** at the top of
the panel to open that same quick list, and **New** beside it to start a blank
conversation.

The panel lives at `/mate`. You can open a specific conversation directly at
`/mate/<thread-id>`, for example `/mate/12345`.

## What it knows

Every question starts with some facts about your vessel, exactly as the
dashboard shows them right now: your live position, heading, speed and apparent
wind, the current place name, and whatever marine warning is in force for your
zone. If a reading is unavailable, Mate says so rather than guessing: an
unknown wind reads as "unknown", not as a number that happens to be wrong.

On request, it can look up more than that. Here is what it can do:

- **Forecasts for any place:** It can look up wind, wave and tide forecasts for
  any named place, not just where you are now. If the name doesn't resolve, a
  provider is down or no tide provider is configured, it says which one failed.
  It never invents a forecast for a place it couldn't find, or a tide time from
  a provider that returned nothing.
- **Wind and sea angle:** When it plans a passage leg, it works out the wind
  and sea angle against your planned course by calculation, not by eye. That
  way a following sea and a head sea are called correctly.
- **A fair-tide departure:** See the next section.
- **Anchorage shelter:** See "Sheltered from which winds" below.
- **Passage time and fuel:** See "Passage estimates" below.
- **Who else is around:** See "Nearby vessels" below.
- **How the boat is doing:** See "A status update" below.
- **Faults in a reading:** See "When a reading looks wrong" below.
- **Maintenance, inventory, alarms, the boat's details and the app:** Each has
  its own paragraphs below.

[What Mate can look up](../reference/mate-tools.md) lists the provider each of
these draws on. It also explains what counts as "nearby" for a place search,
and what you see when a lookup fails or isn't set up.

### A fair-tide departure

For a passage leaving from where the boat is now, Mate can pick a departure
time that gives you a fair tidal stream. It ranks departures over the next day
by how much of the run has the stream with you, then recommends the best one
and names the time to avoid.

It times the stream from the tide station's high and low water. So it tells you
which spell of flood or ebb you'll be in, not how fast it runs.

- **Which way the flood sets:** The direction comes from your standing notes
  first. If they're silent, Mate uses its own general knowledge and says so.
  Either way, check it against the tidal stream atlas or the cruising guide. If
  Mate doesn't know which way the flood sets in that water, it says so instead
  of guessing.
- **Your standing notes:** Put the local flood direction in your standing
  notes, along with any lag between high or low water and the stream turning,
  and Mate uses them.
- **Stream against wind:** Where the stream sets against the forecast wind
  during the run, Mate warns that the seas will be steeper and shorter. It also
  says when the fair-tide departure runs into that.
- **A course across the stream:** A course square across the stream gains
  little from timing, and Mate says so.

### Sheltered from which winds

Mate can tell you which wind directions an anchorage is sheltered from. Ask
about a bay or a position, and it reads the charted coastline and reefs around
it. Then it reports the compass arcs the spot is sheltered, partly sheltered
and exposed from, such as "sheltered from NNE through S (030°–190°)".

- **Bearing table:** A bearing-by-bearing table shows how far the nearest land
  or reef is and what it is.
- **Choosing between anchorages:** When you're choosing between anchorages
  against a forecast, Mate compares those arcs with the wind direction forecast
  for each night of the stay.
- **Inside a reef:** If you're anchored inside a reef, Mate gives the reef's
  width in each direction and tells you that reef shelter depends on the tide.

Treat it as a first look, and Mate says so. It works from the shoreline as
charted, so it can't see wind bending round a headland, gusts funnelling down a
valley or swell wrapping round a point. That means it can read an anchorage as
better sheltered than it is on the side where swell curls in. It's only as good
as the chart.

The shoreline is the [Offline coastline](offline-coastline.md), kept aboard, so
the answer needs no signal. It's off until you turn it on in Settings → Offline
coastline. It covers the water within 200 nm of the boat plus anywhere you've
downloaded before. If it hasn't been downloaded, is off, or a neighbouring
square is missing, Mate tells you which and doesn't guess the shelter.

### Passage estimates

When InfluxDB is configured, Mate can estimate how long a passage will take and
how much fuel it will burn. It works from your own boat's logged speed over
ground and fuel-rate history, not a manufacturer's polar or fuel curve. It reads
a burn rate and rpm off what this vessel has actually done at that speed over
the last few months.

The distance is the way by water. Mate works it out from the boat's position, or
from a start you name, to the destination. It goes round any island in the way.
So a place on the far side of an island is the real run, not the line across the
chart. It needs the [offline coastline](offline-coastline.md) for the water
round both ends. If that's off, a square is missing or a place is on land, Mate
says it can't give a passage distance and why. It doesn't fall back to a
straight line.

It says plainly that this is observed data across whatever conditions occurred,
not a guarantee. It also says that a head sea will add time and fuel beyond
what the log shows. Without InfluxDB configured, Mate skips this and reasons
about the passage in general terms instead.

### Nearby vessels

Mate can tell you who else is around. Ask about a boat by name (a neighbour
anchored nearby, one crossing your path, a boat sharing your marina). Mate
reports its range, bearing, speed and how long it has been sitting there.

That last figure is a floor, not a fact. Mate only knows a boat has been within
about five kilometres of you since it first showed up on your own instruments.
So a boat that arrived before you did reads as "in range" for less time than
it has actually been there.

Ask "when did we last see Solaris" and Mate also answers for a boat that has
since moved on, from the record of past encounters. Your boat's own data logger
can be set up to record other vessels' position history and not just your own,
though most boats leave this off. If yours does, Mate reads that vessel's own
track and gives a tighter answer for how long it has been sitting at its
current spot.

### Your passages and your season

Ask Mate about a season, a cruise, a trip or where you've been, and it tells the
story from the [Passages](passages.md) page. It reads the passages you've
recorded there, so it works however long ago they were. Try "Summarise our
cruising season" or "How far did we go in August?". Mate gives you:

- **The route:** Where you left, then each place you reached in order, using
  the names on your passages.
- **The totals:** How many passages, how far you went, how long you were under
  way, and the fuel burned.
- **The longest legs:** The longest passages, with their dates.
- **The stays:** Where the boat sat longest between passages, and for how many
  days. Mate works a stay out from the gap between one passage ending and the
  next one starting.

Mate takes places only from your passages. If a passage has no name for where it
started or finished, Mate leaves that place out of the route and says so. If one passage finished at a
place and the next left from a differently named one, it tells you both names
and doesn't pick one. It doesn't work out which way you were heading,
or what region a place is in.

When you give Mate dates, such as "August", it means those days on the boat's
clock, in the time zone the Passages page shows for each passage. Mate quotes
times in that zone too.

If the boat is under way as you ask, that passage isn't in the log yet, because
it's recorded when it ends. Mate tells you it's under way and how far it has
gone, and keeps it out of the totals. If a passage you hid on the Passages page
falls inside a stay, Mate flags that the boat moved during it.

Mate also reads each passage's rating and notes. It can tell you which passages
you rated **Lively**, or which ones mention a particular bay.

There are things the log can't tell Mate, and it says so:

- **Fuel:** Passages that came from an imported track, or from before
  Helmcentral logged your fuel rate, have no fuel figure. Mate adds up the passages that have one and tells you how
  many it left out.
- **How you stayed:** The log doesn't record whether you were at anchor, on a
  mooring or in a marina, so Mate doesn't split stays that way.
- **Engine hours:** Mate gives the time you were under way, which isn't the same
  as engine hours.

If the Passages page is empty, or has nothing in the dates you asked about, Mate
says that. It doesn't go looking for passages in the instrument history.

To look at what a reading did on one passage, ask Mate. It finds the passage in
your log, then reads the history for that passage's start and end. That needs a
history log, as described under [When a reading looks wrong](#when-a-reading-looks-wrong).

### A status update

Ask "how's the boat?", "quick update" or "status" and Mate gives you a short
report with the figures already worked out. It reads them from the equipment and
the roles you've set up, so it doesn't have to hunt for the right reading. The
report has four headings and then a few things worth a look:

- **Power:** The house bank's charge, voltage and current, the solar watts and the
  DC load.
- **Tanks:** Each tank's level, with the warn and alarm levels set for it. A tank
  that is in warn or alarm says so.
- **Outside:** Depth, true wind with its gust over the last ten minutes, and the
  pressure with how far it has moved in the last three hours. The three hour
  change appears once Helmcentral has about three hours of pressure readings, and
  it starts again after a restart.
- **Boat:** Whether each engine is running or off, and whether the anchor watch
  is set. An engine whose readings have stopped without it being keyed off shows
  as unknown, not off. It also lists the alarms active and ignored, and your planned stay
  with how far it is from the boat.
- **Worth a look:** At most three things. They come from the sensor checks, which
  catch a frozen, impossible or silent sensor. They also come from readings Mate
  couldn't get, and from the house bank draining faster than usual overnight.

When it raises the overnight draw, Mate asks you a question and gives the
figures. For example: "The bank fell 0.9 percent an hour last night, against
about 0.5 on the nights before. Is anything running that normally isn't?" It
doesn't guess what caused it.

A reading that can't be right, such as a charge above 100 percent or a negative
solar figure, is left out the same way. A tank whose warn and alarm levels don't
make a sensible pair is shown with a note that its alarm isn't running.

Figures are given the way an instrument shows them: solar and load in whole
watts, volts to two places, and wind, current, depth, temperature and levels to
one. When a reading is missing, Mate says which one and why, and doesn't guess
at the cause.

Mate gives each reading in your units. When it has no fresh reading, it leaves
the figure out and says why in a few words. It might say "no house bank role set"
or "no fresh reading, the last one arrived 20 minutes ago". It never fills the
gap with an older figure or an estimate.

The overnight check needs three things. Your house bank must be chosen under
**Settings → SignalK → Roles**. InfluxDB must be set up. And there must be at
least four recent nights at anchor, with no shore power or generator, for Mate to
compare. Without them Mate leaves that check out and tells you why.

The overnight check is a question for you and never an alarm, so nothing is sent
to your phone. Mate doesn't yet notice a sensor reading differently from its
neighbours, or a source that has been silent since it was installed.

### When a reading looks wrong

If a reading looks wrong (missing, stuck on one value, or not there where
you'd expect it on a tile), ask Mate before you go hunting for it yourself. It
checks the live instrument connection directly. It looks at whether the
connection is up at all, which feeds are updating and which have gone quiet,
and the value and age of anything you name. Ask "why is the exhaust temperature
not showing" or "is the depth reading working" and it answers from that check
rather than guessing.

With a history log configured for the boat, Mate goes further. Ask "when did
the tank levels stop updating" and it does three things:

- **Last recorded time:** It finds the last recorded time for each one.
- **The feed behind it:** It names the specific feed behind a reading if that
  feed has gone quiet.
- **The run-up:** It can pull up what a reading was doing in the run-up to when
  it stopped: flat-lined, dropping in and out, or just gone.

Sometimes a reading was only recorded for a short stretch, such as an engine
that has been running for twenty minutes. Then Mate looks at it minute by
minute, so a brief spike still shows. Without a history log configured, Mate can still tell you
what's live right now, but it can't look further back.

Some faults only show up over time: an engine load that jumps for a second
every few minutes, or a voltage that dips when something cycles on. Ask Mate to
keep an eye on it, for example "watch port and starboard engine load for five
minutes and tell me if port spikes". It starts a watch and tells you when it
will report back. See [Watching a reading](#watching-a-reading) below.

### Maintenance

Mate can read your maintenance schedule and service log. Ask "what's overdue", "when was the generator last serviced" or "is the
schedule for the main engine complete". Mate looks up the rules, their status
today and the history behind them.

For a review, it lines your rules up against two things: the manufacturer's
recommended services on the item's profile, and the manuals in your library.
Then it names what's missing or different. If an item's running hours aren't being
read, it says the hours are unknown rather than guess.

It can also propose changes to the schedule as a card under its reply. Nothing
changes until you tap **Apply**. See [Maintenance](maintenance.md) and
[Set up a maintenance schedule with Mate](../how-to/set-up-a-maintenance-schedule-with-mate.md).

### Inventory

The same goes for your inventory. Mate can read equipment, locations, bins and
decks. It can also propose adding, changing or removing them: filing a spare
into a bin, renaming locations, or setting up a deck from a drawing in your
documents.

Every proposal is a card under its reply that shows each change, with what it
is now and what it will become, and a removal says what goes with it. A card can
hold several changes that belong together, such as a new deck and the locations
moved onto it, and it applies all of them or none. You still tap **Apply**, and
Mate won't say anything is done until you have.

Mate can't see pictures, so when a card sets a deck's plan, it shows you the
picture and you check it's the right one. A plan has to be a JPEG or PNG. If the
drawing in your documents is a PDF, Mate asks you to upload a picture of that
page instead.

### Alarms

Mate can read every alarm the boat is watching, and it can propose a new one or a change to one you already have. Say what you want to be told about, such as "tell me if the house bank drops below 50%" or "warn me if the fridge goes above 8 degrees", and Mate checks the alarms you already have first. If one already watches that reading, it proposes changing that alarm instead of adding a second one.

The card under its reply names the alarm in a few words, such as House Bank Low, and says in a sentence what it does: how serious it is, the level it fires at, the level it clears at, and how long the reading has to hold before it goes off. The levels are in the units you use on the boat, so a battery reads in percent, a temperature in degrees Celsius and a speed in knots. For an alarm that fires above or below a level, Mate always sets a clear point a little past it so that the alarm doesn't chatter, and it tells you what it chose. If the boat doesn't report a unit for a reading, Mate can't tell what a number on it means, so it asks you to set that level in the Alarms drawer. You can ask for different numbers before you tap **Apply**.

Mate won't say an alarm is set until you've tapped **Apply**. After that the alarm is watching straight away and shows in the Alarms drawer like one you made yourself.

Some alarms aren't Mate's to change:

- Tank alarms: They come from each tank's own levels. Mate changes those on the tank's equipment item, where you'd change them yourself.
- Gauge zones: A coloured band on a gauge raises its own alarm. You edit the band on the gauge, so Mate can't.
- Built-in alarms: The ones that came with Helmcentral can be changed, switched off or removed like your own. The card says when it's one of them, and one you remove stays gone.

If you ask "what should I be monitoring?", Mate looks at what the boat actually reports and what has no alarm yet. It suggests a few readings that matter, at most three, with the level it would use and why, and each one comes as a card you can tap. It won't suggest an alarm on something the boat doesn't measure. See [Set an alarm threshold](../how-to/set-an-alarm-threshold.md) for doing it by hand.

### The boat's details

Mate can read the boat's details on **Settings → Vessel**: the owner and
insurance, the home marina and berth, the storm delegate, and the hull and
registration. It can also propose filling in or correcting them, as the same
kind of card with **Apply**. Ask "the insurer is now Pantaenius, policy
PY-1234", or give it an answer while it fills in a form, and it offers to keep
it.

There's only one record of the boat's details, so Mate can change what's on it
but can't add a second one or remove it. The boat's name, call sign, MMSI and
draft aren't on it, because they come from the boat's instruments.

Mate never operates the boat. The autopilot, the generator, switching circuits,
the anchor watch and acknowledging an alarm are not things it can propose. Apart
from the boat's details, neither are your settings or logins.

### The app itself

Mate also knows two things about the app itself: what you're looking at when
you ask, and how Helmcentral's own features work.

- **What you're looking at:** Every question asked from the sheet, follow-ups
  included, arrives with the screen behind it noted. So "explain how the upper
  atmosphere graph works", asked from the sheet over the Forecast panel, is
  answered as a question about the panel you're on.
- **An open document:** The same goes for a document. Open Mate from a
  document's Details page and ask "what period does this cover?", and Mate reads
  that document to answer rather than asking which source you mean.
- **How Helmcentral works:** For a question about Helmcentral itself, Mate reads
  the in-app help, the same pages the Help button opens. It answers from what
  they say rather than from a general impression of what a 500mb chart usually
  shows. Ask "what does the upper air chart on the forecast panel actually show"
  and it reads the help's own [Forecast](forecast.md) page and quotes it back.

### Active alarms

Mate can also see the alarms that are active. Ask "investigate the alarm" and it
starts from the alarm itself, including the reading that tripped it, rather than
from the device name on the card. Then it checks that reading's live state and
history. It tells you what the reading was when the alarm was raised, apart from
what it is now.

### Asking about one alarm

Every alarm card in the Alarms drawer has an **Ask Mate** button, and so does
each row in the drawer's History, including alarms that have already cleared.
Tap it and Mate opens on a new conversation with a question already asked, such
as "Why did House Bank Low go off?". Mate knows which alarm you mean, so you can
follow up with "is that normal?" or "what about last night?" without naming it
again.

Mate starts by looking up that alarm and then tells you:

- What tripped it: The reading against the level the alarm fires at, and where it clears and how long it had to hold, in your units.
- Whether the reading looks real: Whether the sensor is still reporting, how the reading moved in the half hour either side of the alarm, and whether other alarms went off or cleared within 15 minutes. It gives these as evidence and doesn't guess at a cause.
- How often it happens: How many times that alarm was raised in the last week.

Under the answer is a chart of the reading around the time the alarm went off.
Mate doesn't reword the alarm itself. The card keeps its own title and message.
If the level looks wrong for the way your boat behaves, for example the alarm
trips every night at the same point, Mate says so with the figures and offers a
card to change the alarm. Nothing changes until you tap **Apply**.

Some parts can be missing, and Mate says so when they are. An alarm raised by
another instrument on the network has no level of its own to quote. A collision
or radar alarm has no reading behind it. Without InfluxDB there is no history to
chart. Mate doesn't group a reading with related ones, so it can't tell you
what the other sensors on the same item did unless you ask it to check them.
The button isn't on the banner across the top. Tap **View** there and the
buttons are on the cards.

## Waiting for a weather window

When you're waiting to make a run in stages, ask Mate which days work:

> We want to leave Townsville for Cape Upstart, spend two nights there, then
> go on to Bowen. Which departure days this week look good?

Mate checks every leg of the trip on the day it would actually sail. Leaving
Thursday with two nights at Cape Upstart puts the Bowen leg on Saturday, and
that leg is judged on Saturday's forecast. A day is only as good as its worst
leg.

The answer comes with a card under it, with one row for each departure day and
one column for each leg. Every cell reads **GO**, **MARGINAL**, **NO-GO** or
**NO DATA**. It also shows the departure and arrival times and the figure that
decided it ("Wind SE 27 kn ahead · Cape Bowling Green → Cape Upstart").

The grading uses your [passage limits](../how-to/set-passage-limits.md): a
no-go wind, and if you want it, a no-go wave height. You set these separately
for wind ahead, abeam and astern of your course. That's why the same twenty
knots can be a go on a downwind leg and a no-go on a beat.

- **Marginal:** It's worked out as 70% of no-go.
- **Gusts:** They're judged against the no-go wind plus 10 knots.
- **Angles:** Mate works out the angle for each part of a leg, including after
  it rounds a headland.
- **No limits set:** Until you set the limits, Mate says so and doesn't grade
  the trip.

When you ask whether or when to go, Mate runs this check alongside its usual
briefing and shows the card without repeating the grid as a table.

The card doesn't fill gaps. A leg past the end of the forecast is **NO DATA**,
not go. Anything it couldn't check is listed under the grid ("Not checked: wave
height, wind against tide"). That happens, for example, when no wave forecast is
available or Mate doesn't know which way the flood runs on that leg. An hour
with no wave height in the forecast is never treated as a flat calm.

"Not checked: wave direction" means the forecast had a wave height but not the
direction it was coming from, and your wave limits don't all agree about it. Mate
compares the height with the limit for each sector. If all three sectors have a
limit and give the same answer, it uses that answer. Otherwise it can't tell which
limit applies, so it leaves the check out rather than guess. When the forecast has a wave period, the card shows it beside the
height, for example "Waves 1.8 m · 6 s abeam". Wind
against tide is judged on the direction and timing of the stream from the tide
station's high and low water, not on its speed.

The card shows how old the forecast was when Mate checked it, and says so once
that's more than six hours. For a fresh answer, ask again.

### What you did with the plan

Helmcentral keeps a note of what you did with each weather window card, so you
can see later whether you sailed the plan or only asked out of curiosity. The
card has a **What you did** line above "Not checked", with a menu you can change
at any time:

- **Went:** There's one "Went" choice for each departure day on the card, for
  example "Went · Thu 23 Oct". The row you chose is marked **WENT**.
- **Stayed:** You had the forecast and stayed where you were.
- **Just looking:** You asked, and never meant to go. Only you can set this one.
- **Not set:** Clears your choice and leaves it blank.

You don't have to fill it in. Helmcentral fills it in for you, and adds
"(inferred)" so you can tell it did:

- **Went:** If the boat leaves within 4 hours of a day on the card, starting
  within 2 NM of the plan's first start point and reaching within 2 NM of the
  end of the first leg, that day is marked as the one you went.
- **Stayed:** Once every day on the card has passed and the boat hasn't left on
  the plan, the card shows Stayed. Helmcentral waits until it has finished
  reading the track for the last day, and a track imported later can still turn
  Stayed into Went.
- **Left blank:** It doesn't guess when it can't tell. If the boat set out on
  the plan but turned back, or the track can't be read, the card stays blank,
  because "Stayed" would be wrong. A newer card for the same day and the same
  first leg also takes over from an older one.

Your own choice always wins, and Helmcentral never changes it. Cards saved
before this was added have no line. A read-only screen shows the same words
without the menu.

## Charting what the boat logged

Mate can draw a chart of any reading your boat logs, so you can see a change
rather than read a summary of it:

> Show the starboard alternator current against house bank state of charge
> for 1–5 October and for 12–16 October. I changed the regulator settings on
> the 8th.

The chart appears under the answer. It can be drawn in two ways:

- **Over time:** Each reading is a line. Readings in different units get their
  own panel, stacked one above the other and sharing the same time scale, so
  amps and percent are never drawn on one axis. Behind each line sits a pale
  band from the lowest to the highest reading in that stretch of time. For wind
  speed, the top of the band is the gusts.
- **One against the other:** Each point is one stretch of time in which both
  readings were logged. That's usually the fairer comparison. An alternator puts
  out less as the bank fills, so a before-and-after on amps alone can mislead if
  the bank was fuller in one period. With two date ranges, each range gets its
  own colour, so you can see whether the cloud of points moved.

You can ask for the readings Helmcentral already knows by their job. These are
house bank state of charge, current and voltage, each alternator's current, and
each engine's RPM, load and coolant temperature. Any other logged reading works by
name too. Mate needs you to say which dates to compare, because it doesn't guess
when a change was made.

Here is how the chart handles the details:

- **Changing the dates:** Change the dates on the chart itself, and it redraws
  without asking Mate again.
- **Units:** Figures are in your units, using the unit the boat reports for each
  reading. A reading whose unit the boat doesn't report is drawn as logged and
  labelled "unit not reported".
- **Nothing logged:** A reading with nothing logged in a range is named under
  the chart rather than drawn as a flat line.
- **Two devices, one reading:** When two devices report the same reading, Mate
  asks which one you mean rather than averaging them.

Charts need the boat's history log (InfluxDB).

### Directions and the wind rose

A direction isn't a quantity you can average like a speed. A wind that swings
between 350° and 10° has been blowing from the north, but a plain average of
the two numbers says south. So Mate treats directions differently from other
readings:

- **Degrees, true or magnetic:** A direction is shown in degrees, marked °T for
  true or °M for magnetic, in a panel of its own.
- **Dots, not a line:** Each stretch of time is a dot on a scale that runs from
  north round to north. A swing through north shows as a jump between dots
  instead of a line sweeping across the chart.
- **Averaged as a direction:** Mate works out the average with the arithmetic
  for directions. For wind, a stronger wind counts for more than a light one in
  the same hour. The note under the chart says which method was used.
- **No average when there isn't one:** Sometimes the wind blew from opposite
  ways in equal measure, or was too light to have a direction. Then Mate says
  there is no average direction and leaves that stretch blank. It doesn't invent
  one.

Ask "how has the wind been trending this week?" and Mate draws true wind speed
and true wind direction in two panels. Ask "where has the wind been coming
from?" and it draws a wind rose instead:

> Show me where the wind has come from since Sunday.

The wind rose is a card under the answer. Each petal points to a compass
direction the wind blew from, and its length is how much of the time it blew
from there. The petal is split into bands by wind speed, from light to strong,
in your units.

- **Calm:** Wind under 1 knot has no direction worth drawing, so it's counted
  apart and shown as a percentage under the rose.
- **Variable:** Wind with no steady direction in a stretch of time is also
  counted apart.
- **Averages, gusts and the strongest wind:** The card gives the mean speed and
  the strongest gust with its time. Each petal is built from averages over short
  stretches of time, and the card says how long they are.
- **Your choice of detail:** Ask for 8, 16 or 32 compass sectors, or for your
  own speed bands, such as "bands every 10 knots".

The wind rose uses the true wind the boat logs. If the direction or speed was
never logged in the dates you give, Mate says so. If two devices logged it, Mate
asks which one you mean. The card is a snapshot of that answer, so changing the
dates means asking Mate again.

## Watching a reading

A watch follows up to six live readings once a second for between one and
thirty minutes. While it runs, a line above the composer says what Mate is
watching and when it ends. For example: "Watching Port engine load and Starboard
engine load · ends 14:35", with a **Stop** button. The end time is on the boat's
clock, the same time Mate gives you. You can keep asking other questions in the
same conversation meanwhile, or leave the page altogether.

When the time is up, a "Watch finished" line appears in the conversation and
Mate explains what it saw:

- **The range:** The range of each reading, and how steady it was.
- **Spikes and dips:** Any spikes or dips, with the time and size of each.
- **Gaps:** Any stretch where a reading dropped out or stopped updating.
- **A second-by-second comparison:** If you watch two readings of the same kind,
  Mate also compares them second by second. That's how you catch one engine
  working harder than the other at the same throttle. Two readings measured in
  different units, such as a bank's voltage and its current, aren't compared
  that way, and Mate says so.

If the chat is open, you see the answer arrive. If it isn't, the answer is there
next time you open the conversation. The answer costs the same as any other
reply and shows in its footer.

Limits:

- Every reading has to be live when the watch starts. If one isn't reporting,
  isn't a number, or hasn't updated in the last ten seconds, Mate says which one
  and doesn't start.
- A reading that drops out partway through is reported as a gap with its times.
  Mate doesn't fill it in or guess what it was doing.
- If two instruments send the same reading, such as two depth sounders, Mate
  says so. A jump may be one instrument disagreeing with the other, rather than
  the reading changing. A watch can't follow just one of them.
- You can have one watch per conversation, and three at once across
  Helmcentral.
- Mate doesn't interrupt you during a watch. If something needs your attention
  right away, that's what alarms are for.
- Restarting Helmcentral cancels any watch in progress, and nothing is reported
  for it. So does **Stop**, and so does deleting the conversation.
- If Mate has been switched off by the time a watch ends, nothing is sent and no
  answer appears.

## Routines

A routine is a check Mate makes on a schedule without being asked each time. Say
"check the boat every hour" or "give me a morning summary at 06:00 while we're
at anchor". Mate looks at the routines you already have first. Then it shows a
card with the new one: its name, how often it runs, and what it should look at.
Nothing is set until you tap **Apply**.

Each routine has its own conversation, named after it, and a small **Routine**
label in the conversation list. Every run adds one line there, "Routine: Morning
summary", followed by Mate's reply. Here is what a reply looks like:

- **Nothing to report:** When nothing needs attention, the first line says
  exactly "Nothing to report."
- **Something needs a look:** Otherwise the first line says what needs attention,
  followed by up to three short findings with their figures. When Mate doesn't
  know the cause, it asks a question and doesn't guess one.
- **Alarms:** A routine doesn't stand in for an alarm. If an alarm is active, the
  reply says so, and the alarm does its own job.

Routines are paused until you allow them. Turn on **Let routines run on their
own** under Routines in Settings → Mate. Runs happen with nobody watching, and
they're paid for from your OpenRouter key. So there's a **Daily limit (US
dollars)** beside the switch. It starts at $1.00 and can be set between $0.05
and $20.
Once routines have spent that much in the last 24 hours, the next runs are
skipped. Settings shows what routines have spent in that time. A run that fails
partway still counts for what it cost. When a run was cut off after it had
started, Helmcentral asks OpenRouter what it was charged, which can take up to a
minute. Helmcentral pauses routines for 24 hours only if it can't find out. A
dropped connection before the request got through costs nothing and pauses
nothing. Settings says until when a pause lasts.

Here is when a routine runs:

- **Every hour:** It runs when an hour has passed since its last run.
- **Daily:** It runs once a day after the time you gave, on the boat's clock.
  The boat's clock comes from its position, so a daily routine can't run without
  one. If the position has been missing for more than a day, the routine's row
  says so.
- **Once a day:** A daily routine runs at most once a day. If you move its time
  later after it has already run, it next runs at the new time tomorrow.
- **Straight away:** An hourly routine runs on the first check after you apply
  it, and then every hour.
- **After the box was off:** A routine that was due runs once. Missed runs aren't
  made up.
- **One at a time:** Routines run one after another. A routine waits for any
  question you're asking in its conversation, and skips if that takes more than
  two minutes.

A run that doesn't happen says why on the routine's row in Settings → Mate. For
example "Skipped: routines spent $1.00 of the $1.00 daily limit", "Skipped: no boat
position, so the boat's time is not known" or "Skipped: Mate is not ready". The
row also shows the first line of the last reply and when it ran.

If Helmcentral can't save the record of a run, it stops all routines until you
restart it. The routines panel says "Routines stopped" and why. If the routine
settings can't be read, such as a daily limit that isn't a number between $0.05
and $20, the panel says "Routines are not running" and why.

In that same place each routine has a switch to pause it and a **Delete**
button, so you can manage routines without asking Mate. To change what a routine
looks at or when it runs, ask Mate. You can have up to eight. Deleting a routine
keeps its conversation, and you can delete that afterwards like any other. While a
routine exists, Helmcentral won't delete its conversation. It tells you to delete
the routine in Settings → Mate first.

Limits:

- A routine reads the boat and never changes anything. It can't propose changes
  or start a watch, because no one is there to tap **Apply**.
- Findings stay in the app for now. Nothing is sent to your phone, so a routine
  is something you look in on, and alarms are still what wake you.
- Each run reads only the last few runs in its conversation, so a long-running
  routine doesn't get more expensive as the days go by.

## Documents

Mate can read from the boat's own document library: manuals, receipts,
invoices, service logs, passage notes, and photos of a part or a data plate.
There are two ways it draws on that:

- **Attach a file:** You attach a file directly to a question, with the
  paperclip in the composer or by dropping a file onto it.
- **Let Mate search:** Mate searches the whole library on its own, the way it
  looks up a forecast or a tide station, with no attachment needed.

See
[Documents](documents.md#attaching-a-document-to-mate-and-mate-searching-on-its-own)
for how much of a document Mate reads and when.

When an answer draws on something specific it found this way, it cites the
source as a small icon next to the text instead of describing it in a sentence.
Tap or click the icon to open that manual, receipt or note directly, and hover
or press it to see its title. A citation to something that has since been
removed from the library shows as a faded, broken icon rather than vanishing.

That one search covers everything in the library at once: uploaded manuals,
your own captured notes, receipts, and photos of a data plate. There's no
separate search that only looks inside your authored manuals, even though a
boat's own [operations manual](notes-and-the-manual.md) lives in that same
library.

A manual-only search would get the obvious question wrong. Ask "what does the
manual say about the impeller", and on most boats that means the engine's own
PDF, not the operations manual you wrote by hand. A search scoped to your own
manual would answer confidently from the wrong book. So Mate is told which
manuals exist and roughly what's in each (see below). It uses the same
library-wide search either way, narrowed to the right folder when a question is
obviously about one manual.

See [Documents](documents.md) for what gets indexed, what it costs, file types
and limits, and how folders and tags work.

## Filling in forms

Insurers and marinas send the same PDF again and again: owner, boat, policy
number, which marina, and who looks after the boat when you're away. Attach the
blank PDF to a message in Mate and ask it to fill it in. Mate reads the form and
takes what it needs from what Helmcentral already holds about the boat and its
owner. It asks you in a single message for anything that's missing. Then it
fills the form in and shows it under its reply as a card.

What Mate fills from is on **Settings, Vessel**. That page holds the owner's
name, phone and email, the insurer and policy number, the home marina and
berth, and the storm delegate (the person who prepares the boat when you're
away). It also holds the boat's length and beam, next to the hull details
already there. The boat's name and its
length and draft come from the live instruments when they're connected.

Nothing is pre-filled, and Mate never makes up a value to fill a gap. When you
give it answers in the chat that belong in that record, it offers to save them,
as a card with **Apply**, so the next form needs no questions.

Mate works with two kinds of PDF:

- **A fillable form:** It has named fields, and Mate sets them.
- **A flat form:** It has no fields. It's a page with questions, blanks and
  boxes drawn on it, and Mate writes the answer after the question and draws a
  tick inside a box. It can tell which box belongs to which marina or option by
  the words beside it.

Both come back as a new PDF, and the blank you attached is left alone.

The card under Mate's reply is where you check the result. Here is what its
buttons do:

- **Open the form:** It shows the PDF.
- **Download:** It saves the PDF to your device.
- **Save to Documents:** It files the PDF in the library under a title and
  folder you can change before you save. Mate suggests a folder such as
  Insurance/2026, and makes it if it isn't there yet. Saving twice gives you the
  same document, not two.
- **Dismiss:** It throws the filled form away.

Until you save, the filled form isn't in Documents.

Limits:

- **Signatures stay yours.** Mate never fills a signature line or a signature
  field. It leaves the date alone too, unless you tell it to put in today's.
- **Plain text only.** On a flat form the answers are written in one plain font,
  in Latin letters. A name with characters it can't write is refused, and Mate
  tells you which.
- **It needs text to read.** A form that's a scan or a photograph has no text
  for Mate to find the blanks by, so it can't fill it in. A flat form whose tick
  boxes it can't find is read for its text only, and Mate asks you what to tick
  instead of guessing.
- **A blank with no words next to it** (a row of small boxes for single letters,
  a cell under a heading) can't be named. Mate fills what it can and tells you
  what is left for you to write in by hand.
- **Check it before you send it.** Mate puts in only what you or the record gave
  it, but you're the one who signs.

Steps are in [Fill in a form with Mate](../how-to/fill-in-a-form-with-mate.md).

## Talking to Mate

Voice is push-to-talk. Tap the microphone in the header, or press `Alt+M` from
anywhere in the app, and speak. The transcript lands in the composer as you talk
and sends the moment you stop.

The composer has its own, separate microphone with a different job. It dictates
into the question you're already typing and doesn't send on its own. That way
you can add a thought by voice without cutting off what you'd typed.

A voice question can also be answered aloud, on top of the full written answer
and never instead of it. Only a question you spoke is answered that way. One
sent from Help, a Settings page or a text selection gets an answer written for
reading.

There's also "Listen for Hey Mate", an experimental always-on mode. Say "Hey
Mate" followed by your question, or say "Hey Mate" alone and Mate waits about
eight seconds for the question to follow. It's off by default, and it has real
costs:

- **The microphone stays open:** It keeps the microphone open the whole time the
  app is on screen, and it uses more battery than push-to-talk.
- **Audio goes to Google:** On Chrome the audio stream goes to Google
  continuously, rather than only when you ask something.
- **Mishearing:** It mishears ordinary conversation as "Hey Mate" from time to
  time, which comes with always listening.
- **Hidden tabs:** It pauses automatically when the browser tab is hidden.

Voice needs the app opened over **https**, with microphone permission granted,
in **Safari** or **Chrome** (Firefox has no speech recognition to use). See
[Talk to Mate](../how-to/talk-to-mate.md) for how to turn each of these on,
reach that address, and what to do when the microphone button won't light up.

## What leaves the boat

Every question sends three things to OpenRouter, and from there to whichever
model you've chosen: your position, the question text itself, and whatever
forecast or tide data Mate decided to look up. This only happens when you ask something and Mate
is switched on. The one exception is a watch you asked for. When it ends, its
summary of the readings goes to the model so Mate can explain it, and only if
Mate is still switched on at that moment.

A document you attach to a question, or one Mate reads through its own library
search, is sent the same way, as text, since it was already read once when it
was indexed. Separately, uploading or reindexing a document while Mate is
switched on sends that document's own content for OCR and summarising. That
happens whether or not you go on to ask a question about it. See [Documents](documents.md) for
what that costs and what leaves the boat at that point, not just when you ask
Mate about one.

With Web search switched on, a search also sends Mate's search words to
OpenRouter and the search service behind it. These might be a place name, a
part number or a question about a rule. Nothing is searched unless Mate decides it needs to, and
nothing at all while the switch is off.

A voice question additionally sends your speech to whichever engine your
browser uses to turn it into text. That's Apple's dictation for Safari
(on-device on recent hardware), or Google's speech service for Chrome. That happens before the
question ever reaches Mate, and it happens for every voice question,
push-to-talk included, not only "Hey Mate".

You bring your own OpenRouter account and your own key. You choose the model.
Different model providers have different data-retention and training policies,
so check the one you pick if that matters to you.

Settings offers two routing modes:

1. A fixed model id, chosen from a tool-capable model list fetched from
	OpenRouter.
2. OpenRouter Auto (`openrouter/auto`), with optional routing constraints:
	allowed model patterns, excluded model ids, and a cost tier cap (`low`,
	`medium`, `high`, `xhigh`, `max`).

Auto routing only applies when the model is set to OpenRouter Auto. For any
other model id, those routing constraints are ignored.

Documents are read and labelled by a separate model, set under Settings → Mate →
Document indexing. You pick it the same way as the Mate model: from a list of
recent choices, or from the catalogue, which here shows only models that can
read images. It runs unattended over the whole library, so pick a cheap one. It
doesn't have to be the model Mate answers with. Until you choose one, it uses
the built-in default.

While Mate is switched off, Settings → Mate shows only the Enable Mate switch
and what Mate sends to OpenRouter. The model, indexing, key, web search, notes
and voice settings appear once it's on.

## Searching the web

Mate answers from your boat first: live instruments, the forecast and tides, the
help and your documents. Some questions need information from outside the boat,
such as a harbour's opening hours, a recent notice, what a fault code means or a
replacement for a part. Switch on **Web search** in Settings → Mate, and Mate
can look those up on the web through OpenRouter.

It's off by default. Each search costs a small extra amount on your OpenRouter
account on top of the reply itself, so Mate only searches when your documents
and instruments can't answer. When it does, the status line says it's searching
the web, and the sources it used appear in the answer as links that open in a
new tab. Open them to check what a page actually says before you rely on it,
especially for safety, regulations or anything about the boat.

Under the switch, **Search model** picks which OpenRouter model carries the
search. Results come from the same search service whichever model you choose, so
a cheap one is fine, and the default is a cheap one. Web search needs a model
chosen, so Settings won't save with the switch on and the model blank.

Anyone can write a web page, so Mate treats what it reads there as information
to weigh, never as instructions. It won't change your maintenance list or any
other record because a web page said to. If a search fails or finds nothing,
Mate tells you so and answers without it.

## Standing notes

Settings → Mate has a notes field that is sent with every question, word for
word. Put anything in it that you know about your own cruising ground and a
general-purpose model wouldn't. That might be which anchorage suits which tide,
which bay is a lee shore in a southerly, or when the fish bite. For example:

> Queenfish fish Hill Inlet on a rising tide.
> Snorkel Blue Pearl Bay in the last 1-2h of flood up to high slack.

Mate is told to prefer these notes over its own general knowledge about a place.
There's no separate rules screen or anchorage database to maintain. If you know
a piece of local knowledge well enough to write it as a sentence, it goes here.

That field is one block of text for the whole boat, sent whole every time. For a
single fact rather than a paragraph, pin a note instead (see [Notes and the
manual](notes-and-the-manual.md)). Open it in Documents and use **Pin for
Mate**. A pinned note's title and full text go with every question too, and get
the same "prefer this over general knowledge" treatment. You don't have to edit
a shared paragraph every time you add or retire a fact.

Pin sparingly. Only a handful of notes and a few thousand characters between
them reach Mate. When you go over that, the prompt says outright how many notes
it left out, rather than cutting one off mid-sentence.

It works the other way too. Sometimes an answer is worth keeping, such as a procedure Mate has just walked
you through or a figure it worked out. **Save as note** under that answer keeps
it without retyping. Mate doesn't write the note. Your
tap creates it, it lands unfiled like any other capture, and you file or edit it
afterwards. Mate never writes anything itself. The same goes for the
maintenance schedule, the inventory, your alarms and the boat's details: Mate proposes, and
your tap on **Apply** makes the change.

Mate is also always told which manuals exist. For each one, it's told the names
of its top-level sections ("Operations Manual (Before Leaving, Getting
Underway), Crew Training (Watchkeeping)"). That's how it knows which book to
point you at by name.
That's an index, not the manual's content. The reading itself goes through the
same document search as everything else in the library (see
[Documents](#documents) above for why there's no manual-specific search).

## Remembering earlier conversations

A new conversation starts without the old one in front of it, so Mate looks back
for itself. It searches your past conversations for that equipment or topic and
reads what was said, in two situations:

- **Before advising on your equipment:** Mate does this before it advises on a
  specific piece of your boat's equipment.
- **When you mention earlier work:** It also does this whenever you refer to
  earlier work ("yesterday", "we already did that").

What you established then, especially from your own photos and observations,
counts as a fact about your boat and comes ahead of general knowledge. Suppose
you showed Mate that a manifold has no sampling valve, or agreed that draining
the other engine's fuel filter bowl was a bad idea. Mate won't suggest either
again, and it tells you when it's going on something from an earlier
conversation.

Mate also doesn't offer options that depend on a fitting, valve or port it
hasn't confirmed is aboard. It asks you, or checks your documents, equipment
records and past conversations first.

Mate can only recall conversations that are still in the list. If you delete a
conversation, Mate no longer knows what was in it.

## Keeping what a conversation worked out

Sometimes a conversation settles something about your boat, such as a fitting
you showed Mate isn't there, a part number or a procedure that worked. You can
save that as a note. Press **Summarise to note** beside the microphone in the message
box. Mate writes a short summary and shows it to you for review. Nothing is
saved until you press **Save**.

The summary has up to three parts, and leaves out any that is empty:

- **What we established**: facts about your boat, above all from your own photos
  and observations.
- **Ruled out**: options that were considered and why they were dropped.
- **Procedure that worked**: the numbered steps you actually followed.

It carries only what the conversation settled about your boat, not general
advice Mate gave that nobody confirmed. Where you corrected Mate, your
correction is what is written down. The note ends with a link back to the
conversation. Mate also suggests the equipment the note is about. Those items
are ticked, and the note is linked to each one you leave ticked, so it turns up
on that equipment's record as well as in document search.

A conversation keeps one note. Once it has one, the button reads **Update
note**. Saving then replaces that note's title and text with a fresh summary of
the whole conversation, instead of making a second note.

- **A deleted note:** If you deleted the note, the next save makes a new one.
- **Nothing to keep:** If the conversation has nothing worth keeping yet, Mate
  says so and writes nothing.
- **Cost:** The summary uses Mate's chat model, so each press costs one answer's
  worth of tokens.

See
[Keep what a Mate conversation worked out](../how-to/keep-what-a-mate-conversation-worked-out.md).

## Conversations and cost

Questions and answers are kept as conversations you can return to, list or
delete, like a chat app. Starting a new conversation clears the context Mate
carries forward. So a fresh question about a different part of the coast
doesn't get answered in light of yesterday's passage. The sheet and the full panel share
the same conversations, so a question asked from the sheet is still there if you
later open the full Mate panel.

Every reply's footer leads with what OpenRouter charged for that exact reply,
for example `$0.056`. A question that also had Mate read the help shows a
tool-round count alongside it (`$0.056 · 1 tool round`), which is one extra
round trip to the model before the answer comes back. So the running cost of
asking questions is on the screen every time you ask one. Hover over the footer
(or long-press on a touchscreen) to see which model answered and how many
tokens the question and answer used between them.

Sometimes part of what Mate sent to the model is served from the provider's
cache, instead of being read afresh. When that happens, the footer adds the
count, for example `$0.012 · 9,820 cached`. Those tokens are billed at a
fraction of the usual price, so a high figure on a follow-up question is a good
sign. Nothing is shown when none were cached. Follow-up questions in one
conversation stay with the same provider, which is what lets the cache be
reused.

## Rating an answer

Under each answer Mate gives you in the chat there's a thumbs-up and a
thumbs-down. Tap one when an answer was right or wrong. A short note field opens
beneath it, asking what was good or what was wrong, and Save note keeps what you
typed. Tap the thumb you chose again to take the rating back, or the other thumb
to change your mind.

Both thumbs matter. A thumbs-down says what went wrong, and a thumbs-up says
what Mate got right and must keep getting right.

- **Rating in the chat:** You rate an answer in the chat as Mate gives it, and
  the rating stays with the answer. Reopen the conversation later, and a rated
  answer still shows its thumb and note, which you can change or take back.
- **Older answers:** Older answers you never rated aren't offered for rating,
  and one more than seven days old can't be newly rated. That way a rating
  always reflects how the answer looked when you asked.

Alongside each answer, Helmcentral keeps three things. It keeps which lookups
Mate made to write it, what each returned (very large results are cut short and
marked as such), and which version of Mate's standing instructions it was
working from. The lookups of
answers you rated are kept. For answers you never rated, they're cleared after
seven days.

Rating an answer doesn't change how Mate behaves today. The ratings are kept on
the boat so that a later improvement to Mate can be checked against answers you
judged good and poor. Nothing changes in how Mate works without you approving
it.

## What it is not

It's not a navigational authority. It reasons from the forecast and tide
providers you've configured and from what it can find about a place by name. It
states its assumptions about shelter and exposure, for example "I am assuming
Blue Pearl Bay is open to the north-west, correct me if not." Check that
assumption against your own chart before you act on it. Weather models and tide
predictions are both approximations before a chat model summarises them.

It needs internet. Without a connection, with no key configured, or switched
off, it says so plainly rather than pretending to work.

It can look things up, explain how Helmcentral works, watch readings for a few
minutes and report back. It can also propose changes to the maintenance
schedule, the inventory, your alarms and the boat's details, which you apply or dismiss. It
can't start anything on the boat, change any other setting or steer. Nothing it proposes takes effect without your tap, whether you typed the
question or said it.

A proposal card is applied by tapping it, never by speaking.

## When it can't run, or can't listen

If Mate can't answer at all, it tells you exactly why instead of failing
silently. The reason is one of these: switched off, no OpenRouter key
configured, or no model chosen. Each one names Settings → Mate as the place to
fix it.

If the model you've chosen doesn't support tool calling, a question comes back
with an upstream error saying no endpoint supports tool use. See [Set up
Mate](../how-to/set-up-the-assistant.md) for what to do about that.

Some models answer a question by writing out a tool call as ordinary text,
rather than making one. Mate refuses that reply instead of showing it to you,
and the error names the model. Pick a different one in Settings → Mate.

A lookup that keeps failing is abandoned rather than retried forever, so a dead
server doesn't use up every round Mate is allowed. See
[What Mate can look up](../reference/mate-tools.md) for when that kicks in and
what Mate says when it does.

If Mate can answer but voice specifically won't work, the header microphone says
why. If the browser has no speech recognition at all, the microphone isn't
shown. Press `Alt+M` anyway, and Mate names the reason in a toast. The
composer's own dictation mic follows the same rule. See
[Talk to Mate](../how-to/talk-to-mate.md) for what each message means and how to
fix it.
