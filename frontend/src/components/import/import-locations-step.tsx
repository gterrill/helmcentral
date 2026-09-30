import { useInventoryZones } from '@/hooks/use-inventory'
import { zoneAction } from '@/lib/import-run'
import { AlreadyImported, decodeChoice, encodeChoice, ImportSelect, RowCard, StepHeading, type StepProps } from '@/components/import/import-shared'

// Step 4: each YachtWave location becomes a zone here, joins one that is
// already here, or is dropped (its name is then kept in front of each
// record's own location detail).

export function ImportLocationsStep({ run, decisions, setDecision }: StepProps) {
  const { zones, loading, error } = useInventoryZones()

  return (
    <div className="space-y-4">
      <StepHeading title="Locations">
        Choose what happens to each place the export names. A location you leave out is not lost: its name is kept with
        each record that was filed there.
      </StepHeading>
      {error !== null && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {loading && <p className="text-sm text-muted-foreground">Loading your locations</p>}
      <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
        {run.staged.locations.map((location) => {
          const decision = decisions.zones[location.key]
          const value = encodeChoice(zoneAction(decisions, location.key), decision?.zone_id ?? '')
          return (
            <RowCard key={location.key}>
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="min-w-0 truncate text-sm font-medium">{location.name}</span>
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                  {location.count} {location.count === 1 ? 'record' : 'records'}
                </span>
                <AlreadyImported run={run} keyName={location.key} />
              </div>
              <ImportSelect
                aria-label={`What to do with ${location.name}`}
                value={value}
                onChange={(e) => {
                  const choice = decodeChoice(e.target.value)
                  setDecision('zones', location.key, { action: choice.action, zone_id: choice.target })
                }}
              >
                <option value="create">Create a new location</option>
                {zones.map((zone) => (
                  <option key={zone.id} value={`match:${zone.id}`}>Use existing: {zone.name}</option>
                ))}
                {value.startsWith('match:') && !zones.some((z) => `match:${z.id}` === value) && (
                  <option value={value}>Existing location (not found)</option>
                )}
                <option value="skip">Leave this location out</option>
              </ImportSelect>
            </RowCard>
          )
        })}
      </ul>
    </div>
  )
}
