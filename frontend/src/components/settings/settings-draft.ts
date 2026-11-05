import type { DeepPartial, PassageLimitsPayload, SettingsPayload } from '@/hooks/use-settings-form'
import { emptyVesselRoles, type VesselRoles } from '@/lib/vessel-settings'

// Keep in step with defaultWebSearchModel in backend/assistant_web_search.go.
export const defaultWebSearchModel = 'google/gemini-2.5-flash-lite'

export type HullType = 'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat'

export type ScopeMethod = 'catenary' | 'ratio'

export type AssistantCostTier = '' | 'low' | 'medium' | 'high' | 'xhigh' | 'max'

/** A sector's no-go figures while editing: strings, so partial typing survives. '' is unset. */
export interface PassageSectorDraft {
  wind: string
  wave: string
}
export interface PassageLimitsDraft {
  ahead: PassageSectorDraft
  abeam: PassageSectorDraft
  astern: PassageSectorDraft
  /** Knots taken off the no-go wind while the stream runs against the wind. */
  tideOffset: string
  daylightArrival: boolean
}

const blankSector = (): PassageSectorDraft => ({ wind: '', wave: '' })
export const blankPassageLimits = (): PassageLimitsDraft => ({
  ahead: blankSector(),
  abeam: blankSector(),
  astern: blankSector(),
  tideOffset: '',
  daylightArrival: false,
})

const numberText = (n: number | null | undefined): string => (typeof n === 'number' ? String(n) : '')

function passageLimitsFromPayload(p: PassageLimitsPayload | undefined): PassageLimitsDraft {
  const sector = (s: PassageLimitsPayload['ahead']): PassageSectorDraft => ({
    wind: numberText(s?.wind_kn_no_go),
    wave: numberText(s?.wave_m_no_go),
  })
  return {
    ahead: sector(p?.ahead),
    abeam: sector(p?.abeam),
    astern: sector(p?.astern),
    tideOffset: numberText(p?.wind_against_tide_offset_kn),
    daylightArrival: p?.daylight_arrival === true,
  }
}

/**
 * A limit as typed: blank is null (never 0), one comma may stand for the
 * decimal point ("12,5"), and anything else that is not a plain number is NaN
 * so passageLimitErrors can say so.
 */
const parseLimit = (value: string): number | null => {
  const trimmed = value.trim()
  if (trimmed === '') return null
  const text = /^[^.,]*,[^.,]*$/.test(trimmed) ? trimmed.replace(',', '.') : trimmed
  return /^-?(\d+\.?\d*|\.\d+)$/.test(text) ? Number(text) : Number.NaN
}

/** The number to save. Unparseable text is null here; passageLimitErrors blocks Save before it is sent. */
const limitNumber = (value: string): number | null => {
  const n = parseLimit(value)
  return n === null || Number.isNaN(n) ? null : n
}

function passageLimitsToPayload(l: PassageLimitsDraft) {
  const sector = (s: PassageSectorDraft) => ({ wind_kn_no_go: limitNumber(s.wind), wave_m_no_go: limitNumber(s.wave) })
  return {
    ahead: sector(l.ahead),
    abeam: sector(l.abeam),
    astern: sector(l.astern),
    wind_against_tide_offset_kn: limitNumber(l.tideOffset),
    daylight_arrival: l.daylightArrival,
  }
}

/**
 * Same rules as the server: numbers above 0, and a wind against tide
 * reduction above 0 and below every sector's no-go wind. Keyed by field,
 * e.g. "abeam.wind", "astern.wave" or "tide".
 */
export function passageLimitErrors(draft: RegularSettingsDraft): Record<string, string> {
  const errors: Record<string, string> = {}
  const limits = draft.assistantPassageLimits
  const sectors = ['ahead', 'abeam', 'astern'] as const
  for (const sec of sectors) {
    const name = sec[0].toUpperCase() + sec.slice(1)
    for (const [field, label] of [['wind', `${name} wind`], ['wave', `${name} wave height`]] as const) {
      const n = parseLimit(limits[sec][field])
      if (Number.isNaN(n)) errors[`${sec}.${field}`] = `${label}: enter a number`
      else if (n !== null && n <= 0) errors[`${sec}.${field}`] = `${label}: enter more than 0`
    }
  }
  const offset = parseLimit(limits.tideOffset)
  if (Number.isNaN(offset)) errors.tide = 'Wind against tide: enter a number'
  else if (offset !== null && offset <= 0) errors.tide = 'Wind against tide: enter more than 0'
  else if (offset !== null) {
    for (const sec of sectors) {
      const noGo = limitNumber(limits[sec].wind)
      if (noGo !== null && offset >= noGo) {
        errors.tide = `Wind against tide: must be less than the ${sec} no-go wind (${noGo} kn)`
        break
      }
    }
  }
  return errors
}

