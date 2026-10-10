# ADR 0168: Place Names Include Marinas and Harbours

## Status

Accepted. Extends ADR 0056 (place names from OSM features, the ring ladder)
and ADR 0101 (place names come from a plugin).

## Context

Alongside in Breakwater Marina, Townsville (-19.252139, 146.823806), the
position tile showed a dash. The osm-overpass plugin's `place_name_at` matched
only `seamark:type=anchorage`, `natural=bay` and `place=island|islet|rock`.
Live Overpass at that point returned "Breakwater Marina" (`leisure=marina`
and `seamark:type=harbour`, 0.1 km away) and nothing else useful: the
nearest bay or island was 7.6 to 8.4 km off, beyond the 5000 m top ring.

## Decision

- The plugin queries two more clauses, `leisure=marina` and
  `seamark:type=harbour`, and classifies both as one kind, "marina". An
  element tagged with both is a single candidate. Unnamed elements are still
  discarded.
- Rank is anchorage 0, marina 1, bay 2, island 3, islet 4, rock 5. A marina
  sits behind an anchorage (someone chose that spot to anchor) and ahead of a
  bay, but only within 500 m of the boat, measured to the feature's centre.
  Inside that the boat is berthed there; further out, a marina ranked ahead
  of bays would name a boat anchored in a bay after the port 1.4 km across
  the water. The marina and harbour clauses are queried at the smaller of the
  ring and 500 m. 500 m leaves room for a large marina whose centre sits a
  few hundred metres from the outer pontoons.
- The query drops its `out ... 20` cap. Overpass returns elements in
  type-and-id order, not by rank or distance, so with five clauses a busy
  port's harbour points could fill the cap and push out the anchorage or bay
  that should win.
- The host ladder gains a 10 km ring: 400, 1500, 5000, 10000 m. This covers
  open water near a coast, such as a bay mapped as a single point well
  offshore. The query keeps its 12 s server-side timeout under the host's 15 s
  plugin call ceiling; a 15 km version of the query answered quickly against
  the live server, so the larger ring does not need a bigger budget.

## Consequences

- At 10 km the nearest-by-distance bay can be across the water. Off
  Townsville, Picnic Bay on Magnetic Island beats Cleveland Bay by 0.2 km.
  Accepted: a name that is close is better than none.
- A fixture captured live from overpass.openstreetmap.fr
  (`testdata/overpass_place_name_townsville_400m.json`) pins the marina case.

## Rejected

- Ranking a marina ahead of a bay at any distance. Simpler, but anchored in
  a bay with a port inside the same ring, the tile would name the port.
- Falling back to suburb or town names (`place=suburb|town`). They are always
  present, so the tile would never be blank, but they read as a street
  address rather than a place on the water, and they would mask the case
  where nothing nautical is nearby.
