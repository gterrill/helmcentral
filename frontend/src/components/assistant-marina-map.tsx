import { lazy, Suspense, useRef } from 'react'

import { useNearViewport } from '@/hooks/use-near-viewport'
import type { MarinaCard } from '@/lib/mate-cards'

// The map pulls in maplibre-gl and react-map-gl at module scope. Behind a
// dynamic import that vendor chunk loads only when a reply carries a marina
// with something to draw, not for every Mate thread.
const AssistantMarinaMapImpl = lazy(() => import('./assistant-marina-map-impl'))

const NOTE = 'flex h-full items-center justify-center text-[10px] uppercase tracking-[0.14em] text-muted-foreground'

/**
 * A long thread can hold many marina cards, and a browser keeps only a few
 * WebGL contexts alive, so a map is mounted only while its card is near the
 * visible area and dropped when it scrolls well away. The box keeps its size
 * either way, so the thread doesn't jump, and stays blank until the first
 * measurement so a card already on screen never flashes the "far away" note.
 */
export function AssistantMarinaMap({ card }: { card: MarinaCard }) {
  const boxRef = useRef<HTMLDivElement>(null)
  const near = useNearViewport(boxRef)
  return (
    <div
      ref={boxRef}
      data-testid="marina-map"
      className="relative h-56 min-w-0 overflow-hidden rounded-md border border-border bg-muted/30 sm:h-72"
    >
      {near === null ? null : near ? (
        <Suspense
          fallback={
            <div className={NOTE} data-testid="marina-map-loading">
              Loading map…
            </div>
          }
        >
          <AssistantMarinaMapImpl card={card} />
        </Suspense>
      ) : (
        <div className={NOTE} data-testid="marina-map-idle">
          Map loads as you scroll to it
        </div>
      )}
    </div>
  )
}
