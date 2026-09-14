export type HullType = 'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat'

export type ScopeMethod = 'catenary' | 'ratio'

export type AnchorConfig = {
  bowRollerHeightM: number
  chainSizeMm: number
  chainOnboardM: number
  hullType: HullType
  scopeMethod: ScopeMethod
  windageAreaM2: number
  gpsFromBowM: number
  loaM: number
}

export type MayaraConfig = {
  address: string
  port: number
}

// ADR 0093 voice phase: the three Settings → Mate → Voice switches, read
// live the same way distance units and the anchor geometry are (see
// hooks/use-app-config.ts) so hooks/use-mate-voice.ts and MateSheet see a
// save take effect immediately rather than needing a reload.
export type AssistantVoiceConfig = {
  voiceInput: boolean
  readAloud: boolean
  wakeWord: boolean
}

export type DistanceUnits = 'metric' | 'imperial'

export type UiConfig = {
  distanceUnits: DistanceUnits
  autoCloseAnchorWatchOnEngine: boolean
}

// Defaults, used until the backend's settings arrive and whenever it can't be
// reached. These were once overlaid with the repo-root settings.yaml read at
// build time, which both froze them at compile time and inlined the builder's
// private config into the bundle — see
// src/test/app-config-no-build-time-bake.test.ts. Operator values now come
// from GET /api/settings at runtime instead; see @/hooks/use-app-config.
export const fallbackUiConfig: UiConfig = {
  distanceUnits: 'metric' as DistanceUnits,
  autoCloseAnchorWatchOnEngine: true,
}

// How often the browser polls the backend for a fresh forecast. This is not
// a user-facing setting: upstream provider calls are already bounded
// independently by each plugin's own ttl_seconds() (open-meteo/weatherkit
// weather 900s, open-meteo-marine waves 3600s, nws warnings 1800s, bom
// warnings 5400s), so this constant only governs how quickly the UI notices
// data the backend has already refreshed. useWeatherToday/useTideToday also
// use this: the backend caches weather for 900s (weather_tide.go) and tide
// predictions change on the order of hours, so both are well inside this
// 600s ceiling — there is nothing weather- or tide-specific to tune here.
export const FORECAST_REFRESH_SECONDS = 600

// /api/anchor-watch's own record (position, radius, rode, sea/seabed state)
// only ever changes through an explicit operator action — dropping,
// repositioning, or editing it — and every one of those mutations already
// applies the server's response to local state immediately (use-anchor-watch.ts),
// so polling exists only to pick up a change made from another session and to
// notice the place name the backend pins shortly after a drop (place_name.go's
// resolveAndPinAnchorWatchPlaceName, retried on tracks.go's 5s track-poll tick
// until it resolves). The live drag alarm itself is computed server-side
// (backend/alarm_anchor.go) and delivered over the independent alarm channel,
// not this poll, so neither interval below gates alarm freshness.
//
// While a watch is set, poll on the same 5s cadence as that backend tick, so
// the pinned place name and any edit from another session land here about as
// fast as the backend itself produces them.
export const ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS = 5
// While idle, there is nothing to notice but a watch dropped elsewhere — rare
// for a single-operator boat — so this only needs to be "eventually", not fast.
export const ANCHOR_WATCH_IDLE_REFRESH_SECONDS = 60

// /api/place-name resolves a reverse geocode of the vessel's position, cached
// server-side per ~550m grid cell (backend/place_name.go's
// placeNameCacheCellDegrees) and refreshed on the same 5s track-poll tick that
// feeds the anchor watch's pinned name above. A vessel has to cover that whole
// cell before the name could change at all, which even at 20kt takes the
// better part of a minute — so there is no benefit to polling faster than this.
export const PLACE_NAME_REFRESH_SECONDS = 60

export const fallbackAnchorConfig: AnchorConfig = {
  bowRollerHeightM: 1.5,
  chainSizeMm: 12,
  chainOnboardM: 150,
  hullType: 'power_cat',
  scopeMethod: 'ratio',
  windageAreaM2: 35,
  gpsFromBowM: 0,
  loaM: 0,
}

// Blank address is the documented "not configured" state — see
// settings-draft.ts's mayaraAddress default for why an empty string, not a
// plausible-looking host, is what an unset radar server normalizes to.
export const fallbackMayaraConfig: MayaraConfig = {
  address: '',
  port: 6502,
}

export const fallbackAssistantVoiceConfig: AssistantVoiceConfig = {
  voiceInput: false,
  readAloud: false,
  wakeWord: false,
}

/** The subset of GET /api/settings this module reads. */
export type AppConfigSettings = {
  units?: string
  anchor?: {
    bow_roller_height_m?: number
    chain_size_mm?: number
    chain_onboard_m?: number
    hull_type?: string
    scope_method?: string
    windage_area_m2?: number
    gps_from_bow_m?: number
    loa_m?: number
  }
  mayara?: {
    address?: string
    port?: number
  }
  assistant?: {
    voice_input?: boolean
    read_aloud?: boolean
    wake_word?: boolean
  }
  boat?: {
    model?: string
  }
}

