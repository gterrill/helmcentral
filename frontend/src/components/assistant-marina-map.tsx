import { lazy, Suspense } from 'react'

import type { MarinaCard } from '@/lib/mate-cards'

// The map pulls in maplibre-gl and react-map-gl at module scope. Behind a
// dynamic import that vendor chunk loads only when a reply carries a marina
// with something to draw, not for every Mate thread.
const AssistantMarinaMapImpl = lazy(() => import('./assistant-marina-map-impl'))

export function AssistantMarinaMap({ card }: { card: MarinaCard }) {
  return (
    <div data-testid="marina-map" className="relative h-56 min-w-0 overflow-hidden rounded-md border border-border bg-muted/30 sm:h-72">
      <Suspense
        fallback={
          <div
            className="flex h-full items-center justify-center text-[10px] uppercase tracking-[0.14em] text-muted-foreground"
            data-testid="marina-map-loading"
          >
            Loading map…
          </div>
        }
      >
        <AssistantMarinaMapImpl card={card} />
      </Suspense>
    </div>
  )
}
