import { useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { useInventoryZones } from '@/hooks/use-inventory'

// ADR 0065 §4 / ADR 0123: zones and bins, plain CRUD, no floor plans this
// cycle. Rename is inline-on-blur rather than a separate edit mode (an
// uncontrolled <Input defaultValue={...}> keyed on the CURRENT server value,
// so a save that lands mid-edit doesn't fight the operator's cursor the way
// a fully controlled field synced straight from `zones` would) - the same
// "commit on blur, only when it actually changed" shape for both a zone's
// name and a bin's code/name. Delete is not behind a confirm dialog here
// (unlike EquipmentEditor's destructive delete) because the interesting
// failure mode - a zone or bin still holding something - is a 409 the
// server itself refuses and explains (ADR 0123: "Zones and bins in use
// cannot be deleted... 409 naming the items"), surfaced verbatim rather than
// pre-empted by a generic "are you sure?".

interface LocationsSectionProps {
  canWrite?: boolean
}

export function LocationsSection({ canWrite = true }: LocationsSectionProps) {
  const { zones, loading, error, createZone, renameZone, deleteZone, createBin, renameBin, deleteBin } = useInventoryZones()

  const [newZoneName, setNewZoneName] = useState('')
  const [addingZone, setAddingZone] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [newBinCode, setNewBinCode] = useState<Record<string, string>>({})
  const [newBinName, setNewBinName] = useState<Record<string, string>>({})

  const handleAddZone = async () => {
    const name = newZoneName.trim()
    if (name === '') return
    setActionError(null)
    setAddingZone(true)
    try {
      await createZone(name)
      setNewZoneName('')
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message (e.g. a
      // case-insensitive duplicate name), never an invented one.
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setAddingZone(false)
    }
  }

  const handleRenameZone = async (id: string, value: string, original: string) => {
    const trimmed = value.trim()
    if (trimmed === '' || trimmed === original) return
    setActionError(null)
    try {
      await renameZone(id, trimmed)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteZone = async (id: string) => {
    setActionError(null)
    try {
      await deleteZone(id)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleAddBin = async (zoneId: string) => {
    const code = (newBinCode[zoneId] ?? '').trim()
    if (code === '') return
    setActionError(null)
    try {
      await createBin(zoneId, code, (newBinName[zoneId] ?? '').trim())
      setNewBinCode((prev) => ({ ...prev, [zoneId]: '' }))
      setNewBinName((prev) => ({ ...prev, [zoneId]: '' }))
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleRenameBin = async (id: string, code: string, name: string, original: { code: string; name: string }) => {
    const trimmedCode = code.trim()
    const trimmedName = name.trim()
    if (trimmedCode === '') return
    if (trimmedCode === original.code && trimmedName === original.name) return
    setActionError(null)
    try {
      await renameBin(id, { code: trimmedCode, name: trimmedName })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteBin = async (id: string) => {
    setActionError(null)
    try {
      await deleteBin(id)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      {actionError && (
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {actionError}
        </p>
      )}
      {error && (
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      )}

      {!loading && zones.length === 0 && !error && (
        <p className="text-sm text-muted-foreground">
          No zones yet. A zone is an area of the boat - salon, engine room, lazarette. Add one below.
        </p>
      )}

      {zones.map((zone) => (
        <Card key={zone.id}>
          <CardHeader className="flex flex-row items-center justify-between gap-2">
            <Input
              key={`${zone.id}-${zone.name}`}
              defaultValue={zone.name}
              aria-label="Zone name"
              className="h-9 max-w-64 font-medium"
              disabled={!canWrite}
              onBlur={(e) => { void handleRenameZone(zone.id, e.target.value, zone.name) }}
            />
            {canWrite && (
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={`Delete zone ${zone.name}`}
                onClick={() => { void handleDeleteZone(zone.id) }}
              >
                <Trash2 className="h-4 w-4" aria-hidden="true" />
              </Button>
            )}
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {zone.bins.map((bin) => (
              <div key={bin.id} className="flex items-center gap-2">
                <Input
                  key={`${bin.id}-code-${bin.code}`}
                  defaultValue={bin.code}
                  aria-label="Bin code"
                  className="h-9 w-28 font-mono"
                  disabled={!canWrite}
                  onBlur={(e) => { void handleRenameBin(bin.id, e.target.value, bin.name, { code: bin.code, name: bin.name }) }}
                />
                <Input
                  key={`${bin.id}-name-${bin.name}`}
                  defaultValue={bin.name}
                  aria-label="Bin name"
                  placeholder="Name (optional)"
                  className="h-9 flex-1"
                  disabled={!canWrite}
                  onBlur={(e) => { void handleRenameBin(bin.id, bin.code, e.target.value, { code: bin.code, name: bin.name }) }}
                />
                {canWrite && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`Delete bin ${bin.code}`}
                    onClick={() => { void handleDeleteBin(bin.id) }}
                  >
                    <Trash2 className="h-4 w-4" aria-hidden="true" />
                  </Button>
                )}
              </div>
            ))}

            {canWrite && (
              <div className="flex items-center gap-2">
                <Input
                  aria-label={`New bin code for ${zone.name}`}
                  placeholder="Code"
                  className="h-9 w-28 font-mono"
                  value={newBinCode[zone.id] ?? ''}
                  onChange={(e) => setNewBinCode((prev) => ({ ...prev, [zone.id]: e.target.value }))}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleAddBin(zone.id) } }}
                />
                <Input
                  aria-label={`New bin name for ${zone.name}`}
                  placeholder="Name (optional)"
                  className="h-9 flex-1"
                  value={newBinName[zone.id] ?? ''}
                  onChange={(e) => setNewBinName((prev) => ({ ...prev, [zone.id]: e.target.value }))}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleAddBin(zone.id) } }}
                />
                <Button type="button" variant="outline" size="sm" className="gap-2" onClick={() => { void handleAddBin(zone.id) }}>
                  <Plus className="h-4 w-4" aria-hidden="true" />
                  Add bin
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      ))}

      {canWrite && (
        <div className="flex items-center gap-2">
          <Input
            aria-label="New zone name"
            placeholder="Zone name"
            className="h-9 max-w-64"
            value={newZoneName}
            onChange={(e) => setNewZoneName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleAddZone() } }}
          />
          <Button type="button" className="gap-2" onClick={() => { void handleAddZone() }} disabled={addingZone}>
            <Plus className="h-4 w-4" aria-hidden="true" />
            Add zone
          </Button>
        </div>
      )}
    </div>
  )
}
