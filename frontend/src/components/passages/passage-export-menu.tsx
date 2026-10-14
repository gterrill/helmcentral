import { Share2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { PassageEmbedDialog } from '@/components/passages/passage-embed-dialog'
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
  exportablePassages,
  exportZone,
  generateEmbed,
  generateStandalonePage,
  prepareExport,
  type ExportInput,
  type PlaySeconds,
} from '@/lib/passage-export'
import { IMAGE_SIZES, type ImageKind } from '@/lib/passage-export-layout'
import { useInsetPreference } from '@/lib/passage-inset-preference'
import type { CruiseStats, PassageDetail } from '@/lib/passages'
import { saveBlob } from '@/lib/save-blob'

export interface PassageExportMenuProps {
  kind: ExportInput['kind']
  /** The card heading: a passage title, the cruise name, or a summary. */
  title: string
  passages: PassageDetail[]
  cruiseStats?: CruiseStats | null
  units: DistanceUnits
}

function failed(action: string, err: unknown) {
  toast.error(`Could not ${action}`, { description: err instanceof Error ? err.message : String(err) })
}

/** Share a passage, a cruise or a selection: the blog embed and two images. */
export function PassageExportMenu({ kind, title, passages, cruiseStats, units }: PassageExportMenuProps) {
  const [playSeconds, setPlaySeconds] = useState<PlaySeconds>(DEFAULT_PLAY_SECONDS)
  const [inset, setInset] = useInsetPreference()
  const [busy, setBusy] = useState(false)
  const [embed, setEmbed] = useState<string | null>(null)
  const hasTrack = exportablePassages(passages).length > 0

  const exportInput = (): ExportInput => ({ kind, title, passages, cruiseStats, units, zone: exportZone(passages), inset, playSeconds })
  // The file name follows the same passages, order and zone as the card itself.
  const fileName = (input: ExportInput, ext: string) => {
    const data = prepareExport(input)
    return exportFileName(title, data.start, data.zone.o, ext)
  }

  const showEmbed = () => {
    try {
      setEmbed(generateEmbed(exportInput()))
    } catch (err) {
      failed('make the blog embed', err)
    }
  }

  const downloadPage = () => {
    try {
      const input = exportInput()
      saveBlob(new Blob([generateStandalonePage(input)], { type: 'text/html' }), fileName(input, 'html'))
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
      saveBlob(blob, fileName(input, 'png'))
      toast.success('Image saved', { id: toastId })
    } catch (err) {
      toast.dismiss(toastId)
      failed('draw the image', err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
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
          onCheckedChange={setInset}
        >
          Overview map in the corner
        </DropdownMenuCheckboxItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={showEmbed}>Copy blog embed</DropdownMenuItem>
        <DropdownMenuItem onClick={downloadPage}>Download embed as HTML</DropdownMenuItem>
        <DropdownMenuItem disabled={busy} onClick={() => void downloadImage('og')}>Download image {IMAGE_SIZES.og.label}</DropdownMenuItem>
        <DropdownMenuItem disabled={busy} onClick={() => void downloadImage('wide')}>Download image {IMAGE_SIZES.wide.label}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    <PassageEmbedDialog embed={embed} onClose={() => setEmbed(null)} />
    </>
  )
}
