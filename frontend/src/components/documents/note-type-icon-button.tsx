import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { NoteType } from '@/hooks/use-notes'
import { NOTE_TYPE_META, NOTE_TYPE_ORDER, noteTypeMeta } from '@/lib/note-type-meta'
import { cn } from '@/lib/utils'

/**
 * The pre-attentive type icon column for a note row (revision "one panel,
 * not three") - its own hit target, tap to override in one tap,
 * independent of opening the note. Shared by every place Documents renders
 * a note row: the ordinary listing and Unfiled notes view
 * (documents-panel.tsx, unfiled-notes-view.tsx) both use this one component
 * rather than two copies of the same six-item dropdown - a filed note's
 * type is no harder to fix than an unfiled one's.
 *
 * `size` keeps each call site's own hit-target sizing rather than forcing
 * one on both: Unfiled notes view is the mobile-first drain-the-inbox row
 * (`lg`, an 11-unit/44px target), while the ordinary dense document table
 * matches its other row icons (`sm`, the default).
 */
export function NoteTypeIconButton({
  noteType,
  onSetType,
  size = 'sm',
}: {
  noteType: string
  onSetType: (type: NoteType) => void
  size?: 'sm' | 'lg'
}) {
  const meta = noteTypeMeta(noteType)
  const Icon = meta.icon
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={`Change type: currently ${meta.label}`}
            className={cn(
              'flex shrink-0 items-center justify-center hover:bg-accent',
              // `sm` keeps the full h-8 w-8 (32px) hit target - shrinking it
              // would fail the same tap-target minimum the `lg` variant
              // exists to satisfy on the Unfiled notes view - but a bare
              // h-4 w-4 MimeIcon on a file row has no such padding, so a
              // note row's icon (and, following it, the title text beside
              // it) sat ~8px further right than a file row's. -mx-2 (16px)
              // cancels the 32px button back down to the same 16px layout
              // footprint as the file row's icon, without touching the
              // button's own size or the icon inside it.
              size === 'lg' ? 'h-11 w-11' : 'h-8 w-8 -mx-2 rounded',
            )}
          >
            <Icon className={cn(size === 'lg' ? 'h-5 w-5' : 'h-4 w-4', meta.className)} aria-hidden="true" />
          </button>
        }
      />
      <DropdownMenuContent>
        {NOTE_TYPE_ORDER.map((type) => {
          const ItemIcon = NOTE_TYPE_META[type].icon
          return (
            <DropdownMenuItem key={type} onClick={() => onSetType(type)}>
              <ItemIcon className={cn('h-4 w-4', NOTE_TYPE_META[type].className)} aria-hidden="true" />
              {NOTE_TYPE_META[type].label}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
