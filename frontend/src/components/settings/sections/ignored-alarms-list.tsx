import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { FormSection } from '@/components/patterns'
import { useIgnoredAlarms } from '@/hooks/use-ignored-alarms'
import { formatIgnoredOn } from '@/lib/alarm-display'

/**
 * Network alarms the operator stopped showing from the alarms drawer
 * (backend/alarm_ignored_alarms.go), each with the date and a Restore. Rows
 * are added from the alarm cards; this is only where they are reviewed and
 * brought back.
 */
export function IgnoredAlarmsList() {
  const { alarms, loading, error, restore } = useIgnoredAlarms()
  const [restoring, setRestoring] = useState<string | null>(null)

  const handleRestore = async (kind: string) => {
    setRestoring(kind)
    try {
      await restore(kind)
    } catch (err) {
      toast.error('Could not restore this alarm', { description: err instanceof Error ? err.message : String(err) })
    } finally {
      setRestoring(null)
    }
  }

  return (
    <FormSection
      title="Ignored alarms"
      description="Alarms from other devices on the boat that you chose to stop showing. Restore one to see it again."
    >
      <div className="flex flex-col gap-1.5">
        {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
        {!loading && error && <p className="text-sm text-destructive" role="alert">{error}</p>}
        {!loading && !error && alarms.length === 0 && (
          <p className="text-sm text-muted-foreground">Alarms you stop showing from the alarms drawer appear here.</p>
        )}
        {alarms.map(({ kind, title, ignored_at }) => (
          <div key={kind} className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5">
            <div className="min-w-0">
              <p className="truncate text-sm text-foreground">{title}</p>
              {formatIgnoredOn(ignored_at) && (
                <p className="truncate text-xs text-muted-foreground">Ignored {formatIgnoredOn(ignored_at)}</p>
              )}
            </div>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              aria-label={`Restore ${title}`}
              disabled={restoring === kind}
              onClick={() => void handleRestore(kind)}
            >
              Restore
            </Button>
          </div>
        ))}
      </div>
    </FormSection>
  )
}
