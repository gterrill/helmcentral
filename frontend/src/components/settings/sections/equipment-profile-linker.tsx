import { useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useEquipment, BLANK_DRAFT, createEquipment, fetchEquipment, toEquipmentInput, updateEquipment, type EquipmentItem, type EquipmentSystem } from '@/hooks/use-inventory'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'

const CREATE_NEW = '__create__'

interface EquipmentProfileLinkerProps {
  /** Which equipment registry item is currently linked (vessel.engines[].equipment_id or vessel.house_bank.equipment_id), or '' for none. */
  equipmentId: string
  onEquipmentIdChange: (id: string) => void
  /** Only profiles of this kind are offered - an engine row must not offer a battery profile and vice versa. */
  profileKind: 'engine' | 'battery'
  /** Category/system a newly created item is seeded with. */
  defaultSystem: EquipmentSystem
  namePlaceholder: string
  /** Reports the linked item (and its own profile_id) up whenever it changes, so a caller (the engine row's "apply gauge zones" step) knows the current profile without fetching it a second time. */
  onLinkedItemChange?: (item: EquipmentItem | null) => void
}

/**
 * "Pick an existing inventory item or create one, then assign it a profile" -
 * the plan's "one home for the profile" (ADR 0102): the profile lives on the
 * linked registry item's own profile_id, read and written here, never
 * duplicated into vessel settings. Shared by the Engines row and the Power
 * fieldset's house-bank picker, since both need exactly this pairing.
 */
export function EquipmentProfileLinker({
  equipmentId, onEquipmentIdChange, profileKind, defaultSystem, namePlaceholder, onLinkedItemChange,
}: EquipmentProfileLinkerProps) {
  const { items, refresh: refreshItems } = useEquipment({})
  const { profiles } = useEquipmentProfiles(true)
  const kindProfiles = useMemo(() => profiles.filter((p) => p.kind === profileKind), [profiles, profileKind])

  const [linkedItem, setLinkedItem] = useState<EquipmentItem | null>(null)
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!equipmentId) {
      setLinkedItem(null)
      onLinkedItemChange?.(null)
      return
    }
    let cancelled = false
    void fetchEquipment(equipmentId).then((item) => {
      if (cancelled) return
      setLinkedItem(item)
      onLinkedItemChange?.(item)
    }).catch(() => {
      if (cancelled) return
      setLinkedItem(null)
      onLinkedItemChange?.(null)
    })
    return () => { cancelled = true }
    // onLinkedItemChange is a fresh closure most renders; keying only on
    // equipmentId is deliberate, the same reasoning use-inventory.ts's own
    // filterKey comment gives for not re-running on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [equipmentId])

  const handleCreate = async () => {
    const name = newName.trim()
    if (!name) return
    setBusy(true)
    try {
      const created = await createEquipment({ ...BLANK_DRAFT, name, category: 'mechanical', system: defaultSystem })
      setNewName('')
      setCreating(false)
      onEquipmentIdChange(created.id)
      void refreshItems()
    } finally {
      setBusy(false)
    }
  }

  const handleProfileChange = async (profileId: string) => {
    if (!linkedItem) return
    setBusy(true)
    try {
      const input = toEquipmentInput(linkedItem)
      const updated = await updateEquipment(linkedItem.id, { ...input, profile_id: profileId })
      setLinkedItem(updated)
      onLinkedItemChange?.(updated)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      {creating ? (
        <div className="flex min-w-0 gap-1.5">
          <Input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder={namePlaceholder}
            className="h-8 min-w-0 text-sm"
            aria-label={`New ${namePlaceholder} name`}
          />
          <Button type="button" size="sm" disabled={busy || !newName.trim()} onClick={() => void handleCreate()}>
            Create
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => { setCreating(false); setNewName('') }}>
            Cancel
          </Button>
        </div>
      ) : (
        <select
          className="h-8 w-full min-w-0 rounded-md border bg-transparent px-2 text-sm"
          value={equipmentId}
          aria-label="Linked equipment item"
          onChange={(e) => {
            if (e.target.value === CREATE_NEW) { setCreating(true); return }
            onEquipmentIdChange(e.target.value)
          }}
        >
          <option value="">Not linked</option>
          {items.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
          <option value={CREATE_NEW}>+ Create new item</option>
        </select>
      )}

      {linkedItem && (
        <select
          className="h-8 w-full min-w-0 rounded-md border bg-transparent px-2 text-sm"
          value={linkedItem.profile_id}
          disabled={busy}
          aria-label="Equipment profile"
          onChange={(e) => void handleProfileChange(e.target.value)}
        >
          <option value="">No profile</option>
          {kindProfiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      )}
    </div>
  )
}
