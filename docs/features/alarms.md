# Alarms

Alarms watch the boat's own instrument readings around the clock, so a
rising engine temperature, a failing bilge pump or a falling barometer gets
your attention whether or not anyone is on watch to notice it.

A rule can be built on any live reading published anywhere on the boat's
instrument network, including gear from other manufacturers already wired
into it. Helmcentral watches for newly published readings rather than
working from a fixed list, so a rule for something new needs no Helmcentral
release to support it first.

Every alarm carries one of five severities: normal, alert, warn, alarm and
emergency. An alarm raised elsewhere on the instrument network, by a battery
monitor, an engine gateway or another instrument, arrives here with the
severity it already had. A hit on one of your own rules is broadcast back
onto that same network in the same terms, so a separate alarm buzzer or a
chartplotter already wired to it can react too.

## Three decisions worth spelling out

**Dwell and hysteresis are mandatory.** A rule has to hold for its dwell before
it fires, and the value has to return past a deadband before it clears.
Both are required to limit repeated alarms when readings fluctuate around a
threshold.

The deadband is also why acknowledging does not clear an alarm. Acknowledge
says you have seen it; it stops the sound and the visual alert, and the alarm
stays on the board until the value has recovered. Every alarm card shows its
clearing condition in your selected units: "Falling 1.1 mb/hr. Clears once the
fall eases to 0.7 mb/hr." An alarm raised by something else on the network,
which has no rule behind it, says so and clears when its source clears it.

An alarm raised elsewhere on the network can also go stale the other way: the
source clears it, or forgets it, without Helmcentral noticing, since Helmcentral
only hears about what changes. Helmcentral re-checks every alarm it did not
raise itself every 30 seconds to catch that, so an alarm the source has
forgotten leaves the list within half a minute. Acknowledging or silencing
one inside that window can answer with "Alarm not found" rather than success,
and the card then disappears on its own rather than staying stuck.

**Missing readings do not satisfy thresholds.** This prevents a freshly booted
boat from firing every rule at once while the instrument network comes up. The
same rule applies in reverse: a live alarm does not clear when its reading goes
stale, so a sensor outage cannot clear an active alarm.

**Failed deliveries are queued for retry.** Retries use backoff from 30 seconds
to 30 minutes, for up to 24 hours. After that, deliveries are discarded to avoid
presenting old alarms as current. Heartbeats are never queued, since delayed
heartbeats could incorrectly indicate that the system is still running.

## Built in

Anchor drag and the watchdog for a lost instrument connection are built in and
use the same logging, acknowledgement and delivery as your own rules.

The watchdog raises an alarm if the connection to the boat's instrument
network fails, since frozen readings can otherwise look like stable
conditions. A periodic heartbeat sent off the boat also allows an external
monitor to detect when the whole system stops responding.

## Getting told

Five transports, none of which needs a paid subscription:

| Transport | Notes |
| --- | --- |
| **ntfy** | Self-hostable, or the free public server. No account needed. |
| **SMTP** | Your own mail server or provider. |
| **Webhook** | Anything that accepts an HTTP POST. |
| **Publish to SignalK** | Broadcasts onto the boat's own instrument network, so a buzzer or an MFD already wired to it can react. Needs no internet at all. |
| **Web push** | Alarms on your phone's lock screen with no app to install. Needs Helmcentral served over https; see [Web push over Tailscale](../reference/configuration.md#web-push-over-tailscale). |

A **Send Test** button probes every enabled transport at once, so you can
check delivery before relying on it. See [Set up alarm
notifications](../how-to/set-up-alarm-notifications.md) for entering the
details each transport needs.

Changing the ntfy server or the SMTP host clears the token or password
stored for that transport. This is deliberate: a credential is bound to the
destination it was entered for, and repointing that destination does not
carry it across. Paste the token or password back in after changing either
address; the transport stays quiet until you do. Changing the SMTP username
clears the password too, since AUTH PLAIN sends the two together.

Helmcentral picks up alarms from other producers on the instrument network, so
it has to recognise its own output coming back. It does that by name: anything
under `helmcentral.` is its own, and so is any of your enabled rules. These are
not shown as duplicate external alarms. An alarm under its own name that no
rule here is holding, left behind by a restart or by another Helmcentral
install pointed at the same instrument network, is cleared automatically. For
that reason a development copy of Helmcentral should leave **Publish to
SignalK** switched off if it shares an instrument network with the boat's own
install.

