# Alarms

Alarms watch the boat's own instrument readings around the clock. A rising
engine temperature, a failing bilge pump or a falling barometer gets your
attention, whether or not anyone is on watch to notice it.

A rule can be built on any live reading published anywhere on the boat's
instrument network, including gear from other manufacturers that's already wired
into it. Helmcentral watches for newly published readings rather than working
from a fixed list, so a rule for something new needs no software update first.

Every alarm carries one of five severities: normal, alert, warn, alarm and
emergency. An alarm raised elsewhere on the instrument network, by a battery
monitor, an engine gateway or another instrument, arrives here with the severity
it already had. When one of your own rules fires, it goes back onto that same
network in the same terms. That way an alarm buzzer or chartplotter that's
already wired to it can react too.

## Three decisions worth spelling out

**Dwell and hysteresis are mandatory.** A rule has to hold for its dwell before
it fires, and the value has to return past a deadband before it clears. That
stops repeated alarms when a reading hovers around a threshold.

The deadband is also why acknowledging doesn't clear an alarm. Acknowledge says
you've seen it. It stops the sound and the visual alert, and the alarm stays on
the board until the value has recovered. Every alarm card shows its clearing
condition in your selected units: "Falling 1.1 mb/hr. Clears once the fall eases
to 0.7 mb/hr."

An alarm raised by something else on the network, such as a radar guard zone or
an engine gateway, has no rule behind it. Here is how those cards behave:

- **Clearing:** The alarm clears when the instrument that raised it lets go. The
  card gives no clearing condition it can't vouch for.
- **Title:** The card names the event in plain words, "Radar Guard Zone 1",
  rather than the instrument's own address. With two radars live, the title also
  carries the name you gave the radar, so you can tell which one raised it.
- **Guard zone figures:** While the radar is still tracking the target, a guard
  zone card also gives its bearing, range and closest approach. For example:
  "Target in guard zone 1 · 042°T · 1.4 NM · CPA 0.3 NM". If the radar has lost the target, or
  its feed is down, the card only says that a target is in the zone.
- **Getting worse after you acknowledge:** If an alarm like this gets worse after
  you've acknowledged it, it comes back as active and needs a fresh
  acknowledgement. The card shows when it was acknowledged, even if you did it at
  the chartplotter.

An alarm raised elsewhere on the network can also go stale the other way. The
source clears it or forgets it, and Helmcentral doesn't notice, because it only
hears about what changes. To catch that, Helmcentral re-checks every alarm it
didn't raise itself every 30 seconds. An alarm the source has forgotten then
leaves the list within half a minute. If you acknowledge or silence one inside
that window, you might get "Alarm not found" instead of success. The card then
disappears on its own.

**Missing readings do not satisfy thresholds.** A freshly booted boat doesn't
fire every rule at once while the instrument network comes up. The same holds in
reverse. A live alarm doesn't clear when its reading goes stale, so a sensor
outage can't clear an active alarm.

**Failed deliveries are queued for retry.** Retries back off from 30 seconds to
30 minutes, for up to 24 hours. After that the delivery is dropped, so an old
alarm isn't presented as current. Heartbeats are never queued, because a late
heartbeat would wrongly suggest the system is still running.

## Built in

Anchor drag and the watchdog for a lost instrument connection are built in. They
use the same logging, acknowledgement and delivery as your own rules.

The watchdog raises an alarm if the connection to the boat's instrument network
fails, because frozen readings can look like stable conditions. A periodic
heartbeat sent off the boat lets an external monitor notice when the whole
system stops responding.

## Getting told

There are five transports. None needs a paid subscription.

| Transport | Notes |
| --- | --- |
| **ntfy** | Self-hostable, or the free public server. No account needed. |
| **SMTP** | Your own mail server or provider. |
| **Webhook** | Anything that accepts an HTTP POST. |
| **Publish to SignalK** | Broadcasts onto the boat's own instrument network, so a buzzer or an MFD already wired to it can react. Needs no internet at all. |
| **Web push** | Alarms on your phone's lock screen with no app to install. Needs Helmcentral served over https. See [Web push over Tailscale](../reference/configuration.md#web-push-over-tailscale). |

