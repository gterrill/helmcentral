import { Flame } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'

export const HotWaterTile = memo(function HotWaterTile() {
  return (
    <Tile title="Hot Water" className="@container" icon={<Flame className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="rounded-md border bg-background/60 px-3 py-2 @max-2xs:px-2">
        <div className="flex items-center justify-between gap-2 text-sm uppercase tracking-[0.16em] text-muted-foreground">
          <div className="inline-flex min-w-0 items-center gap-2">
            <span className="h-2.5 w-2.5 shrink-0 rounded-full bg-muted-foreground/40" />
            <span className="truncate">Hot Water</span>
          </div>
          <span className="shrink-0 font-semibold">Off</span>
        </div>
        {/* Four buttons in a row need 18rem; below that they sit two by two. */}
        <div className="mt-2 grid grid-cols-4 gap-2 @max-2xs:grid-cols-2">
          <Button variant="outline" size="sm" className="h-9 text-xs">1 HR</Button>
          <Button variant="outline" size="sm" className="h-9 text-xs">1.5 HR</Button>
          <Button variant="outline" size="sm" className="h-9 text-xs">2 HR</Button>
          <Button variant="outline" size="sm" className="h-9 text-xs">ON</Button>
        </div>
      </div>
    </Tile>
  )
})
