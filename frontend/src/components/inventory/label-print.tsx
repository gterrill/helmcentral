import { useState } from 'react'
import { createPortal } from 'react-dom'
import { Printer } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { QrCode } from '@/components/inventory/qr-code'
import { binTagPath, tagUrl } from '@/lib/tag-url'

// ADR 0152: printable QR labels. A label is a QR code of the same address the
// NFC tag holds, with the bin code (or equipment name) printed large beneath.
// The dialog shows a preview; a second copy sits in a portal on <body> that
// the @media print rules in index.css leave as the only visible thing.

export interface LabelSpec {
  key: string
  path: string
  /** Printed large under the QR code. */
  code: string
  /** Smaller line beneath the code. */
  caption?: string
}

function Label({ spec }: { spec: LabelSpec }) {
  return (
    <figure className="label-cell flex min-w-0 flex-col items-center gap-2 p-3">
      <QrCode value={tagUrl(spec.path)} className="aspect-square w-full" />
      <figcaption className="flex w-full min-w-0 flex-col items-center gap-1 text-center">
        <span className="label-code w-full break-words font-display text-3xl font-bold leading-tight">{spec.code}</span>
        {spec.caption && <span className="w-full truncate text-xs">{spec.caption}</span>}
      </figcaption>
    </figure>
  )
}

function LabelSheet({ labels, sheet }: { labels: LabelSpec[]; sheet: boolean }) {
  return (
    <div
      data-testid="label-sheet"
      className={sheet ? 'label-sheet grid grid-cols-3 gap-0' : 'label-single mx-auto w-64'}
    >
      {labels.map((spec) => <Label key={spec.key} spec={spec} />)}
    </div>
  )
}

interface LabelDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  labels: LabelSpec[]
  sheet: boolean
  emptyText?: string
}

function LabelDialog({ open, onOpenChange, title, description, labels, sheet, emptyText }: LabelDialogProps) {
  const empty = labels.length === 0
  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          {empty ? (
            <p className="text-sm text-muted-foreground">{emptyText}</p>
          ) : (
            <div className="rounded-md border border-border bg-white text-black">
              <LabelSheet labels={labels} sheet={sheet} />
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Close</Button>
            <Button type="button" disabled={empty} className="gap-1.5" onClick={() => window.print()}>
              <Printer className="h-4 w-4" aria-hidden="true" />
              Print
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {open && !empty && createPortal(
        <div className="label-print-root"><LabelSheet labels={labels} sheet={sheet} /></div>,
        document.body,
      )}
    </>
  )
}

interface PrintLabelButtonProps {
  path: string
  code: string
  caption?: string
}

/** "Print label" for one bin or equipment item, shown beside its tag row. */
export function PrintLabelButton({ path, code, caption }: PrintLabelButtonProps) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button type="button" variant="outline" size="sm" className="w-fit gap-1.5" onClick={() => setOpen(true)}>
        <Printer className="h-3.5 w-3.5" aria-hidden="true" />
        Print label
      </Button>
      <LabelDialog
        open={open}
        onOpenChange={setOpen}
        title="Print label"
        description="Scanning the code opens this page for anyone who can reach the boat."
        labels={[{ key: path, path, code, caption }]}
        sheet={false}
      />
    </>
  )
}

export interface BinLabelSource {
  id: string
  code: string
  zoneName: string
}

interface PrintBinLabelsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  bins: BinLabelSource[]
}

/** Every bin's label on one sheet, three across, to print and cut out. */
export function PrintBinLabelsDialog({ open, onOpenChange, bins }: PrintBinLabelsDialogProps) {
  const labels = bins.map((b) => ({ key: b.id, path: binTagPath(b.code), code: b.code, caption: b.zoneName }))
  return (
    <LabelDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Print bin labels"
      description="One label per bin. Print on plain paper and cut along the dashed lines."
      labels={labels}
      sheet
      emptyText="No bins to print yet."
    />
  )
}