A **Send Test** button probes every enabled transport at once, so you can check
delivery before you rely on it. See [Set up alarm
notifications](../how-to/set-up-alarm-notifications.md) for entering the details
each transport needs.

Changing the ntfy server or the SMTP host clears the token or password stored
for that transport. That's deliberate. A credential belongs to the destination
it was entered for, and it doesn't follow you to a new one. After you change
either address, paste the token or password back in, because the transport stays
quiet until you do. Changing the SMTP username clears the password too, since
the two are sent together.

Helmcentral picks up alarms from other producers on the instrument network, so
it has to recognise its own output coming back. It does that by name. Anything
under `helmcentral.` is its own, and so is any of your enabled rules, so neither
shows up as a duplicate external alarm. Sometimes an alarm sits under its own
name with no rule here holding it, left behind by a restart or by another
Helmcentral install on the same instrument network. Helmcentral clears that
automatically. For that reason, a test copy of Helmcentral should leave
**Publish to SignalK** switched off if it shares an instrument network with the
boat's own install.

Restarting the boat's instrument network hub is the surest way for an alarm from
another device to go stale. Whatever the hub was holding is gone when it comes
back, whatever the underlying condition is doing, and the reconnection gives no
sign that anything changed. The 30-second re-check described above catches it,
so the alarm doesn't sit on your board until the same reading happens to change
again.

Arrival circle and waypoint perpendicular passed alarms, raised from the
plotter's active route, go stale a different way. Stopping or cancelling the
route on the plotter sends no signal that the alarm should clear, so it could
stay lit long after the route is gone. Helmcentral notices when the plotter stops
sending route updates, and it drops these two alarms from the board within a few
minutes.

The plotter's two route alarms are worded for the watch:

- **Waypoint Passed:** "The boat has passed the active waypoint."
- **Arriving at Waypoint:** "The boat is inside the active waypoint's arrival
  circle."

The plotter doesn't send the waypoint's name or a distance with them, so the
card doesn't invent one.

### A chart with the alarm

When an alarm is about a reading Helmcentral keeps, the notification carries a
chart of that reading, so you can see on your phone how it got there. Here is
what that covers:

- Which alarms: Your own alarm rules on a numeric reading, including the house
  bank rule, and the tank level alarms. A chart shows the last 6 hours in your
  units, with the alarm's limit as a dashed line. A tank chart has two dashed
  lines, one for the warning level and one for the alarm level.
- Which alarms don't: Anchor dragging, the lost instrument connection, collision
  risk, and alarms raised by other devices on the network. Those aren't a reading
  that Helmcentral records, so there is nothing to draw. A "no data for a while"
  rule gets no chart either, because a silent reading has no curve.
- When it's raised or gets worse: A chart goes with the first message. It also
  goes with the message when a live alarm gets worse, such as a tank going from
  warning to alarm. There's no chart when an alarm clears or eases back. There's
  none on the reminder that nobody has acknowledged it, either.
- Where it appears: In an email the chart sits under the message. On ntfy it's
  an attachment. A webhook receives it as a picture and as the numbers behind it.
  Web push and Publish to SignalK stay as they were, and the alarm card in the
  app is unchanged.

The chart never holds an alarm back. Helmcentral gives the history 3 seconds,
counted from when the alarm was raised. If it hasn't answered by then, or
InfluxDB isn't set up and there's no history to read, the alarm goes out on time
without a chart. It ends with a line saying why, such as "Chart left out: the
history did not answer in time". Helmcentral never fills in a line it doesn't
have. If a notification has to be retried, the retry carries the same
chart as the first try.

## Stopping an alarm you can't act on

Some alarms raised by other devices stay true for a whole leg and ask nothing of
you, such as the plotter's Waypoint Passed. Choose **Stop showing** on the
alarm's card and confirm. Here is what happens then:

