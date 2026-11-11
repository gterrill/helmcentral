# Frequently asked questions

Short answers to the questions people ask before they install Helmcentral.
Each one links to the page that covers it in full.

## What hardware and software does it need?

Helmcentral runs on a computer that stays on aboard the boat. Here's what you
need:

- **A computer on the boat:** Linux, macOS or Windows. Linux builds cover
  x86-64, arm64 and armv7, which covers the Raspberry Pi 2 and every later
  model, including the Zero 2 W. The older ARMv6 boards aren't covered: the
  Pi 1, Pi Zero, Zero W and Compute Module 1. On Linux it runs as a
  systemd service. It also runs in Docker, and the image works on the same
  three kinds of processor.
- **A SignalK server:** Helmcentral reads the boat's instruments through
  SignalK, which turns the NMEA network into readings it can use. Add the
  `signalk-derived-data` plugin for true wind and other worked-out navigation
  figures. If you have a Victron GX, add `signalk-venus-plugin` too, so that
  you get generator and detailed electrical readings.
- **A screen with a recent browser:** Chrome or Edge 111 or later, Firefox 111
  or later, or Safari 16.4 or later. On an iPad or iPhone that means iOS 16.4
  or later. Older tablets aren't supported, so check a wall screen with
  `/display-probe.html` before you mount it.

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
  works until it does, alarms and anchor watch included. Here's how to get it
  going again:
  - SignalK slow to start: Under Docker, Helmcentral keeps trying and comes up
    once SignalK answers. The systemd service retries too, but it can give up.
    If `systemctl status helmcentral` shows it failed, run
    `sudo systemctl restart helmcentral` once SignalK is up.
  - SignalK's security turned off: Turn it back on in SignalK, and Helmcentral
    starts with login as before.
  - As a last resort: Set `auth.mode: none` in `settings.yaml` and restart.
    That turns login off for everyone, so turn it back on once SignalK is
    sorted out. See [Configuration](configuration.md#startup-behaviour).
- **Keep it off the open internet:** Run it on a boat network you trust. For
  access from ashore, use a VPN or a reverse proxy that asks for a login, and
  never forward a port to it.
- **Passwords and keys are encrypted:** You paste SignalK credentials, API keys
  and your OpenRouter key into Settings, never into a file. They're stored
  encrypted. Back up the whole `data/` folder, because without
  `data/secrets.key` the stored credentials can't be read again. See
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

The NMEA network itself has no security. Any device on it can send anything,
and no software can fix that. So keep the boat's network apart from the
internet, and keep control of who can send commands.
