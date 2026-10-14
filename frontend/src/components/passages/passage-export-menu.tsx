import { Share2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { DistanceUnits } from '@/config/app-config'
import {
  DEFAULT_PLAY_SECONDS,
  PLAY_LENGTHS,
  exportFileName,
  generateEmbed,
  generateStandalonePage,
  prepareExport,
  type ExportInput,
  type PlaySeconds,
} from '@/lib/passage-export'
import { IMAGE_SIZES, type ImageKind } from '@/lib/passage-export-layout'
import { loadInsetPreference, saveInsetPreference } from '@/lib/passage-inset-preference'
import { vesselZone, type CruiseStats, type PassageDetail } from '@/lib/passages'

export interface PassageExportMenuProps {
  kind: ExportInput['kind']
  /** The card heading: a passage title, the cruise name, or a summary. */
  title: string
  passages: PassageDetail[]
  cruiseStats?: CruiseStats | null
  units: DistanceUnits
}

function download(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    link.click()
  } finally {
    URL.revokeObjectURL(url)
  }
}

function failed(action: string, err: unknown) {
  toast.error(`Could not ${action}`, { description: err instanceof Error ? err.message : String(err) })
}

/** Share a passage, a cruise or a selection: the blog embed and two images. */
export function PassageExportMenu({ kind, title, passages, cruiseStats, units }: PassageExportMenuProps) {
  const [playSeconds, setPlaySeconds] = useState<PlaySeconds>(DEFAULT_PLAY_SECONDS)
  const [inset, setInset] = useState(loadInsetPreference)
  const [busy, setBusy] = useState(false)
  const hasTrack = passages.some((p) => p.track.some((s) => s.length > 0))

  const exportInput = (): ExportInput => {
    const lon = passages.find((p) => p.start_lon !== null)?.start_lon ?? 0
    return { kind, title, passages, cruiseStats, units, zone: vesselZone(lon), inset, playSeconds }
  }
  const firstStart = () => Math.min(...passages.map((p) => Date.parse(p.start) / 1000))

  const copyEmbed = async () => {
    try {
      await navigator.clipboard.writeText(generateEmbed(exportInput()))
      toast.success('Blog embed copied', { description: 'Paste it into a Ghost HTML card.' })
    } catch (err) {
      failed('copy the blog embed', err)
    }
  }

  const downloadPage = () => {
    try {
      const input = exportInput()
      download(
        new Blob([generateStandalonePage(input)], { type: 'text/html' }),
        exportFileName(title, firstStart(), input.zone.offsetHours, 'html'),
      )
    } catch (err) {
      failed('download the embed', err)
    }
  }

  const downloadImage = async (imageKind: ImageKind) => {
    if (busy) return
    setBusy(true)
    const label = IMAGE_SIZES[imageKind].label
    const toastId = toast.loading(`Drawing the ${label} image`)
    try {
      const input = exportInput()
      const data = prepareExport(input)
      const { renderPassageImage } = await import('@/lib/passage-export-image')
      const blob = await renderPassageImage(data, imageKind)
      download(blob, exportFileName(title, firstStart(), input.zone.offsetHours, 'png'))
      toast.success('Image saved', { id: toastId })
    } catch (err) {
      toast.dismiss(toastId)
      failed('draw the image', err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button type="button" variant="outline" disabled={!hasTrack} aria-label="Export">
            <Share2 className="h-4 w-4" aria-hidden="true" />
            Export
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Play length</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={String(playSeconds)} onValueChange={(v) => setPlaySeconds(Number(v) as PlaySeconds)}>
            {PLAY_LENGTHS.map((s) => (
              <DropdownMenuRadioItem key={s} value={String(s)}>{s} seconds</DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuCheckboxItem
          checked={inset}
          onCheckedChange={(on) => { setInset(on); saveInsetPreference(on) }}
        >
          Overview map in the corner
        </DropdownMenuCheckboxItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => void copyEmbed()}>Copy blog embed</DropdownMenuItem>
        <DropdownMenuItem onClick={downloadPage}>Download embed as HTML</DropdownMenuItem>
        <DropdownMenuItem disabled={busy} onClick={() => void downloadImage('og')}>Download image {IMAGE_SIZES.og.label}</DropdownMenuItem>
        <DropdownMenuItem disabled={busy} onClick={() => void downloadImage('wide')}>Download image {IMAGE_SIZES.wide.label}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
