import { Tile } from 'helmcentral-dashboard'
import { Wind, Anchor, Gauge } from 'lucide-react'

export function WindSpeed() {
  return (
    <Tile title="Wind Speed" icon={<Wind />} className="w-72">
      <div className="mt-2 flex items-end gap-2">
        <p className="font-display text-5xl leading-none">14</p>
        <p className="pb-1 text-lg font-semibold text-muted-foreground">kts</p>
      </div>
      <p className="mt-1 text-sm text-muted-foreground">Gusting 19kts · SW 225°</p>
    </Tile>
  )
}

export function AnchorWatch() {
  return (
    <Tile title="Anchor Watch" icon={<Anchor />} className="w-72">
      <div className="mt-2 flex items-end gap-2">
        <p className="font-display text-5xl leading-none">12</p>
        <p className="pb-1 text-lg font-semibold text-muted-foreground">m radius</p>
      </div>
      <p className="mt-1 text-sm text-muted-foreground">Holding within drift zone</p>
    </Tile>
  )
}

export function WithTitleExtra() {
  return (
    <Tile
      title="Speed Over Ground"
      icon={<Gauge />}
      className="w-72"
      titleExtra={<span className="text-xs text-muted-foreground">Live</span>}
    >
      <div className="mt-2 flex items-end gap-2">
        <p className="font-display text-5xl leading-none">6.4</p>
        <p className="pb-1 text-lg font-semibold text-muted-foreground">kts</p>
      </div>
      <p className="mt-1 text-sm text-muted-foreground">Heading 048° True</p>
    </Tile>
  )
}
