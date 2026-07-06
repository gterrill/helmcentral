import { useVesselIdentity } from '@/hooks/use-vessel-identity'

export function MarineHeader() {
  const { vesselStatus, boatName, boatModel } = useVesselIdentity()

  const vesselNameLabel = boatName ?? 'VESSEL NAME NOT SET'
  const vesselModelLabel = boatModel ?? 'MODEL NOT SET'
  const statusText = `${vesselModelLabel} · ${vesselStatus}`

  return (
    <header className="rounded-xl border bg-card/90 shadow-sm backdrop-blur-sm">
      <div className="flex min-h-16 items-center gap-2 px-2 py-2 md:px-4">
        <div className="flex min-w-0 shrink flex-col">
          <p className="truncate font-display text-[1.28rem] leading-none tracking-[0.12em] text-primary md:text-[1.45rem] lg:text-[1.7rem]">
            {vesselNameLabel}
          </p>
          <p className="truncate text-[9px] font-medium uppercase tracking-[0.08em] text-muted-foreground md:text-[10px] lg:text-xs">
            {statusText}
          </p>
        </div>
      </div>
    </header>
  )
}
