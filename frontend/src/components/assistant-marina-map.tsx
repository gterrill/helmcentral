import { lazy } from 'react'

import { LazyCardMap } from '@/components/lazy-card-map'
import type { MarinaCard } from '@/lib/mate-cards'

// The map pulls in maplibre-gl and react-map-gl at module scope. Behind a
// dynamic import that vendor chunk loads only when a reply carries a marina
// with something to draw, not for every Mate thread.
const AssistantMarinaMapImpl = lazy(() => import('./assistant-marina-map-impl'))

export function AssistantMarinaMap({ card }: { card: MarinaCard }) {
  return (
    <LazyCardMap name="marina" className="h-56 sm:h-72">
      <AssistantMarinaMapImpl card={card} />
    </LazyCardMap>
  )
}
