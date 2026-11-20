import { ApiError, apiRequest, invalidateQueries, setQueryData, useApiQuery } from '@/lib/api-query'

// The facts about the boat that live instrument data does not carry: builder,
// hull, registration and so on. One record, read and replaced whole. Name,
// MMSI, call sign and the main dimensions are NOT here; they stay with the
// live instrument data and are shown beside these, read only.

export interface VesselParticulars {
  builder: string
  model: string
  year: number | null
  hin: string
  flag: string
  hailing_port: string
  hull_type: string
  hull_material: string
  displacement_kg: number | null
  shore_power: string
  system_voltage: string
  registration: string
  imo: string
  epirb_id: string
  date_acquired: string
  /** Length overall and beam in metres, as the paperwork wants them. Live
   * instrument data supplies the length too when the boat publishes it. */
  loa_m: number | null
  beam_m: number | null
  /** Height of the helm above the waterline in metres, entered by the operator.
   * Blank means not set; there is no default. Sideways g at the helm needs it. */
  helm_height_m: number | null
  /** Owner and insurance: what every insurer declaration and berth
   * application asks again. */
  owner_name: string
  owner_phone: string
  owner_email: string
  insurer: string
  policy_number: string
  home_marina: string
  berth: string
  /** Who prepares the boat when the owner is away, with how to reach them. */
  storm_delegate: string
  updated_at: string | null
}

// A rejected value arrives as an ApiError carrying the server's own words,
// which are written for the operator and shown as they came. The record
// field the server refused, when it named one, rides along for the form.
export class ParticularsSaveError extends Error {
  readonly field: string | null
  constructor(message: string, field: string | null) {
    super(message)
    this.field = field
  }
}

const PATH = '/api/vessel/particulars'

/** The tallest helm height the server accepts, in metres (backend helmHeightMaxM). */
const HELM_HEIGHT_MAX_M = 30

/**
 * The server's own refusal for a helm height outside 0 to 30 m, found before
 * Save so the operator is not sent away to learn it. Blank is allowed (not
 * set); any precision above 0 is.
 */
export function helmHeightProblem(heightM: number | null): string | null {
  if (heightM === null) return null
  if (heightM > 0 && heightM <= HELM_HEIGHT_MAX_M) return null
  return `helm height must be above 0 and at most ${HELM_HEIGHT_MAX_M} metres`
}

/** Mate's Apply wrote the vessel details. */
export function vesselParticularsRecordApplied(): void {
  void invalidateQueries([PATH])
}

export async function saveVesselParticulars(particulars: VesselParticulars): Promise<VesselParticulars> {
  try {
    const saved = await apiRequest<VesselParticulars>(PATH, { method: 'PUT', json: particulars })
    setQueryData<VesselParticulars>([PATH], saved)
    return saved
  } catch (err) {
    if (err instanceof ApiError) {
      throw new ParticularsSaveError(err.message, Object.keys(err.fieldErrors ?? {})[0] ?? null)
    }
    throw err
  }
}

export function useVesselParticulars() {
  const { data, error, refetch } = useApiQuery<VesselParticulars>({ queryKey: [PATH], path: PATH })
  return { particulars: data ?? null, error: error?.message ?? null, reload: refetch }
}