A restart of the boat's instrument network hub is the sharpest way an
externally-raised alarm goes stale: whatever it was holding for another
producer is simply gone the moment it comes back, whatever the underlying
condition is actually doing, and nothing about the reconnection says this
happened, since nothing being watched actually changed from its point of
view. The 30-second re-check described above is what catches it, rather than
leaving that alarm on your board until the same reading happens to change
again.

## Anomaly detection

A separate set of checks (frozen and impossible sensor readings, charging
into a full house bank, and twin engines pulling apart from each other)
gets its own page: [Anomaly detection](anomaly-detection.md).

## AIS collision alarms

Collision warnings for AIS targets come from a separate AIS Target
Prioritizer plugin on the boat's own instrument-network server, not from a
Helmcentral rule. It works out CPA and TCPA for every target from the AIS
data already on the network and raises a warning per target when one crosses
the thresholds of the profile in force. It keeps four profiles, anchored,
harbor, coastal and offshore, each with its own warning and alarm tiers.
Helmcentral selects the profile from the vessel's navigation state (anchored,
moored, under way) and never edits the numbers in it.

Those numbers are yours, and the shipped harbor profile needs attention before
a marina. It warns on any target closer than half a mile with a reported speed
over half a knot. Alongside a pontoon every neighbour is inside half a mile,
and GPS jitter puts a tied-up boat over half a knot every few minutes, so the
warning fires on boats doing nothing. Each collision alarm card carries a link
to the plugin's own settings, which is where the thresholds live. Raising the
warning tier's speed floor to a couple of knots quietens a marina without
losing a boat actually moving down the fairway.

When both vessels are moving and the picture is clear enough to read, the card
adds a second line naming the encounter and what the rules ask of you, for
example:

> Crossing, she is on our starboard bow (040° rel). We give way (Rule 15):
> alter to starboard, pass astern.

It names the rule and the role: overtaking, head-on, or crossing, who gives
way and who stands on, and for a stand-on power-driven vessel the reminder not
to turn to port for a target on your own port side. Vessel type comes from
whatever each of you is actually doing right now, motoring or sailing, not
from AIS ship type, which says nothing about whether an engine is running.
When the other vessel's type isn't being transmitted, or when either of you is
stopped, at anchor, or already opening, the line is left off rather than
guessed at: a wrong role on the card is worse than a missing one. This is a
read of the rule in force, not a course to steer, and it doesn't replace
keeping a proper lookout or working out risk of collision by every means you
have. A future release will add the same line to a radar-only contact once
that alarm exists; for now the classifier behind it flags a radar contact for
what it is, no vessel type known, and gives only the restricted-visibility
caution about which way not to turn.

## The rules list

Rules are grouped by what they watch: electrical, environment, propulsion,
tanks, radar and so on, sorted alphabetically and worked out automatically
from what each rule reads, with no manual categorising needed. A rule whose
alarm is live is marked as firing. Each one shows its threshold and the point
it clears at, in your own units, and deleting one asks first.

## Gauge bands are alarm rules

A coloured band on a gauge set to an alarm severity defines an alarm rule.
Dragging engine temperature into the red on the dial raises an alarm through
the configured transports. You do not need to re-enter the threshold on a
separate screen or convert it to different units.

Rules derived this way appear in the alarms list alongside hand-written ones,
marked as coming from a gauge, and are edited on the gauge rather than in the
list. See [Set an alarm threshold](../how-to/set-an-alarm-threshold.md) for
the steps either way.

## Values Helmcentral works out for itself

A rule is not limited to a single instrument reading. Some useful numbers,
a rate of change, a relationship between two readings, a total across every
tank, don't exist as a single reading and have to be worked out from the
history Helmcentral already keeps.