const HULL_TYPES: HullType[] = ['power_cat', 'sail_mono', 'power_mono', 'sail_cat']
const SCOPE_METHODS: ScopeMethod[] = ['catenary', 'ratio']

// Every field is validated rather than trusted: settings.yaml is hand-editable
// and the endpoint will faithfully return whatever it was given. An
// unrecognised or non-positive value falls back to the default for that field
// alone, so one bad key can't take out the rest of the config.
function positiveNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : fallback
}

// gps_from_bow_m defaults to 0, meaning "no correction" — unlike every other
// anchor field, 0 is itself the meaningful, valid value (not an absent one),
// so it must not be rejected the way positiveNumber rejects 0.
function nonNegativeNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : fallback
}

// A radar-server port has an upper bound a generic positiveNumber() doesn't
// enforce — out of range falls back to the default rather than propagating a
// value that could never be a real TCP port.
function validPort(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 && value <= 65535 ? value : fallback
}

export function normalizeUiConfig(settings: AppConfigSettings | null | undefined): UiConfig {
  const config: UiConfig = { ...fallbackUiConfig }

  const units = settings?.units
  if (typeof units === 'string') {
    const normalized = units.trim().toLowerCase()
    if (normalized === 'metric' || normalized === 'imperial') {
      config.distanceUnits = normalized
    }
  }

  return config
}

export function normalizeAnchorConfig(settings: AppConfigSettings | null | undefined): AnchorConfig {
  const anchor = settings?.anchor

  const hullType = typeof anchor?.hull_type === 'string'
    ? anchor.hull_type.trim().toLowerCase()
    : ''

  const scopeMethod = typeof anchor?.scope_method === 'string'
    ? anchor.scope_method.trim().toLowerCase()
    : ''

  return {
    bowRollerHeightM: positiveNumber(anchor?.bow_roller_height_m, fallbackAnchorConfig.bowRollerHeightM),
    chainSizeMm: positiveNumber(anchor?.chain_size_mm, fallbackAnchorConfig.chainSizeMm),
    chainOnboardM: positiveNumber(anchor?.chain_onboard_m, fallbackAnchorConfig.chainOnboardM),
    hullType: (HULL_TYPES as string[]).includes(hullType)
      ? (hullType as HullType)
      : fallbackAnchorConfig.hullType,
    scopeMethod: (SCOPE_METHODS as string[]).includes(scopeMethod)
      ? (scopeMethod as ScopeMethod)
      : fallbackAnchorConfig.scopeMethod,
    windageAreaM2: positiveNumber(anchor?.windage_area_m2, fallbackAnchorConfig.windageAreaM2),
    gpsFromBowM: nonNegativeNumber(anchor?.gps_from_bow_m, fallbackAnchorConfig.gpsFromBowM),
    loaM: positiveNumber(anchor?.loa_m, fallbackAnchorConfig.loaM),
  }
}

// settings.yaml is hand-editable and the endpoint returns whatever it was
// given, so a non-string address or a non-finite/out-of-range port falls
// back to the default for that field alone rather than propagating to the
// map — same per-field discipline as normalizeUiConfig/normalizeAnchorConfig.
export function normalizeMayaraConfig(settings: AppConfigSettings | null | undefined): MayaraConfig {
  const mayara = settings?.mayara

  const address = typeof mayara?.address === 'string' ? mayara.address.trim() : fallbackMayaraConfig.address
  const port = validPort(mayara?.port, fallbackMayaraConfig.port)

  return { address, port }
}

// Each switch defaults false independently on a non-boolean value, rather
// than falling the whole block back to defaults - same per-field discipline
// as normalizeAnchorConfig/normalizeMayaraConfig above.
export function normalizeAssistantVoiceConfig(settings: AppConfigSettings | null | undefined): AssistantVoiceConfig {
  const assistant = settings?.assistant

  return {
    voiceInput: typeof assistant?.voice_input === 'boolean' ? assistant.voice_input : fallbackAssistantVoiceConfig.voiceInput,
    readAloud: typeof assistant?.read_aloud === 'boolean' ? assistant.read_aloud : fallbackAssistantVoiceConfig.readAloud,
    wakeWord: typeof assistant?.wake_word === 'boolean' ? assistant.wake_word : fallbackAssistantVoiceConfig.wakeWord,
  }
}

// Read live by hooks/use-vessel-identity.ts (the "Vessel" header tile and the
// wall display's vessel identity), which used to poll /api/settings on its
// own timer just for this one field — folded into the settings this module
// already single-flights. Null (not '') is "not set", matching the tile's
// existing "MODEL NOT SET" placeholder check.
export function normalizeBoatModel(settings: AppConfigSettings | null | undefined): string | null {
  const model = settings?.boat?.model
  if (typeof model !== 'string') return null
  const trimmed = model.trim()
  return trimmed.length > 0 ? trimmed : null
}
