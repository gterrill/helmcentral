# POI categories

The Nearby feature (`GET /api/poi`) groups points of interest into eleven
fixed categories. Every POI-provider plugin is expected to understand all
eleven, though not every plugin can actually supply data for each - a
category a plugin has no source for comes back in the response's
`unsupported` list rather than silently empty.

| Category | What it covers |
| --- | --- |
| `anchorage` | A named or charted spot boats anchor |
| `bay` | A named bay or inlet |
| `island` | A named island or islet |
| `marina` | A marina or managed harbour |
| `fuel` | A fuel dock or waterway fuel point |
| `ramp` | A boat ramp or slipway |
| `mooring` | A mooring buoy or field |
| `historic` | A historic site, monument or lighthouse |
| `viewpoint` | A scenic lookout |
| `dive` | A dive or snorkelling site |
| `trail` | A walking track or trailhead |

## The default provider: OpenStreetMap via Overpass

`osm-overpass` (`docs/examples/poi-plugins/osm-overpass`) is the bundled,
keyless default. It maps every category above onto an OpenStreetMap tag
combination and fetches all of them in a single Overpass request. The exact
tag table lives in that plugin's source (`osm-overpass.go`'s
`poiCategories`), summarised here for an operator judging whether it will
find anything useful on their cruising ground:

| Category | Tags | Cap | Named only |
| --- | --- | --- | --- |
| `anchorage` | `seamark:type=anchorage`, `anchorage=yes` | 40 | no |
| `bay` | `natural=bay` | 40 | yes |
| `island` | `place=island` or `place=islet` | 60 | yes |
| `marina` | `leisure=marina`, or `seamark:type=harbour` + `seamark:harbour:category=marina` | 40 | no |
| `fuel` | `seamark:type=small_craft_facility` (category containing "fuel"), `amenity=fuel` + `boat=yes`, or `waterway=fuel` | 20 | no |
| `ramp` | `leisure=slipway` or `seamark:type=slipway` | 40 | no |
| `mooring` | `seamark:type=mooring` | 80 | no |
| `historic` | `historic=*` or `heritage=*` (both need a name), or `man_made=lighthouse` (name not required) | 60 | yes, except lighthouses |
| `viewpoint` | `tourism=viewpoint` | 40 | no |
| `dive` | `sport=scuba_diving` or `sport=snorkelling`, `natural=reef` (needs a name), or `leisure=dive_centre` | 40 | no |
| `trail` | a named `route=hiking` relation, a named `highway=trailhead`, or a named `highway=path`/`footway` | 40 | yes |

"Named only" means an unnamed feature never appears for that category - an
unnamed reef or an unnamed stretch of coastal path is noise, not a point of
interest an operator would navigate to. The cap is the most this plugin
returns for that category from any one fetch; a cap being hit is reported in
the response's `truncated` list.

### Coverage caveats

OpenStreetMap's coverage of these categories is real but uneven, because it
reflects what volunteer mappers have actually surveyed and tagged:

- **Anchorages, marinas and moorings** are generally well covered in popular
  cruising areas (the Whitsundays, the US East Coast, the Mediterranean) and
  thin or absent in less-visited waters.
- **Snorkelling and dive sites** are sparsely tagged worldwide - `sport=
  scuba_diving`/`snorkelling` nodes are rare outside a handful of well-known
  dive destinations, so this category often returns little even in good
  diving water. A named `natural=reef` catches some of the gap.
- **Walking trails** are noisy in the other direction: `highway=path` and
  `highway=footway` are used for everything from a maintained national-park
  track to a short unmarked footpad between two streets. The named-only
  filter removes the worst of it, but an urban area can still surface trail
  results an operator wouldn't consider a bushwalk.
- **Historic sites** skew toward what's locally notable enough for someone
  to have mapped and named it - a lighthouse is reliably present (it's a
  navigation aid, independently useful to chart), but a historic wreck site
  or midden is hit-or-miss.

None of this is a defect this plugin can fix by itself - it is a direct
report of what the underlying map data contains. Wikipedia enrichment (the
first sentence of the linked article, for features that carry an OSM
`wikipedia` tag) adds useful context to the subset of features that have it,
but does not fill in a category with no OSM tagging in a given area at all.

This plugin queries the public `overpass-api.de` by default. If your
network refuses it, open its settings (**Settings → Tiles → Nearby →
osm-overpass's gear icon**) and point **Overpass server** at a mirror
instead - see [configuration.md](configuration.md#overpass) and the
[plugin's own README](../examples/poi-plugins/osm-overpass/README.md#pointing-at-an-overpass-mirror)
for the allowlist a mirror other than `overpass.openstreetmap.fr` also
needs.

## Google Places

`google-places` (`docs/examples/poi-plugins/google-places`) trades free/
keyless for Google's business and place database, at the cost of narrower
category coverage: only `marina`, `historic`, `viewpoint` and `trail` map
onto a real Google Places type. See that plugin's README for the exact type
mapping and the cost/billing implications of switching to it.

## Why a fixed category list

Every provider answers the same eleven categories, translated into whatever
vocabulary its own upstream data source uses, rather than each provider
exposing its own native category list. This is what lets the operator switch
providers in Settings without the Nearby tile's category filter changing
meaning underneath them, and it is why a provider with no equivalent for a
category reports it as unsupported instead of inventing a mapping that
doesn't really fit.
