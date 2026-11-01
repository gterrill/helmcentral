import { apiRequest, invalidateQueries, useApiQuery } from '@/lib/api-query'

/**
 * Saved places (backend/saved_places.go): named positions the operator gave
 * Mate, which find_places answers from before it asks the map. Mate adds them
 * through its Apply card; this is where they are read, renamed, moved and
 * deleted.
 */
export interface SavedPlace {
  id: string
  name: string
  lat: number
  lon: number
  note: string
  /** Metres at chart datum; null when no depth is known. */
  depth_m: number | null
  depth_source: 'charted' | 'sounded' | ''
  /** The day the depth was taken or entered, YYYY-MM-DD; "" with no depth. */
  depth_date: string
  created_at: string
  updated_at: string
}

/**
 * A new depth comes with its own source and date and replaces the old; a
 * depth_m of null clears all three. The server refuses a source or date with
 * no depth_m.
 */
export interface SavedPlaceChange {
  name?: string
  lat?: number
  lon?: number
  note?: string
  depth_m?: number | null
  depth_source?: 'charted' | 'sounded'
  depth_date?: string
}

export interface NewSavedPlace {
  name: string
  lat: number
  lon: number
  note: string
  depth_m?: number
  depth_source?: 'charted' | 'sounded'
  depth_date?: string
}

export const SAVED_PLACES_PATH = '/api/saved-places'

const NO_PLACES: SavedPlace[] = []

async function send(path: string, init: Parameters<typeof apiRequest>[1]): Promise<void> {
  await apiRequest(path, init)
  await invalidateQueries([SAVED_PLACES_PATH])
}

const add = (place: NewSavedPlace) => send(SAVED_PLACES_PATH, { method: 'POST', json: place })
const change = (id: string, patch: SavedPlaceChange) => send(`${SAVED_PLACES_PATH}/${encodeURIComponent(id)}`, { method: 'PATCH', json: patch })
const remove = (id: string) => send(`${SAVED_PLACES_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })

export function useSavedPlaces() {
  const query = useApiQuery<{ places: SavedPlace[] }>({ queryKey: [SAVED_PLACES_PATH], path: SAVED_PLACES_PATH })

  return {
    places: query.data?.places ?? NO_PLACES,
    loading: query.isLoading,
    error: query.error?.message ?? null,
    refresh: query.refetch,
    add,
    change,
    remove,
  }
}
