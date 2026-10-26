import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'
import type { TankLevelDefaults } from '@/lib/tank-levels'

/**
 * The warn and alarm levels each kind of tank uses unless its own equipment
 * page says otherwise (backend/tank_alarms.go, ADR 0214). Percentages of full.
 */
const PATH = '/api/alarms/tank-levels'

interface TankLevelsBody {
  fuel: { warn: number; alarm: number }
  water: { warn: number; alarm: number }
  waste: { warn: number; alarm: number }
}

export async function saveTankLevelDefaults(defaults: TankLevelDefaults): Promise<TankLevelDefaults> {
  const saved = await apiRequest<TankLevelsBody>(PATH, { method: 'PUT', json: defaults })
  setQueryData<TankLevelsBody>([PATH], saved)
  return saved
}

export function useTankLevelDefaults() {
  const { data, error } = useApiQuery<TankLevelsBody>({ queryKey: [PATH], path: PATH })
  return { defaults: data ?? null, error: error?.message ?? null }
}
