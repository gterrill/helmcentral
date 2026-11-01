import { Share2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { PassageEmbedDialog } from '@/components/passages/passage-embed-dialog'
import { PassageVideoDialog, type PassageVideoSource } from '@/components/passages/passage-video-dialog'
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
import { useInsetView } from '@/hooks/use-inset-view'
import { DEFAULT_REPLAY_LENGTH, replaySeconds, type ReplaySpeedId } from '@/hooks/use-replay'
import { useReplaySigns } from '@/hooks/use-replay-signs'
import {
  DEFAULT_PLAY_SECONDS,
  PLAY_LENGTHS,
  estimateEmbed,
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
import { insetViewRange } from '@/lib/passage-inset-view'
import { useInsetPreference } from '@/lib/passage-inset-preference'
import type { CruiseStats, PassageDetail } from '@/lib/passages'
import { prepareSignPhotos } from '@/lib/passage-export-photos'
import { CARD_LIMIT_BYTES, fmtMB, signSummary, signsInRange } from '@/lib/passage-export-signs'
import { saveBlob } from '@/lib/save-blob'
import { errorMessage } from '@/lib/errors'

export interface PassageExportMenuProps {
  kind: ExportInput['kind']
  /** The card heading: a passage title, the cruise name, or a summary. */
  title: string
  passages: PassageDetail[]
  cruiseStats?: CruiseStats | null
  units: DistanceUnits
  /** The app theme, which the replay's map follows and so does the video's. */
  isDarkTheme?: boolean
  /** The length the replay beside this menu is set to; the video is made to it. */
  replayLength?: ReplaySpeedId
}

const NO_PHOTOS: ReadonlyMap<string, string> = new Map()

function failed(action: string, err: unknown) {
  toast.error(`Could not ${action}`, { description: errorMessage(err) })
}

/** Share a passage, a cruise or a selection: the blog embed and two images. */
export function PassageExportMenu({ kind, title, passages, cruiseStats, units, isDarkTheme = false, replayLength = DEFAULT_REPLAY_LENGTH }: PassageExportMenuProps) {
  const [playSeconds, setPlaySeconds] = useState<PlaySeconds>(DEFAULT_PLAY_SECONDS)
  const [inset, setInset] = useInsetPreference()
  // Terrain in 3D follows the passage map: 3D on there, 3D here.
  const [threeD, setThreeD] = use3dPreference()
  const hull = useBoatHull()
  // Still images follow the passage map: satellite on there, satellite here.
  const [satellite] = useImageryPreference()
  const [busy, setBusy] = useState(false)
  const [embed, setEmbed] = useState<string | null>(null)
  const [videoOpen, setVideoOpen] = useState(false)
  const exportable = useMemo(() => exportablePassages(passages), [passages])
  const hasTrack = exportable.length > 0
  // Camera shots always go in. They belong to real time, so the ones saved
  // between the first departure and the last arrival are the ones that play.
  // The span is over every passage shown, as the replay's own is, so the video
  // is made from the same shots and signs the replay beside this menu plays.
  const span = useMemo(() => (
    hasTrack
      ? { from: Math.min(...passages.map((p) => Date.parse(p.start))), to: Math.max(...passages.map((p) => Date.parse(p.end))) }
      : null
  ), [passages, hasTrack])
  const { shots, loading: shotsLoading, error: shotsError } = useCameraShots(span ? span.from : null, span ? span.to : null)
  // The overview map's corner and zoom step are saved with the replay, keyed by
  // its range over every passage shown (the key the replay saves under); the
  // card and the images draw it the way the replay does.
  const insetRange = useMemo(() => insetViewRange(passages), [passages])
  const { view: insetView, loading: insetViewLoadingRaw, error: insetViewErrorRaw } = useInsetView(hasTrack ? insetRange : null)
  // The placement only matters when the overview map is drawn: with it off, a
  // placement still loading or failed to load must not hold up an export.
  const insetViewLoading = inset && insetViewLoadingRaw
  const insetViewError = inset ? insetViewErrorRaw : null

  // Signs belong to real time too. The ones inside an exported passage go in
  // when Include signs is on, photos included.
  const [includeSigns, setIncludeSigns] = useState(true)
  const { signs: allSigns, loading: signsLoading, error: signsError } = useReplaySigns(span ? span.from : null, span ? span.to : null)
  const signsInCard = useMemo(
    () => signsInRange(allSigns, exportable.map((x) => ({ a: Date.parse(x.p.start) / 1000, b: Date.parse(x.p.end) / 1000 }))),
    [allSigns, exportable],
  )
  const photoSigns = useMemo(() => signsInCard.filter((s) => s.photo_url), [signsInCard])
  const [photos, setPhotos] = useState<ReadonlyMap<string, string>>(new Map())
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
  const [photoError, setPhotoError] = useState<string | null>(null)
  // The photo signs the card needs, by sign and document: a change in the set
  // is a different job, and a run for an earlier set is not the answer to it.
  const photoKey = useMemo(() => photoSigns.map((s) => `${s.id}:${s.document_id}`).sort().join('|'), [photoSigns])
  const latestKey = useRef(photoKey)
  latestKey.current = photoKey
  const preparing = useRef<{ key: string; promise: Promise<ReadonlyMap<string, string>> } | null>(null)
  const ready = photoSigns.every((s) => photos.has(s.id))

  // Photos are fetched, shrunk and cleaned in this browser, one run per set of
  // photos. Everything prepared already is handed back as it is.
  const ensurePhotos = (): Promise<ReadonlyMap<string, string>> => {
    if (!includeSigns || photoSigns.length === 0) return Promise.resolve(NO_PHOTOS)
    if (ready) return Promise.resolve(photos)
    const key = photoKey
    if (preparing.current?.key === key) return preparing.current.promise
    setPhotoError(null)
    const current = () => latestKey.current === key
    const promise = prepareSignPhotos(photoSigns, { onProgress: (done, total) => { if (current()) setProgress({ done, total }) } })
      .then((prepared) => { if (current()) setPhotos(prepared); return prepared })
      .catch((err: unknown) => { if (current()) setPhotoError(errorMessage(err)); throw err })
      .finally(() => { if (preparing.current?.key === key) { preparing.current = null; setProgress(null) } })
    preparing.current = { key, promise }
    return promise
  }
  const [menuOpen, setMenuOpen] = useState(false)
  useEffect(() => {
    // Ready by the time the operator chooses, and the size shown is true.
    if (menuOpen && includeSigns && photoSigns.length > 0 && !signsLoading) ensurePhotos().catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [menuOpen, includeSigns, photoKey, signsLoading])

  const exportInput = (prepared: ReadonlyMap<string, string> = photos): ExportInput => ({
    kind, title, passages, cruiseStats, units, zone: exportZone(passages), inset, ...(insetView ? { insetView } : {}), playSeconds, threeD, shots, hull,
    joins: allSigns,
    ...(includeSigns ? { signs: signsInCard, photos: prepared } : {}),
  })

  // Join signs split the track into legs, so every export needs the signs,
  // whether or not Include signs draws them.
  const signsWait = signsLoading
  const signsFailed = signsError !== null

  // The card's size: exact once every photo is prepared, a guess (kept on the
  // high side) before that.
  const size = useMemo(() => {
    if (!hasTrack || shotsLoading || shotsError || insetViewLoading || insetViewError || signsWait || signsFailed) return null
    try {
      return estimateEmbed(exportInput())
    } catch {
      return null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasTrack, shotsLoading, shotsError, insetViewLoading, insetViewError, signsWait, signsFailed, signsInCard, photos, includeSigns, kind, title, passages, cruiseStats, units, inset, insetView, playSeconds, threeD, shots, hull])
  // The file name follows the same passages, order and zone as the card itself.
  const fileName = (input: ExportInput, ext: string) => {
    const data = prepareExport(input)
    return exportFileName(title, data.start, data.zone.o, ext)
  }

  // An export drawn without the overview placement the operator saved would be
  // a different picture with no word said, so images stop for it too.
  const insetReady = () => {
    if (insetViewError) {
      failed('read where the overview map goes', new Error(insetViewError))
      return false
    }
    return true
  }

  // An export without the shots or signs the operator saved would be a
  // different card with no word said, so a list that failed to load stops it.
  const listsReady = () => {
    if (shotsError) {
      failed('read the camera shots', new Error(shotsError))
      return false
    }
    if (!insetReady()) return false
    if (signsFailed) {
      failed('read the signs', new Error(signsError as string))
      return false
    }
    return true
  }

  // The export input with every photo prepared, or null after saying why not.
  const readyInput = async (action: string): Promise<ExportInput | null> => {
    if (!listsReady()) return null
    const toastId = photoSigns.length > 0 && includeSigns && !ready ? toast.loading('Preparing sign photos') : null
    try {
      return exportInput(await ensurePhotos())
    } catch (err) {
      failed(action, err)
      return null
    } finally {
      if (toastId !== null) toast.dismiss(toastId)
    }
  }

  const showEmbed = async () => {
    const input = await readyInput('make the blog embed')
    if (!input) return
    try {
      setEmbed(generateEmbed(input))
    } catch (err) {
      failed('make the blog embed', err)
    }
  }

  const downloadPage = async () => {
    const input = await readyInput('download the embed')
    if (!input) return
    try {
      saveBlob(new Blob([generateStandalonePage(input)], { type: 'text/html' }), fileName(input, 'html'))
    } catch (err) {
      failed('download the embed', err)
    }
  }

  // The video leaves the overview map out, so only the shots and signs must have been read.
  const openVideo = () => {
    if (shotsError) { failed('read the camera shots', new Error(shotsError)); return }
    if (signsFailed) { failed('read the signs', new Error(signsError as string)); return }
    setVideoOpen(true)
  }

  // Read when the video is started, so it is the replay as it is then.
  const videoSource = (): PassageVideoSource => ({
    passages,
    shots,
    signs: allSigns,
    hull,
    threeD,
    satellite,
    isDarkTheme,
    lengthS: replaySeconds(replayLength),
    summary: prepareExport({ ...exportInput(), signs: undefined, photos: undefined }),
  })

  const downloadImage = async (imageKind: ImageKind) => {
    if (busy || !insetReady()) return
    if (signsFailed) {
      failed('read the signs', new Error(signsError as string))
      return
    }
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
    <DropdownMenu onOpenChange={setMenuOpen}>
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
        <DropdownMenuCheckboxItem
          checked={includeSigns}
          onCheckedChange={setIncludeSigns}
        >
          Include signs
        </DropdownMenuCheckboxItem>
        <div className="px-2 pb-1.5 text-xs text-muted-foreground" data-testid="sign-summary">
          <p>{signsLoading ? 'Reading signs' : signsError ? 'Signs could not be read' : signSummary(signsInCard)}</p>
          {includeSigns && progress && <p role="status">Preparing photos, {progress.done} of {progress.total}</p>}
          {includeSigns && photoError && <p role="alert" className="text-destructive">{photoError}</p>}
          {size && (
            <p data-testid="card-size" className={size.bytes > CARD_LIMIT_BYTES ? 'font-medium text-destructive' : undefined}>
              {size.bytes > CARD_LIMIT_BYTES
                ? `Card ${size.exact ? '' : 'about '}${fmtMB(size.bytes)}, over the ${CARD_LIMIT_BYTES / (1024 * 1024)} MB limit`
                : `Card ${size.exact ? '' : 'about '}${fmtMB(size.bytes)}`}
            </p>
          )}
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={shotsLoading || insetViewLoading || signsWait} onClick={() => void showEmbed()}>Copy blog embed</DropdownMenuItem>
        <DropdownMenuItem disabled={shotsLoading || insetViewLoading || signsWait} onClick={() => void downloadPage()}>Download embed as HTML</DropdownMenuItem>
        <DropdownMenuItem disabled={busy || insetViewLoading || signsWait} onClick={() => void downloadImage('og')}>Download image {IMAGE_SIZES.og.label}</DropdownMenuItem>
        <DropdownMenuItem disabled={busy || insetViewLoading || signsWait} onClick={() => void downloadImage('wide')}>Download image {IMAGE_SIZES.wide.label}</DropdownMenuItem>
        <DropdownMenuItem disabled={shotsLoading || signsWait} onClick={openVideo}>Video</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    <PassageEmbedDialog embed={embed} onClose={() => setEmbed(null)} />
    <PassageVideoDialog open={videoOpen} onClose={() => setVideoOpen(false)} source={videoSource} />
    </>
  )
}
