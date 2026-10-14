import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { apiRequest, invalidateQueries, setQueryData, useApiQuery } from '@/lib/api-query'

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

const CATALOGUE_PATH = '/api/equipment-profiles/catalogue'
interface CatalogueBody {
  entries?: CatalogueEntry[]
}

const NONE: CatalogueEntry[] = []

/**
 * Lists the profiles that ship with Helmcentral and copies the chosen one into
 * the operator's own list. A profile already in the list stays visible but
 * cannot be added a second time.
 */
export function ProfileCatalogueDialog({ open, onOpenChange, onAdded }: ProfileCatalogueDialogProps) {
  const [addError, setAddError] = useState<string | null>(null)
  const [addingID, setAddingID] = useState<string | null>(null)
  const { data, isLoading: loading, error: loadError } = useApiQuery<CatalogueEntry[]>({
    queryKey: [CATALOGUE_PATH],
    path: CATALOGUE_PATH,
    enabled: open,
    select: (body: CatalogueBody | null) => (Array.isArray(body?.entries) ? body.entries : NONE),
  })
  const entries = loadError ? NONE : (data ?? NONE)
  const error = addError ?? loadError?.message ?? null

  useEffect(() => {
    if (open) setAddError(null)
  }, [open])

  const handleAdd = async (entry: CatalogueEntry) => {
    setAddingID(entry.id)
    setAddError(null)
    try {
      await apiRequest(`${CATALOGUE_PATH}/${encodeURIComponent(entry.id)}`, { method: 'POST' })
      // The cache holds the response body as sent, before select reads it.
      setQueryData<CatalogueBody>([CATALOGUE_PATH], (current) => ({
        entries: (current?.entries ?? NONE).map((item) => (item.id === entry.id ? { ...item, added: true } : item)),
      }))
      // The copy is now one of the operator's own profiles, so every list of
      // them is out of date.
      invalidateQueries(['/api/equipment-profiles'])
      onAdded(entry.id)
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Unable to add profile.')
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
