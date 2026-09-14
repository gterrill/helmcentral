import { lazy, Suspense } from 'react'

import type { PoiMapTileProps } from '@/components/poi-map-tile-impl'

// Same kiosk bundle-split reasoning as assistant-markdown.tsx /
// manual-markdown.tsx: this widget imports maplibre-gl and react-map-gl at
// module scope, and every /kiosk page loaded that whole map-vendor chunk
// (1048 KB raw / 280 KB gzip) at startup even on pages with no map at all,
// because App.tsx mounts PoiMapTile eagerly for any dashboard that has a
// Nearby widget configured. Behind a dynamic import, map-vendor only loads
// once a page that actually renders this tile does.
const PoiMapTileImpl = lazy(() => import('./poi-map-tile-impl'))

export type { PoiMapTileProps }

export function PoiMapTile(props: PoiMapTileProps) {
  return (
    <Suspense
      fallback={
        <div
          className="flex h-full min-h-[160px] items-center justify-center rounded-xl border bg-card text-[10px] uppercase tracking-[0.14em] text-muted-foreground"
          data-testid="poi-map-tile-loading"
        >
          Loading map…
        </div>
      }
    >
      <PoiMapTileImpl {...props} />
    </Suspense>
  )
}
