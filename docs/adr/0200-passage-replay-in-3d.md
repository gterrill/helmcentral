# ADR 0200: The passage replay can be watched in 3D

## Status

Accepted (2026-10-23). First of four steps toward a cinematic passage replay.

## Context

The replay is a flat map with a dot moving on it. The plan is to make it
something worth watching: relief under the track, a boat model, camera moves
that follow or orbit it, and signs for photos and notes along the way. It is
built in four steps, each shipped on its own: the 3D base (this ADR), the boat
model, the camera, and the signs.

Terrain is the base the rest stands on. MapLibre can drape the chart and the
satellite imagery over a raster elevation source, but the elevation data has to
come from somewhere, and it has to work from the boat's cache when the link is
poor.

## Decision

1. **Elevation comes from AWS Terrarium tiles, through the boat.**
   `GET /api/terrain/:z/:x/:y` fetches
   `elevation-tiles-prod/terrarium` tiles (to zoom 15, deeper requests
   clamped as the imagery proxy does), and keeps each processed tile in the
   existing tile cache under its own source key. It uses the same circuit
   breaker as the other proxies.
2. **The sea is clamped to 0 m on the server.** Terrarium carries bathymetry.
   Draped over it, the chart and the imagery sag into the seabed and the coast
   looks like a bowl. Every pixel below 0 m is rewritten to exactly 0 m before
   the tile is cached and served, so the sea is flat and only land rises.
3. **Exaggeration is 1.5**, with a faint hillshade layer under the seamarks and
   place names. Camera pitch defaults to 50 degrees when 3D comes on and the
   operator can tilt to 70 and rotate.
4. **A 3D button beside the satellite one**, on by default, remembered under its
   own key (`passages.replay3d`). Off is exactly the flat map from before: pitch
   0, north up, no terrain, rotation disabled. The overview map stays flat.
5. **Fail loud.** If any terrain tile fails, the map drops terrain, stays flat
   and shows "Terrain unavailable". The proxy answers 502 with
   `Cache-Control: no-store`; it never serves a flat or coarser stand-in, since
   a quietly wrong relief is worse than none. The camera levels out and the tilt and rotate gestures go with it. The terrain is tried again after 60 seconds, and the notice clears when its tiles load. Turning 3D off and on also starts afresh.
6. **The track draws progressively.** While the replay is off the start, each
   passage is drawn solid up to the boat (the full track cut at the cursor, ending
   exactly on the boat marker's position) and faint beyond. The old per-passage
   solid/faint switch applies only when not replaying. The satellite casing
   follows both parts.

## Rejected

- **Terrarium straight from the browser.** The bathymetry sag above, and no
  offline cache on the boat.
- **MapTiler terrain.** Needs an API key, which this project does not ship or
  ask operators for.
- **A silent flat fallback when tiles fail.** The operator would see a flat map
  and not know why the 3D button did nothing.

## Consequences

- The tile cache grows with terrain; the existing cache-clear endpoint clears it
  with the rest.
- Later steps build on a pitched, rotatable map: the boat model is a custom
  layer in this map, the camera moves drive pitch and bearing, and the signs sit
  on the track by time.

- A tilted camera loses the ends of a track that fitted flat, so the framing and the fly-in back the zoom off until every corner of the track is inside the padded view at the target pitch.
