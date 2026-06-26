import { useRef, useState } from 'react'
import { Upload, Trash2, MapPinned } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { SatChart } from '@/hooks/use-sat-charts'

interface SatChartsDrawerProps {
  charts: SatChart[]
  loading: boolean
  error: string | null
  uploadChart: (file: File) => Promise<SatChart | null>
  deleteChart: (id: string) => Promise<boolean>
}

export function SatChartsDrawer({ charts, loading, error, uploadChart, deleteChart }: SatChartsDrawerProps) {
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)

  async function handleFileSelected(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setUploading(true)
    setUploadError(null)
    try {
      const result = await uploadChart(file)
      if (!result) {
        setUploadError('Upload failed — check the file is a valid MBTiles export.')
      }
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Satellite Charts
        </h3>
        <Button type="button" size="sm" onClick={() => fileInputRef.current?.click()} disabled={uploading}>
          <Upload className="h-4 w-4" /> {uploading ? 'Uploading…' : 'Upload MBTiles'}
        </Button>
        <input
          ref={fileInputRef}
          type="file"
          accept=".mbtiles"
          className="hidden"
          onChange={handleFileSelected}
          data-testid="sat-chart-file-input"
        />
      </div>

      {uploadError && <p className="py-2 text-center text-sm text-amber-600">{uploadError}</p>}
      {error && <p className="py-2 text-center text-sm text-amber-600">{error}</p>}
      {loading && charts.length === 0 && (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading charts…</p>
      )}
      {!loading && charts.length === 0 && !error && (
        <p className="py-8 text-center text-sm text-muted-foreground">
          No satellite charts uploaded yet. Use SASPlanet/Sat2Chart (or any MBTiles exporter) to prepare a
          chart, then upload it here.
        </p>
      )}

      <ul className="space-y-2">
        {charts.map((chart) => (
          <li key={chart.id} className="rounded-md border border-border bg-background/60 px-3 py-2">
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-semibold text-foreground">{chart.name}</p>
                <p className="flex items-center gap-1 text-xs text-muted-foreground">
                  <MapPinned className="h-3 w-3 shrink-0" />
                  {chart.bounds.map((b) => b.toFixed(2)).join(', ')} · zoom {chart.minzoom}–{chart.maxzoom} ·{' '}
                  {(chart.size_bytes / 1024 / 1024).toFixed(1)} MB
                </p>
              </div>
              <button
                type="button"
                onClick={() => deleteChart(chart.id)}
                aria-label={`Delete ${chart.name}`}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-red-500/10 hover:text-red-600"
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}
