# Helmcentral documentation

Press the `?` button in the header to open help for the screen you're on. The
**Help** item in the sidebar brings you back to this contents page. Help works
without an internet connection. A few links lead to engineering notes or
source code on GitHub, and those do need one.

## Features

What each feature does and where it stops. Start here to decide whether
Helmcentral suits your boat.

- [The dashboard](features/dashboard.md): the pages of instrument tiles, gauges
  and embeds you set up at the helm.
- [Forecast](features/forecast.md): weather, wind, waves and tide, plus the
  heavy-weather warning signs the wave graph watches for.
- [Engine Cluster](features/engine-cluster.md): one engine's dial and readouts,
  with fuel and electrical figures along the bottom.
- [Anchor watch](features/anchor-watch.md): watching the anchor, and the rode
  planner for working out how much to let out.
- [Alarms](features/alarms.md): alarm rules, how alerts reach you, and why they
  behave the way they do.
- [Passages](features/passages.md): the logbook of every leg the boat has run.
  Replay any of them on the chart and see cruise totals for nights, fuel, wind,
  generator, battery and water.
- [Watches](features/watches.md): regular checks of the boat on a schedule, each
  with a report, findings from rules you can read, and Mate's review.
- [Anomaly detection](features/anomaly-detection.md): catches frozen or
  impossible sensor readings, charging into a full house bank, and engines
  drifting apart from each other.
- [Mate](features/assistant.md): a chat assistant that answers passage-planning
  questions from the boat's own live data. Ask it from any page, by typing or
  by voice.
- [Place names and nearby places](features/place-names.md): naming the bay,
  marina or island you're in, the Nearby map, and Mate's place lookups. It also
  explains why an Overpass server aboard makes them faster and keeps them
  working offline.
- [Offline coastline](features/offline-coastline.md): charted coastline and
  reefs for the waters around the boat, stored aboard. Mate uses it to judge
  anchorage shelter without an internet connection.
- [Documents](features/documents.md): a searchable library for manuals,
  receipts and logs. Once Mate reads a scan or a photo, it adds the text, a
  summary and tags. There's no Documents tile for the dashboard yet.
- [Notes and the manual](features/notes-and-the-manual.md): write down what you
  know about the boat in one step, then gather it into an operations manual the
  crew and the next owner can follow.
- [Inventory](features/inventory-tracking.md): the equipment register, with one
  record per system aboard, its documents, where it's stowed, and the profile
  it follows.
- [Maintenance](features/maintenance.md): jobs that fall due on running hours,
  on the calendar, or both, and the service log you sign them off in.
- [Outside assistants](features/outside-assistants.md): let Claude or ChatGPT
  read the boat's status, alarms and logs. It can only read, and only on your
  network or through one Tailscale Funnel address.
- [Import](features/import.md): bring equipment, spares, the service log, notes
  and documents across from YachtWave.

## How-to guides

Step-by-step instructions for specific jobs.

- [Installing Helmcentral](how-to/install.md): with Docker or by hand.
- [Upgrading across a breaking release](how-to/upgrading.md)
- [Plan a stay](how-to/plan-a-stay.md): see the forecast for a marina you've
  booked.
- [Set an anchor watch](how-to/set-an-anchor-watch.md): drop the watch, move
  the anchor or radius, and raise it.
- [Set an alarm threshold](how-to/set-an-alarm-threshold.md): create an alarm
  rule directly or from a gauge.
