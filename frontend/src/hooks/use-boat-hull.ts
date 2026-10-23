import { useMemo } from 'react'

import { useAppConfig } from '@/hooks/use-app-config'
import { useVesselParticulars } from '@/hooks/use-vessel-particulars'
import type { BoatHull } from '@/lib/boat-model'

/** The hull the passage replay draws: the type from Anchor Watch settings with
 * the vessel's length and beam, or null while no hull type is set. */
export function useBoatHull(): BoatHull | null {
  const { boatHull, anchor } = useAppConfig()
  const { particulars } = useVesselParticulars()
  // Length overall: the vessel details, or the Length Overall the operator
  // entered under Anchor Watch settings. Both are the boat's real length, so
  // the second is another source of the same figure, not a stand-in.
  const loa = particulars?.loa_m ?? (anchor.loaM > 0 ? anchor.loaM : null)
  const beam = particulars?.beam_m ?? null
  return useMemo(() => (boatHull ? { type: boatHull, loaM: loa, beamM: beam } : null), [boatHull, loa, beam])
}
