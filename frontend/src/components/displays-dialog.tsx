import { useRef, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import type { Display } from '@/lib/displays'
import type { DisplayFields } from '@/hooks/use-displays'

// Mirrors backend/displays.go's own constants exactly (displayNameMaxLen,
// displaySlugMaxLen, displayMinPx/displayMaxWidthPx/displayMaxHeightPx,
// displayMinScale/displayMaxScale, displaySlugPattern, validDisplayRotations)
// - for immediate feedback only. The server stays the authority: a save it
// rejects surfaces its own message (via useDisplays' toast), never a local
// guess dressed up as the real error.
const DISPLAY_NAME_MAX_LEN = 48
const DISPLAY_SLUG_MAX_LEN = 32
const DISPLAY_MIN_PX = 200
const DISPLAY_MAX_WIDTH_PX = 7680
const DISPLAY_MAX_HEIGHT_PX = 4320
const DISPLAY_MIN_SCALE = 0.5
const DISPLAY_MAX_SCALE = 4.0
const DISPLAY_SLUG_PATTERN = /^[a-z0-9]+(-[a-z0-9]+)*$/

function validName(name: string): boolean {
  return name.trim() !== '' && name.length <= DISPLAY_NAME_MAX_LEN
}

function validSlug(slug: string): boolean {
  return slug !== '' && slug.length <= DISPLAY_SLUG_MAX_LEN && DISPLAY_SLUG_PATTERN.test(slug)
}

function validGeometry(width: number, height: number): boolean {
  if (Number.isNaN(width) || Number.isNaN(height)) return false
  if (width === 0 && height === 0) return true
  return width >= DISPLAY_MIN_PX && width <= DISPLAY_MAX_WIDTH_PX &&
    height >= DISPLAY_MIN_PX && height <= DISPLAY_MAX_HEIGHT_PX
}

function validScale(scale: number, width: number): boolean {
  if (Number.isNaN(scale)) return false
  if (scale === 0 || scale === 1) return true
  if (scale < DISPLAY_MIN_SCALE || scale > DISPLAY_MAX_SCALE) return false
  return width !== 0
}

function deleteConfirmationMessage(display: Display, pageCount: number): string {
  if (pageCount === 0) return `Delete ${display.name}? It has no pages on it.`
  const pages = pageCount === 1 ? '1 page' : `${pageCount} pages`
  return `Delete ${display.name}? Its ${pages} stay, and move back into the Dashboard list.`
}

interface DisplayRowProps {
  display: Display
  pageCount: number
  onUpdate: (id: string, patch: DisplayFields) => Promise<Display | null>
  onRequestDelete: (display: Display) => void
  canWrite: boolean
}

function DisplayRow({ display, pageCount, onUpdate, onRequestDelete, canWrite }: DisplayRowProps) {
  const [nameInvalid, setNameInvalid] = useState(false)
  const [slugInvalid, setSlugInvalid] = useState(false)
  const [geometryInvalid, setGeometryInvalid] = useState(false)
  const [scaleInvalid, setScaleInvalid] = useState(false)
  const widthRef = useRef<HTMLInputElement>(null)
  const heightRef = useRef<HTMLInputElement>(null)

  const commitName = (raw: string) => {
    const name = raw.trim()
    if (!validName(name)) { setNameInvalid(true); return }
    setNameInvalid(false)
    if (name !== display.name) void onUpdate(display.id, { name })
  }

  const commitSlug = (raw: string) => {
    const slug = raw.trim()
    if (!validSlug(slug)) { setSlugInvalid(true); return }
    setSlugInvalid(false)
    if (slug !== display.slug) void onUpdate(display.id, { slug })
  }

  const commitGeometry = () => {
    const width = Number(widthRef.current?.value ?? '')
    const height = Number(heightRef.current?.value ?? '')
    if (!validGeometry(width, height)) { setGeometryInvalid(true); return }
    setGeometryInvalid(false)
    if (width !== display.width || height !== display.height) void onUpdate(display.id, { width, height })
  }

  const commitScale = (raw: string) => {
    const scale = Number(raw)
    if (!validScale(scale, display.width)) { setScaleInvalid(true); return }
    setScaleInvalid(false)
    if (scale !== (display.scale || 1)) void onUpdate(display.id, { scale })
  }

  const commitOnEnter = (commit: (value: string) => void) => (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return
    e.preventDefault()
    commit(e.currentTarget.value)
  }
  const commitGeometryOnEnter = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return
    e.preventDefault()
    commitGeometry()
  }

  return (
    <div className="grid gap-2 rounded-md border border-border p-3 sm:grid-cols-[1.5fr_1fr_1.6fr_auto_auto_auto_auto]">
      <Field>
        <FieldLabel htmlFor={`display-${display.id}-name`}>Name</FieldLabel>
        <Input
          id={`display-${display.id}-name`}
          key={`${display.id}-name-${display.name}`}
          defaultValue={display.name}
          maxLength={DISPLAY_NAME_MAX_LEN}
          disabled={!canWrite}
          className={cn(nameInvalid && 'border-destructive text-destructive')}
          onBlur={(e) => commitName(e.target.value)}
          onKeyDown={commitOnEnter(commitName)}
        />
      </Field>

      <Field>
        <FieldLabel htmlFor={`display-${display.id}-slug`}>Slug</FieldLabel>
        <Input
          id={`display-${display.id}-slug`}
          key={`${display.id}-slug-${display.slug}`}
          defaultValue={display.slug}
          maxLength={DISPLAY_SLUG_MAX_LEN}
          disabled={!canWrite}
          className={cn(slugInvalid && 'border-destructive text-destructive')}
          onBlur={(e) => commitSlug(e.target.value)}
          onKeyDown={commitOnEnter(commitSlug)}
        />
      </Field>

      <Field>
        <FieldLabel>Canvas &amp; magnification</FieldLabel>
        <div className="flex items-center gap-1">
          <Input
            ref={widthRef}
            key={`${display.id}-width-${display.width}`}
            type="number"
            aria-label={`Width for ${display.name}`}
            defaultValue={display.width}
            disabled={!canWrite}
            className={cn('w-20', geometryInvalid && 'border-destructive text-destructive')}
            onBlur={commitGeometry}
            onKeyDown={commitGeometryOnEnter}
          />
          <span aria-hidden="true">×</span>
          <Input
            ref={heightRef}
            key={`${display.id}-height-${display.height}`}
            type="number"
            aria-label={`Height for ${display.name}`}
            defaultValue={display.height}
            disabled={!canWrite}
            className={cn('w-20', geometryInvalid && 'border-destructive text-destructive')}
            onBlur={commitGeometry}
            onKeyDown={commitGeometryOnEnter}
          />
          <span aria-hidden="true">@</span>
          <Input
            key={`${display.id}-scale-${display.scale}`}
            type="number"
            step={0.1}
            aria-label={`Scale for ${display.name}`}
            defaultValue={display.scale || 1}
            disabled={!canWrite}
            className={cn('w-16', scaleInvalid && 'border-destructive text-destructive')}
            onBlur={(e) => commitScale(e.target.value)}
            onKeyDown={commitOnEnter(commitScale)}
          />
          <span aria-hidden="true">×</span>
        </div>
      </Field>

      <Field>
        <FieldLabel htmlFor={`display-${display.id}-rotate`}>Rotation</FieldLabel>
        <select
          id={`display-${display.id}-rotate`}
          className="h-10 rounded-md border border-input bg-background px-2 text-sm"
          value={display.rotate}
          disabled={!canWrite}
          onChange={(e) => void onUpdate(display.id, { rotate: Number(e.target.value) as 0 | 180 })}
        >
          <option value={0}>0°</option>
          <option value={180}>180°</option>
        </select>
      </Field>

      <Field>
        <FieldLabel htmlFor={`display-${display.id}-oled`} title="Shifts the image a few pixels on a slow cycle so a static board doesn't burn a ghost into the panel.">
          <input
            id={`display-${display.id}-oled`}
            type="checkbox"
            checked={display.pixel_shift}
            disabled={!canWrite}
            onChange={(e) => void onUpdate(display.id, { pixel_shift: e.target.checked })}
          />
          OLED panel
        </FieldLabel>
      </Field>

      <Field>
        <FieldLabel htmlFor={`display-${display.id}-wake`}>
          <input
            id={`display-${display.id}-wake`}
            type="checkbox"
            checked={display.wake_lock}
            disabled={!canWrite}
            onChange={(e) => void onUpdate(display.id, { wake_lock: e.target.checked })}
          />
          Keep awake
        </FieldLabel>
      </Field>

      <div className="flex flex-col items-end justify-between gap-2">
        <span className="text-xs text-muted-foreground">{pageCount === 1 ? '1 page' : `${pageCount} pages`}</span>
        {canWrite && (
          <Button variant="ghost" size="sm" aria-label={`Delete ${display.name}`} onClick={() => onRequestDelete(display)}>
            <Trash2 className="size-3.5" />
          </Button>
        )}
      </div>
    </div>
  )
}

