import { memo } from 'react'

import { useAppConfig } from '@/hooks/use-app-config'
import { useVesselFields } from '@/hooks/use-vessel-state'
import { Tile } from '@/components/ui/tile'
import { cn } from '@/lib/utils'

// The header names the boat and says what it is doing. It reads those fields
// alone, so the clock's tick and a vessel-state tick that changes neither
// leave it as it is (ADR 0218).
const HEADER_FIELDS = ['boatName', 'navigationState'] as const

export const MarineHeader = memo(function MarineHeader() {
  const { boatName, navigationState } = useVesselFields(HEADER_FIELDS)
  const { boatModel } = useAppConfig()

  const vesselNameLabel = boatName ?? 'VESSEL NAME NOT SET'
  const vesselModelLabel = boatModel ?? 'MODEL NOT SET'

  return (
    <Tile title="Vessel">
      <div className="flex min-w-0 shrink flex-col">
        {/* An unset name is a placeholder, not data: DESIGN.md's zero-state
            rule bars styling the two the same way. A real name gets the hero
            display treatment; the unset string drops to a muted caption so it
            reads as "nothing configured yet," never as a vessel actually
            named this. */}
        <p
          className={cn(
            'truncate leading-none',
            boatName !== null
              ? 'font-display text-[1.28rem] tracking-[0.12em] text-primary md:text-[1.45rem] lg:text-[1.7rem]'
              : 'text-sm text-muted-foreground',
          )}
        >
          {vesselNameLabel}
        </p>
        <p className="truncate text-[9px] font-medium uppercase tracking-[0.08em] text-muted-foreground md:text-[10px] lg:text-xs">
          <span className={boatModel === null ? 'italic' : undefined}>{vesselModelLabel}</span>
          {/* Until the boat has reported its state the header says nothing
              about it: no status, and no separator pointing at one. */}
          {navigationState !== null && ` · ${navigationState}`}
        </p>
      </div>
    </Tile>
  )
})
