# Place names and nearby places

Helmcentral puts names to the water around the boat: the bay you're anchored in,
the marina you're lying in, the islands and reefs a passage ran between. It also
answers "what's near here" and "where is that". You can find the nearest boat
ramp or fuel dock, or ask Mate about an anchorage by name, without leaving the
helm.

## Where the names show up

- **Position tile** and **anchor pin**: the nearest named anchorage, marina,
  bay or island. The rules for which name wins are in
  [Add a Nearby map](../how-to/add-a-nearby-map.md#place-names-is-a-separate-picker).
- **Nearby map**: marinas, anchorages, moorings, boat ramps, fuel and the rest
  of the [categories](../reference/poi-categories.md) around the boat.
- **Passages**: the names for each end of a leg in the logbook.
- **Mate**: finding a place you name ("how far to Cid Harbour?"), suggesting a
  nearby anchorage, and naming where the boat is right now.

## Where the names come from

All of this comes from OpenStreetMap, the open map that sailors and others keep
up to date. By default Helmcentral asks a public **Overpass server** for it each
time it needs a name. A Google Places plugin can serve the Nearby map instead,
but not place names.

The public servers are run by volunteers and shared by everyone who uses them.
That works ashore, but from a boat it often doesn't:

- **They're slow:** Over marina wifi or a satellite link a single lookup often
  takes 5 to 15 seconds. Mate asks several at once when it compares anchorages,
  and those wait in line behind each other.
- **They refuse requests:** Each server gives one address only a couple of
  lookups at a time and turns the rest away when it's busy. Some servers now
  answer only networks they have approved, and the main one has refused some
  boat networks outright.
- **They need a link:** Out of mobile range, or with the satellite link down,
  there are no names at all.

When a lookup fails, here is what you see:

- **Position tile:** It goes without a name.
- **Nearby map:** It keeps the last places it found and shows how old they are,
  along with the error.
- **Mate:** It carries on without names. After a few failures in one answer,
  Mate stops looking places up and says so.

## Places you give Mate

Plenty of anchorages and waypoints have no name on the map. You might call one
"Thomas Island South" because it's where you anchor in a southerly. The only
thing that marks it is a position you wrote in the log, so Mate can't find it
by looking the name up.

So when you tell Mate the position of a named spot that it didn't know, it
offers to keep it. Here is how that works:

- **Mate asks first:** It shows a card with **Apply** and **Dismiss**, like any
  other change it proposes. Nothing is saved until you tap **Apply**.
- **Mate looks here first:** Once a place is saved, Mate finds it by name in
  every later conversation, with its distance and bearing from the boat. It does
  that before it asks the map, and it tells you it's using your saved place.
- **A name that's already saved:** If you give Mate a new position for a name it
  has, it proposes moving that place. The card shows the old position and the
  new one.
- **A depth:** A saved place can also hold the depth of the water there, in
  metres at chart datum. If you tell Mate the depth when you give it the
  position, it adds the depth to the card and the card shows it. A place keeps one
  depth, with where it came from (charted, which is a figure you gave) and the
  date. A new depth replaces the old one, and the card shows both. When you
  ask Mate about an anchorage within 0.1 nm of the place, and no nearer place has a depth, it uses the depth for
  the swing and clearance checks. See [Mate](assistant.md#depth-swing-and-clearance).
- **Names match loosely:** Capitals, punctuation and spacing don't matter, and
  "Is" or "Isl" stands for "Island". "Thomas Is. South" finds "Thomas Island
  South". Two places can't have names that match like this.

An outside assistant that can look up place names finds your saved places the same way.

Mate only saves a position, and a depth, that you gave it. It never works one
out and saves that.

You can add, rename, move and delete saved places under **Settings → Saved places**.
The steps are in [Manage the places you've given Mate](../how-to/manage-saved-places.md).
Positions are shown, and typed, as degrees and decimal minutes, such as
`020°32.676' S`.

## Running your own server

You can run an Overpass server aboard on the same home server that runs
Helmcentral, loaded with OpenStreetMap data for your cruising ground. Every
place-name and Nearby lookup then goes to it instead of the internet. The
steps are in
[Run your own Overpass server](../how-to/run-your-own-overpass-server.md).

What it gets you:

- Lookups take a fraction of a second instead of several seconds. Mate can
  check five anchorages by name in the time one used to take, and the Nearby
  map fills as soon as you open it.
- It works with no signal. At anchor out of range, or on passage with the
  link down, the position tile still names the bay and Mate can still find
  places by name. With the [offline coastline](offline-coastline.md) turned on
  as well, Mate can name an anchorage and judge its shelter with no internet at
  all.
- The server is yours alone, so Mate's lookups never wait behind other users'
  and are never turned away. If a public server changes its rules, goes down or
  blocks your network, nothing changes aboard.
- Each lookup carries the boat's position. A server on your own network keeps
  that off the internet.

What it costs:

- A home server that runs Docker apps, with a few gigabytes of free disk.
  The Queensland coast, Gold Coast to Torres Strait, takes about 4 GB.
- A first import of one to several hours, which needs the internet.
- It covers only the area you load. Sail beyond it and lookups find nothing
  there until you load a bigger area. Load the area for the season's cruising,
  with room to spare.
- It doesn't update itself. Re-import every few months to pick up new names
  and changes. Place names change slowly.
- The short descriptions on Nearby places still come from Wikipedia, so they
  need the internet. Without it, places show their names without the
  description.

## Limits

- A name is only as good as OpenStreetMap's mapping. A well-known anchorage
  may be missing, or mapped under a slightly different name, so Mate may not
  find it by the name you use.
- Names come from the nearest named feature, so near a headland the name can
  belong to the bay across the water.
- A saved place is found only by its whole name. Mate doesn't find one from part
  of the name. Saved places don't change the name the position tile or the
  Nearby map shows.