function passageLimitsEqual(a: PassageLimitsDraft, b: PassageLimitsDraft): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}

function rolesEqual(a: VesselRoles | null, b: VesselRoles | null): boolean {
  if (a === null || b === null) return a === b
  return (Object.keys(emptyVesselRoles) as Array<keyof VesselRoles>).every((key) => a[key] === b[key])
}

function normalizeModelPatterns(values: string[] | undefined): string[] {
  if (!Array.isArray(values)) return []
  return values
    .map((value) => value.trim())
    .filter((value) => value.length > 0)
}

function normalizeAssistantCostTier(value: string | undefined): AssistantCostTier {
  const normalized = (value ?? '').trim().toLowerCase()
  if (normalized === '' || normalized === 'low' || normalized === 'medium' || normalized === 'high' || normalized === 'xhigh' || normalized === 'max') {
    return normalized
  }
  return ''
}

const parseNumber = (value: string, fallback: number) => {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

// Empty leaves the transducer depth unset (0). Anything typed is sent as it is
// read, and text that is not a number is sent as -1, so the server refuses it
// and names the field instead of the box quietly emptying.
const parseTransducerDepth = (value: string) => {
  if (value.trim() === '') return 0
  const parsed = Number(value.trim().replace(',', '.'))
  return Number.isFinite(parsed) ? parsed : -1
}

// gps_from_bow_m defaults to 0, meaning "no correction" - unlike every other
// numeric anchor field, 0 is the meaningful explicit value here, not an
// absent one, so parseNumber's `parsed > 0` guard would wrongly discard it.
const parseNonNegativeNumber = (value: string, fallback: number) => {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback
}

// ---------------------------------------------------------------------------
// The field table
// ---------------------------------------------------------------------------

/** The sections of the settings payload a draft field can sit in. `units` is a bare top-level value. */
type WireSection = Exclude<keyof SettingsPayload, 'units' | 'roles' | 'roles_error'>

/** Every place in the settings payload a draft field can sit. */
type PayloadPath =
  | { [S in WireSection]: { [N in keyof NonNullable<SettingsPayload[S]> & string]-?: readonly [S, N] }[keyof NonNullable<SettingsPayload[S]> & string] }[WireSection]
  | readonly ['units']

type ValueAt<P extends PayloadPath> = P extends readonly ['units']
  ? SettingsPayload['units']
  : P extends readonly [infer S extends WireSection, infer N extends string]
    ? N extends keyof NonNullable<SettingsPayload[S]> ? NonNullable<SettingsPayload[S]>[N] : never
    : never

/**
 * The payload paths that accept a value of type W. A field that writes a
 * string cannot be pointed at a number's path, and a name the payload does not
 * have is not a path at all. Two fields of the same type can still be swapped:
 * settings-draft-fields.test.ts pins the paths.
 */
type PathFor<W, P extends PayloadPath = PayloadPath> = P extends PayloadPath
  ? [W] extends [NonNullable<ValueAt<P>>] ? P : never
  : never

/**
 * One draft field's whole life: T is the draft's type for it and W what a save
 * sends.
 */
interface DraftField<T, W> {
  at: PathFor<W>
  /** The value before the server has answered, and when the server's value is absent or unusable. */
  initial: T
  /** The draft value for what the server sent; undefined keeps `initial`. */
  read: (raw: unknown) => T | undefined
  /** What a save sends for this draft value; the value itself when left out. */
  write?: (value: T) => W
  /** How two values compare for the unsaved-changes check; `===` when left out. */
  same?: (a: T, b: T) => boolean
}

/** Text. `trim` trims it on save; `nonEmpty` ignores an empty answer from the server (the starting value stays). */
const text = (at: PathFor<string>, initial: string, opts: { trim?: boolean, nonEmpty?: boolean } = {}): DraftField<string, string> => ({
  at,
  initial,
  read: (raw) => (typeof raw === 'string' && !(opts.nonEmpty && raw === '') ? raw : undefined),
  write: opts.trim ? (value) => value.trim() : undefined,
})

const flag = (at: PathFor<boolean>, initial: boolean): DraftField<boolean, boolean> => ({
  at,
  initial,
  read: (raw) => (typeof raw === 'boolean' ? raw : undefined),
})

/** One of a closed set of strings; anything else from the server keeps the starting value. */
const choice = <T extends string>(at: PathFor<string>, initial: NoInfer<T>, allowed: readonly T[]): DraftField<T, string> => ({
  at,
  initial,
  read: (raw) => allowed.find((option) => option === raw),
})

/**
 * A number kept as text while editing. Text that is not a number saves as the
 * starting value, so each default is written once. `nonNegative` lets 0 stand
 * for itself; otherwise only numbers above 0 do.
 */
const numberField = (at: PathFor<number>, initial: string, range: 'positive' | 'nonNegative'): DraftField<string, number> => {
  const fallback = Number(initial)
  return {
    at,
    initial,
    read: (raw) => (typeof raw === 'number' ? String(raw) : undefined),
    write: (value) => (range === 'positive' ? parseNumber(value, fallback) : parseNonNegativeNumber(value, fallback)),
  }
}

const port = (at: PathFor<number>, initial: string): DraftField<string, number> => ({
  at,
  initial,
  read: (raw) => (typeof raw === 'number' ? String(raw) : undefined),
  write: (value) => Number.parseInt(value, 10) || Number(initial),
})

const modelList = (at: PathFor<string[]>): DraftField<string[], string[]> => ({
  at,
  initial: [],
  read: (raw) => normalizeModelPatterns(raw as string[] | undefined),
  write: normalizeModelPatterns,
  same: (a, b) => a.length === b.length && a.every((item, i) => item === b[i]),
})

/** Gives a field with rules of its own the types its table entry is read by. */
const ownRules = <T, W>(field: DraftField<T, W>) => field

/**
 * Adding a plain setting is one entry here and its type in the payload
 * (use-settings-form.ts). The draft's type is read off this table. Entries
 * marked "own rules" write their `read` and `write` out.
 */
const draftFields = {
  signalkAddress: text(['signalk', 'address'], 'localhost', { trim: true, nonEmpty: true }),
  signalkPort: port(['signalk', 'port'], '3000'),

  vesselPrefix: text(['boat', 'vessel_prefix'], '', { trim: true }),
  boatModel: text(['boat', 'model'], '', { trim: true }),

  distanceUnits: choice(['units'], 'metric', ['metric', 'imperial']),

  tideStationId: text(['ui', 'tide_station_id'], ''),
  tideStationName: text(['ui', 'tide_station_name'], ''),
  tideAutoStation: flag(['ui', 'tide_auto_station'], false),

  bowRollerHeightM: numberField(['anchor', 'bow_roller_height_m'], '1.5', 'positive'),
  // 0 is a meaningful explicit value ("no correction"), not an absent one.
  gpsFromBowM: numberField(['anchor', 'gps_from_bow_m'], '0', 'nonNegative'),
  loaM: numberField(['anchor', 'loa_m'], '0', 'positive'),
  // How far below the waterline the depth transducer sits; '' leaves it unset
  // (no default). Own rules: the server sends 0 for unset and the box stays
  // empty then; see parseTransducerDepth.
  transducerDepthM: ownRules<string, number>({
    at: ['anchor', 'transducer_depth_m'],
    initial: '',
    read: (raw) => (typeof raw === 'number' && raw > 0 ? String(raw) : undefined),
    write: parseTransducerDepth,
  }),
  chainSizeMm: numberField(['anchor', 'chain_size_mm'], '12', 'positive'),
  chainOnboardM: numberField(['anchor', 'chain_onboard_m'], '150', 'positive'),
  hullType: choice<HullType>(['anchor', 'hull_type'], 'power_cat', ['power_cat', 'sail_mono', 'power_mono', 'sail_cat']),
  scopeMethod: choice<ScopeMethod>(['anchor', 'scope_method'], 'ratio', ['catenary', 'ratio']),
  windageAreaM2: numberField(['anchor', 'windage_area_m2'], '35', 'positive'),
  // ADR 0099: gates the server-side anchor auto-raise watcher. Matches the
  // server's own default (buildSettingsPayload): the feature ships on until
  // the operator explicitly turns it off. An explicit false hydrates as false,
  // not as "absent". A plain boolean: no partial typing to preserve.
  autoRaiseOnMotoring: flag(['anchor', 'auto_raise_on_motoring'], true),
  // ADR 0135: water the operator wants under the keel at the next low tide
  // before Anchor Watch's low-water clearance warning fires. Matches the
  // server's own default: a generic 0.5m seamanship margin, not
  // vessel-specific. 0 is a meaningful explicit value, not an absent one.
  minClearanceAtLowM: numberField(['anchor', 'min_clearance_at_low_m'], '0.5', 'nonNegative'),

  influxdbEnabled: flag(['influxdb', 'enabled'], false),
  influxdbUrl: text(['influxdb', 'url'], '', { trim: true }),
  influxdbOrg: text(['influxdb', 'org'], '', { trim: true }),
  influxdbBucket: text(['influxdb', 'bucket'], '', { trim: true }),

  // Blank by default and left blank when unset, trimmed and never defaulted: a
  // plausible-looking default host would produce a confusing failure (the
  // overlay silently pointed at the wrong box) rather than an obvious one (no
  // address configured). An explicit blank from the server hydrates as blank.
  mayaraAddress: text(['mayara', 'address'], '', { trim: true }),
  mayaraPort: port(['mayara', 'port'], '6502'),

  authMode: choice<'none' | 'signalk'>(['auth', 'mode'], 'none', ['none', 'signalk']),

  assistantEnabled: flag(['assistant', 'enabled'], false),
  // A blank model on the server (nothing configured, or explicitly cleared)
  // hydrates as blank, not as the default model id.
  assistantModel: text(['assistant', 'model'], 'anthropic/claude-sonnet-4.5', { trim: true }),
  assistantDocumentModel: text(['assistant', 'document_model'], '', { trim: true }),
  assistantNotes: text(['assistant', 'notes'], ''),
  assistantAllowedModels: modelList(['assistant', 'allowed_models']),
  assistantExcludedModels: modelList(['assistant', 'excluded_models']),
  // Own rules: anything outside the known tiers reads and saves as ''.
  assistantCostTier: ownRules<AssistantCostTier, AssistantCostTier>({
    at: ['assistant', 'cost_tier'],
    initial: '',
    read: (raw) => normalizeAssistantCostTier(typeof raw === 'string' ? raw : undefined),
    write: normalizeAssistantCostTier,
  }),
  // Voice phase (ADR 0093): all three default false, same as assistantEnabled.
  assistantVoiceInput: flag(['assistant', 'voice_input'], false),
  assistantReadAloud: flag(['assistant', 'read_aloud'], false),
  assistantWakeWord: flag(['assistant', 'wake_word'], false),
  assistantVerboseRunLog: flag(['assistant', 'verbose_run_log'], false),
  assistantWebSearch: flag(['assistant', 'web_search'], false),
  assistantWebSearchModel: text(['assistant', 'web_search_model'], defaultWebSearchModel, { trim: true }),
  // Own rules: a nested block of text boxes; see passageLimitsFromPayload and passageLimitErrors.
  assistantPassageLimits: ownRules<PassageLimitsDraft, ReturnType<typeof passageLimitsToPayload>>({
    at: ['assistant', 'passage_limits'],
    initial: blankPassageLimits(),
    read: (raw) => passageLimitsFromPayload(raw as PassageLimitsPayload | undefined),
    write: passageLimitsToPayload,
    same: passageLimitsEqual,
  }),

  // Off until the operator turns it on: it downloads in the background.
  offlineCoastlineEnabled: flag(['offline_coastline', 'enabled'], false),
}

type DraftFields = typeof draftFields
type DraftKey = keyof DraftFields

/**
 * Local-form mirror of every field owned by the "regular" settings
 * sections (SignalK Connection, Boat & UI, Labels, Anchor, InfluxDB) plus
 * the Tide station fields that live visually in the Widgets > Tide tab but
 * are still plain form fields saved through the same pinned "Save
 * Settings" button, not provider-card auto-save. Numbers are kept as
 * strings while editing (matching the old panel's UX of allowing partial
 * numeric input), and only coerced to numbers when building the save
 * patch.
 *
 * Read off `draftFields`, plus `roles`.
 */
export type RegularSettingsDraft = {
  [K in DraftKey]: DraftFields[K]['initial']
} & {
  /**
   * null until the server's roles block has been read. It stays null if the
   * settings fetch fell back without one, and the patch then omits `roles`
   * so a save cannot wipe a setup this page never saw.
   */
  roles: VesselRoles | null
}

/**
 * The table in the one shape the loops below need: each field's functions are
 * only ever called with its own key's value, which `never` stands for.
 */
interface LoadedField {
  key: DraftKey
  at: PayloadPath
  initial: unknown
  read: (raw: unknown) => unknown
  write: (value: never) => unknown
  same: (a: never, b: never) => boolean
}

const identity = <V>(value: V) => value
const strictEqual = (a: unknown, b: unknown) => a === b

const draftFieldList: LoadedField[] = (Object.keys(draftFields) as DraftKey[]).map((key) => {
  const field = draftFields[key]
  return {
    key,
    at: field.at,
    initial: field.initial,
    read: field.read,
    write: field.write ?? identity,
    same: field.same ?? strictEqual,
  }
})

const readWire = (settings: SettingsPayload, at: PayloadPath): unknown => {
  if (at.length === 1) return settings[at[0]]
  return (settings[at[0]] as Record<string, unknown> | undefined)?.[at[1]]
}

const writeWire = (patch: Record<string, unknown>, at: PayloadPath, value: unknown): void => {
  if (at.length === 1) {
    patch[at[0]] = value
    return
  }
  const [section, name] = at
  const block = (patch[section] ??= {}) as Record<string, unknown>
  block[name] = value
}

export const initialRegularSettingsDraft: RegularSettingsDraft = {
  ...(Object.fromEntries(draftFieldList.map((field) => [field.key, field.initial])) as Omit<RegularSettingsDraft, 'roles'>),
  roles: null,
}

/** Builds the draft's starting values from a freshly-fetched settings payload. */
export function hydrateDraftFromSettings(settings: SettingsPayload): RegularSettingsDraft {
  const draft: Record<string, unknown> = { ...initialRegularSettingsDraft }

  for (const field of draftFieldList) {
    const value = field.read(readWire(settings, field.at))
    if (value !== undefined) draft[field.key] = value
  }

  if (settings.roles) draft.roles = { ...emptyVesselRoles, ...settings.roles }

  return draft as RegularSettingsDraft
}

/**
 * Deep-equality check for two RegularSettingsDraft values, used to derive
 * whether the draft actually differs from what's saved (rather than
 * tracking "was any field ever touched," which incorrectly stays dirty if a
 * field is edited and then changed back to its original value).
 */
export function draftsEqual(a: RegularSettingsDraft, b: RegularSettingsDraft): boolean {
  for (const field of draftFieldList) {
    if (!field.same(a[field.key] as never, b[field.key] as never)) return false
  }
  return rolesEqual(a.roles, b.roles)
}

/**
 * Builds the settings patch for the pinned "Save Settings" button. Note
 * this deliberately does NOT include `ui.tide_provider` /
 * `ui.weather_provider` / `ui.wave_provider` / `ui.poi_provider` /
 * `ui.place_name_provider` / `ui.forecast_warnings_provider`
 * — those are owned exclusively by the Widgets provider cards' own
 * immediate `save()` calls, and omitting them here means the settings
 * form's one-level-per-subobject merge (see `deepMergeSettings`) leaves
 * whatever is already persisted for them untouched.
 */
export function buildRegularSettingsPatch(draft: RegularSettingsDraft): DeepPartial<SettingsPayload> {
  const patch: Record<string, unknown> = {}
  for (const field of draftFieldList) {
    writeWire(patch, field.at, field.write(draft[field.key] as never))
  }
  // Omitted while the roles block was never read (see the draft field).
  if (draft.roles !== null) patch.roles = draft.roles
  return patch as DeepPartial<SettingsPayload>
}
