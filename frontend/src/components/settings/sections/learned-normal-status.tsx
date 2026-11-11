import { useState } from 'react'
import { toast } from 'sonner'

import { FormSection, LoadError } from '@/components/patterns'
import { Switch } from '@/components/ui/switch'
import { NORMAL_STATUS_PATH, useNormalStatus, type NormalStatus, type NormalStatusReading } from '@/hooks/use-normal-status'
import { useIgnoredSensors } from '@/hooks/use-ignored-sensors'
import { invalidateQueries } from '@/lib/api-query'
import { errorMessage } from '@/lib/errors'

/**
 * What the learned normal holds (ADR 0276), under the one-minute bucket field:
 * when it was learned and from where, then each engine's and the house bank's
 * readings with a switch and the automatic verdict beside it. The switch is the ignored-sensor list
 * (Settings, Alarms), so a dead sender is off everywhere at once. Readings
 * default to on; one that was repaired is picked up without a visit here.
 */
export function LearnedNormalStatus() {
  const { status, loading, error, refresh } = useNormalStatus()
  const { ignore, unignore, setIgnored } = useIgnoredSensors()
  const [busy, setBusy] = useState<string | null>(null)

  const flip = async (reading: NormalStatusReading, on: boolean) => {
    setBusy(reading.path)
    try {
      // A reading with several parts is switched through all of them in one request.
      if (reading.paths && reading.paths.length > 1) await setIgnored(reading.paths, !on)
      else if (on) await unignore(reading.path)
      else await ignore(reading.path)
      await invalidateQueries([NORMAL_STATUS_PATH])
    } catch (err) {
      toast.error('Could not change this reading', { description: errorMessage(err) })
    } finally {
      setBusy(null)
    }
  }

  return (
    <FormSection
      title="Learned normal"
      description="Helmcentral learns the usual readings of each engine at each rpm, and of the house bank for whatever is charging it, from their history. It suggests when a reading looks unusual for that engine or the bank. It is a suggestion, never an alarm."
    >
      <div className="flex min-w-0 flex-col gap-3" data-testid="learned-normal-status">
        {loading && !status && <p className="text-sm text-muted-foreground">Loading…</p>}
        {error && <LoadError reason={error} hasRows={Boolean(status)} onRetry={() => void refresh()} />}
        {status && <StatusBody status={status} busy={busy} onFlip={(r, on) => void flip(r, on)} />}
      </div>
    </FormSection>
  )
}

interface StatusBodyProps {
  status: NormalStatus
  busy: string | null
  onFlip: (reading: NormalStatusReading, on: boolean) => void
}

function StatusBody({ status, busy, onFlip }: StatusBodyProps) {
  return (
    <>
      {status.summary && <p className="min-w-0 text-sm text-foreground">{status.summary}</p>}
      {status.note && <p className="min-w-0 text-sm text-muted-foreground">{status.note}</p>}
      {status.subsystems.map((group) => (
        <div key={group.id} className="flex min-w-0 flex-col gap-3" data-testid="learned-normal-group">
          {/* A group whose one subject has the group's name (the house bank) shows it once. */}
          {!(group.subjects.length === 1 && group.subjects[0].name === group.title) && (
            <h3 className="text-sm font-semibold text-foreground">{group.title}</h3>
          )}
          {group.note && <p className="min-w-0 text-sm text-muted-foreground">{group.note}</p>}
          {group.subjects.map((subject) => (
            <div key={subject.id} className="flex min-w-0 flex-col gap-1">
              <p className="text-sm font-semibold text-foreground">
                {subject.name}
                <span className="font-normal text-muted-foreground">
                  {' '}
                  {subject.bands} {subject.bands === 1 ? 'band' : 'bands'}
                </span>
              </p>
              {subject.problem && <p className="min-w-0 text-sm text-muted-foreground">{subject.problem}</p>}
              <ul className="grid min-w-0 grid-cols-1 gap-x-6 gap-y-1 md:grid-cols-2" aria-label={`${subject.name} readings`}>
                {subject.readings.map((r) => (
                  <li key={r.path} className="flex min-w-0 items-start gap-2">
                    <Switch
                      checked={r.on}
                      disabled={(r.required && r.on) || busy === r.path}
                      onCheckedChange={(checked) => onFlip(r, checked)}
                      aria-label={r.name.startsWith(subject.name) ? r.name : `${subject.name} ${r.name}`}
                      className="shrink-0"
                    />
                    {/* The name is never cut. The verdict sits under it and wraps. */}
                    <div className="flex min-w-0 flex-1 flex-col">
                      <span data-testid="reading-name" className="min-w-0 break-words text-sm text-foreground">{r.name}</span>
                      <span data-testid="reading-status" className="min-w-0 break-words text-xs text-muted-foreground">
                        {r.required && r.on ? 'needed to judge it' : r.status}
                      </span>
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      ))}
    </>
  )
}
