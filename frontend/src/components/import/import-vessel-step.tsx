import { Checkbox } from '@/components/ui/checkbox'
import { useVesselIdentity } from '@/hooks/use-vessel-identity'
import { useVesselState } from '@/hooks/use-vessel-state'
import { issuesFor, type StagedParticular } from '@/lib/import-run'
import { Pill, RowCard, StepHeading, type StepProps } from '@/components/import/import-shared'

// Step 3: the vessel. Particulars the boat's live instrument data does not
// carry can be stored; the ones it does carry are shown against the export
// and never written, so the two can be compared.

function metres(text: string): number | null {
  const match = /^\s*([0-9]+(?:\.[0-9]+)?)\s*m\b/i.exec(text)
  return match ? Number(match[1]) : null
}

type Comparison = { live: string; state: 'match' | 'differs' } | { live: null; state: 'unavailable' }

function compare(particular: StagedParticular, live: { name: string | null; length: number | null; draft: number | null }): Comparison {
  const { label, value } = particular
  if (label === 'Name' && live.name !== null) {
    return { live: live.name, state: live.name.toLowerCase().includes(value.toLowerCase()) ? 'match' : 'differs' }
  }
  const liveMetres = label === 'Length overall' ? live.length : label === 'Draft' ? live.draft : null
  const exportMetres = metres(value)
  if (liveMetres !== null && exportMetres !== null) {
    return { live: `${liveMetres.toFixed(1)} m`, state: Math.abs(liveMetres - exportMetres) <= 0.05 ? 'match' : 'differs' }
  }
  return { live: null, state: 'unavailable' }
}

export function ImportVesselStep({ run, decisions, setDecision }: StepProps) {
  const { boatName } = useVesselIdentity()
  const { vesselLengthOverallM, vesselDraftM } = useVesselState()
  const { particulars } = run.staged

  const storable = particulars.filter((p) => p.field !== '' && !p.signalk && p.value !== '')
  const owned = particulars.filter((p) => p.signalk)
  const noHome = particulars.filter((p) => p.field === '' && !p.signalk && p.value !== '')

  return (
    <div className="space-y-6">
      <StepHeading title="Vessel">
        Helmcentral stores the particulars below. Name, call sign, MMSI and the main dimensions stay with the boat&apos;s
        live instrument data.
      </StepHeading>

      <section aria-label="Particulars to store" className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Stored in Helmcentral</h3>
        {storable.length === 0 ? (
          <p className="text-sm text-muted-foreground">The export holds no particulars to store.</p>
        ) : (
          <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
            {storable.map((p) => {
              const flagged = issuesFor(run.staged, p.key)
              return (
                <RowCard key={p.key}>
                  <div className="flex min-w-0 items-center gap-3">
                    <Checkbox
                      aria-label={`Store ${p.label}`}
                      checked={decisions.particulars[p.key] === 'apply'}
                      onCheckedChange={(checked) => setDecision('particulars', p.key, checked ? 'apply' : 'skip')}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{p.label}</span>
                      <span className="block truncate text-sm">{p.value}</span>
                    </span>
                    {run.already_imported.includes(p.key) && <Pill>Already imported</Pill>}
                  </div>
                  {flagged.map((issue) => (
                    <p key={issue.code} className="text-sm text-amber-700 dark:text-amber-400">{issue.message}</p>
                  ))}
                </RowCard>
              )
            })}
          </ul>
        )}
      </section>

      <section aria-label="Kept with live instrument data" className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Kept with live instrument data</h3>
        <p className="text-xs text-muted-foreground">
          These are not written anywhere. Where the boat reports its own value, the two are shown side by side.
        </p>
        <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
          {owned.map((p) => {
            const result = compare(p, { name: boatName, length: vesselLengthOverallM, draft: vesselDraftM })
            return (
              <RowCard key={p.key}>
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{p.label}</span>
                  {result.state === 'match' && <Pill>Matches</Pill>}
                  {result.state === 'differs' && <Pill tone="warning">Differs</Pill>}
                </div>
                <dl className="grid grid-cols-2 gap-2 text-sm">
                  <div className="min-w-0">
                    <dt className="text-xs text-muted-foreground">YachtWave</dt>
                    <dd className="truncate tabular-nums">{p.value === '' ? '--' : p.value}</dd>
                  </div>
                  <div className="min-w-0">
                    <dt className="text-xs text-muted-foreground">Live instrument data</dt>
                    <dd className="truncate tabular-nums">{result.live ?? '--'}</dd>
                  </div>
                </dl>
                {result.state === 'unavailable' && (
                  <p className="text-xs text-muted-foreground">
                    Not shown here. The boat&apos;s live instrument data stays the source for this.
                  </p>
                )}
              </RowCard>
            )
          })}
        </ul>
      </section>

      {noHome.length > 0 && (
        <section aria-label="No place in Helmcentral" className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Nowhere to keep these</h3>
          <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
            {noHome.map((p) => (
              <RowCard key={p.key}>
                <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{p.label}</span>
                <span className="truncate text-sm">{p.value}</span>
              </RowCard>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
