import { ChoiceList } from '@/components/patterns'
import { FieldDescription } from '@/components/ui/field'
import type { ZoneChoice } from '@/hooks/use-slot-zone-editing'
import type { TileZonePlan } from '@/lib/equipment-tiles'
import { cn } from '@/lib/utils'

interface ZoneConflictsProps {
  plan: TileZonePlan
  choices: Record<number, ZoneChoice>
  onChoose: (index: number, choice: ZoneChoice) => void
  /** Labels of readings whose zones this tile holds while it has no equipment. */
  heldWithoutItem: string[]
  className?: string
}

/**
 * Where the zones a converted tile still holds are going (ADR 0187). It says
 * what stays on the tile, what moves to the equipment's profile when the tile
 * is saved, and, where the profile already has zones of its own, shows both
 * sets and makes the operator pick one. Nothing is dropped without that choice.
 */
export function ZoneConflicts({ plan, choices, onChoose, heldWithoutItem, className }: ZoneConflictsProps) {
  const nothing = plan.moves.length === 0 && plan.conflicts.length === 0 && plan.stays.length === 0 && heldWithoutItem.length === 0
  if (nothing) return null
  return (
    <div data-testid="zone-migration" className={cn('grid gap-2 rounded-md border border-amber-500/40 bg-amber-50/60 p-3 text-sm dark:bg-amber-950/30', className)}>
      {heldWithoutItem.length > 0 && (
        <FieldDescription>
          This tile keeps its zones ({heldWithoutItem.join(', ')}) and they still raise their alarms. Choose the equipment
          and they move to its profile.
        </FieldDescription>
      )}
      {plan.stays.length > 0 && (
        <FieldDescription>
          Zones on {plan.stays.join(', ')} stay on this tile, and keep raising their alarms, until the equipment has a
          profile with a gauge for them. They move the first time you save this tile after that.
        </FieldDescription>
      )}
      {plan.moves.length > 0 && (
        <FieldDescription>
          Zones on {plan.moves.map((m) => m.label).join(', ')} move to the equipment's profile when you save, and
          then apply to every tile and alarm using it.
        </FieldDescription>
      )}
      {plan.conflicts.map((c) => (
        <div key={c.index} data-testid="zone-conflict">
          <ChoiceList
            // A question Save waits on, so it reads as one rather than as a field caption.
            label={<span className="text-sm font-medium normal-case tracking-normal text-foreground">{c.label}: two sets of zones</span>}
            value={choices[c.index]}
            onChange={(v) => onChoose(c.index, v)}
            choices={[
              { value: 'profile', label: "Keep the profile's", details: c.profileText },
              { value: 'tile', label: "Use this tile's, replacing the profile's", details: c.tileText },
            ]}
          />
        </div>
      ))}
    </div>
  )
}
