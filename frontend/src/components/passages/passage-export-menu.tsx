import { Share2 } from 'lucide-react'
import { useMemo, useState } from 'react'
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
import { useBoatHull } from '@/hooks/use-boat-hull'
import { useCameraShots } from '@/hooks/use-camera-shots'
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
import { use3dPreference } from '@/lib/passage-3d-preference'
import { IMAGE_SIZES, type ImageKind } from '@/lib/passage-export-layout'
import { useImageryPreference } from '@/lib/passage-imagery-preference'
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
  // Terrain in 3D follows the passage map: 3D on there, 3D here.
  const [threeD, setThreeD] = use3dPreference()
  const hull = useBoatHull()
  // Still images follow the passage map: satellite on there, satellite here.
  const [satellite] = useImageryPreference()
  const [busy, setBusy] = useState(false)
  const [embed, setEmbed] = useState<string | null>(null)
  const exportable = useMemo(() => exportablePassages(passages), [passages])
  const hasTrack = exportable.length > 0
  // Camera shots always go in. They belong to real time, so the ones saved
  // between the first departure and the last arrival are the ones that play.
  const span = useMemo(() => (
    hasTrack
      ? { from: Math.min(...exportable.map((x) => Date.parse(x.p.start))), to: Math.max(...exportable.map((x) => Date.parse(x.p.end))) }
      : null
  ), [exportable, hasTrack])
  const { shots, loading: shotsLoading, error: shotsError } = useCameraShots(span ? span.from : null, span ? span.to : null)

  const exportInput = (): ExportInput => ({ kind, title, passages, cruiseStats, units, zone: exportZone(passages), inset, playSeconds, threeD, shots, hull })
  // The file name follows the same passages, order and zone as the card itself.
  const fileName = (input: ExportInput, ext: string) => {
    const data = prepareExport(input)
    return exportFileName(title, data.start, data.zone.o, ext)
  }

  // An export without the shots the operator saved would be a different card
  // with no word said, so a shot list that failed to load stops the export.
  const shotsReady = () => {
    if (shotsError) {
      failed('read the camera shots', new Error(shotsError))
      return false
    }
    return true
  }

  const showEmbed = () => {
    if (!shotsReady()) return
    try {
      setEmbed(generateEmbed(exportInput()))
    } catch (err) {
      failed('make the blog embed', err)
    }
  }

  const downloadPage = () => {
    if (!shotsReady()) return
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
      const blob = await renderPassageImage(data, imageKind, satellite)
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
        <DropdownMenuCheckboxItem
          checked={threeD}
          onCheckedChange={setThreeD}
        >
          3D
        </DropdownMenuCheckboxItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={shotsLoading} onClick={showEmbed}>Copy blog embed</DropdownMenuItem>
        <DropdownMenuItem disabled={shotsLoading} onClick={downloadPage}>Download embed as HTML</DropdownMenuItem>
        <DropdownMenuItem disabled={busy} onClick={() => void downloadImage('og')}>Download image {IMAGE_SIZES.og.label}</DropdownMenuItem>
        <DropdownMenuItem disabled={busy} onClick={() => void downloadImage('wide')}>Download image {IMAGE_SIZES.wide.label}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    <PassageEmbedDialog embed={embed} onClose={() => setEmbed(null)} />
    </>
  )
}
