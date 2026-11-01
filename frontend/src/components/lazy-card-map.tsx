import { Suspense, useRef, type ReactNode } from 'react'

import { useNearViewport } from '@/hooks/use-near-viewport'

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
export function LazyCardMap({ name, className, children }: { name: 'marina' | 'anchorage'; className: string; children: ReactNode }) {
  const boxRef = useRef<HTMLDivElement>(null)
  const near = useNearViewport(boxRef)
  return (
    <div ref={boxRef} data-testid={`${name}-map`} className={`relative min-w-0 overflow-hidden rounded-md border border-border bg-muted/30 ${className}`}>
      {near === null ? null : near ? (
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
  )
}