- **On the boat:** That kind of alarm leaves the alarm board and the banner.
- **Notifications:** It stops reaching your phone and the other notification
  transports.
- **Mate and history:** Mate no longer hears of it, and it stops being added to
  the alarm history.
- **Which device:** It applies to that alarm whichever device raises it.
- **On the instrument network:** It doesn't acknowledge anything there, so the
  plotter keeps showing its own.

Safety alarms never offer **Stop showing**. They are:

- man overboard, fire, sinking, flooding, collision, grounding, listing, adrift,
  piracy and abandon ship
- anchor alarms
- collision warnings for AIS targets
- received distress calls
- radar guard zones
- anything the instrument network marks as an emergency

If an alarm you've stopped showing comes back as an emergency, it shows.

To see what you've stopped showing, or to bring one back, open **Settings →
Alarms** and look under **Ignored alarms**. If you restore an alarm while its
condition is still true, it appears again as a fresh alarm and notifies you.

Mate can tell you which alarms are on the list when you ask why you weren't told
about one. It can't add to the list or take from it. The steps are in [Stop
showing a network alarm](../how-to/stop-showing-a-network-alarm.md).

An AIS distress alarm is a SART, man-overboard beacon or EPIRB heard over AIS.
It's titled "AIS Distress" and carries the position sentence the receiver gave,
such as how far away it is and which way.

- **Clearing:** The receiver raises it once and never withdraws it, so
  Helmcentral clears it about ten minutes after the beacon was last heard.
- **When the feed is down:** Helmcentral never clears it while the feed from the
  instrument network is down. It also waits until that feed has been back for
  ten minutes. Silence during a loss of data says nothing about the beacon.
- **After the feed returns:** A cleared alarm doesn't come back when the feed
  reconnects and replays the beacon's last position. It does come back if the
  beacon is genuinely heard again.
- **Acknowledging:** Helmcentral never acknowledges an emergency for you.

## Anomaly detection

A separate set of checks covers frozen and impossible sensor readings, charging
into a full house bank, and twin engines pulling apart from each other. It has
its own page: [Anomaly detection](anomaly-detection.md).

## AIS collision alarms

Collision warnings for AIS targets come from a separate AIS Target Prioritizer
plugin on the boat's own instrument-network server, not from a Helmcentral rule.
It works out CPA and TCPA for every target from the AIS data already on the
network. When a target crosses the thresholds of the profile in force, it raises
a warning for that target. It keeps four profiles (anchored, harbor, coastal and
offshore), each with its own warning and alarm tiers. Helmcentral picks the
profile from the vessel's navigation state (anchored, moored, under way) and
never edits the numbers in it. It only switches when that state changes. If you pick
a different profile yourself, it stays in force until the state next changes,
even across a Helmcentral restart or update. Going from sailing to motoring
counts as a change, so starting the engine puts you back on coastal.

Helmcentral won't switch to a profile that can't raise an alarm, such as the
shipped anchored profile, where every range is zero. It leaves the profile
alone and shows a Collision Profile Not Changed warning instead. Set real
numbers in that profile, or choose a working one yourself in the plugin. The
warning clears by itself within about a minute of the plugin being on a
profile that can raise alarms, and it also clears the next time the navigation
state changes to one Helmcentral can switch to.

Those numbers are yours, and the shipped harbor profile needs attention before a
marina. It warns on any target closer than half a mile with a reported speed over
half a knot. Alongside a pontoon every neighbour is inside half a mile. GPS
jitter also puts a tied-up boat over half a knot every few minutes, so the
warning fires on boats doing nothing. Each collision alarm card links to the plugin's own
settings, which is where the thresholds live. Raise the warning tier's speed
floor to a couple of knots and a marina goes quiet. You still won't lose a boat
that's actually moving down the fairway.

The vessel in alarm is also marked in the Nearby Vessels tile. Its row gets a red
or amber highlight and a label naming the severity. It moves to the top of the
list, worst state first.

When both vessels are moving and the picture is clear enough to read, the card
adds a second line. It names the encounter and what the rules ask of you, for
example:

