import { toast } from 'sonner'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'

import { CameraEditor, type CameraShotChange } from '@/components/passages/camera-editor'
import { SignPhotoDialog, signPhotoSrc } from '@/components/passages/sign-card'
import { SignEditor, type SignChange } from '@/components/passages/sign-editor'
import { SignMarkers } from '@/components/passages/sign-markers'
import type { DocumentLinkPickerResult } from '@/components/inventory/document-link-picker'
import { InsetMap, PassageMap, type CameraDriver, type PassageMapHandle } from '@/components/passages/passage-map'
import { use3dPreference } from '@/lib/passage-3d-preference'
import { useInsetPreference } from '@/lib/passage-inset-preference'
import { PassageReplayBar } from '@/components/passages/passage-replay-bar'
import { PhotoMoment } from '@/components/passages/photo-moment'
import { MapLabel } from '@/components/passages/map-label'
import { passageLabelBox, passageLabelsOf } from '@/lib/replay-passage-label'
import { useElementSize } from '@/hooks/use-element-size'
import { useBoatHull } from '@/hooks/use-boat-hull'
import { useSignMoments } from '@/hooks/use-sign-moments'
import { cameraShotsKey, useCameraShots } from '@/hooks/use-camera-shots'
import { useInsetView } from '@/hooks/use-inset-view'
import { insetViewApi } from '@/lib/inset-view-api'
import { insetViewRange, type InsetView } from '@/lib/passage-inset-view'
import { usePrefersReducedMotion } from '@/hooks/use-prefers-reduced-motion'
import { cameraShotsApi, type CameraShotPatch } from '@/lib/camera-shots-api'
import { getQueryData, setQueryData } from '@/lib/api-query'
import { replaySignsKey } from '@/hooks/use-replay-signs'
import { replaySignsApi, type ReplaySignPatch } from '@/lib/replay-signs-api'
import { usePassageLegs } from '@/hooks/use-passage-legs'
import { labelledSigns, legPassages, piecesOf, playableEntryAt, playableShots, replayItems, wantsHeading } from '@/lib/replay-plan'
import { boatOffsetHours as boatOffsetOf, staysOf, stayLabelBox } from '@/lib/replay-stay'
import { AWAITING_NAME, placeAt, resolveSigns, type ReplaySign, SIGN_LIFT_PX, SIGN_SHOW_DEFAULT, SIGN_TITLE_MAX } from '@/lib/replay-signs'
import { moveByCapture, placeByCapture } from '@/lib/sign-placement'
import { captureOfDocument, captureOfFile, uploadSignPhoto } from '@/lib/sign-photo'
import { shotStartState } from '@/lib/replay-camera'
import { playedIds } from '@/lib/replay-time'
import { INTRO_MS, introStartCamera } from '@/lib/replay-intro'
import { ReplayController } from '@/lib/replay-controller'
import { cruisePaceSpans, entryAt, type CameraMode, type CameraShot } from '@/lib/replay-clock'
import { introLanding } from '@/lib/replay-framing'
import { REPLAY_3D_PITCH } from '@/lib/terrain'
import { useReplay } from '@/hooks/use-replay'
import { replaySeconds, type ReplayLength, type ReplaySpeedId } from '@/lib/replay-lengths'
import {
  buildTimeline,
  clockFor,
  isMultiPassage,
  formatLocalDateTime, localDateTimeParts,
  segmentBounds,
  type PassageDetail,
} from '@/lib/passages'
import { replayClockOffsetHours } from '@/lib/dev-flags'
import { errorMessage } from '@/lib/errors'

/** The passage and real instant the replay is standing at. */
export interface ReplayCursor {
  id: string
  at: number
}

export interface ReplayPanelProps {
  passages: PassageDetail[]
  isDarkTheme: boolean
  /** How long the replay runs: the page holds it, since the video export makes the video of the replay as it is set. */
  length: ReplaySpeedId
  /** The lengths on offer; a single passage's unless the page says more. */
  lengths?: readonly ReplayLength[]
  onLengthChange: (length: ReplaySpeedId) => void
  /** Written with the replay position as it moves, for the edit buttons. */
  cursorRef?: MutableRefObject<ReplayCursor | null>
}

const MODE_NAMES: Record<CameraMode, string> = { overview: 'Overview', follow: 'Follow', orbit: 'Orbit' }
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v))
const round = (v: number, digits: number) => Math.round(v * 10 ** digits) / 10 ** digits
/** An angle brought into -180..180. */
const signedDeg = (d: number) => ((((d % 360) + 540) % 360) - 180)

/** Runs writes one at a time, in the order they were asked for, so a title
 * saved a moment after a move cannot cross it. Each write reports its own
 * failure; a later success does not wipe it. The old error is dropped only
 * when a new action starts with nothing else in flight. `writes` is the
 * number queued or running. */
