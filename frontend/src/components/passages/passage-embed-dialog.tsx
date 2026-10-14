import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { copyTextFrom } from '@/lib/copy-text'

export interface PassageEmbedDialogProps {
  /** The embed to show; null keeps the dialog closed. */
  embed: string | null
  onClose: () => void
}

/** The blog embed in a selected, read-only box with a Copy button. */
export function PassageEmbedDialog({ embed, onClose }: PassageEmbedDialogProps) {
  return (
    <Dialog open={embed !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-2xl">
        {embed !== null && <EmbedBody embed={embed} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function EmbedBody({ embed, onClose }: { embed: string; onClose: () => void }) {
  const area = useRef<HTMLTextAreaElement>(null)
  const [state, setState] = useState<'idle' | 'copied' | 'manual'>('idle')

  // Selected as soon as it opens, so Ctrl+C works without a click.
  useEffect(() => {
    area.current?.focus()
    area.current?.select()
  }, [])

  const copy = async () => {
    if (!area.current) return
    const ok = await copyTextFrom(area.current, embed)
    setState(ok ? 'copied' : 'manual')
    if (ok) toast.success('Blog embed copied', { description: 'Paste it into a Ghost HTML card.' })
  }

  return (
    <div className="flex flex-col gap-4">
      <DialogHeader>
        <DialogTitle>Blog embed</DialogTitle>
        <DialogDescription>Copy this into a Ghost HTML card.</DialogDescription>
      </DialogHeader>
      <Textarea
        readOnly
        aria-label="Blog embed"
        className="h-64 resize-none font-mono text-xs"
        value={embed}
        ref={area}
      />
      {state === 'manual' && (
        <p role="status" className="text-sm text-muted-foreground">Press ⌘C (Ctrl+C) to copy the selected text</p>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>Close</Button>
        <Button type="button" onClick={() => void copy()}>{state === 'copied' ? 'Copied' : 'Copy'}</Button>
      </DialogFooter>
    </div>
  )
}
