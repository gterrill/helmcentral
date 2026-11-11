# Frequently asked questions

Short answers to the questions people ask before they install Helmcentral.
Each one links to the page that covers it in full.

## What hardware and software does it need?

Helmcentral runs on a computer that stays on aboard the boat, and you look at
it in a web browser on your phones, tablets and screens. Here's what you need:

- **A connection to the boat's instruments:** Your instruments talk to each
  other over NMEA 2000, NMEA 0183 or both. Helmcentral can't plug into those
  directly. It needs a gateway that passes their data to a computer, such as a
  USB adaptor for NMEA 0183 or an NMEA 2000 gateway. SignalK's
  [installation page](https://signalk.org/installation/) covers which adaptors
  and gateways work and how to connect them.
- **Sensors, as many or as few as you have:** You don't need any particular
  sensor to start. With no instruments at all you still get the inventory,
  maintenance by the calendar, documents and notes. A GPS, with SignalK's
  autostate plugin, adds the anchor watch, the logbook and the forecast. Each
  sensor after that adds more. A depth sounder, wind instrument, AIS receiver,
  battery monitor, engines and tanks each open up their own tiles and alarms.
  When a sensor is missing, the tiles that need it say so rather than showing
  an invented number. [Hierarchy of sensor needs](sensor-needs.md) shows what
  each sensor adds, level by level.
- **A SignalK server:** SignalK reads the gateway and turns the NMEA data into
  readings Helmcentral can use. If you don't have it yet, install it first,
  following the same [installation page](https://signalk.org/installation/).
  It can run on the same computer as Helmcentral. Add the
  `signalk-derived-data` plugin for true wind and other worked-out navigation
  figures. If you have a Victron GX, add `signalk-venus-plugin` too, so that
  you get generator and detailed electrical readings.
- **A computer on the boat:** Linux, macOS or Windows. Linux builds cover
  x86-64, arm64 and armv7, which covers the Raspberry Pi 2 and every later
  model, including the Zero 2 W. The older ARMv6 boards aren't covered: the
  Pi 1, Pi Zero, Zero W and Compute Module 1. On Linux it runs as a
  systemd service. It also runs in Docker, and the image works on the same
  three kinds of processor.
- **Phones, tablets and screens to look at it on:** There's no app to install.
  Any phone, tablet, laptop or wall screen with a recent browser works: Chrome
  or Edge 111 or later, Firefox 111 or later, or Safari 16.4 or later. On an
  iPad or iPhone that means iOS 16.4 or later. Older tablets aren't supported,
  so check a wall screen with `/display-probe.html` before you mount it.
- **A network on the boat:** Each phone, tablet and screen has to be on the
  same network as the computer running Helmcentral. On most boats that's the
  boat's WiFi. If your boat doesn't have WiFi yet, you'll need a WiFi router
  or access point aboard first, with the Helmcentral computer plugged into it
  or joined to it. A phone's hotspot isn't a good substitute, because the
  network goes away whenever the phone leaves the boat. To look at it from
  ashore, see [Can I use it while off the boat?](#can-i-use-it-while-off-the-boat).

There's no database server to install. Helmcentral is a single program with
the web app built in, and it keeps its own data on the computer it runs on.
InfluxDB is optional, and only adds a longer history. The steps are in
[Installing Helmcentral](../how-to/install.md).

## Does it need an internet connection?

No, not for the parts that look after the boat. The dashboard, alarms, anchor
watch, passages, inventory, maintenance, documents and the help pages all work
with no connection at all. An alarm can still reach a buzzer or a chartplotter
through SignalK when the boat is offline.

These parts do use the internet:

- **Forecast and tides:** Weather, waves, tides and marine warnings are fetched
  from online providers. When a provider can't be reached, its section of the
  forecast says so rather than showing a stale number.
- **Place names and the Nearby map:** These ask a public OpenStreetMap server,
  unless you [run your own Overpass server](../how-to/run-your-own-overpass-server.md)
  aboard.
- **Maps:** The chart shows offline only for areas Helmcentral has already
  shown and kept, so a new cruising ground needs a connection first. The
  buoys and other sea marks, and the satellite photographs, always need a
  connection.
- **Notifications off the boat:** ntfy, email, webhooks and web push all need a
  connection to leave the boat.
- **Mate:** Every question goes to the model through OpenRouter, so Mate
  can't answer anything without a connection. The
  [offline coastline](../features/offline-coastline.md) keeps the shoreline
  aboard, so a weak or slow connection is enough for Mate to judge an
  anchorage's shelter.

## How much does it cost?

Helmcentral is free. It's open source under the MIT licence, and there's no
subscription or cloud account.

The default weather, wave, tide and place-name providers are free and need no
key. The notification channels don't need a paid service either, although an
email server or ntfy server you already pay for works fine.

The only running cost is Mate, if you turn it on. You use your own
[OpenRouter](https://openrouter.ai/) account and pay OpenRouter for each
question. Each reply shows what it cost, for example `$0.056`. Mate is off
until you turn it on. See [Mate](../features/assistant.md#conversations-and-cost).

## What locations does it work in?

Anywhere in the world for the core of it. Most of what's local comes from the
boat's own GPS, so the forecast, the default tides and boat time follow you
without you setting a position. A tide station you've picked stays where it
is, so choose a new one when you move on.

Some providers only cover certain waters:

- **Weather, waves and upper air:** Open-Meteo, worldwide.
- **Tides:** A worldwide model by default. Its heights are measured from mean
  sea level, not chart datum. In Australia or the US you can choose the BOM or
  NOAA provider instead, pick a station, and get published predictions on chart
  datum.
- **Marine warnings:** Australia (BOM, the default) and the US (NWS) only.
  Elsewhere, the forecast shows no official warnings.
- **Place names and nearby places:** OpenStreetMap, worldwide. How much it
  knows depends on how well local people have mapped your area. A Google
  Places provider is also included. It needs a key and covers some kinds of
  place.
- **Offline coastline:** Worldwide, except across the 180° meridian and near
  the poles.

Other regions can be added as a plugin without changing Helmcentral itself.
See [Provider plugins](plugins.md).

## Where does it get weather, wave and tide information?

Each comes from its own provider plugin, and you choose which one in Settings:

- **Weather:** Open-Meteo by default, which needs no key. Apple WeatherKit is
  the other choice, and it needs Apple developer keys.
- **Waves:** Open-Meteo Marine.
- **Tides:** Open-Meteo's worldwide model by default, or BOM in Australia and
  NOAA in the US.
- **Marine warnings:** BOM in Australia and NWS in the US.

The forecast card says which provider answered, whether the data came from the
cache, and how long ago it was refreshed. If one provider is down, only that
section shows the failure. See [Forecast](../features/forecast.md).

## Can I use it while off the boat?

Yes. Here are the ways to keep in touch from ashore:

- **Alarm notifications:** Alarms reach your phone through ntfy, email, a
  webhook or web push. Each one is described in
  [Set up alarm notifications](../how-to/set-up-alarm-notifications.md).
  Web push needs Helmcentral served over https, and the supported way to do
  that is Tailscale.
- **The dashboard:** Reach it over a VPN such as Tailscale, which puts your
  phone on the boat's network wherever you are. Don't open a port on the
  boat's router to the internet.
- **Knowing the boat has gone quiet:** Helmcentral can send a regular
  heartbeat off the boat, so that an outside monitor tells you when the whole
  server has stopped. See [Alarms](../features/alarms.md).
- **Asking Claude or ChatGPT:** An [outside assistant](../features/outside-assistants.md)
  can read the boat's status, alarms and logs from your phone. It can only
  read.

The boat needs an internet connection for any of these to work.

## What about security?

**Login is off by default.** On a fresh install, anyone on the boat's network
can use Helmcentral. That includes starting and stopping the generator and
switching CZone circuits, if you have them. It's off so that an upgrade can
never lock you out of a running boat, but you should turn it on.

Here's how Helmcentral keeps the boat safe:

- **Login through SignalK:** Turn on SignalK's security first, then turn on
  login in **Settings → Security**. Everyone then logs in with their SignalK
  account. SignalK's read-only, read-write and admin levels decide what each
  person can do, and an unknown level gets no access at all. Helmcentral won't
  save the setting until SignalK security is on.
- **Login depends on SignalK at startup:** With login on, Helmcentral checks
  SignalK's security when it starts. If SignalK isn't running yet, or its
  security has been turned off, Helmcentral won't start, and nothing else
  works until it does, alarms and anchor watch included. Depending on how you
  run it, a power cut can leave it stopped until you start it again. How to
  get it going again on Docker, systemd, macOS and Windows is in
  [Login and SignalK at startup](configuration.md#login-and-signalk-at-startup).
- **Keep it off the open internet:** Run it on a boat network you trust. For
  access from ashore, use a VPN or a reverse proxy that asks for a login, and
  never forward a port to it. The one exception is Tailscale Funnel for
  outside assistants, below. It passes on the assistant's address and refuses
  everything else.
- **Passwords and keys are encrypted:** Every password, token and API key goes
  into its Settings section, not into `settings.yaml`. That covers the SignalK
  login, the InfluxDB token, the email password, the ntfy token, the WeatherKit
  and Google Places keys, and your OpenRouter key. Helmcentral keeps them in
  its own store on the boat, encrypted with AES-256-GCM. The key that unlocks
  them is the file `data/secrets.key`, unless you've set
  `HELMCENTRAL_MASTER_KEY` instead.
- **Some credentials belong to an address:** Changing the SignalK address
  clears the SignalK login, and changing the InfluxDB address clears its
  token. Changing the ntfy server clears the ntfy token, and changing the
  email server or username clears the email password. Enter it again
  afterwards, or that channel stays quiet.
- **Back up the key with the store:** Back up the whole state directory,
  `settings.yaml` included, and `HELMCENTRAL_MASTER_KEY` too if you use it.
  Without the key, Helmcentral won't start until you move the old store aside.
  Then every credential has to be entered again, and every phone has to turn
  web push off and on again. Anyone with the folder and its key can read the
  credentials, so keep the backup as safe as the boat's computer. See
  [Configuration](configuration.md#back-this-up).
- **Provider plugins are fenced in:** A plugin can't read your files or start
  programs. It can only reach the web addresses listed alongside it, and it's
  stopped if it runs too long.
- **Outside assistants can only read:** Each program gets its own token, which
  you can revoke at any time. Without a token, every request is refused. Only
  Claude on a phone or the web and ChatGPT need a public address, through
  Tailscale Funnel, and that address leads to the assistant and nothing else.
- **Mate asks before it changes anything:** Mate can suggest changes to alarms,
  maintenance, the inventory and the boat's details, but nothing changes until
  you tap **Apply**.
  It can't start, stop, switch or steer anything.

### What leaves the boat

Helmcentral has no account, no cloud service and no usage reporting. Your
records, documents, logs and settings stay on the boat's computer. These are
the things that leave it. The forecast, map and place-name providers are free
public services until you choose others, and the rest go only where you've
pointed them:

- **The boat's position, for forecasts and tides:** The weather, wave, tide
  and warning providers are told where the boat is, so that they can answer for
  it.
- **The boat's exact position, for the Windy map:** Opening the Windy weather
  map sends the boat's position to Windy, which shows it. Windy isn't a
  provider plugin, so the plugin fence doesn't apply to it.
- **The area around the boat, for maps and place names:** Maps, sea marks,
  satellite photographs and place names are fetched for the area you're in or
  looking at. So is the offline coastline. A server that answers those
  requests can work out roughly where the boat is. Running your own
  [Overpass server](../how-to/run-your-own-overpass-server.md) keeps the place
  lookups aboard.
- **Alarm and watch messages:** Each alarm goes to the notification channels
  you've turned on, such as ntfy, email, a webhook or web push. It carries its
  title, its reading and sometimes a small chart. Watch reports go the same
  way, with their figures and findings. If you set a heartbeat, a regular
  "still alive" message does too.
- **Mate's questions, only with Mate on:** Each question goes to OpenRouter,
  and from there to the model you chose. It carries the question, the boat's
  position and whatever Mate looks up. Watch reports and uploaded documents
  are sent too, and so are search words if you turn on web search. With Mate
  off, nothing goes. [What leaves the
  boat](../features/assistant.md#what-leaves-the-boat) lists each case.
- **Your voice, if you talk to Mate:** The browser turns speech into text
  before Mate sees it. Chrome sends the audio to Google's speech service.
  Safari uses Apple's dictation, which runs on the device on recent hardware.
- **What an outside assistant asks for:** A program you've given a token, such
  as Claude or ChatGPT, gets the answers to what it asks. Those reach whoever
  runs that program, such as Anthropic or OpenAI. The boat's particulars leave
  out your name, phone, email and insurance details. See
  [Outside assistants](../features/outside-assistants.md).

### What Helmcentral can't protect you from

NMEA 2000 and NMEA 0183 were built for a boat's own trusted wiring, so they
have no security of their own. Any device on the network can claim to be any
other and send any reading. Helmcentral reads those readings through SignalK
and can't tell a real one from a fake. Here's what that means:

- **Spoofed or jammed GPS:** Helmcentral catches a position that arrives with
  a poor fix or stops arriving. At anchor or moored, it also catches a position
  that jumps further than the boat could move. In each case it freezes the
  last trusted position and raises its own alarm instead of a drag alarm. A
  spoof that reports a good fix and drifts the position slowly gets through,
  though. Helmcentral then shows the false position, and the anchor watch can
  raise a false drag alarm or miss a real one.
- **False AIS targets:** AIS has no way to prove who sent a message. A fake
  target shows on the maps and can raise a collision alarm, and a real vessel
  that isn't transmitting doesn't show at all.
- **A faulty or rogue device:** A sensor or gateway that sends wrong readings
  is believed. Anomaly detection catches readings that are frozen or
  impossible, but it can't catch one that is wrong and still believable.
- **Commands on the network:** Anything on the NMEA network can send commands
  to the autopilot or switching directly, without going through Helmcentral.
  Helmcentral's login only controls what's sent through Helmcentral.

So keep the boat's network apart from the internet, and be careful which
devices you connect to it. Keep a lookout and your chartplotter's own alarms
too, rather than relying on any one screen.