Helmcentral computes a set of these values itself and makes them available
the same way as any instrument reading: they show up alongside everything
else when you're choosing what a rule or a gauge should watch, they carry
their own units, and they report nothing rather than a misleading zero when
there isn't yet enough history to answer. See [Alarm rules
reference](../reference/alarm-rules.md#derived-values) for the full list,
what each one needs before it starts reporting, and when it goes quiet again.

### Why a squash zone needs a signal of its own

A squash zone is a high and a low close enough together to accelerate the wind
between them. Pressure and wind direction can both hold steady while the wind
builds, so a barometer-only rule would not detect this pattern.

*Surviving the Storm* calls squash zones the cause of most heavy-weather
trouble yachts encounter, and singles out the western South Pacific around New
Zealand and Australia as getting more than its share. See
[Forecast](forecast.md) for where these thresholds come from.

## The heavy-weather rule set

Helmcentral ships two rules using thresholds from that book, created once on
first run. See [Alarm rules reference](../reference/alarm-rules.md#heavy-weather-rule-set)
for the two rules and their thresholds.

**They arrive switched off.** These are one crew's thresholds, published in 1999,
and they have not been calibrated against your boat or your cruising ground.
Turn on the ones that suit where you sail, and adjust them once you have watched your own
barometer for a season.

For example, in the tropics the barometer
swings about 1.5 mb either way every day on its own, so any departure from that
rhythm is significant. Outside the tropics the same rule can raise false alarms.

Once created, these rules can be edited, retuned or deleted like any other. A
rule you delete stays deleted; the set is not re-created on the next restart.

### One thing the rules cannot know

A rate of fall means different things depending on which way you are going. A
system moving at 15 knots closes 190 miles a day on a boat running with it and
528 on one heading into it, and the barometer moves accordingly. The same
1 mb/hr is a different situation in each case.

At anchor this does not apply. Underway, account for your course relative to
the weather system when interpreting an alarm; the thresholds do not adjust
for it.

## The Law of Storms barometer rules

R. J. Ellis, "Secret Law of Storms", worldstormcentral.co (Rules for storms
and gales page), turns the same three-hour tendency into a ladder: a 6 mb
move either way means strong wind, a 10 mb move either way means gale, and
two further rules read the tendency against the barometer's own height
rather than its movement alone. Helmcentral ships seven rules from this
ladder, created once on first run. See [Alarm rules
reference](../reference/alarm-rules.md#the-law-of-storms-rule-set) for the
full ladder.

**Six of these seven arrive switched on.** Unlike the two rules above, this
is a single ladder rather than three rules restating one falling barometer,
its windows match the tendency a marine forecast already quotes, and each
rung's severity matches the wind range the page attributes to it.

**Storm signature ships switched off.** The page itself calls 3 mb the
minimum for this rule and 4 mb only "a margin of comfort" above that, and a
4 mb fall under 1009 mb is a routine afternoon on a temperate coast, not the
storm the label promises. Turn it on somewhere you judge it means something.

**If you installed Helmcentral before this ladder shipped,** your rules list
still carries Barometer falling, Barometer plummeting and Barometer down
3mb in three hours. These three are retired in favour of the ladder above,
but nothing deletes a rule for you: delete them yourself, once, and they
stay gone.

## The forecast warning rules

The forecast-warnings plugin (see [Forecast](forecast.md)) already knows which
official marine warnings are in force for the vessel's own zone. Helmcentral
polls it every ten minutes in the background, ranks the result, and ships
four rules bound to it, created once on first run. See [Alarm rules
reference](../reference/alarm-rules.md#forecast-warning-rule-set) for the four
rules.

**These arrive switched on.** Unlike the heavy-weather set there is nothing to
calibrate: the source is the met service's own call for your zone, not a
threshold read from a book. A gale warning issued while you are ashore reaches
every transport you have enabled, and shows in the alarm banner on every
screen until you acknowledge it there. The banner carries its own Acknowledge
button now, right beside View, so silencing the sound never needs a trip to
the Active Alarms tile.

"Unavailable" means what it says. It raises when there is no forecast-warnings
provider installed, when the boat has had no position fix, or when the provider
has been down for half an hour. It exists so a dead provider looks like a dead
provider rather than a quiet sea. If your boat has no provider and you do not
want the standing alert, disable that rule like any other.

The alarm card says which warning is in force, with a link straight to the
bulletin right there on the card, both in the banner and on the Active
Alarms tile. The region and the days it covers stay on the Forecast page.
