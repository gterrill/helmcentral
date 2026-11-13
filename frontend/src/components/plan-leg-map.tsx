import { lazy } from 'react'

import { LazyCardMap } from '@/components/lazy-card-map'
import type { PlanLeg, PlanStop } from '@/lib/plans'

// maplibre-gl loads only when a leg's map is first opened.
const PlanLegMapImpl = lazy(() => import('./plan-leg-map-impl'))

export interface PlanLegMapProps {
  from: PlanStop
  to: PlanStop
  leg: PlanLeg
}

/** A small map of one leg's routed path, to check the route runs where the operator means. */
export function PlanLegMap(props: PlanLegMapProps) {
  return (
    <LazyCardMap name="plan-leg" className="h-56 sm:h-64">
      <PlanLegMapImpl {...props} />
    </LazyCardMap>
  )
}
