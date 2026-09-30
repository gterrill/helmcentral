import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { apiBaseUrl } from '@/config/api'

export interface CatalogueEntry {
  source: string
  id: string
  name: string
  kind: string
  manufacturer: string
  model: string
  sha256: string
  added: boolean
}

interface ProfileCatalogueDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Called with the id of a profile that was just copied into the list. */
  onAdded: (id: string) => void
}

/**
 * Lists the profiles that ship with Helmcentral and copies the chosen one into
 * the operator's own list. A profile already in the list stays visible but
 * cannot be added a second time.
 */
export function ProfileCatalogueDialog({ open, onOpenChange, onAdded }: ProfileCatalogueDialogProps) {
  const [entries, setEntries] = useState<CatalogueEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [addingID, setAddingID] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    const load = async () => {
      setLoading(true)
      setError(null)
      try {
        const response = await fetch(`${apiBaseUrl}/api/equipment-profiles/catalogue`)
        const body = (await response.json().catch(() => null)) as { entries?: CatalogueEntry[]; error?: string } | null
        if (cancelled) return
        if (!response.ok) {
          setError(body?.error ?? `HTTP ${response.status}`)
          setEntries([])
          return
        }
        setEntries(Array.isArray(body?.entries) ? body.entries : [])
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
          setEntries([])
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [open])

  const handleAdd = async (entry: CatalogueEntry) => {
    setAddingID(entry.id)
    setError(null)
    try {
      const response = await fetch(`${apiBaseUrl}/api/equipment-profiles/catalogue/${encodeURIComponent(entry.id)}`, {
        method: 'POST',
      })
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(body?.error ?? `HTTP ${response.status}`)
      }
      setEntries((current) => current.map((item) => (item.id === entry.id ? { ...item, added: true } : item)))
      onAdded(entry.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to add profile.')
    } finally {
      setAddingID(null)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add from catalogue</DialogTitle>
          <DialogDescription>
            Copy a profile that ships with Helmcentral into your own list. Your copy can be edited freely.
          </DialogDescription>
        </DialogHeader>

        {error && (
          <p className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive" role="alert">
            {error}
          </p>
        )}

        {loading ? (
          <p className="text-sm text-muted-foreground">Loading catalogue...</p>
        ) : (
          <ul className="grid gap-2">
            {entries.map((entry) => (
              <li key={entry.id} className="flex min-w-0 items-center gap-3 rounded-md border border-border p-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{entry.name}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {[entry.kind, entry.manufacturer, entry.model].filter(Boolean).join(' · ')}
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label={`Add ${entry.name}`}
                  disabled={entry.added || addingID !== null}
                  onClick={() => { void handleAdd(entry) }}
                >
                  {entry.added ? 'Added' : addingID === entry.id ? 'Adding...' : 'Add'}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  )
}
