# ADR 0056: Place Names From OSM Features, Not the Nearest Town

## Status
Accepted

## Context

Anchored at Goldsmith Island, the position tile read "Lindeman Islands", a different island group about 29 km away. Inspection of `backend/geonames.go` found two independent causes.

**The cache cell was 56 km across.** The handler rounded position to 0.5 degrees to build its cache key:

```
Goldsmith anchorage -20.685, 149.145   -> key "-20.5,149.0"
Lindeman Island     -20.4467, 149.0353 -> key "-20.5,149.0"
separation 28.9 km, cell 56 km N-S by 52 km E-W
```

Both positions share a cache entry. Passing within a few nautical miles of Lindeman could therefore cause its name to be served at Goldsmith for the next hour.

**The query only ever returns populated places.** The handler called GeoNames' `findNearbyPlaceNameJSON` with `radius=300`. That endpoint returns feature class P only, towns and villages. Goldsmith Island is uninhabited national park, so it can never come back from that call at any radius. A 300 km radius on that endpoint isn't "what island am I near", it's "what's the nearest town", and Airlie Beach or the Lindeman resort will always win that contest from most of the Whitsundays.

Both the cache key and the query needed to change: a finer cache would still store town names, and a corrected query would still share results across a 56 km cell.

### What actually answers the question

Measured against the live Overpass API at both coordinates, widening the search ring in three steps:

| Query point | 400 m ring | 1500 m ring | 5000 m ring |
|---|---|---|---|
| Goldsmith anchorage | **Goldsmith Island** (sole hit) | Goldsmith Island, Farrier Island | not needed |
| Lindeman Island | empty | **Lindeman Island** (sole hit) | 13 candidates |

Three things fall out of that table:

1. A tight first ring gives an unambiguous, correct answer at both points. No ranking needed when there's one candidate.
2. Widening is mandatory, not an optimization. Overpass's `around:` filter measures distance to the element's *linework*, not containment. A point sitting well inside a wide bay or a large island's interior can be further from that island's mapped coastline than a 400 m ring reaches, which is exactly why Lindeman's own 400 m ring comes back empty even though the point is on the island.
3. Stopping at the first non-empty ring limits ambiguity. At 5000 m the Lindeman point returns 13 candidates, including Shaw, Pentecost, Brush, several bays and an unnamed islet, compared with one candidate at 1500 m.

For comparison, Nominatim's plain reverse geocode at the Goldsmith anchorage returns "Sir James Smith Group" at both zoom 14 and zoom 16, because on water it falls back to the nearest place *node* rather than the nearest polygon. Same centroid trap as GeoNames, different vendor. Overpass avoids it because `around:` does real geometric filtering server-side instead of nearest-point matching.

## Decision

### Source: OpenStreetMap via Overpass, GeoNames gone entirely

`backend/geonames.go` is deleted, not patched. `backend/place_name.go` replaces it. `GEONAMES_USERNAME` comes out of `knownSecretKeys` and `coreEnvSecretKeys`; any row already sitting in the secrets store for it is left orphaned rather than migrated, harmless on a single-operator boat.

### The ladder

```
[out:json][timeout:25];
(
  nwr["seamark:type"="anchorage"](around:R,lat,lon);
  nwr["natural"="bay"](around:R,lat,lon);
  nwr["place"~"^(island|islet|rock)$"](around:R,lat,lon);
);
out tags center 20;
```

`R` walks `{400, 1500, 5000}` metres, tightest first, and the first ring that returns a *named* match wins. An element with no `name` tag is discarded before ranking, so an unnamed islet in a wide ring can never be picked over a named one further out.

Within the winning ring, candidates rank anchorage, then bay, then island, then islet, then rock, an anchorage tag means a human already decided this is where you drop the hook, so it outranks the geography around it. Ties within a rank break on great-circle distance to the query point, using the existing `haversineMeters` (`backend/signalk.go:2113`). No second haversine implementation.

**`out tags center`, deliberately, not `out geom`.** Geometry for a mainland coastline relation can run to megabytes; the point of `around:` is that the geometric filtering already happened server-side. All the ranking step needs afterward is a representative point and the tags, which is what `center` gives for a way or relation. Node elements carry `lat`/`lon` directly instead of `center`, both shapes are handled.

### Overpass's rate limit doesn't look like a rate limit

Confirmed while capturing this feature's test fixtures: Overpass signals rate limiting with **HTTP 200 and an HTML body**, not a 4xx or a `Retry-After` header. The body contains `Dispatcher_Client::request_read_and_idx::rate_limited`. Checking `resp.StatusCode` alone is not sufficient, a 200 with an unparseable body has to be distinguished from a 200 with zero legitimate `elements`, or a rate-limited response reads identically to "nothing out there" and silently degrades accuracy instead of surfacing as a failure. `fetchOverpassRing` tries to parse the body as JSON first; if that fails and the body looks like the rate-limit page (HTML content type, or the marker string), it logs that condition explicitly and returns an error, rather than falling through to "no name found."

The fetch is injected through a small `overpassFetcher` interface, mirroring the existing `tileFetcher` pattern (`backend/tile_cache.go:60`), so tests supply canned fixture bytes instead of hitting the real API. `*http.Client` satisfies the interface directly. Requests are POSTed with a `data=` form body, a descriptive User-Agent, and `http.NewRequestWithContext` against a real timeout (20 s; Overpass is not fast). That last part also means the old `//nolint:noctx` suppression in `geonames.go` disappears rather than moving somewhere else.

### Cache: successes only

The cache cell shrinks from the old 0.5 degrees (~56 km) to 0.005 degrees (~550 m), matching the tightest ring so a lookup is good for about 550 m of travel. Entries carry a 24 h TTL, names don't change, the TTL exists to bound memory, not to chase staleness, and the map is capped at 512 entries, evicting the oldest on insert, since a finer grid accumulates far more distinct cells over a day's cruising than the old coarse one ever did.

