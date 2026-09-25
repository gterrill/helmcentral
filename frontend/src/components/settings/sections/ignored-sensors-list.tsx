import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { FieldLegend, FieldSet } from '@/components/ui/field'
import { useIgnoredSensors } from '@/hooks/use-ignored-sensors'

/**
 * Sees and un-ignores the sensor-health checks' own exclusion list
 * (backend/alarm_ignored_sensors.go): every SignalK path or $source id an
 * "Ignore this sensor" action on the frozen/impossible/silent-source alarm
 * cards has added. Not a settings list to build up from scratch here - the
 * cards are where an entry gets added; this is only where it's reviewed and
 * removed.
 */
export function IgnoredSensorsList() {
  const { identifiers, loading, error, unignore } = useIgnoredSensors()
  const [removing, setRemoving] = useState<string | null>(null)

  const handleRemove = async (identifier: string) => {
    setRemoving(identifier)
    try {
      await unignore(identifier)
    } catch (err) {
      toast.error('Could not remove this sensor from the ignore list', { description: err instanceof Error ? err.message : String(err) })
    } finally {
      setRemoving(null)
    }
  }

  return (
    <FieldSet>
      <FieldLegend variant="label">Ignored sensors</FieldLegend>
      <p className="mt-1 text-[11px] text-muted-foreground">
        Sensors excluded from the frozen, impossible-reading and silent-source checks, added from each alarm
        card&apos;s own &quot;Ignore this sensor&quot; action.
      </p>
      <div className="mt-3 flex flex-col gap-1.5">
        {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
        {!loading && error && <p className="text-sm text-destructive" role="alert">{error}</p>}
        {!loading && !error && identifiers.length === 0 && (
          <p className="text-sm text-muted-foreground">No sensors are currently ignored.</p>
        )}
        {identifiers.map((identifier) => (
          <div key={identifier} className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5">
            <span className="min-w-0 truncate font-mono text-[11px] text-foreground">{identifier}</span>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={removing === identifier}
              onClick={() => void handleRemove(identifier)}
            >
              Remove
            </Button>
          </div>
        ))}
      </div>
    </FieldSet>
  )
}