> Crossing, she is on our starboard bow (040° rel). We give way (Rule 15):
> alter to starboard, pass astern.

It names the rule and the role: overtaking, head-on or crossing, who gives way
and who stands on. For a stand-on power-driven vessel it adds a reminder not to
turn to port for a target on your own port side. The vessel type comes from
what each of you is doing right now, motoring or sailing. It doesn't come from
the AIS ship type, which says nothing about whether an engine is running.

The line is left off rather than guessed at when the other vessel's type isn't
transmitted, or when either of you is stopped, at anchor or already opening. A
wrong role on the card is worse than a missing one. This line is a read of the
rule in force, not a course to steer. It doesn't replace keeping a proper
lookout, or judging risk of collision by every means you have.

A radar-only contact doesn't get this line yet. For now it carries only the
restricted-visibility caution about which way not to turn.

## The rules list

Rules are grouped by what they watch: electrical, environment, propulsion,
tanks, radar and so on. The grouping is sorted alphabetically and worked out
from what each rule reads, so there's nothing to categorise by hand. A rule whose
alarm is live is marked as firing. Each one shows its threshold and the point it
clears at, in your own units, and deleting one asks first.

## Asking Mate about an alarm

Each alarm card has an **Ask Mate** button next to **Acknowledge**, and each row
in History has a small Mate button. It opens a new conversation with Mate that
already knows which alarm you mean, and asks why it went off. Mate looks at the
level the alarm fires at, the reading and how recent it is, how the reading
moved around the time it went off, and what else went off or cleared within
15 minutes either side, then charts it. Mate answers in plain words. If the
alarm keeps going off it says so, and if the cause points to something you can
check, it ends with what to check. The card itself isn't changed. See
[Mate](assistant.md) for what it can and can't tell you.

## Having Mate review an alarm when it goes off

If you'd rather not open Mate and ask every time, you can have Mate look at an
alarm for you the moment it goes off. It's a switch on each alarm rule, and it's
off until you turn it on. Turning it on is your say-so for that alarm, so Mate
never reviews one you haven't chosen.

To turn it on, open the Alarms drawer, tap the pencil on a rule, tick **Mate
reviews it** and tap **Save Rule**. While Mate is off, the switch is greyed
out, with a note saying Mate won't review the alarm. You can still see the
setting and turn it off. You can also ask Mate to turn it on for an alarm, and
it's a card you tap **Apply** on like any other change.

Here is what happens once it's on:

- When it starts: The review starts when the alarm goes off, after the alarm's
  own notification has gone out. If the same alarm goes off again within an
  hour of the last review, it isn't reviewed again.
- What Mate looks at: The same things as when you tap **Ask Mate**. That's the
  level, the reading and how it moved. It also covers what else went off nearby
  in time, and how often it has happened. Mate only reads. It can't change a rule, search
  the web or fill in a form.
- How long it looks: Nobody is waiting, so Mate makes at most 8 rounds of
  lookups, over at most 3 minutes, and then writes the review with what it has.
- What you get: A short review in a Mate conversation named after the alarm.
  The first sentence says what the reading is doing and whether it needs you
  now. If Mate thinks the rule itself is wrong, it says so in words, once, and
  leaves the change to you.
- On the card: The alarm card says **Mate reviewed this**, shows the first
  sentence and what the review cost, and links to the conversation. While Mate
  is working it says **Mate is reviewing this**.
- On your phone: A second message, "Mate on House Bank Low", goes to the same
  channels the alarm uses, with the first sentence. If you've set the **Address
  for links** in Settings, it includes a link that opens the conversation. It
  comes after the alarm and never delays it. If the alarm has cleared by the
  time Mate finishes, nothing is sent.

If Mate is turned off, a rule with the switch on gets no review, and nothing is
recorded. If a review fails, the card says so and says why. For example, Mate
isn't set up, it took longer than three minutes, or Helmcentral restarted. A
review cut short by a restart doesn't count towards the once-an-hour limit. Helmcentral doesn't try again and
doesn't put anything in its place. The alarm itself is never changed, and the
card keeps its own title and message.

