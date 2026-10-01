import { useCallback, useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'

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
  updated_at: string | null
}

// The server answers a rejected value with {"field","message"} and anything
// else with {"error"}. Both are written for the operator, so they are shown
// as they came; a bare status code is the last resort for a body that is
// neither.
export class ParticularsSaveError extends Error {
  /** The record field the server refused, when it named one. */
  readonly field: string | null
  constructor(message: string, field: string | null) {
    super(message)
    this.field = field
  }
}

async function failure(res: Response): Promise<ParticularsSaveError> {
  const body = (await res.json().catch(() => ({}))) as { error?: string; message?: string; field?: string }
  return new ParticularsSaveError(body.message ?? body.error ?? `HTTP ${res.status}`, body.field ?? null)
}

export async function fetchVesselParticulars(): Promise<VesselParticulars> {
  const res = await fetch(`${apiBaseUrl}/api/vessel/particulars`)
  if (!res.ok) throw await failure(res)
  return (await res.json()) as VesselParticulars
}

export async function saveVesselParticulars(particulars: VesselParticulars): Promise<VesselParticulars> {
  const res = await fetch(`${apiBaseUrl}/api/vessel/particulars`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(particulars),
  })
  if (!res.ok) throw await failure(res)
  return (await res.json()) as VesselParticulars
}

export function useVesselParticulars() {
  const [particulars, setParticulars] = useState<VesselParticulars | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setParticulars(await fetchVesselParticulars())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => { void load() }, [load])

  return { particulars, error, reload: load }
}