**Only a successful resolution gets cached.** The old code cached whatever it got back, including a blank on a failed GeoNames call, which then pinned "no name" for the full TTL. That's exactly the masking fallback AGENTS.md rules out: an upstream hiccup shouldn't look identical to "there's genuinely nothing here." A failed lookup here returns `""` without writing to the cache, so the next tick just tries again.

### Resolution moves onto the server's own poll tick

`GET /api/place-name` used to make its own live GeoNames call on every request, gated by a cache that only warmed up because the frontend happened to poll it regularly. That's the coupling documented in the old file: `cachedPlaceName` (used by the nearby-vessel contact recorder) depended on the frontend's polling cadence to keep anything in the cache at all, so nearby-vessel geotagging was best-effort at the mercy of whether a browser tab was open.

Now resolution runs once per server-owned 5 second tick, in `tracks.go`'s `sampleTracks`, alongside `recordNearbyVesselContacts` (ADR 0001: the server owns sampling, not the client). `/api/place-name` becomes a pure cache read with no HTTP call of its own.

**The tick starts resolution, it does not wait for it.** `startTrackPoller` drives `sampleTracks` sequentially on one goroutine (`for range ticker.C`), and Go drops ticks while that receiver is busy, so anything blocking in the tick stalls everything else the tick samples: wind, depth and solar history, self track points, the motoring trail, contact recording. The ladder makes that a real risk rather than a theoretical one, because a cache miss where the tight rings come back empty is three sequential Overpass calls at 20 seconds each. The measured Lindeman position is exactly that shape. So `updateTickPlaceName` answers from what is already known and hands the lookup to a background goroutine, behind a single-flight guard that stops a 5 second tick stacking goroutines against a resolution that slow. A plain mutex and flag, since there is no `golang.org/x/sync` dependency and this does not justify adding one.

The published name is tagged with the cache cell it was resolved for. A background lookup that lands after the vessel has already moved to another cell is discarded rather than displayed, and moving into an unresolved cell shows nothing rather than holding the previous cell's answer. Both are the reported bug in miniature: a name shown for somewhere the vessel is not. The nearby-vessel geoname becomes reliable instead of best-effort, since the tick resolves it directly rather than hoping a stale cache entry exists. `use-place-name.ts` and the `usePlaceName` hook signature don't change; they were already just polling an endpoint.

### Anchor-bound name

`anchorWatchData` gains `PlaceName string`. `setAnchorWatch` starts resolution through the same single-flight guard without delaying the anchor-drop response, then persists the result via the existing `saveAnchorWatch`. A resolution failure is logged and leaves the field empty; the regular tick keeps retrying at the anchor's position until it succeeds.

Once a name is pinned, both the tick and `/api/place-name` serve `anchorWatchState.PlaceName` and skip live resolution entirely for the duration of that watch. Without this, the name would drift as the boat swings on its rode and occasionally crosses into a neighboring OSM feature's tighter ring, which is worse than picking one name and holding it, the whole point of anchoring somewhere is that you're not moving to a different place.

### No fallback chain between sources

There is exactly one source. GeoNames is not kept as a second attempt when Overpass fails, and Overpass is not tried at a wider radius as a fallback when the tight ring comes up empty in a way that looks like failure. Per AGENTS.md's fallback policy, a failed lookup surfaces as no name and retries on the next tick; it does not silently drop to a worse, differently-biased source. Two providers with different centroid heuristics (GeoNames' town-node bias, Overpass's ring-based approach) disagreeing with each other would be strictly harder to reason about than one provider being temporarily unavailable, and would reintroduce the same kind of coordinate-dependent extra behavior this ADR exists to remove.

## Consequences

- Goldsmith and Lindeman resolve to distinct, correct names, and the anchor tile stops changing while swinging at anchor.
- Overpass is a public, shared instance. Rate limiting is real, it fired once during fixture capture at roughly four queries in a minute, and the public endpoint allows 2 concurrent slots per IP. This feature's steady-state rate is at most one query per ~550 m of travel, comfortably inside fair use. On failure the tile shows no name and retries rather than caching a blank.
- Ranking is a heuristic, sound when the tightest hit ring returns exactly one candidate, which both measured test coordinates do. Confidence drops when only the widest ring hits and several candidates tie on rank. A manual override for this case is deferred, out of scope here.
- OSM coverage is uneven outside well-mapped waters. Queensland, where this was measured, is well mapped; a mid-ocean or poorly-surveyed anchorage may legitimately return nothing at any ring, which is the correct behavior for genuinely sparse data, not a bug to work around with a fallback source.
- The previously-saved plugin-extraction plan (`~/.claude/plans/let-s-continue-extracting-plugins-wondrous-waterfall.md`) was written around GeoNames as the provider to extract. Its structure still applies to a future WASM-provider extraction, but the specific provider it describes no longer exists after this change.
- `docs/adr/0055-host-derived-vessel-paths.md` already claimed 0055; this is 0056, not the 0055 an earlier draft of this plan assumed.

## Related
- ADR 0001: the server owns sampling, extended here to place-name resolution running on the same tick as nearby-vessel contact recording rather than being driven by client polling.
- ADR 0009 (GSHHG Coastline Fallback): a related instance of "a public dataset with real, measured limitations, used deliberately without a fallback chain that would mask those limitations."

## Verification

`cd backend && go test -short ./...` and `cd frontend && npm test` both pass. `go vet ./...` is clean. The `//nolint:noctx` suppression that lived at the old `geonames.go:94` is gone, not relocated, `fetchOverpassRing` uses `http.NewRequestWithContext` throughout.