Each review is billed by your model provider like any other Mate answer, and
there's no spending limit beyond the once-an-hour rule. Alarms that come from
the boat's network, tank level alarms, gauge bands and the checks that came with
Helmcentral can't be reviewed yet.

## Gauge bands are alarm rules

A coloured band on a gauge set to an alarm severity defines an alarm rule. Drag
engine temperature into the red on the dial and it raises an alarm through the
configured transports. You don't re-enter the threshold on a separate screen or
convert it to different units.

Rules derived this way appear in the alarms list alongside hand-written ones,
marked as coming from a gauge. You edit them on the gauge rather than in the
list. A tile can show a piece of equipment, and then the bands are the equipment
profile's. That gives you one alarm per engine and reading, however many tiles
show it, and changing the profile's limit changes the alarm. The alarm is named
for the equipment, such as "Port engine Oil Press". See [Set an alarm
threshold](../how-to/set-an-alarm-threshold.md) for the steps either way.

## Tank level alarms

Helmcentral watches every tank you've given an equipment item, so a fuel tank
running low never depends on a chartplotter noticing first. The tank alarms
share their levels with the Tanks tile and the Engine Cluster's fuel bars, so
the colour you see and the alarm you get always agree.

- **Two levels per tank:** A warn level raises a warn alarm and an alarm level
  raises an alarm. Fuel and fresh water use 20 and 10 percent, because a low
  tank is the bad one. Waste tanks use 80 and 90, because a full tank is the
  bad one.
- **Your own levels:** Change the defaults for each kind of tank under
  **Settings → Alarms → Tank levels**, or give one tank its own levels on its
  equipment page. See [Change a tank's alarm
  levels](../how-to/change-tank-alarm-levels.md).
- **Only tanks you've named:** A tank with no equipment item isn't listed on
  the Tanks tile and doesn't alarm.
- **No flapping:** The level has to stay past a level for 60 seconds before the
  alarm is raised. It clears once the level is 5 percentage points back, so a
  warn at 20 percent clears above 25, and a waste warn at 80 clears below 75.
  Those two figures are fixed.
- **A silent sender raises nothing:** If a tank's sender stops reporting for two
  minutes, Helmcentral doesn't raise a level alarm from the old reading. The
  Tanks tile goes stale instead, and the instrument health alarm covers the
  sender.
- **One alarm per tank:** A tank is a single alarm that moves from warn to alarm
  as the level falls, and back to warn as it recovers. It only clears once the
  tank is clear of the warn level. Acknowledging it acknowledges the tank. If
  it recovers from alarm to warn it stays acknowledged, and if it falls back to
  the alarm level it is raised again, unacknowledged.
- **Alarms off:** A tank can have its alarms switched off. The Tanks tile still
  shows its level and colours.

The card is named for the tank, for example "Fuel Port Fwd Low" or "Black Water
Stbd High". It shows the level and, when the tank reports its capacity, what
that is in litres: "19% · 228 L". The line underneath says where the level has
to get back to, such as "Clears above 25%". The alarms appear in the rules list
marked as coming from the tank levels, and you change them on the tank rather
than there.

## Values Helmcentral works out for itself

A rule isn't limited to a single instrument reading. Some useful numbers don't
exist as one reading. They have to be worked out from the history Helmcentral
already keeps: a rate of change, a relationship between two readings, a total
across every tank.

