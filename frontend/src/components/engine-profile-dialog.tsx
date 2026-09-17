import { useEffect, useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import type { GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import {
  alarmZoneCount,
  commonInstancePrefix,
  profileHeroGaugeIndex,
  instancePrefixCandidates,
  mergeGaugeSettingsBySuffix,
  profileToGauges,
  type EngineProfile,
  type EngineProfileGauge,
} from '@/lib/engine-profiles'
import { unitOption } from '@/lib/quantities'

interface EngineProfileDialogProps {
  open: boolean
  onCancel: () => void
  /** Suffixes are passed alongside so a dotted one matches as a whole. */
  onApply: (title: string, gauges: GaugeWidgetConfig[], suffixes: string[], hero?: number) => void
  /** Set when applying to a group that already exists, to reword the action. */
  applyLabel?: string
  /**
   * The tile's current gauges, when applying to one that already exists. Their
   * shared prefix seeds the instance field, and they drive the updated/added
   * summary.
   */
  existingGauges?: readonly GaugeWidgetConfig[]
}

/**
 * Builds a configured engine tile from a profile (ADR 0053).
 *
 * The preview is not decoration. A profile supplies alarm thresholds, and this
 * is where the operator sees exactly which ones will fire before any of them
 * do — which is why applying is a dialog rather than a menu item.
 */
export function EngineProfileDialog({
  open, onCancel, onApply, applyLabel = 'Add tile', existingGauges,
}: EngineProfileDialogProps) {
  const { profiles, problems, loading, error } = useEquipmentProfiles(open)
  const { paths } = useSignalKPaths(open)

  const [profileID, setProfileID] = useState('')
  const [instance, setInstance] = useState('')
  const [title, setTitle] = useState('')

  const profile: EngineProfile | undefined = useMemo(
    () => profiles.find((p) => p.id === profileID) ?? profiles[0],
    [profiles, profileID],
  )

  const candidates = useMemo(
    () => (profile ? instancePrefixCandidates(profile, paths) : []),
    [profile, paths],
  )

  // A tile being edited names its own engine; the published paths are only a
  // fallback for a tile that has none yet.
  const seededPrefix = useMemo(
    () => (existingGauges && profile ? commonInstancePrefix(existingGauges, profile.gauges.map((g) => g.path_suffix)) : null)
      ?? candidates[0] ?? '',
    [existingGauges, candidates, profile],
  )

  // The seed candidates come from a separate fetch (useSignalKPaths) than
  // the one that resolves `profile`, so it can still land after the operator
  // has already typed an instance by hand. `instanceTouchedRef` is what
  // stops that clobber; `seededProfileIdRef` is what still lets a genuine
  // profile switch reseed even after the field has been touched. Both live
  // in one effect (rather than a separate profile-change effect resetting
  // the ref for this one to read) so there is no cross-effect ordering to
  // rely on - each render's seeding decision is made from values current as
  // of that same synchronous pass.
  const instanceTouchedRef = useRef(false)
  const seededProfileIdRef = useRef<string | undefined>(undefined)

  useEffect(() => {
    if (!profile) return
    if (seededProfileIdRef.current !== profile.id) {
      seededProfileIdRef.current = profile.id
      instanceTouchedRef.current = false
    }
    if (instanceTouchedRef.current) return
    setInstance(seededPrefix)
    setTitle(seededPrefix ? titleFor(seededPrefix) : profile.name)
  }, [profile?.id, seededPrefix])

  const alarms = profile ? alarmZoneCount(profile) : 0

  // What applying will actually do to the tile that is already there.
  const change = useMemo(() => {
    if (!profile || !existingGauges) return null
    const { updated, added } = mergeGaugeSettingsBySuffix(
      existingGauges,
      profileToGauges(profile, instance),
      profile.gauges.map((g) => g.path_suffix),
    )
    return { updated, added }
  }, [profile, existingGauges, instance])
  const canApply = profile !== undefined && instance.trim() !== '' && title.trim() !== ''

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Equipment profile</DialogTitle>
          <DialogDescription>
            Builds a configured tile from a manufacturer profile — paths, scales and operating bands.
          </DialogDescription>
        </DialogHeader>

        {problems.length > 0 && (
          <div className="rounded-md border border-amber-500/40 bg-amber-50/60 p-3 dark:bg-amber-950/30">
            <p className="text-[10px] font-medium uppercase tracking-wider text-amber-700 dark:text-amber-400">
              {problems.length} profile{problems.length === 1 ? '' : 's'} failed to load
            </p>
            {problems.map((problem) => (
              <p key={problem.file} className="truncate text-[11px] text-muted-foreground">
                {problem.file}: {problem.error}
              </p>
            ))}
          </div>
        )}

        {error ? (
          // A failed fetch and an empty profiles directory must not look the
          // same — one means "nothing installed", the other means the
          // backend is broken, and conflating them hides a real failure.
          <p className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive" role="alert">
            Could not load equipment profiles: {error}
          </p>
        ) : !loading && profiles.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No equipment profiles installed. Drop a .json profile into plugins/engine-profiles and restart.
          </p>
        ) : (
          <div className="grid gap-3">
            <Field>
              <FieldLabel htmlFor="engine-profile">Profile</FieldLabel>
              <select
                id="engine-profile"
                className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={profile?.id ?? ''}
                onChange={(e) => setProfileID(e.target.value)}
              >
                {profiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}{p.kind ? ` (${p.kind})` : ''}
                  </option>
                ))}
              </select>
              {profile?.source && <FieldDescription>{profile.source}</FieldDescription>}
            </Field>

            <div className="grid gap-3 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="engine-instance">Engine instance</FieldLabel>
                <Input
                  id="engine-instance"
                  list="engine-instance-options"
                  value={instance}
                  onChange={(e) => {
                    instanceTouchedRef.current = true
                    setInstance(e.target.value)
                  }}
                  placeholder="propulsion.port"
                />
                {/* Suggestions only: with the engines off nothing under
                    propulsion.* is published, and that is exactly when engine
                    gauges get set up (ADR 0039 §5). */}
                <datalist id="engine-instance-options">
                  {candidates.map((c) => <option key={c} value={c} />)}
                </datalist>
                <FieldDescription>
                  {candidates.length > 0
                    ? `${candidates.length} matching instance${candidates.length === 1 ? '' : 's'} publishing now.`
                    : 'Nothing matching is publishing. Type it anyway — an engine that is off has no paths.'}
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="engine-tile-title">Tile title</FieldLabel>
                <Input id="engine-tile-title" value={title} onChange={(e) => setTitle(e.target.value)} />
              </Field>
            </div>

            {profile && (
              <div className="rounded-md border border-border p-3">
                <p
                  data-testid="engine-profile-alarm-count"
                  className={`text-[10px] font-medium uppercase tracking-wider ${alarms > 0 ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground'}`}
                >
                  {alarms} alarm threshold{alarms === 1 ? '' : 's'} will be set
                </p>

                {change && (
                  <p data-testid="engine-profile-change-summary" className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                    {change.updated} gauge{change.updated === 1 ? '' : 's'} updated,{' '}
                    {change.added} added
                  </p>
                )}

                <div data-testid="engine-profile-preview" className="mt-2 grid gap-1.5">
                  {profile.gauges.map((gauge) => (
                    <GaugeRow key={gauge.path_suffix} gauge={gauge} instance={instance} />
                  ))}
                </div>

                {profile.notes && (
                  <p className="mt-2 text-[11px] text-muted-foreground">{profile.notes}</p>
                )}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button
            disabled={!canApply}
            onClick={() => profile && onApply(
              title.trim(),
              profileToGauges(profile, instance),
              profile.gauges.map((g) => g.path_suffix),
              profileHeroGaugeIndex(profile),
            )}
          >
            {applyLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function GaugeRow({ gauge, instance }: { gauge: EngineProfileGauge; instance: string }) {
  const unit = unitOption(gauge.quantity, gauge.unit).label

  return (
    <div className="grid min-w-0 gap-0.5 border-b border-border/60 pb-1.5 last:border-b-0 last:pb-0">
      <div className="flex min-w-0 items-baseline justify-between gap-2">
        <span className="truncate text-sm">
          {gauge.label}
          {gauge.hero && (
            <span className="ml-2 rounded-xs border border-border bg-background px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-muted-foreground">
              Hero
            </span>
          )}
        </span>
        <span className="shrink-0 text-[10px] uppercase tracking-wider text-muted-foreground">
          {gauge.min ?? 0}–{gauge.max ?? 100} {unit}
        </span>
      </div>
      <span className="truncate text-[10px] text-muted-foreground">
        {instance.trim().replace(/\.+$/, '') || '…'}.{gauge.path_suffix}
      </span>
      {(gauge.zones ?? []).map((zone, index) => (
        <span key={index} className="truncate text-[11px] text-muted-foreground">
          {zone.state === 'normal' ? 'Healthy' : zone.state} · {zone.direction}{' '}
          {zone.threshold === null || zone.threshold === undefined
            ? <span className="text-muted-foreground">not set{zone.note ? ` — ${zone.note}` : ''}</span>
            : `${zone.threshold} ${unit}`}
        </span>
      ))}
    </div>
  )
}

/** "propulsion.port" -> "Port" */
function titleFor(instance: string): string {
  const last = instance.split('.').filter(Boolean).slice(-1)[0] ?? ''
  return last.charAt(0).toUpperCase() + last.slice(1)
}
