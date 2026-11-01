import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAppConfig } from '@/hooks/use-app-config'
import { useSavedPlaces } from '@/hooks/use-saved-places'
import type { AnchorWatchRecord } from '@/hooks/use-anchor-watch'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { depthSourceText, dropDay, saveDepthLabel, saveFigureM, saveTargetPlace } from '@/lib/anchor-save-depth'
import { errorMessage } from '@/lib/errors'
import { depthSummary, depthToText, depthUnitLabel } from '@/lib/saved-place-depth'
import { selectTimeZone } from '@/lib/vessel-time'

type Watch = Pick<AnchorWatchRecord, 'active' | 'anchorLat' | 'anchorLon' | 'setAt' | 'placeName' | 'datumDepthM' | 'datumDepthReason' | 'planningDepthSource'>

/** The server refuses a saved place name longer than this. */
const SAVED_PLACE_NAME_MAX = 80

/**
 * Saves the depth under the anchor, at chart datum, to a saved place as a
 * sounded figure. Nothing is written until the operator confirms. When the
 * figure cannot be worked out the reason stands where the button would be.
 */
export function AnchorSaveDepth({ watch }: { watch: Watch }) {
  const { ui } = useAppConfig()
  const units = ui.distanceUnits
  const timeZone = useVesselStateSelector(selectTimeZone)
  const { places, loading, error, add, change } = useSavedPlaces()
  const [confirming, setConfirming] = useState(false)
  const [name, setName] = useState('')
  const [saving, setSaving] = useState(false)
  const [writeError, setWriteError] = useState<string | null>(null)

  const { anchorLat, anchorLon } = watch
  const target = useMemo(
    () => (anchorLat === null || anchorLon === null ? null : saveTargetPlace(places, anchorLat, anchorLon)),
    [places, anchorLat, anchorLon],
  )

  if (!watch.active || anchorLat === null || anchorLon === null) return null

  const reason = (text: string) => (
    <p role="note" className="text-xs text-muted-foreground" data-testid="anchor-save-depth-reason">{text}</p>
  )
  const depthM = watch.datumDepthM
  if (depthM === null) return reason(watch.datumDepthReason || 'The depth at datum could not be worked out.')
  const day = dropDay(watch.setAt, timeZone)
  if (day === null) return reason("The boat's time zone is not known yet, so the depth cannot be dated.")
  if (error) return reason('The saved places could not be read, so the depth cannot be saved yet.')
  if (loading) return null

  const unit = depthUnitLabel(units)
  // One figure, to a centimetre: it is what the operator is shown and what is sent.
  const sendM = saveFigureM(depthM)
  const shown = `${depthToText(sendM, units)} ${unit}`
  const figure = `${shown} (sounded ${day})`
  const nameBlank = target === null && name.trim() === ''

  const open = () => {
    setName(watch.placeName.slice(0, SAVED_PLACE_NAME_MAX))
    setWriteError(null)
    setConfirming(true)
  }

  const confirm = async () => {
    setSaving(true)
    setWriteError(null)
    try {
      const stamp = { depth_m: sendM, depth_source: 'sounded' as const, depth_date: day }
      if (target) {
        await change(target.id, stamp)
        toast.success(`Saved ${shown} to ${target.name}`)
      } else {
        await add({ name: name.trim(), lat: anchorLat, lon: anchorLon, note: '', ...stamp })
        toast.success(`Saved ${name.trim()} with ${shown}`)
      }
      setConfirming(false)
    } catch (err) {
      setWriteError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex flex-col gap-2" data-testid="anchor-save-depth">
      <p className="text-sm font-medium">{saveDepthLabel(sendM, units)}</p>
      {watch.planningDepthSource && (
        <p className="text-xs text-muted-foreground">{`Worked out from ${depthSourceText(watch.planningDepthSource)}.`}</p>
      )}
      {!confirming ? (
        <Button type="button" variant="outline" size="sm" className="self-start" onClick={open}>
          {target ? `Save depth to ${target.name}` : 'Save as a saved place'}
        </Button>
      ) : (
        <div className="flex flex-col gap-2 rounded-md border p-3">
          {target === null && (
            <div className="flex flex-col gap-1.5">
              <label htmlFor="anchor-save-depth-name" className="text-xs font-medium text-muted-foreground">Name</label>
              <Input id="anchor-save-depth-name" value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
            </div>
          )}
          <p className="text-xs">
            {target !== null && target.depth_m !== null
              ? `Replaces ${depthSummary(target, units)} with ${figure}.`
              : `Saves ${figure}${target ? ` to ${target.name}` : ' at the anchor'}.`}
          </p>
          {writeError && <p role="alert" className="text-xs text-destructive">{writeError}</p>}
          <div className="flex gap-2">
            <Button type="button" size="sm" disabled={saving || nameBlank} onClick={() => void confirm()}>Confirm</Button>
            <Button type="button" variant="ghost" size="sm" disabled={saving} onClick={() => setConfirming(false)}>Cancel</Button>
          </div>
        </div>
      )}
    </div>
  )
}