Helmcentral computes a set of these itself. They show up alongside every other
instrument reading when you choose what a rule or a gauge should watch, and they
carry their own units. When there isn't yet enough history, they report nothing
rather than a misleading zero. See [Alarm rules
reference](../reference/alarm-rules.md#derived-values) for the full list, what
each one needs before it starts reporting, and when it goes quiet again.

### Why a squash zone needs a signal of its own

A squash zone is a high and a low close enough together to accelerate the wind
between them. Pressure and wind direction can both hold steady while the wind
builds, so a barometer-only rule won't catch it.

*Surviving the Storm* blames squash zones for most of the heavy-weather trouble
yachts meet. It also singles out the western South Pacific around New Zealand
and Australia as getting more than its share. See [Forecast](forecast.md) for
where these thresholds come from.

## The heavy-weather rule set

Helmcentral ships two rules using thresholds from that book. They're created
once, on first run. See [Alarm rules
reference](../reference/alarm-rules.md#heavy-weather-rule-set) for the two rules
and their thresholds.

**They arrive switched off.** These are one crew's thresholds, published in 1999,
and they haven't been calibrated against your boat or your cruising ground. Turn
on the ones that suit where you sail, and adjust them once you've watched your
own barometer for a season.

In the tropics, for example, the barometer swings about 1.5 mb either way every
day on its own, so any departure from that rhythm is significant. Outside the
tropics the same rule can raise false alarms.

Once created, these rules can be edited, retuned or deleted like any other. A
rule you delete stays deleted. The set isn't re-created on the next restart.

### One thing the rules cannot know

A rate of fall means different things depending on which way you're going. A
system moving at 15 knots closes 190 miles a day on a boat running with it, and
528 on one heading into it, and the barometer moves accordingly. The same 1
mb/hr is a different situation in each case.

At anchor this doesn't apply. Underway, allow for your course relative to the
weather system when you read an alarm. The thresholds don't adjust for it.

## The Law of Storms barometer rules

R. J. Ellis, "Secret Law of Storms", worldstormcentral.co (Rules for storms and
gales page), turns the same three-hour tendency into a ladder. A 6 mb move
either way means strong wind, and a 10 mb move either way means gale. Two
further rules read the tendency against the barometer's own height rather than
its movement alone. Helmcentral ships seven rules from this ladder, created once
on first run. See [Alarm rules
reference](../reference/alarm-rules.md#the-law-of-storms-rule-set) for the full
ladder.

**Six of these seven arrive switched on.** Unlike the two rules above, this is
one ladder rather than three rules restating a single falling barometer. Its
windows match the tendency a marine forecast already quotes, and each rung's
severity matches the wind range the page gives for it.

**Storm signature ships switched off.** The page itself calls 3 mb the minimum
for this rule, and 4 mb only "a margin of comfort" above that. A 4 mb fall under
1009 mb is a routine afternoon on a temperate coast, not the storm the label
promises. Turn it on somewhere you judge it means something.

**If you installed Helmcentral before this ladder shipped,** your rules list
still carries Barometer falling, Barometer plummeting and Barometer down 3mb in
three hours. The ladder replaces all three, but nothing deletes a rule for you.
Delete them yourself, once, and they stay gone.

## The forecast warning rules

The forecast-warnings plugin (see [Forecast](forecast.md)) already knows which
official marine warnings are in force for the vessel's own zone. Helmcentral
polls it every ten minutes in the background, ranks the result, and ships four
rules bound to it. They're created once, on first run. See [Alarm rules
reference](../reference/alarm-rules.md#forecast-warning-rule-set) for the four
rules.

**These arrive switched on.** Unlike the heavy-weather set, there's nothing to
calibrate. The source is the met service's own call for your zone, not a
threshold read from a book. A gale warning issued while you're ashore reaches
every transport you have enabled, and it shows in the alarm banner on every
screen until you acknowledge it. The banner has its own Acknowledge button beside
View, so silencing the sound doesn't need a trip to the Active Alarms tile.

"Unavailable" means what it says. It raises in any of these cases:

- **No provider:** No forecast-warnings provider is installed.
- **No position:** The boat has had no position fix.
- **Provider down:** The provider has been down for half an hour.

It's there so a dead provider looks like a dead provider rather than a quiet sea.
If your boat has no provider and you don't want the standing alert, disable that
rule like any other.

The alarm card says which warning is in force and links straight to the bulletin,
in the banner and on the Active Alarms tile. The region and the days it covers
stay on the Forecast page.
