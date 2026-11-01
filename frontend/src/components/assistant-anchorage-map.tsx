import { lazy } from 'react'

import { LazyCardMap } from '@/components/lazy-card-map'
import type { AnchorageCard } from '@/lib/mate-cards'

// The map pulls in maplibre-gl and react-map-gl at module scope. Behind a
// dynamic import that vendor chunk loads only when the operator opens the map
// on an anchorage card, not for every Mate thread.
const AssistantAnchorageMapImpl = lazy(() => import('./assistant-anchorage-map-impl'))

export function AssistantAnchorageMap({ card }: { card: AnchorageCard }) {
  return (
    <LazyCardMap name="anchorage" className="h-64 sm:h-80">
      <AssistantAnchorageMapImpl card={card} />
    </LazyCardMap>
  )
}
