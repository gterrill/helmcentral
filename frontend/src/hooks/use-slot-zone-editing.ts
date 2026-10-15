import { useCallback, useEffect, useState } from 'react'

import type { GaugeEquipmentMode } from '@/components/gauge-fields'
import { saveEquipmentProfile } from '@/hooks/use-equipment-profiles'
import type { GaugeWidgetConfig, GaugeZone } from '@/lib/dashboard-widgets'
import {
  itemsUsingProfile,
  profileBands,
  profileGaugeFor,
  profileOf,
  withGaugeZones,
  type EquipmentTileContext,
  type TileItem,
} from '@/lib/equipment-tiles'

interface ZoneEdit {
  bands: GaugeZone[]
  min: number
  max: number
}

/**
 * Zone editing for the slots of a tile that shows an equipment item (ADR
 * 0185). Zones live on the item's profile, so the form edits the profile's
 * bands; nothing is written until the tile is saved, when `commit` saves the
 * profile. The notice names everything else the change touches.
 */
export function useSlotZoneEditing(ctx: EquipmentTileContext, item: TileItem | undefined) {
  const profile = item ? profileOf(ctx, item) : undefined
  const [edits, setEdits] = useState<Record<string, ZoneEdit>>({})

  // A different item or profile is a different set of zones.
  const profileId = profile?.id ?? ''
  useEffect(() => { setEdits({}) }, [item?.id, profileId])

  const notice = profile
    ? `Applies to every tile and alarm using ${profile.name} (${itemsUsingProfile(ctx, profile.id).join(', ')}). Saved to the profile when you save this tile.`
    : ''

  const modeFor = useCallback((slot: GaugeWidgetConfig): GaugeEquipmentMode => {
    const suffix = (slot.suffix ?? '').trim()
    const gauge = profileGaugeFor(profile, suffix)
    const min = slot.min ?? gauge?.min ?? 0
    const max = slot.max ?? gauge?.max ?? 100
    return {
      profileGauges: profile?.gauges ?? [],
      zones: gauge ? (edits[suffix]?.bands ?? profileBands(gauge, min, max)) : undefined,
      onZonesChange: (bands) => setEdits((current) => ({ ...current, [suffix]: { bands, min, max } })),
      zoneNotice: notice,
    }
  }, [profile, edits, notice])

  const dirty = Object.keys(edits).length > 0

  /** Saves the edited zones to the profile. Throws if the server refuses them. */
  const commit = useCallback(async () => {
    if (!profile || !dirty) return
    let next = profile
    for (const [suffix, edit] of Object.entries(edits)) next = withGaugeZones(next, suffix, edit.bands, edit.min, edit.max)
    await saveEquipmentProfile(next)
    setEdits({})
  }, [profile, edits, dirty])

  return { profile, modeFor, commit, dirty }
}
