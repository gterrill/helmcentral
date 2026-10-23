import { lazy, Suspense } from 'react'

import type { OfflineCoastlineMapProps } from '@/components/offline-coastline-map-impl'

// The map pulls in maplibre-gl and react-map-gl at module scope. Behind a
// dynamic import that vendor chunk loads only when someone opens the Offline
// coastline settings section, not for every Settings page.
const OfflineCoastlineMapImpl = lazy(() => import('./offline-coastline-map-impl'))

export function OfflineCoastlineMap(props: OfflineCoastlineMapProps) {
  return (
    <Suspense
      fallback={
        <div
          className="flex h-full items-center justify-center text-[10px] uppercase tracking-[0.14em] text-muted-foreground"
          data-testid="offline-coastline-map-loading"
        >
          Loading map…
        </div>
      }
    >
      <OfflineCoastlineMapImpl {...props} />
    </Suspense>
  )
}
