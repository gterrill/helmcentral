# ADR 0202: The replay boat is a model of the operator's hull

## Status

Accepted (2026-10-23). Second of four steps toward a cinematic passage replay,
after [ADR 0200](0200-passage-replay-in-3d.md).

## Context

With the land standing up in 3D, the dot that marks the boat looks out of place.
The replay should show a boat of the right kind, the right size, pointing the
way she is going. Helmcentral already knows the hull type (Anchor Watch
settings, one of power catamaran, sailing monohull, power monohull or sailing
catamaran) and usually the length and beam (vessel particulars).

## Decision

1. **The boat is drawn in code, not loaded from a model file.** Four low-poly
   boats are built from three.js geometry: a sailing monohull (hull, deck, mast,
   sail), a sailing catamaran (two hulls, bridgedeck, mast, sail), a power
   monohull (hull, cabin) and a power catamaran (two hulls, bridgedeck, cabin and
   flybridge). There is nothing to source or license, no binary to download, and
   the hull is sized from the vessel's own length and beam. A light hull, a
   darker deck and dark windows read on the pale chart and over satellite water.
   No size is invented. The length comes from the vessel details, or the Length
   Overall entered under Anchor Watch settings (the same figure, so a second
   source rather than a stand-in). A missing beam is taken from a typical
   length-to-beam ratio for the hull type (about 3 for a monohull, 2 for a
   catamaran). With no length at all the model is drawn at the minimum size on
   screen at every zoom and never claims to be to scale. The console says once
   which figure is missing.
2. **No hull type, no model.** The model is drawn only for a hull type the
   operator has set. An empty or unrecognised one keeps the dot. The model also
   gives way to the dot when 3D is off or the terrain has failed, since the
   boat is then on a flat map and the model has nothing to stand on.
3. **three.js loads on demand.** The layer and three.js are a separate chunk,
   fetched with `import()` only when 3D is on, the map is up and a hull type is
   set. The main bundle does not grow. The dot stays until the chunk has loaded.
   If it cannot load, the console says so and the dot stays.
4. **A MapLibre custom layer** (3D rendering mode) draws into the map's own
   WebGL context, so the depth buffer and the camera are the map's. The boat
   stands at sea level, or on the terrain elevation where the map reports it
   (the sea is clamped flat). Each replay step hands the position and heading
   straight to the layer, which asks the map for a frame; React does not
   re-render it. The layer's three.js resources are disposed when it is
   removed, which happens when 3D goes off, the terrain fails, or the map goes.
   A change of map style re-adds it.
5. **Minimum size on screen.** The model is true size, but never drawn shorter
   than 40 px: below the zoom where the boat reaches that, it is enlarged by
   just enough. This is a pure function of zoom, latitude and length. With no
   length it is the minimum at every zoom. True size
   at zoom 16 would be a 12 m boat about 12 px long, too small to see, so the
   floor applies at every zoom rather than switching at zoom 16.
6. **The bow follows the course over ground.** The replay data carries
   positions and times, not a course, so the heading is the bearing of the
   track just sailed, averaged over chords of 30, 60 and 90 s (a circular mean,
   so 359 and 1 degrees give north). A chord that implies under 0.5 kn is
   ignored. With none left behind her, the way she is about to go is used, and
   only with that absent too is the last heading held. A held heading carries
   on only while playback moves forward within one passage; a seek, a backward
   scrub or the next passage starts from the track again, so a boat leaving
   the berth on a new passage points the way she sails. The model sits level,
   with no bob or heel.
7. **One layer, built once.** A map style change rebuilds the style without
   telling the layer, so the layer builds its renderer, lights and model once
   and reuses them when the map adds it again; they are disposed only on a real
   removal. After creating its renderer it clears three.js's state and marks
   MapLibre's state cache dirty. Each frame hands three.js MapLibre's current
   viewport, since the canvas may have been resized; MapLibre restores its own
   state after a custom layer, so nothing is put back afterwards. The terrain
   height under the boat is looked up when the position changes or terrain
   tiles arrive, not every frame; the sea is clamped to 0 m, so sea level and
   "no terrain tile yet" are the same height. If the layer cannot be re-added
   the console says so and the dot returns.

## Rejected

- **Bundled glTF models.** Sourcing and licensing, a heavier download, a fixed
  size that has to be rescaled to the vessel, and four binary files to keep.
- **three.js in the main bundle.** About 130 kB gzipped for a feature behind a
  toggle.
- **Heading from the next position as the replay moves.** It flips when the
  operator scrubs backwards. Reading the track by time does not.

## Consequences

- The hull type has to be set under Anchor Watch settings for the model to show.
  Because the settings default to a power catamaran when saved empty, a boat
  that has never chosen one may see that hull; the length and beam still come
  from the vessel.
- Later steps (camera follow, signs along the track) can use the same layer and
  the heading it holds.
