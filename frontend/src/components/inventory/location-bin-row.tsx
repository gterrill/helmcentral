import { useEffect, useState } from 'react'
import { ArrowUpRight, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { InventoryBin } from '@/hooks/use-inventory'

// One bin inside a location's page: a <li> for the ResourceList frame, with
// the bin's code and name editable in place. Lifted out of the old
// LocationsSection unchanged in behaviour (ADR 0123, ADR 0127); the review
// fixes recorded below are why its state is shaped the way it is.

interface BinRowProps {
  bin: InventoryBin
  canWrite: boolean
  /** Resolves true on a successful rename (or a no-op commit that changed
   * nothing), false on a failed one - BinRow's own commit() awaits this to
   * decide whether the "tags no longer open this bin" warning belongs on
   * screen, rather than showing it optimistically before the write is even
   * known to have landed. */
  onRename: (id: string, code: string, name: string, original: { code: string; name: string }) => Promise<boolean>
  onDelete: (id: string) => void
  onOpen: (code: string) => void
}

/**
 * Code review: code and name used to be two separate <Input>s in the parent
 * map, each committing on blur with `bin.code`/`bin.name` filled in for the
 * OTHER field - those are LocationEditor's own props as of its last
 * render, not what the operator has typed since. Editing code, tabbing to
 * name, typing there and blurring fired the name commit with the CODE
 * field's pre-edit prop value, reverting the code change that was still
 * in flight (the rename PUT hadn't resolved and re-rendered LocationEditor
 * yet). Pulling both fields into their own row component with co-located
 * state fixes it structurally: `code`/`name` here are state, not props, so
 * one field's onBlur always reads the other field's latest typed value from
 * this same render, never a stale one from further up the tree.
 *
 * Code review: this row used to remount on ANY server-confirmed rename
 * (keyed on `${bin.id}-${bin.code}-${bin.name}`), to keep a saved field in
 * sync with the refetch that follows its own PUT. That's too broad: a code
 * blur's refetch changes the key, and the whole row - including the NAME
 * field the operator has since tabbed into and is still typing - gets torn
 * down and rebuilt from props, losing focus and the typed text. Keyed on
 * bin.id alone below, the row never remounts on a rename. Each field
 * instead re-syncs from its OWN prop in its own effect, so a
 * server-confirmed code change re-syncs code without touching name (and
 * vice versa) - the field the operator isn't touching catches up to the
 * server, the field they are stays exactly as typed.
 */
export function BinRow({ bin, canWrite, onRename, onDelete, onOpen }: BinRowProps) {
  const [code, setCode] = useState(bin.code)
  const [name, setName] = useState(bin.name)
  // ADR 0127: "Renaming a code orphans that bin's tags, and the rename UI
  // says so." Non-blocking (a plain muted line under the row, not a
  // confirm dialog that would slow down an ordinary rename) - the operator
  // typed the new code deliberately, this is information, not a gate.
  // Cleared on the NEXT commit (whatever it says) rather than lingering
  // forever, and never shown for a first render / a server-driven resync
  // (the effects above), only for a code that actually changed under the
  // operator's own edit.
  //
  // Review finding: this used to be set BEFORE onRename's own PUT had even
  // resolved, and was never cleared if that PUT then failed - so a failed
  // rename left a permanent, wrong "tags no longer open this bin" notice
  // under a bin whose code never actually changed. commit() now awaits
  // onRename and only shows the warning once it reports success; any other
  // outcome (a no-op commit, or a failed write) clears it instead. The
  // failure itself still surfaces - onRename's own caller
  // (LocationEditor's handleRenameBin) sets the shared actionError banner
  // on the same rejection this awaits.
  const [renameWarning, setRenameWarning] = useState<string | null>(null)

  useEffect(() => { setCode(bin.code) }, [bin.code])
  useEffect(() => { setName(bin.name) }, [bin.name])

  const commit = async () => {
    const trimmedCode = code.trim()
    const codeChanged = trimmedCode !== '' && trimmedCode !== bin.code
    const previousCode = bin.code
    const ok = await onRename(bin.id, code, name, { code: bin.code, name: bin.name })
    setRenameWarning(codeChanged && ok ? `Tags written for ${previousCode} no longer open this bin.` : null)
  }

  return (
    <li className="flex flex-col gap-1 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={`Open bin ${bin.code}`}
          className="h-9 shrink-0 justify-start gap-1 px-2 font-mono text-primary"
          onClick={() => onOpen(bin.code)}
        >
          <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
          {bin.code}
        </Button>
        <Input
          value={code}
          aria-label="Bin code"
          className="h-9 w-28 font-mono"
          disabled={!canWrite}
          onChange={(e) => setCode(e.target.value)}
          onBlur={() => { void commit() }}
        />
        <Input
          value={name}
          aria-label="Bin name"
          placeholder="Name (optional)"
          className="h-9 min-w-0 flex-1 basis-40"
          disabled={!canWrite}
          onChange={(e) => setName(e.target.value)}
          onBlur={() => { void commit() }}
        />
        {canWrite && (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={`Remove bin ${bin.code}`}
            onClick={() => onDelete(bin.id)}
          >
            <Trash2 className="h-4 w-4" aria-hidden="true" />
          </Button>
        )}
      </div>
      {renameWarning && (
        <p className="pl-2 text-xs text-muted-foreground">{renameWarning}</p>
      )}
    </li>
  )
}
