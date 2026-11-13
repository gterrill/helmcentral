import { createContext, Suspense, useContext, useRef, useState, type ReactNode } from 'react'

import type { CardMapName } from '@/components/mate-card-map'
import { useNearViewport } from '@/hooks/use-near-viewport'

type Expanded = readonly [boolean, (expanded: boolean) => void]
const ExpandedContext = createContext<Expanded | null>(null)

/**
 * Whether the card's map is open full screen. A LazyCardMap owns the state, so
 * it keeps the map mounted however far the card scrolls; outside one the card
 * map keeps it for itself.
 */
export function useCardMapExpanded(): Expanded {
  const own = useState(false)
  return useContext(ExpandedContext) ?? own
}

const NOTE = 'flex h-full items-center justify-center text-[10px] uppercase tracking-[0.14em] text-muted-foreground'

/**
 * The box a Mate card's map sits in. A long thread can hold many cards, and a
 * browser keeps only a few WebGL contexts alive, so the map (`children`, a
 * lazy component: maplibre-gl and react-map-gl load only when one is first
 * mounted) is mounted only while the card is near the visible area and
 * dropped when it scrolls well away. The box keeps its size either way, so the
 * thread doesn't jump, and stays blank until the first measurement so a card
 * already on screen never flashes the "far away" note.
 */
export function LazyCardMap({ name, className, children }: { name: CardMapName; className: string; children: ReactNode }) {
  const boxRef = useRef<HTMLDivElement>(null)
  const near = useNearViewport(boxRef)
  const expandedState = useState(false)
  const [expanded] = expandedState
  return (
    <ExpandedContext.Provider value={expandedState}>
      <div ref={boxRef} data-testid={`${name}-map`} className={`relative min-w-0 overflow-hidden rounded-md border border-border bg-muted/30 ${className}`}>
        {near === null ? null : near || expanded ? (
          <Suspense
            fallback={
              <div className={NOTE} data-testid={`${name}-map-loading`}>
                Loading map…
              </div>
            }
          >
            {children}
          </Suspense>
        ) : (
          <div className={NOTE} data-testid={`${name}-map-idle`}>
            Map loads as you scroll to it
          </div>
        )}
      </div>
    </ExpandedContext.Provider>
  )
}