- [Change a tank's alarm levels](how-to/change-tank-alarm-levels.md): when a
  tank turns amber or red and raises an alarm, for a kind of tank or for one
  tank.
- [Set up alarm notifications](how-to/set-up-alarm-notifications.md): turn on
  a way for alarms to reach you, and check it works.
- [Create a dashboard page](how-to/create-a-dashboard-page.md)
- [Reorder dashboard pages](how-to/reorder-dashboard-pages.md)
- [Pin an indicator ribbon](how-to/pin-an-indicator-ribbon.md)
- [Set up a wall display](how-to/set-up-a-wall-display.md)
- [Add a Nearby map](how-to/add-a-nearby-map.md)
- [Connect an outside assistant](how-to/connect-an-outside-assistant.md): make a
  token and add the boat to Claude Code or Claude Desktop.
- [Give outside assistants a public address](how-to/give-outside-assistants-a-public-address.md):
  use Tailscale Funnel to pass on the outside assistant and its sign-in notes,
  and nothing else.
- [Connect Claude on your phone or the web](how-to/connect-claude-on-the-phone-or-web.md):
  make a token and add the boat as a connector in Claude.
- [Connect ChatGPT](how-to/connect-chatgpt.md): make a token, add the boat as
  an app in ChatGPT, and sign in by pasting the token.
- [Set up Mate](how-to/set-up-the-assistant.md)
- [Talk to Mate](how-to/talk-to-mate.md)
- [Keep what a Mate conversation worked out](how-to/keep-what-a-mate-conversation-worked-out.md)
- [Fill in a form with Mate](how-to/fill-in-a-form-with-mate.md): insurer and
  marina paperwork, filled in from details the boat already holds.
- [Start a ship's manual](how-to/start-a-ships-manual.md): what belongs in one,
  and what the builder's handbook already covers.
- [Write the boat's manual](how-to/write-the-boats-manual.md): capturing,
  filing, ordering and illustrating each section.
- [Stop showing a network alarm](how-to/stop-showing-a-network-alarm.md): hiding
  one you don't need, and bringing it back.
- [Set battery state-of-charge bands](how-to/set-battery-state-of-charge-bands.md)
- [Set passage limits](how-to/set-passage-limits.md)
- [Turn on the offline coastline](how-to/turn-on-the-offline-coastline.md):
  filling it, keeping it current and clearing it.
- [Run your own Overpass server](how-to/run-your-own-overpass-server.md): a
  place-names server aboard that answers quickly and works offline.
- [Set up your vessel](how-to/set-up-your-vessel.md): choose which engines and
  house bank anomaly detection watches, and enter owner and insurance details.
- [Show equipment on a tile](how-to/show-equipment-on-a-tile.md)
- [Duplicate a gauge group onto another instance](how-to/duplicate-a-gauge-group.md)
- [Tag bins and equipment](how-to/tag-bins-and-equipment.md): writing an NFC
  tag, using it, and the iPhone Copy route.
- [Photograph your gear](how-to/photograph-your-gear.md): from an item's
  editor, or on the spot while adding it from a bin.
- [Draw your deck plans](how-to/draw-deck-plans.md): add a picture of each
  deck, outline locations on it and pin bins.
- [Run a stocktake](how-to/run-a-stocktake.md): scan a bin and its contents to
  confirm what's aboard.
- [Set up a maintenance schedule](how-to/set-up-a-maintenance-schedule.md):
  follow a profile's service intervals, change them for one item, or add rules
  by hand.
- [Set up a maintenance schedule with Mate](how-to/set-up-a-maintenance-schedule-with-mate.md):
  ask Mate for a schedule and apply what it suggests.
- [Log a service](how-to/log-a-service.md): complete a scheduled job, or record
  a repair or improvement.
- [Import GPX tracks](how-to/import-gpx-tracks.md): add trips Helmcentral
  didn't record.
- [Edit a passage](how-to/edit-passages.md): split, trim, merge and hide
  passages, and file them into cruises.
- [Film a passage replay](how-to/film-a-passage-replay.md): direct the replay
  camera with shots, and pin notes and photos along the way with signs.
- [Share a passage on your blog](how-to/share-a-passage-on-your-blog.md): copy
  an animated map into a Ghost post and save still images.
- [Import from YachtWave](how-to/import-from-yachtwave.md): each page of the
  import wizard in turn.
- [Development](developers/development.md): for working on Helmcentral itself,
  running it locally, testing and building releases.

## Reference

Details to look up: settings, file formats and where things are stored.

- [Configuration](reference/configuration.md): every environment variable,
  where data is kept, and what happens at startup.
- [Boat time](reference/boat-time.md): how the boat's clock is chosen from its
  position, what happens without a fix, and where the zone data comes from.
- [Tiles](reference/tiles.md): the built-in tiles, their fields, and how every
  tile shows stale data and alert colours.
- [Alarm rules](reference/alarm-rules.md): the built-in alarm rules and the
  extra values a rule or gauge can watch.
- [What Mate can look up](reference/mate-tools.md): each lookup Mate can make
  and the provider it draws on.
- [Watch report webhook](reference/webhook-payloads.md): the JSON a watch posts
  to a webhook, and how to tell it from an alarm.
- [Equipment profiles](reference/equipment-profiles.md): the profile file
  format for engines, alternators and generators.
- [Provider plugins](reference/plugins.md): how plugins are kept apart from
  the rest of Helmcentral, and what each kind has to provide.
- [YachtWave import fields](reference/yachtwave-import.md): where each part of
  a YachtWave export ends up.
- [POI categories](reference/poi-categories.md): the eleven points-of-interest
  categories and what each provider covers.

## Engineering notes

The [design decisions](adr/) behind Helmcentral are kept separately for anyone
working on the code. You won't need them to use the app.