function useWriteQueue(setError: (message: string | null) => void) {
  const [writes, setWrites] = useState(0)
  const queue = useRef<Promise<void>>(Promise.resolve())
  const pending = useRef(0)
  const run = (write: () => Promise<void>) => {
    if (pending.current === 0) setError(null)
    pending.current += 1
    setWrites((n) => n + 1)
    queue.current = queue.current.then(async () => {
      try {
        await write()
      } catch (err) {
        setError(errorMessage(err))
      } finally {
        pending.current -= 1
        setWrites((n) => n - 1)
      }
    })
    return queue.current
  }
  return { run, writes }
}

export function ReplayPanel({ passages, isDarkTheme, length, lengths, onLengthChange, cursorRef }: ReplayPanelProps) {
  const [cameraOpen, setCameraOpen] = useState(false)
  const [selectedShotId, setSelectedShotId] = useState<string | null>(null)
  const [shotError, setShotError] = useState<string | null>(null)
  const { run: runShotWrite, writes: shotWrites } = useWriteQueue(setShotError)
  const shotBusy = shotWrites > 0
  const [signsOpen, setSignsOpen] = useState(false)
  const [selectedSignId, setSelectedSignId] = useState<string | null>(null)
  const [signError, setSignError] = useState<string | null>(null)
  const { run: runSignWrite, writes: signWrites } = useWriteQueue(setSignError)
  const [signNotice, setSignNotice] = useState<string | null>(null)
  // A pin the operator opened into its card, and the sign whose photo is open full size.
  const [openPinId, setOpenPinId] = useState<string | null>(null)
  const [photoSignId, setPhotoSignId] = useState<string | null>(null)
  // Bumped when the operator takes the camera back from a drag (Play, a scrub).
  const [resume, setResume] = useState(0)
  // The map says whether it is flat: 3D off, or the terrain failed.
  const [mapFlat, setMapFlat] = useState(false)
  const reducedMotion = usePrefersReducedMotion()
  const [insetOn, setInsetOn] = useInsetPreference()
  // The opening move is running: from the press of Play until it has landed.
  // The controller owns the move; React hears only of its start and its end.
  const [flying, setFlying] = useState(false)
  const introClock = useRef(0)
  const mapHandle = useRef<PassageMapHandle>(null)
  // How big the map is, kept as it is resized: the label of a stay is placed by it.
  const [mapBoxRef, mapSize] = useElementSize<HTMLDivElement>()

  const built = useMemo(() => {
    try {
      return {
        error: null as string | null,
        items: replayItems(passages),
      }
    } catch (err) {
      return { error: errorMessage(err), items: [] }
    }
  }, [passages])

  const timeline = useMemo(() => buildTimeline(built.items), [built.items])
  const allSegments = useMemo(() => built.items.flatMap((i) => i.line), [built.items])
  const bounds = useMemo(() => segmentBounds(allSegments), [allSegments])
  // The server cuts nights by the zone of the first fix; read the clock the same way. The first passage is the earliest, whatever order they are given in: the same one the offset below, the stays and the day labels count by.
  const timeZone = useMemo(() => (timeline.entries[0] ? passages.find((p) => p.id === timeline.entries[0].id)?.timezone ?? null : null), [timeline, passages])
  // Hours east of UTC at the first departure, for reading a camera's own clock as boat time: the offset the stays and the day labels count by too. Null when the passage has no known zone: a photo with no zone of its own is then not placed by its time.
  const boatOffsetHours = useMemo(() => boatOffsetOf(timeline, passages), [timeline, passages])

  // Camera shots belong to real time, so the ones inside the replayed
  // passages are the ones that play; a trimmed-out shot is simply not there.
  const spanMs = useMemo(() => {
    if (built.items.length === 0) return null
    return {
      from: Math.min(...built.items.map((i) => i.start)) * 1000,
      to: Math.max(...built.items.map((i) => i.end)) * 1000,
    }
  }, [built.items])
  const shotsFrom = spanMs ? spanMs.from : null
  const shotsTo = spanMs ? spanMs.to : null
  const { shots } = useCameraShots(shotsFrom, shotsTo)

  // The overview map's corner and zoom step are saved with the replay, keyed by
  // its range (first departure to last arrival). A change shows at once and
  // holds until the server has answered the last write; a refused write puts the
  // saved view back. Writes go one at a time, so the last click is what the
  // server keeps and what the cache ends up showing.
  const insetRange = useMemo(() => insetViewRange(passages), [passages])
  const { view: savedInset, error: insetError, refetch: retryInset } = useInsetView(insetRange)
  const [pendingInset, setPendingInset] = useState<InsetView | null>(null)
  const insetWrites = useRef(0)
  const insetQueue = useRef<Promise<void>>(Promise.resolve())
  const insetView = pendingInset ?? savedInset
  // The corner the overview map is shown in, which the labels over the map keep clear of.
  const labelInset = insetOn && !flying && insetView ? insetView.corner : null
  useEffect(() => {
    if (insetError) toast.error('Could not read where the overview map goes', { description: insetError })
  }, [insetError])
  const changeInsetView = (next: InsetView) => {
    if (!insetRange) return
    setPendingInset(next)
    insetWrites.current += 1
    insetQueue.current = insetQueue.current
      .then(() => insetViewApi.save(insetRange.from, insetRange.to, next))
      .then(() => undefined)
      .catch((err: unknown) => {
        toast.error('Could not save the overview map', { description: errorMessage(err) })
      })
      .finally(() => {
        insetWrites.current -= 1
        if (insetWrites.current === 0) setPendingInset(null)
      })
  }
  const resolved = useMemo(() => playableShots(shots, timeline), [shots, timeline])
  const lengthS = replaySeconds(length)
  // A cruise shares its length between the passages, each at least a few seconds, and holds for a stay between each pair.
  const spans = useMemo(() => cruisePaceSpans(resolved, timeline, lengthS), [resolved, timeline, lengthS])
  const stays = useMemo(() => staysOf(timeline, passages), [timeline, passages])
  const passageLabels = useMemo(() => passageLabelsOf(timeline, passages), [timeline, passages])

  // Signs, like shots, belong to real time: the ones inside the replayed
  // passages are the ones placed; a trimmed-out sign is kept but not shown.
  const legRanges = legPassages(built.items).map((r) => ({ id: r.id, start: new Date(r.startMs).toISOString(), end: new Date(r.endMs).toISOString() }))
  const { legs, signs, from: signsFrom, to: signsTo } = usePassageLegs(legRanges)
  const placedSigns = useMemo(() => resolveSigns(signs, timeline), [signs, timeline])
  // A join still waiting for its name is listed but draws nothing on the map.
  const labelled = useMemo(() => labelledSigns(placedSigns), [placedSigns])
  // A sign with a photo holds the replay for a moment where the boat meets it.
  const moments = useSignMoments(placedSigns)
  const replay = useReplay(timeline.total, lengthS, spans, moments, stays)

  const hull = useBoatHull()
  // The bow follows the course sailed; worked out only when there is a model
  // to point. Everything that moves with time is the controller's: the
  // heading, the sailed track, the signs up and the camera. This asks it for
  // the frame at the clock, in performance.now() time.
  const [threeD] = use3dPreference()
  const [controller] = useState(() => new ReplayController())
  // A development check of dusk and night on a passage sailed by day (lib/dev-flags); 0 in production.
  const [devSunOffsetH] = useState(() => (import.meta.env.DEV ? replayClockOffsetHours() : 0))
  // The track as the map draws it, which the speed colours go on; and the map's
  // zoom, which sizes the wake.
  const mapPassages = useMemo(() => built.items.map((i) => ({ id: i.id, segments: i.line })), [built.items])
  const pieces = useMemo(() => piecesOf(built.items, legs), [built.items, legs])
  const [zoomBucket, setZoomBucket] = useState(12)
  const frameInput = {
    wall: replay.wall,
    nowMs: performance.now(),
    playing: replay.playing,
    resume,
    reduced: reducedMotion,
    wantHeading: wantsHeading(hull, threeD, resolved),
    signsOpen,
    sunOffsetH: devSunOffsetH,
    view: { zoom: zoomBucket },
  }
  // Working the frame out changes nothing; it is taken once this render is the
  // one on screen, before the map places its camera against it.
  const frame = controller.frameAt({ timeline, time: replay.time, stays, passageLabels, resolved, signs: labelled, pieces, isDarkTheme }, frameInput)
  useLayoutEffect(() => { controller.commit(frame) })
  const { here, boat, sailedTo, cards, pins } = frame
  const shownHeading = frame.heading
  useEffect(() => {
    if (cursorRef) cursorRef.current = here ? { id: here.entry.id, at: here.at } : null
  })

  // The opening move runs on requestAnimationFrame, straight onto the map.
  // Landing, or a person taking hold of the map, ends it once: playback then
  // starts from the start, unless the operator moved the replay meanwhile (a
  // jump to a shot or a sign) and so chose where it stands.
  const latest = useRef(replay)
  useEffect(() => { latest.current = replay })
  const landIntro = () => {
    if (!controller.introActive) return
    controller.endIntro()
    setFlying(false)
    const r = latest.current
    if (r.clock !== introClock.current) return
    if (r.ended) r.setClock(0)
    r.setPlaying(true)
  }
  // A hand on the map ends whatever is holding the replay's attention: the
  // opening move, or a photo moment.
  const interrupt = () => (controller.introActive ? landIntro() : replay.skipMoment())
  const landRef = useRef(landIntro)
  useEffect(() => { landRef.current = landIntro })
  // The camera and the projection (the globe, flat by the time it lands) go to
  // the map; the projection when the zoom it is flat from changes.
  const introFlatAt = useRef<number | null | undefined>(undefined)
  const placeIntro = useCallback((at: NonNullable<ReturnType<typeof controller.introAt>>) => {
    mapHandle.current?.jumpTo(at.camera)
    if (at.flatAtZoom !== introFlatAt.current) {
      introFlatAt.current = at.flatAtZoom
      mapHandle.current?.setProjection(at.projection, at.flatAtZoom)
    }
  }, [controller])
  useEffect(() => {
    if (!flying) return
    let id = requestAnimationFrame(function tick() {
      const at = controller.introAt(performance.now())
      if (!at) return
      placeIntro(at)
      if (at.done) landRef.current()
      else id = requestAnimationFrame(tick)
    })
    return () => cancelAnimationFrame(id)
  }, [flying, controller, placeIntro])

  // Play from the very start (or from the end, which starts over) opens with
  // the zoom-in; resuming from a pause or a scrub just plays.
  const handlePlay = () => {
    if (replay.playing) { replay.toggle(); return }
    // An opened pin is a paused preview; playing hands the cards back to each sign's own window.
    setOpenPinId(null)
    setResume((n) => n + 1)
    const framing = replay.wall <= 0 || replay.ended ? mapHandle.current?.getFraming() : null
    if (framing && bounds) {
      const from = introStartCamera(bounds, framing.bearing)
      // A cruise opens on its first passage; a single passage on the whole track.
      const landing = introLanding(framing, timeline)
      controller.startIntro(from, landing, performance.now(), reducedMotion ? 0 : INTRO_MS)
      introClock.current = replay.clock
      setFlying(true)
      introFlatAt.current = undefined
      const first = controller.introAt(performance.now())
      if (first) placeIntro(first)
      if (first?.done) landIntro()
      return
    }
    replay.toggle()
  }

  // The camera: asked for by clock position; the map applies it per frame.
  const cameraDriver: CameraDriver | null = (resolved.length > 0 || isMultiPassage(timeline)) && !flying
    ? {
        clock: replay.clock,
        wall: replay.wall,
        reduced: reducedMotion,
        resume,
        resolve: (env) => controller.cameraAt(env, performance.now()),
      }
    : null

  // A pin pressed mid-play or mid-fly-in must not open a card that outlasts its window.
  const openId = !replay.playing && !flying && pins.some((p) => p.sign.id === openPinId) ? openPinId : null
  const photoSign = signs.find((s) => s.id === photoSignId) ?? null
  const momentAt = frame.moment ? labelled.find((r) => r.sign.id === frame.moment!.signId) : undefined

  const selectedSign = signs.find((s) => s.id === selectedSignId && placedSigns.some((r) => r.sign.id === s.id)) ?? null
  const selectSign = (id: string | null) => {
    if (id !== selectedSignId) setSignNotice(null)
    setSelectedSignId(id)
    setSignError(null)
  }
  // Every jump the operator makes: pause, set the clock, and give the camera back to the shots.
  const seekClock = (clock: number, opts?: Parameters<typeof replay.setClock>[1]) => {
    replay.setPlaying(false)
    replay.setClock(clock, opts)
    setResume((n) => n + 1)
  }
  const seekToSign = (at: number) => {
    const entry = entryAt(live.current.timeline, at)
    const clock = entry ? clockFor(live.current.timeline, entry.id, at) : null
    if (clock === null) return
    seekClock(clock, { onSign: true, at })
  }
  const addSign = () => {
    if (!here) return
    setSignNotice(null)
    void runSignWrite(async () => {
      const created = await replaySignsApi.create({
        t_ms: Math.round(here.at * 1000), title: 'New sign', note: '', document_id: null, visibility: 'passing', show_s: SIGN_SHOW_DEFAULT,
      })
      setSelectedSignId(created.id)
    })
  }
  // A new sign for a photo: pinned where the photo was taken if that is inside
  // the replay, otherwise at the cursor, and the editor says which.
  const addPhotoSign = (name: string, getCapture: () => Promise<Awaited<ReturnType<typeof captureOfFile>>>, getDocumentId: () => Promise<string>) => {
    if (!here) return
    setSignNotice(null)
    const cursorAt = here.at
    void runSignWrite(async () => {
      const placement = placeByCapture(await getCapture(), boatOffsetHours, timeline, cursorAt)
      const documentId = await getDocumentId()
      const created = await replaySignsApi.create({
        t_ms: Math.round(placement.at * 1000), title: name, note: '', document_id: documentId, visibility: 'passing', show_s: SIGN_SHOW_DEFAULT,
      })
      setSelectedSignId(created.id)
      setSignNotice(placement.message)
      if (placement.placedByPhoto) seekToSign(placement.at)
    })
  }
  const titleFor = (name: string) => name.replace(/\.[^.]+$/, '').replace(/[_-]+/g, ' ').trim().slice(0, SIGN_TITLE_MAX) || 'Photo'
  const addPhoto = (file: File) => addPhotoSign(titleFor(file.name), () => captureOfFile(file), () => uploadSignPhoto(file))
  const addFromLibrary = (doc: DocumentLinkPickerResult) =>
    addPhotoSign(titleFor(doc.title || doc.filename), () => captureOfDocument(doc.document_id), () => Promise.resolve(doc.document_id))
  const changeSign = (change: SignChange) => {
    setSignNotice(null)
    if (selectedSign) void runSignWrite(async () => { await patchSign(selectedSign.id, change) })
  }
  const replacePhoto = (file: File) => {
    if (!selectedSign) return
    setSignNotice(null)
    void runSignWrite(async () => { await patchSign(selectedSign.id, { document_id: await uploadSignPhoto(file) }) })
  }
  const replaceFromLibrary = (doc: DocumentLinkPickerResult) => {
    if (!selectedSign) return
    setSignNotice(null)
    void runSignWrite(async () => { await patchSign(selectedSign.id, { document_id: doc.document_id }) })
  }
  const removePhoto = () => {
    setSignNotice(null)
    if (selectedSign) void runSignWrite(async () => { await patchSign(selectedSign.id, { document_id: null }) })
  }
  const deleteSign = () => {
    if (!selectedSign) return
    setSignNotice(null)
    void runSignWrite(async () => {
      await replaySignsApi.remove(selectedSign.id)
      setSelectedSignId(null)
    })
  }
  // Queued writes run after the render that queued them: they read the replay
  // as it is then, and the signs as the server last held them. The patch
  // returns only once the signs have been read again, so the cache is the
  // answer to "where is this sign now".
  const live = useRef({ timeline, boatOffsetHours, signsFrom, signsTo, shotsFrom, shotsTo })
  useEffect(() => { live.current = { timeline, boatOffsetHours, signsFrom, signsTo, shotsFrom, shotsTo } }, [timeline, boatOffsetHours, signsFrom, signsTo, shotsFrom, shotsTo])
  const currentSign = (id: string) =>
    getQueryData<{ signs?: ReplaySign[] }>(replaySignsKey(live.current.signsFrom, live.current.signsTo))?.signs?.find((s) => s.id === id)
  // The saved sign goes into the cache at once, so a read that fails after the
  // write cannot leave the next queued move working from the old time.
  const patchSign = async (id: string, patch: ReplaySignPatch) => {
    const saved = await replaySignsApi.patch(id, patch)
    setQueryData<{ signs?: ReplaySign[] }>(replaySignsKey(live.current.signsFrom, live.current.signsTo), (prev) => ({
      ...prev,
      signs: (prev?.signs ?? []).map((s) => (s.id === id ? saved : s)),
    }))
    return saved
  }
  // A move goes only where the replay can show the sign.
  const checkMoveLands = (tMs: number) => {
    if (!placeAt(tMs / 1000, live.current.timeline)) {
      throw new Error('That moment is outside this replay, so the sign was not moved.')
    }
  }
  const moveSignTo = (signId: string, target: (latestMs: number) => number, seek: boolean) => {
    void runSignWrite(async () => {
      const sign = currentSign(signId)
      if (!sign) throw new Error('That sign is no longer there.')
      const tMs = target(sign.t_ms)
      checkMoveLands(tMs)
      await patchSign(signId, { t_ms: tMs })
      if (seek) seekToSign(tMs / 1000)
    })
  }
  const moveSignHere = () => {
    if (!selectedSign || !here) return
    setSignNotice(null)
    const tMs = Math.round(here.at * 1000)
    moveSignTo(selectedSign.id, () => tMs, false)
  }
  const nudgeSign = (seconds: number) => {
    if (!selectedSign) return
    setSignNotice(null)
    moveSignTo(selectedSign.id, (latest) => latest + seconds * 1000, true)
  }
  // Reads the capture time again, by the rules a new photo sign is placed by,
  // and moves the sign only when that lands inside the replay.
  const backToPhotoTime = () => {
    if (!selectedSign || selectedSign.document_id === null) return
    const { id } = selectedSign
    setSignNotice(null)
    void runSignWrite(async () => {
      const sign = currentSign(id)
      if (!sign) throw new Error('That sign is no longer there.')
      if (sign.document_id === null) throw new Error('That sign has no photo.')
      const placement = moveByCapture(await captureOfDocument(sign.document_id), live.current.boatOffsetHours, live.current.timeline)
      if (!placement.placedByPhoto) { setSignNotice(placement.message); return }
      const tMs = Math.round(placement.at * 1000)
      checkMoveLands(tMs)
      setSignNotice(placement.message)
      if (currentSign(id)?.t_ms !== tMs) await patchSign(id, { t_ms: tMs })
      seekToSign(placement.at)
    })
  }
  const pickSign = (id: string) => {
    const r = placedSigns.find((x) => x.sign.id === id)
    if (!r) return
    seekToSign(r.at)
    selectSign(id)
    setSignsOpen(true)
  }
  const signTicks = placedSigns.map((r) => ({
    id: r.sign.id,
    fraction: timeline.total > 0 ? r.clock / timeline.total : 0,
    label: `Sign ${r.sign.title || AWAITING_NAME} at ${formatLocalDateTime(r.at, timeZone)}`,
    selected: r.sign.id === selectedSign?.id,
  }))

  // The chips show the time alone, with the date only when the signs span days;
  // the full date, time and zone stay in each chip's label.
  const selectedSignId2 = selectedSign?.id
  const signChips = useMemo(() => {
    const when = placedSigns.map((r) => ({ r, parts: localDateTimeParts(r.at, timeZone) }))
    const oneDay = new Set(when.map(({ parts }) => parts.date)).size <= 1
    return when.map(({ r, parts }) => ({
      id: r.sign.id,
      title: r.sign.title || AWAITING_NAME,
      timeLabel: `${parts.date} ${parts.time} ${parts.zone}`,
      shortTime: oneDay ? parts.time : `${parts.date} ${parts.time}`,
      photoSrc: signPhotoSrc(r.sign),
      selected: r.sign.id === selectedSignId2,
    }))
  }, [placedSigns, timeZone, selectedSignId2])

  const selectShot = (id: string | null) => {
    setSelectedShotId(id)
    setShotError(null)
  }
  const selectedShot = shots.find((s) => s.id === selectedShotId && resolved.some((r) => r.shot.id === s.id)) ?? null
  const addShot = () => {
    if (!here) return
    const view = mapHandle.current?.getView()
    if (!view) { setShotError('The map is not ready'); return }
    void runShotWrite(async () => {
      const created = await cameraShotsApi.create({
        t_ms: Math.round(here.at * 1000),
        mode: 'follow',
        zoom: round(clamp(view.zoom, 0, 22), 2),
        pitch: round(clamp(view.pitch ?? REPLAY_3D_PITCH, 0, 70), 1),
        bearing_offset_deg: 0,
        orbit_deg_per_s: 0,
        pace: 1,
      })
      selectShot(created.id)
    })
  }
  const changeShot = (change: CameraShotChange) => {
    if (selectedShot) void runShotWrite(async () => { await patchShot(selectedShot.id, change) })
  }
  const deleteShot = () => {
    if (!selectedShot) return
    void runShotWrite(async () => {
      await cameraShotsApi.remove(selectedShot.id)
      selectShot(null)
    })
  }
  const useCurrentView = () => {
    const view = mapHandle.current?.getView()
    const rs = resolved.find((r) => r.shot.id === selectedShot?.id)
    if (!view || !rs || !selectedShot) { setShotError('The map is not ready'); return }
    const change: CameraShotChange = { zoom: round(clamp(view.zoom, 0, 22), 2) }
    if (view.pitch !== null) change.pitch = round(clamp(view.pitch, 0, 70), 1)
    if (selectedShot.mode === 'follow' && view.pitch !== null) {
      const bow = shotStartState(timeline, rs)?.heading ?? null
      if (bow !== null) change.bearing_offset_deg = round(signedDeg(view.bearing - bow), 1)
    }
    void runShotWrite(async () => { await patchShot(selectedShot.id, change) })
  }
  // Times alone on a one-day replay; the date too once the shots span days.
  const selectedShotId2 = selectedShot?.id
  const { shotTicks, shotHeading, shotChips } = useMemo(() => {
    const parts = resolved.map((r) => localDateTimeParts(r.at, timeZone))
    const oneDay = new Set(parts.map((p) => p.date)).size <= 1
    const short = (p: { date: string; time: string }) => (oneDay ? p.time : `${p.date} ${p.time}`)
    const chips = resolved.map((r, i) => ({
      id: r.shot.id,
      number: i + 1,
      modeName: MODE_NAMES[r.shot.mode],
      timeLabel: formatLocalDateTime(r.at, timeZone),
      shortTime: short(parts[i]),
      selected: r.shot.id === selectedShotId2,
    }))
    const ticks = chips.map((c, i) => ({
      id: c.id,
      fraction: timeline.total > 0 ? resolved[i].clock / timeline.total : 0,
      label: `Camera shot ${c.number}, ${c.modeName}, at ${c.timeLabel}`,
      title: `Shot ${c.number} · ${c.shortTime}`,
      selected: c.selected,
    }))
    const idx = resolved.findIndex((r) => r.shot.id === selectedShotId2)
    if (idx < 0) return { shotTicks: ticks, shotHeading: null, shotChips: chips }
    // The shot ends at the next shot or where its own passage ends, whichever
    // is first; "end" is only for the last shot of the last passage.
    const entry = timeline.entries.find((e) => e.id === resolved[idx].entryId)
    const nextAt = idx + 1 < resolved.length ? resolved[idx + 1].at : null
    const lastPassage = entry !== undefined && entry === timeline.entries[timeline.entries.length - 1]
    let to: string | null = null
    if (nextAt !== null && (!entry || nextAt <= entry.end)) to = short(parts[idx + 1])
    else if (entry && !(nextAt === null && lastPassage)) to = short(localDateTimeParts(entry.end, timeZone))
    return { shotTicks: ticks, shotHeading: { number: idx + 1, total: resolved.length, from: short(parts[idx]), to }, shotChips: chips }
  }, [resolved, timeZone, timeline, selectedShotId2])
  // A mark or a chip: pause, go to the shot, pick it.
  const pickShot = (id: string) => {
    const rs = resolved.find((r) => r.shot.id === id)
    if (!rs) return
    seekClock(rs.clock)
    selectShot(id)
    setCameraOpen(true)
  }
  // Where a shot is now: as the server last saved it, written into the cache
  // at once so a read that fails afterwards cannot leave a move on the old time.
  const shotsKey = () => cameraShotsKey(live.current.shotsFrom, live.current.shotsTo)
  const patchShot = async (id: string, patch: CameraShotPatch) => {
    const saved = await cameraShotsApi.patch(id, patch)
    setQueryData<{ shots?: CameraShot[] }>(shotsKey(), (prev) => ({
      ...prev,
      shots: (prev?.shots ?? []).map((s) => (s.id === id ? saved : s)),
    }))
    return saved
  }
  // Moves the picked shot. A shot plays wherever the replay has a passage for
  // its time, so that is the only place a move may land.
  const moveShotTo = (target: (latestMs: number) => number, seek: boolean) => {
    if (!selectedShot) return
    const { id } = selectedShot
    void runShotWrite(async () => {
      const all = getQueryData<{ shots?: CameraShot[] }>(shotsKey())?.shots ?? []
      const shot = all.find((s) => s.id === id)
      if (!shot) throw new Error('That shot is no longer there.')
      const tMs = target(shot.t_ms)
      const at = tMs / 1000
      if (!playableEntryAt(live.current.timeline, at)) throw new Error('That moment is outside this replay, so the shot was not moved.')
      // Two shots on one instant: only one of them would ever play.
      if (all.some((s) => s.id !== id && s.t_ms === tMs)) throw new Error('Another shot starts at that moment, so the shot was not moved.')
      await patchShot(id, { t_ms: tMs })
      // The replay may have been rebuilt while the write ran, so the passage is looked up again.
      const entry = seek ? playableEntryAt(live.current.timeline, at) : undefined
      const clock = entry ? clockFor(live.current.timeline, entry.id, at) : null
      if (clock !== null) seekClock(clock)
    })
  }
  const moveShotHere = () => {
    if (!here) return
    const tMs = Math.round(here.at * 1000)
    moveShotTo(() => tMs, false)
  }
  const nudgeShot = (seconds: number) => moveShotTo((latest) => latest + seconds * 1000, true)


  const played = useMemo(() => playedIds(timeline, replay.clock), [timeline, replay.clock])

  if (built.error) {
    return <p role="alert" className="rounded-md border border-border bg-card p-3 text-sm text-destructive">{built.error}</p>
  }
  if (!bounds) {
    return (
      <p className="rounded-md border border-border bg-card p-3 text-sm text-muted-foreground" data-testid="passage-no-track">
        No track was recorded for this passage.
      </p>
    )
  }

  return (
    <div className="flex flex-col overflow-hidden rounded-md border border-border bg-card" data-testid="replay-panel">
      <div ref={mapBoxRef} className="relative h-[26rem] min-h-0">
        <PassageMap
          className="h-full"
          ref={mapHandle}
          passages={mapPassages}
          pieces={pieces}
          legs={legs}
          playedIds={played}
          sailedTo={sailedTo}
          boat={boat}
          boatHeading={shownHeading}
          hull={hull}
          cameraDriver={cameraDriver}
          introActive={flying}
          speed={frame.speed}
          onZoomBucket={setZoomBucket}
          light={frame.light}
          momentActive={frame.hold !== null && replay.playing}
          onInterrupt={interrupt}
          onFlatChange={setMapFlat}
          controlsDisabled={replay.playing || flying}
          markers={
            <SignMarkers
              cards={cards}
              pins={pins}
              openId={openId}
              momentId={momentAt?.sign.id ?? null}
              canOpenPhoto={!replay.playing}
              still={reducedMotion}
              onOpenPhoto={(sign) => setPhotoSignId(sign.id)}
              onPinClick={(id) => {
                // A press during playback or the fly-in opens nothing, now or when it pauses.
                if (!replay.playing && !flying) setOpenPinId(id)
                if (signsOpen) selectSign(id)
              }}
              onCloseOpen={() => setOpenPinId(null)}
            />
          }
          isDarkTheme={isDarkTheme}
          insetCorner={insetView?.corner}
          inset={
            insetOn && !flying && insetView ? (
              <InsetMap
                className="h-32 w-44 overflow-hidden rounded-md border border-border shadow-sm"
                bounds={bounds}
                isDarkTheme={isDarkTheme}
                zoomOffset={insetView.zoomOffset}
              />
            ) : null
          }
        />
        {mapSize && frame.stay && <MapLabel testId="stay-label" text={frame.stay.stay.label} box={stayLabelBox(mapSize, frame.stay.elapsed, labelInset)} />}
        {mapSize && frame.passageLabel && <MapLabel testId="passage-label" text={frame.passageLabel.label.text} box={passageLabelBox(mapSize, frame.passageLabel.elapsed, labelInset)} />}
        {frame.moment && momentAt && (
          <PhotoMoment
            view={frame.moment}
            sign={momentAt.sign}
            lift={SIGN_LIFT_PX[Math.min(Math.max(0, cards.indexOf(momentAt)), SIGN_LIFT_PX.length - 1)]}
            project={() => mapHandle.current?.project(momentAt.lon, momentAt.lat) ?? null}
          />
        )}
      </div>
      <PassageReplayBar
        clock={replay.clock}
        total={timeline.total}
        playing={replay.playing}
        readout={here ? formatLocalDateTime(here.at, timeZone) : '--'}
        speed={length}
        lengths={lengths}
        insetOn={insetOn}
        disabled={timeline.total <= 0 || flying}
        onToggle={() => void handlePlay()}
        onScrub={(c) => seekClock(c)}
        onSpeed={onLengthChange}
        onInsetChange={setInsetOn}
        insetView={insetView ?? undefined}
        insetViewError={insetError}
        onInsetViewRetry={() => void retryInset()}
        onInsetViewChange={changeInsetView}
        shotTicks={shotTicks}
        shotTicksDisabled={shotBusy}
        cameraOpen={cameraOpen}
        onCameraToggle={() => setCameraOpen((o) => !o)}
        onTickClick={pickShot}
        signTicks={signTicks}
        signsOpen={signsOpen}
        onSignsToggle={() => setSignsOpen((o) => !o)}
        onSignTickClick={pickSign}
        signsEditor={
          <SignEditor
            sign={selectedSign}
            signs={signChips}
            onPick={pickSign}
            disabled={timeline.total <= 0 || flying}
            whenLabel={selectedSign ? formatLocalDateTime(selectedSign.t_ms / 1000, timeZone) : null}
            busy={signWrites > 0}
            error={signError}
            notice={signNotice}
            onAdd={addSign}
            onAddPhoto={addPhoto}
            onAddFromLibrary={addFromLibrary}
            onChange={changeSign}
            onReplacePhoto={replacePhoto}
            onReplaceFromLibrary={replaceFromLibrary}
            onRemovePhoto={removePhoto}
            onDelete={deleteSign}
            onMoveHere={moveSignHere}
            moveHereDisabled={!here || (selectedSign !== null && Math.round(here.at * 1000) === selectedSign.t_ms)}
            onNudge={nudgeSign}
            onBackToPhotoTime={backToPhotoTime}
          />
        }
        editor={
          <CameraEditor
            shot={selectedShot}
            chips={shotChips}
            onPick={pickShot}
            disabled={timeline.total <= 0 || flying}
            onMoveHere={moveShotHere}
            moveHereDisabled={!here || replay.playing || (selectedShot !== null && Math.round(here.at * 1000) === selectedShot.t_ms)}
            onNudge={nudgeShot}
            shotCount={resolved.length}
            heading={shotHeading}
            flat={mapFlat}
            busy={shotBusy}
            error={shotError}
            onAdd={addShot}
            onChange={changeShot}
            onDelete={deleteShot}
            onUseView={useCurrentView}
          />
        }
      />
      <SignPhotoDialog sign={photoSign} onClose={() => setPhotoSignId(null)} />
    </div>
  )
}
