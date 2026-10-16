import { useCallback, useEffect, useMemo, useState } from 'react'

import type { GaugeEquipmentMode } from '@/components/gauge-fields'
import { saveEquipmentProfile } from '@/hooks/use-equipment-profiles'
import type { GaugeWidgetConfig, GaugeZone } from '@/lib/dashboard-widgets'
import {
  effectiveScale,
  gaugeHasZones,
  itemsUsingProfile,
  planTileZones,
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

export type ZoneChoice = 'profile' | 'tile'

/**
 * Zone editing for the slots of a tile that shows an equipment item (ADR
 * 0187). Zones live on the item's profile, so the form edits the profile's
 * bands; nothing is written until the tile is saved, when `commit` saves the
 * profile. The notice names everything else the change touches.
 *
 * A tile converted before it had equipment, or a profile, still holds zones of
 * its own. `slots` is the flat list of the tile's slots; when the item has a
 * profile the zones a slot holds move onto it at save (the profile has none for
 * the reading), or the operator is asked which set to keep (it has its own).
 * With no profile they stay on the tile and move at the first save after one is
 * linked, so there is no separate action to remember. Nothing is dropped
 * without a choice.
 */
export function useSlotZoneEditing(ctx: EquipmentTileContext, item: TileItem | undefined, slots: readonly GaugeWidgetConfig[] = [], paths: readonly { path: string }[] = []) {
  const profile = item ? profileOf(ctx, item) : undefined
  const [edits, setEdits] = useState<Record<string, ZoneEdit>>({})
  const [choices, setChoices] = useState<Record<number, ZoneChoice>>({})

  // A different item or profile is a different set of zones.
  const profileId = profile?.id ?? ''
  useEffect(() => {
    setEdits({})
    setChoices({})
  }, [item?.id, profileId])

  const plan = useMemo(() => planTileZones(item ? profile : undefined, item ? slots : []), [item, profile, slots])
  // Zones are only held while there is no item to hold them for (stays).
  const heldWithoutItem = useMemo(
    () => (item ? [] : slots.filter((s) => (s.zones?.length ?? 0) > 0).map((s) => s.label.trim() || (s.suffix ?? ''))),
    [item, slots],
  )

  // What the boat reports under the item's source, as suffixes, for the Reading pick.
  const reportedSuffixes = useMemo(() => {
    if (!item || item.signalk_source === '') return []
    const prefix = `${item.signalk_source}.`
    const inProfile = new Set((profile?.gauges ?? []).map((g) => g.path_suffix))
    return [...new Set(paths.filter((p) => p.path.startsWith(prefix)).map((p) => p.path.slice(prefix.length)))]
      .filter((suffix) => suffix !== '' && !inProfile.has(suffix))
      .sort()
  }, [item, profile, paths])

  const notice = profile
    ? `Applies to every tile and alarm using ${profile.name} (${itemsUsingProfile(ctx, profile.id).join(', ')}). Saved to the profile when you save this tile.`
    : ''

  const modeFor = useCallback((slot: GaugeWidgetConfig): GaugeEquipmentMode => {
    const suffix = (slot.suffix ?? '').trim()
    const gauge = profileGaugeFor(profile, suffix)
    const { min, max } = effectiveScale(slot.min, slot.max, gauge)
    return {
      profileGauges: profile?.gauges ?? [],
      zones: gauge ? (edits[suffix]?.bands ?? profileBands(gauge, min, max)) : undefined,
      onZonesChange: (bands) => setEdits((current) => ({ ...current, [suffix]: { bands, min, max } })),
      reportedSuffixes,
      zoneNotice: notice,
      holdsOwnZones: (slot.zones?.length ?? 0) > 0 && !gaugeHasZones(gauge),
    }
  }, [profile, edits, notice, reportedSuffixes])

  const unresolved = plan.conflicts.filter((c) => choices[c.index] === undefined).length
  const dirty = Object.keys(edits).length > 0 || plan.moves.length > 0 || plan.conflicts.length > 0

  /**
   * Writes what the profile has to take: the edited zones, the zones that move
   * onto it, and the tile's set where the operator chose it over the profile's.
   * Resolves to the positions of the slots whose own zones are now settled and
   * must be cleared from the tile. Throws if the server refuses them.
   */
  const commit = useCallback(async (): Promise<Set<number>> => {
    const cleared = new Set<number>()
    if (!profile) return cleared
    const writes: Record<string, ZoneEdit> = { ...edits }
    for (const move of plan.moves) {
      cleared.add(move.index)
      // What the operator edited in the form for the same reading wins.
      writes[move.suffix] ??= { bands: move.bands, min: move.min, max: move.max }
    }
    for (const conflict of plan.conflicts) {
      cleared.add(conflict.index)
      if (choices[conflict.index] === 'tile') {
        writes[conflict.suffix] = { bands: conflict.bands, min: conflict.min, max: conflict.max }
      }
    }
    if (Object.keys(writes).length > 0) {
      let next = profile
      for (const [suffix, edit] of Object.entries(writes)) next = withGaugeZones(next, suffix, edit.bands, edit.min, edit.max)
      await saveEquipmentProfile(next)
    }
    setEdits({})
    return cleared
  }, [profile, edits, plan, choices])

  return { profile, modeFor, commit, dirty, plan, choices, setChoice: (index: number, choice: ZoneChoice) => setChoices((c) => ({ ...c, [index]: choice })), unresolved, heldWithoutItem }
}
