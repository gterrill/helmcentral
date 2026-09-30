import { useRef, useState, type DragEvent } from 'react'
import { Upload } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { createImportRun, type ImportRun } from '@/lib/import-run'
import { StepHeading } from '@/components/import/import-shared'
import { cn } from '@/lib/utils'

// Step 1: hand over the YachtWave "Vessel Export" page. The server decides
// whether it is one; its refusal is shown word for word.

export interface ImportUploadStepProps {
  onCreated: (run: ImportRun) => void
}

export function ImportUploadStep({ onCreated }: ImportUploadStepProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const choose = (picked: File | undefined) => {
    setError(null)
    if (picked) setFile(picked)
  }

  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragging(false)
    choose(event.dataTransfer.files[0])
  }

  const upload = async () => {
    if (file === null) return
    setUploading(true)
    setError(null)
    try {
      onCreated(await createImportRun('yachtwave', file))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setUploading(false)
    }
  }

  return (
    <div className="space-y-4">
      <StepHeading title="Upload your YachtWave export">
        In YachtWave, open the vessel and export the Vessel Export page, then pick the saved HTML file here. Nothing
        is added to Helmcentral until the last page.
      </StepHeading>

      <div
        onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        className={cn(
          'flex flex-col items-center gap-3 rounded-lg border border-dashed border-border p-8 text-center',
          dragging && 'border-primary bg-primary/5',
        )}
      >
        <Upload className="h-6 w-6 text-muted-foreground" aria-hidden="true" />
        <p className="text-sm text-muted-foreground">
          {file ? file.name : 'Drop the export file here, or choose it from this device.'}
        </p>
        <input
          ref={inputRef}
          type="file"
          accept=".html,.htm,text/html"
          className="sr-only"
          aria-label="YachtWave export file"
          onChange={(e) => choose(e.target.files?.[0])}
        />
        <Button type="button" variant="outline" onClick={() => inputRef.current?.click()}>
          Choose file
        </Button>
      </div>

      {error !== null && <p role="alert" className="text-sm text-destructive">{error}</p>}

      <div className="flex justify-end">
        <Button type="button" onClick={() => void upload()} disabled={file === null || uploading}>
          {uploading ? 'Reading export' : 'Read export'}
        </Button>
      </div>
    </div>
  )
}
