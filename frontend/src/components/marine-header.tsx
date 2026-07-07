import { useVesselIdentity } from '@/hooks/use-vessel-identity'
import { Tile } from '@/components/ui/tile'

export function MarineHeader() {
  const { vesselStatus, boatName, boatModel } = useVesselIdentity()

  const vesselNameLabel = boatName ?? 'VESSEL NAME NOT SET'
  const vesselModelLabel = boatModel ?? 'MODEL NOT SET'
  const statusText = `${vesselModelLabel} · ${vesselStatus}`

  return (
    <Tile title="Vessel" className="py-2">
      <div className="flex min-w-0 shrink flex-col">
        <p className="truncate font-display text-[1.28rem] leading-none tracking-[0.12em] text-primary md:text-[1.45rem] lg:text-[1.7rem]">
          {vesselNameLabel}
        </p>
        <p className="truncate text-[9px] font-medium uppercase tracking-[0.08em] text-muted-foreground md:text-[10px] lg:text-xs">
          {statusText}
        </p>
      </div>
    </Tile>
  )
}