export interface DisplaysDialogPage {
  display_id?: string
}

interface DisplaysDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  displays: Display[]
  /** Every dashboard page — used only to count each display's pages, for the
   * "N pages" column and the delete confirmation's release wording. */
  pages: DisplaysDialogPage[]
  onCreate: (fields: DisplayFields & { name: string }) => Promise<Display | null>
  onUpdate: (id: string, patch: DisplayFields) => Promise<Display | null>
  /** Resolves to the released page ids on success, or null on failure — the
   * exact shape use-displays.ts's deleteDisplay already returns, so this can
   * be wired to it directly. */
  onDelete: (id: string) => Promise<string[] | null>
  canWrite?: boolean
}

/**
 * Where displays are managed (ADR 0110 plan §6): a dialog opened from the
 * sidebar's display group, not a route — the same precedent
 * lamp-strip-config-dialog.tsx set for the ribbon, and ADR 0074's rule that
 * dialogs carry no URL.
 */
export function DisplaysDialog({ open, onOpenChange, displays, pages, onCreate, onUpdate, onDelete, canWrite = true }: DisplaysDialogProps) {
  const [pendingDelete, setPendingDelete] = useState<Display | null>(null)
  const [newName, setNewName] = useState('')
  // Optional: the server derives a slug from the name and truncates it to
  // fit. This field is the escape hatch for the one case it cannot resolve
  // on its own, a derived slug that collides with an existing display.
  const [newSlug, setNewSlug] = useState('')
  const [creating, setCreating] = useState(false)

  const pageCountFor = (displayId: string) => pages.filter((p) => p.display_id === displayId).length

  const handleCreate = async () => {
    const name = newName.trim()
    if (name === '' || name.length > DISPLAY_NAME_MAX_LEN) return
    setCreating(true)
    try {
      const slug = newSlug.trim()
      const created = await onCreate({ name, width: 1920, height: 1080, scale: 1, rotate: 0, ...(slug === '' ? {} : { slug }) })
      if (created) {
        setNewName('')
        setNewSlug('')
      }
    } finally {
      setCreating(false)
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[85vh] max-w-4xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Wall displays</DialogTitle>
            <DialogDescription>
              Each screen is its own record: name, address, canvas size and magnification.
              Putting a page on one moves it out of the Dashboard list into this display's own group.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-3">
            {displays.map((display) => (
              <DisplayRow
                key={display.id}
                display={display}
                pageCount={pageCountFor(display.id)}
                onUpdate={onUpdate}
                onRequestDelete={setPendingDelete}
                canWrite={canWrite}
              />
            ))}

            {displays.length === 0 && (
              <p className="text-sm text-muted-foreground">No displays yet. Add one below to put a page on a wall.</p>
            )}
          </div>

          {canWrite && (
            <div className="flex items-end gap-2 border-t border-border pt-3">
              <Field>
                <FieldLabel htmlFor="new-display-name">New display</FieldLabel>
                <Input
                  id="new-display-name"
                  value={newName}
                  maxLength={DISPLAY_NAME_MAX_LEN}
                  placeholder="Saloon TV"
                  onChange={(e) => setNewName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key !== 'Enter') return
                    e.preventDefault()
                    void handleCreate()
                  }}
                />
                <FieldDescription>Starts at 1920×1080, scale 1×, upright. Edit any of that once it's added.</FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="new-display-slug">Address</FieldLabel>
                <Input
                  id="new-display-slug"
                  value={newSlug}
                  maxLength={DISPLAY_SLUG_MAX_LEN}
                  placeholder="from the name"
                  onChange={(e) => setNewSlug(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key !== 'Enter') return
                    e.preventDefault()
                    void handleCreate()
                  }}
                />
                <FieldDescription>Optional. Sets the /display/… address.</FieldDescription>
              </Field>
              <Button disabled={newName.trim() === '' || creating} onClick={() => void handleCreate()}>
                <Plus className="size-3.5" />
                Add
              </Button>
            </div>
          )}

          <DialogFooter>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>Close</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pendingDelete !== null} onOpenChange={(isOpen) => { if (!isOpen) setPendingDelete(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pendingDelete ? `Delete ${pendingDelete.name}?` : 'Delete this display?'}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete && deleteConfirmationMessage(pendingDelete, pageCountFor(pendingDelete.id))}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => {
                if (pendingDelete) void onDelete(pendingDelete.id)
                setPendingDelete(null)
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
