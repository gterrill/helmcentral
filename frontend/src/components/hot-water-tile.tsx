import { Flame } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'

export const HotWaterTile = memo(function HotWaterTile() {
  return (
    <Tile title="Hot Water" icon={<Flame className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="rounded-md border bg-background/60 px-3 py-2">
        <div className="flex items-center justify-between text-sm uppercase tracking-[0.16em] text-muted-foreground">
          <div className="inline-flex items-center gap-2">
            <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/40" />
            Hot Water
          </div>
          <span className="font-semibold">Off</span>
        </div>
        <div className="mt-2 grid grid-cols-4 gap-2">
          <Button variant="outline" size="sm" className="h-9 text-xs">1 HR</Button>
          <Button variant="outline" size="sm" className="h-9 text-xs">1.5 HR</Button>
          <Button variant="outline" size="sm" className="h-9 text-xs">2 HR</Button>
          <Button variant="outline" size="sm" className="h-9 text-xs">ON</Button>
        </div>
      </div>
    </Tile>
  )
})
