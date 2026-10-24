# Place names and nearby places

Helmcentral puts names to the water around the boat: the bay you are anchored
in, the marina you are lying in, the islands and reefs a passage ran between.
It also answers "what's near here" and "where is that", so you can find the
nearest boat ramp or fuel dock, or ask Mate about an anchorage by name, without
leaving the helm.

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

All of this comes from OpenStreetMap, the open map that sailors and others
keep up to date. By default Helmcentral asks a public **Overpass server** for
it each time it needs a name. A Google Places plugin can serve the Nearby map
instead, but not place names.

The public servers are run by volunteers and shared by everyone who uses them.
That works ashore. From a boat it often doesn't:

- They are slow. Over marina wifi or a satellite link a single lookup often
  takes 5 to 15 seconds. Mate asks several at once when it compares
  anchorages, and those wait in line behind each other.
- They refuse requests. Each server gives one address only a couple of lookups
  at a time and turns the rest away when it is busy. Some servers now answer
  only networks they have approved, and the main one has refused some boat
  networks outright.
- They need a link. Out of mobile range, or with the satellite link down,
  there are no names at all.

When a lookup fails, the position tile goes without a name, the Nearby map
keeps the last places it found and shows how old they are along with the
error, and Mate carries on without names. After a few failures in one answer
Mate stops looking places up and says so.

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
