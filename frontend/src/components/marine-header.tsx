import { useVesselIdentity } from '@/hooks/use-vessel-identity'
import { Tile } from '@/components/ui/tile'
import { cn } from '@/lib/utils'

export function MarineHeader() {
  const { vesselStatus, boatName, boatModel } = useVesselIdentity()

  const vesselNameLabel = boatName ?? 'VESSEL NAME NOT SET'
  const vesselModelLabel = boatModel ?? 'MODEL NOT SET'

  return (
    <Tile title="Vessel" className="py-2">
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
          {' · '}
          {vesselStatus}
        </p>
      </div>
    </Tile>
  )
}
