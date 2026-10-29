import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { FormSection } from '@/components/patterns'
import { useIgnoredSensors } from '@/hooks/use-ignored-sensors'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { formatIgnoredOn } from '@/lib/alarm-display'
import { selectTimeZone } from '@/lib/vessel-time'

/**
 * Sees and un-ignores the sensor-health checks' own exclusion list
 * (backend/alarm_ignored_sensors.go): every sensor an
 * "Ignore" action on the frozen/impossible/silent-source alarm
 * cards has added. Not a settings list to build up from scratch here - the
 * cards are where an entry gets added; this is only where it's reviewed and
 * removed.
 */
export function IgnoredSensorsList() {
  const timeZone = useVesselStateSelector(selectTimeZone)
  const { sensors, loading, error, unignore } = useIgnoredSensors()
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
    <FormSection
      title="Ignored sensors"
      description={'Sensors excluded from the frozen, impossible-reading and silent-source checks, added from each alarm card\'s own "Ignore this sensor" action.'}
    >
      <div className="flex flex-col gap-1.5">
        {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
        {!loading && error && <p className="text-sm text-destructive" role="alert">{error}</p>}
        {!loading && !error && sensors.length === 0 && (
          <p className="text-sm text-muted-foreground">No sensors are currently ignored.</p>
        )}
        {sensors.map(({ identifier, label, ignored_at }) => (
          <div key={identifier} className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5">
            <div className="min-w-0">
              <p className="truncate text-sm text-foreground">{label}</p>
              {formatIgnoredOn(ignored_at, timeZone) && (
                <p className="truncate text-xs text-muted-foreground">Ignored {formatIgnoredOn(ignored_at, timeZone)}</p>
              )}
            </div>
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
    </FormSection>
  )
}
