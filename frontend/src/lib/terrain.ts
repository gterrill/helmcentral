// Terrain for the 3D passage replay. The boat serves the elevation tiles with
// the sea flattened to 0 m, so the chart and the imagery draped over the relief
// stay level on the water instead of sagging into the bathymetry.

export const TERRAIN_SOURCE_ID = 'terrain-dem'
export const TERRAIN_TILES = '/api/terrain/{z}/{x}/{y}'
export const TERRAIN_MAX_ZOOM = 15
export const TERRAIN_EXAGGERATION = 1.5

/** Camera pitch when 3D comes on, and the most the operator can tilt to. */
export const REPLAY_3D_PITCH = 50
export const REPLAY_3D_MAX_PITCH = 70

export const TERRAIN_ATTRIBUTION = 'Terrain: Mapzen, NASADEM, SRTM, GEBCO and others (AWS Open Data)'

/** The raster-dem source spec, shared by the relief and the hillshade layer. */
export const terrainSource = {
  type: 'raster-dem' as const,
  tiles: [TERRAIN_TILES],
  encoding: 'terrarium' as const,
  tileSize: 256,
  maxzoom: TERRAIN_MAX_ZOOM,
  attribution: TERRAIN_ATTRIBUTION,
}

// MapLibre paints with literal colours. Neutral and faint, so the shading
// reads on both the light and the dark basemap and over imagery.
const HILLSHADE_SHADOW = '#1f2933'
const HILLSHADE_HIGHLIGHT = '#ffffff'

export const hillshadePaint = {
  'hillshade-exaggeration': 0.35,
  'hillshade-shadow-color': HILLSHADE_SHADOW,
  'hillshade-highlight-color': HILLSHADE_HIGHLIGHT,
  'hillshade-accent-color': HILLSHADE_SHADOW,
}

/** The terrain setting for the map, or undefined for a flat map. */
export const terrainSetting = { source: TERRAIN_SOURCE_ID, exaggeration: TERRAIN_EXAGGERATION }

// The sky over the tilted 3D map, so the area above the horizon fades from the
// horizon colour into the sky instead of standing blank. Literal colours, as
// for the hillshade: MapLibre paints with them directly.
const sky = (skyColour: string, horizon: string) => ({
  'sky-color': skyColour,
  'horizon-color': horizon,
  'fog-color': horizon,
  'sky-horizon-blend': 0.6,
  'horizon-fog-blend': 0.8,
  'fog-ground-blend': 0.2,
})
export const SKY_DAY = sky('#8fb8d8', '#dfe9ef')
export const SKY_NIGHT = sky('#0a1826', '#1a3045')
export const skyFor = (dark: boolean) => (dark ? SKY_NIGHT : SKY_DAY)

/** Where the elevation tiles come from, upstream of the boat's proxy. The blog
 * card asks for them directly; its reader's browser never needs the boat. */
export const TERRARIUM_UPSTREAM_TILES = 'https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png'
