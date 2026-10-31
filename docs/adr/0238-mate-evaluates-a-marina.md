# ADR 0238: Mate evaluates a marina

## Status

Accepted (2026-10-31). Builds on [ADR 0056](0056-place-names-from-osm-features.md)
and [ADR 0101](0101-place-names-come-from-a-plugin.md) (place names from a
plugin's optional exports), [ADR 0168](0168-place-names-include-marinas.md) (the
500 m rule for a marina), [ADR 0194](0194-passage-limits-are-no-go-only.md)
(a limit the boat reaches is a no-go) and
[ADR 0203](0203-offline-coastline-and-anchorage-shelter.md) (the other optional
export beyond place names). The tool is exposed to outside assistants under
[ADR 0236](0236-more-tools-for-outside-assistants.md).

## Context

When the operator plans a stop, `find_places` gives Mate a marina's name and
position and nothing else. Mate then answers from memory about berths, fuel and
depth, and memory about a marina is the kind of confident answer that puts a
1.9 m keel on a 1.5 m bar.

OpenStreetMap does know a good deal, but not on the marina. Looking at Coral Sea
Marina Resort, Airlie Beach (way 245930150) on the boat's local Overpass mirror,
the marina way carries `capacity=520`, `fee`, `power_supply` and a website, and no
VHF, phone, hours, `maxdraught`, `maxlength` or depth. The useful detail is
mapped as separate features beside it: two `waterway=fuel` nodes with no fuel
type, a `man_made=crane` with `crane:type=travel_lift`, a boatyard
`small_craft_facility`, a named `leisure=slipway`, toilets, a dozen floating
piers named "A Arm", "B&C Arm" and so on, breakwaters, and lateral beacons.
Shops and services sit further out. Reading the marina's tags alone would have
reported almost nothing.

The mirror also has no meta files. `out meta` there answers HTTP 200 with a
runtime error remark, which `parseOverpassBody` already turns into an error.

## Decision

**A new optional plugin export, `marina_details`,** takes a position and returns
what OSM records about the marina and its surroundings. A plugin that lacks it
(google-places, or an older osm-overpass) gets a clear error from the tool that
names the plugin. There is no fallback to another source and no empty result.
The flag is `supportsMarinaDetails`, detected on the throwaway instance like the
other optional exports. It stands alone, being one export. The call has a 30 s
budget instead of the shared 15 s, because the plugin makes two requests.

**Two requests.** The plugin first finds the nearest `leisure=marina` within
500 m of the position (ADR 0168's distance), preferring it over a bare
`seamark:type=harbour`, and answers "no marina charted within 500 m of that
position" when there is none. It then queries by that element's id: the element
itself, marine features within 300 m (fuel, cranes, piers, breakwaters, slipways,
toilets, showers, water, pump-out, small craft facilities, lateral beacons and
buoys, chandlers), and named shops and services within 1 km (supermarket,
convenience, laundry, pharmacy, doctors, hospital, ATM, bank). Hotels, tourism,
restaurants and unnamed services are left out, being noise for this question.
The output is capped at 400 elements and says when it hit the cap. Neither
request asks for `meta`.

**Classification lives in the plugin.** Small craft facility categories, travel
lifts, `amenity` tags and the marina's own `fuel:*`, `sanitary_dump_station`,
`drinking_water`, `shower` and `toilets` tags are mapped to a fixed set of kinds.
Identical features collapse to one entry with a count. Fuel facts come only from
tags: a fuel point with no `fuel:*` tag has no facts, and the host never fills
the gap. `maxdraught`, `maxlength` and `depth` are read as metres, or feet when
tagged so. A value that cannot be read is listed as an unread tag, never guessed.
A marina tagged `toilets=no` is reported as tagged absent, which is the one place
the map says "no".

**Geometry rides along, out of the model's sight.** The details query asks for
`out tags geom`, so the plugin returns the marina outline, every pier (named or
not, private or not) and a shape for each facility and mark, and a position for
each service ashore. The host keeps these on fields tagged `json:"-"` and reads
them in a second pass over the plugin's JSON, so a later map card can draw them
and the model is never given coordinates. A test pins that the result Mate sees
carries none. The host rejects a shape that is not a point, a line of two or
more positions or a closed polygon.

**The host checks the answer strictly,** as it does for `search_places`: a marina
with no OSM id, a position off the planet, a negative or implausible limit, a
facility with no kind or no count, or a service with no name is an error. Every
text from the map is cleaned of control characters and cut to length before Mate
sees it, because OSM is editable by anyone. Lists are bounded and the result says
when it was cut.

**The limit check is the host's.** For draft and for length the result has one
line: `ok`, `exceeds`, `not charted` or `particular not set`. The boat's draft
comes from live instrument data. Length overall comes from live data, or from the
stored length when the instruments do not publish one, and the source is named.
A figure that cannot be read leaves the line at `particular not set` with the
reason. Nothing is assumed. A limit the boat exceeds is a no-go, as passage limits
are (ADR 0194): the result's reading says to treat the marina as a no-go and the
prompt says to say so first. Equal to the limit passes. Air height is not checked:
Helmcentral holds no air height for the boat.

**The reading rule** is in the tool description, where the prompt cache holds it
(ADR 0236), and the part that depends on the result is in `reading`: the data is
volunteer-mapped; anything not listed is UNKNOWN, not absent; fuel type is unknown
unless a fuel fact says so; counts are features, and one thing can be mapped
twice; charted depths are not tide-referenced; call the marina to confirm berths,
depth and fuel, with the phone, email, website or VHF channel when tagged, or a
plain statement that none is.

**Outside assistants.** The tool is on by default, like `find_places` and
`anchorage_shelter`. It is read-only, and the boat's length and draft it compares
against are already given by `get_vessel_particulars`.

## Consequences

- An installed osm-overpass plugin must be replaced for the tool to work. The
  plugin file is not part of the Helmcentral binary. Until it is, Mate says the
  plugin has no marina lookup.
- The plugin fixtures were captured from the exact queries the plugin sends,
  against the local mirror (data current 2026-10-29). They pin Coral Sea Marina:
  520 berths, power, two fuel points of unknown type, the travel lift, boatyard,
  ramp, 13 named pontoon arms, four lateral marks, and Woolworths ashore.
- Nothing in the data says a berth is free, and the tool never implies it.
- Shops ashore are measured from the marina's centre, so a large marina can show
  a distance above 1 km for a place inside the search radius.

## Rejected

- **Reading only the marina's tags.** Reports 520 berths and power, and nothing
  about fuel, hauling out or the ramp.
- **Falling back to Google Places or a model's memory** when the plugin lacks the
  export. That hides the missing source, against the fallback policy.
- **Requesting `out meta`** for edit dates. The boat's mirror has no meta files,
  and the request fails there.
- **Guessing the boat's draft or length** when unset. The line says it could not
  be checked.
- **One query with the nearest marina chosen in Overpass.** QL cannot pick the
  nearest of several; the plugin picks, then queries around the winner.
