import { useRef, useState, type ClipboardEvent, type DragEvent } from 'react'
import { ExternalLink } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { documentContentUrl } from '@/lib/document-download'
import { fileSettled, uploadImportFile, usableEquipment, type ImportRun, type StagedFile } from '@/lib/import-run'
import { ImportSelect, Pill, StepHeading, type StepProps } from '@/components/import/import-shared'
import { cn } from '@/lib/utils'

// Step 8, once per photo or document. YachtWave's links point at its own
// servers and Helmcentral never fetches them: the operator opens the link,
// saves or copies the file, and hands it over here by paste, drop or picker.
// The page is settled when the file is uploaded or skipped.

export interface ImportFileStepProps extends StepProps {
  file: StagedFile
  position: { index: number; total: number }
  onUploaded: (run: ImportRun, fileKey: string) => void
}

function extensionFor(type: string): string {
  if (type === 'image/jpeg') return 'jpg'
  if (type === 'image/png') return 'png'
  if (type === 'application/pdf') return 'pdf'
  return 'bin'
}

export function ImportFileStep({ run, decisions, setDecision, file, position, onUploaded }: ImportFileStepProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const decision = decisions.files[file.key] ?? { document_id: '', skipped: false, equipment_key: '' }
  const settled = fileSettled(decision)
  const uploaded = decision.document_id !== '' && !decision.skipped
  const isPhoto = file.kind === 'photo'

  const send = async (blob: Blob | undefined, name?: string) => {
    if (blob === undefined) return
    setUploading(true)
    setError(null)
    try {
      const filename = name && name !== '' ? name : `${file.label.replace(/[^\w.-]+/g, '-')}.${extensionFor(blob.type)}`
      const result = await uploadImportFile(run.id, file.key, blob, filename)
      onUploaded(result.run, file.key)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setUploading(false)
    }
  }

  const onPaste = (event: ClipboardEvent<HTMLDivElement>) => {
    const pasted = event.clipboardData.files[0]
    if (pasted === undefined) return
    event.preventDefault()
    void send(pasted, pasted.name)
  }

  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragging(false)
    const dropped = event.dataTransfer.files[0]
    void send(dropped, dropped?.name)
  }

  return (
    <div className="space-y-4">
      <StepHeading title={`${isPhoto ? 'Photo' : 'Document'} ${position.index + 1} of ${position.total}`}>
        Open the link, save the file, then paste it, drop it or choose it below. Or skip it.
      </StepHeading>

      <div className="space-y-2 rounded-md border border-border bg-card p-3">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h3 className="min-w-0 truncate text-sm font-medium">{file.label}</h3>
          <Pill>{isPhoto ? 'Photo' : 'Document'}</Pill>
        </div>
        <dl className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
          <div className="min-w-0">
            <dt className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Type</dt>
            <dd className="truncate">{file.type || (isPhoto ? 'Photo' : '--')}</dd>
          </div>
          <div className="min-w-0">
            <dt className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Attached to</dt>
            <dd className="truncate">{file.attached_to || '--'}</dd>
          </div>
          <div className="min-w-0">
            <dt className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Date</dt>
            <dd className="truncate tabular-nums">{file.date || '--'}</dd>
          </div>
          <div className="min-w-0">
            <dt className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Size</dt>
            <dd className="truncate">{file.size || '--'}</dd>
          </div>
        </dl>
        <a
          href={file.url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-sm text-primary underline-offset-4 hover:underline"
        >
          Open in YachtWave
          <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
        </a>
      </div>

      {!decision.skipped && (
        <div
          tabIndex={0}
          role="group"
          aria-label="Paste or drop the file here"
          onPaste={onPaste}
          onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
          className={cn(
            'flex min-h-32 flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border p-4 text-center focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring',
            dragging && 'border-primary bg-primary/5',
          )}
        >
          {uploaded && isPhoto && (
            <img
              src={documentContentUrl(decision.document_id)}
              alt={`Uploaded ${file.label}`}
              className="max-h-64 max-w-full rounded-md object-contain"
            />
          )}
          <p className="text-sm text-muted-foreground">
            {uploaded
              ? 'File received. Paste or drop another to replace it.'
              : 'Click here and paste, drop the file, or choose it.'}
          </p>
          <input
            ref={inputRef}
            type="file"
            accept={isPhoto ? 'image/jpeg,image/png,.jpe,.jpg,.jpeg,.png' : undefined}
            className="sr-only"
            aria-label="Choose the file"
            onChange={(e) => {
              const picked = e.target.files?.[0]
              void send(picked, picked?.name)
              e.target.value = ''
            }}
          />
          <Button type="button" variant="outline" disabled={uploading} onClick={() => inputRef.current?.click()}>
            {uploading ? 'Uploading' : 'Choose file'}
          </Button>
        </div>
      )}

      {error !== null && <p role="alert" className="text-sm text-destructive">{error}</p>}

      {decision.skipped && <p role="status" className="text-sm text-muted-foreground">This file will be skipped.</p>}

      {!decision.skipped && (run.staged.equipment.length > 0) && (
        <label className="flex max-w-md flex-col gap-1">
          <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Attach to equipment</span>
          <ImportSelect
            aria-label="Attach to equipment"
            value={decision.equipment_key}
            onChange={(e) => setDecision('files', file.key, { ...decision, equipment_key: e.target.value })}
          >
            <option value="">Keep as a document, not attached</option>
            {usableEquipment(run, decisions).map((e) => (
              <option key={e.key} value={e.key}>{e.name}{e.detail ? `, ${e.detail}` : ''}</option>
            ))}
          </ImportSelect>
        </label>
      )}

      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => setDecision('files', file.key, { ...decision, skipped: !decision.skipped })}
        >
          {decision.skipped ? 'Undo skip' : 'Skip this file'}
        </Button>
        {!settled && <span className="text-xs text-muted-foreground">Upload or skip to go on.</span>}
      </div>
    </div>
  )
}
