import { toast } from 'sonner'
import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'

import { CameraEditor, type CameraShotChange } from '@/components/passages/camera-editor'
import { SignPhotoDialog, signPhotoSrc } from '@/components/passages/sign-card'
import { SignEditor, type SignChange } from '@/components/passages/sign-editor'
import { SignMarkers } from '@/components/passages/sign-markers'
import type { DocumentLinkPickerResult } from '@/components/inventory/document-link-picker'
import { InsetMap, PassageMap, type CameraDriver, type PassageMapHandle } from '@/components/passages/passage-map'
import { use3dPreference } from '@/lib/passage-3d-preference'
import { useInsetPreference } from '@/lib/passage-inset-preference'
import { PassageReplayBar } from '@/components/passages/passage-replay-bar'
import { useBoatHull } from '@/hooks/use-boat-hull'
import { useCameraShots } from '@/hooks/use-camera-shots'
import { useInsetView } from '@/hooks/use-inset-view'
import { insetViewApi } from '@/lib/inset-view-api'
import { insetViewRange, type InsetView } from '@/lib/passage-inset-view'
import { usePrefersReducedMotion } from '@/hooks/use-prefers-reduced-motion'
import { HeadingSmoother, nextHeading, type HeadingState } from '@/lib/boat-model'
import { cameraShotsApi } from '@/lib/camera-shots-api'
import { replaySignsApi } from '@/lib/replay-signs-api'
import { usePassageLegs } from '@/hooks/use-passage-legs'
import { AWAITING_NAME, resolveSigns, SIGN_SHOW_DEFAULT, SIGN_TITLE_MAX, visibleSigns } from '@/lib/replay-signs'
import { placeByCapture } from '@/lib/sign-placement'
import { captureOfDocument, captureOfFile, uploadSignPhoto } from '@/lib/sign-photo'
import { CameraDirector, shotStartState } from '@/lib/replay-camera'
import { paceSpans, resolveShots, wallAtTimeline, type CameraMode } from '@/lib/replay-clock'
import { REPLAY_3D_PITCH } from '@/lib/terrain'
import { replaySeconds, useReplay, type ReplaySpeedId } from '@/hooks/use-replay'
import {
  buildTimeline,
  formatLocalDateTime, localDateTimeParts,
  locate,
  positionAt,
  sailedAt,
  segmentBounds,
  timedSegments,
  vesselZone,
  type PassageDetail,
  type TimedSegment,
} from '@/lib/passages'

/** The passage and real instant the replay is standing at. */
export interface ReplayCursor {
  id: string
  at: number
}

export interface ReplayPanelProps {
  passages: PassageDetail[]
  isDarkTheme: boolean
  /** Written with the replay position as it moves, for the edit buttons. */
  cursorRef?: MutableRefObject<ReplayCursor | null>
}

const MODE_NAMES: Record<CameraMode, string> = { overview: 'Overview', follow: 'Follow', orbit: 'Orbit' }
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v))
const round = (v: number, digits: number) => Math.round(v * 10 ** digits) / 10 ** digits
/** An angle brought into -180..180. */
const signedDeg = (d: number) => ((((d % 360) + 540) % 360) - 180)

export function ReplayPanel({ passages, isDarkTheme, cursorRef }: ReplayPanelProps) {
  const [speed, setSpeed] = useState<ReplaySpeedId>('15s')
  const [cameraOpen, setCameraOpen] = useState(false)
  const [selectedShotId, setSelectedShotId] = useState<string | null>(null)
  const [shotBusy, setShotBusy] = useState(false)
  const [shotError, setShotError] = useState<string | null>(null)
  const [signsOpen, setSignsOpen] = useState(false)
  const [selectedSignId, setSelectedSignId] = useState<string | null>(null)
  // Writes run one at a time; busy is the number queued or running.
  const [signWrites, setSignWrites] = useState(0)
  const signQueue = useRef<Promise<void>>(Promise.resolve())
  const signWritesPending = useRef(0)
  const [signError, setSignError] = useState<string | null>(null)
  const [signNotice, setSignNotice] = useState<string | null>(null)
  // A pin the operator opened into its card, and the sign whose photo is open full size.
  const [openPinId, setOpenPinId] = useState<string | null>(null)
  const [photoSignId, setPhotoSignId] = useState<string | null>(null)
  // Bumped when the operator takes the camera back from a drag (Play, a scrub).
  const [resume, setResume] = useState(0)
  // The map says whether it is flat: 3D off, or the terrain failed.
  const [mapFlat, setMapFlat] = useState(false)
  const [director] = useState(() => new CameraDirector())
  const reducedMotion = usePrefersReducedMotion()
  const [insetOn, setInsetOn] = useInsetPreference()
  const [flying, setFlying] = useState(false)
  const mapHandle = useRef<PassageMapHandle>(null)

  const built = useMemo(() => {
    try {
      return {
        error: null as string | null,
        items: passages.map((p) => ({
          id: p.id,
          start: Date.parse(p.start) / 1000,
          end: Date.parse(p.end) / 1000,
          segments: timedSegments(p.replay_track, p.replay_times) as TimedSegment[],
          line: timedSegments(p.track, p.track_times) as TimedSegment[],
        })),
      }
    } catch (err) {
      return { error: err instanceof Error ? err.message : String(err), items: [] }
    }
  }, [passages])

  const timeline = useMemo(() => buildTimeline(built.items), [built.items])
  const allSegments = useMemo(() => built.items.flatMap((i) => i.line), [built.items])
  const bounds = useMemo(() => segmentBounds(allSegments), [allSegments])
  // The server cuts nights by the zone of the first fix; read the clock the same way.
  const lon = passages[0]?.start_lon ?? (bounds ? (bounds[0] + bounds[2]) / 2 : 0)

  // Camera shots belong to real time, so the ones inside the replayed
  // passages are the ones that play; a trimmed-out shot is simply not there.
  const spanMs = useMemo(() => {
    if (built.items.length === 0) return null
    return {
      from: Math.min(...built.items.map((i) => i.start)) * 1000,
      to: Math.max(...built.items.map((i) => i.end)) * 1000,
    }
  }, [built.items])
  const { shots } = useCameraShots(spanMs ? spanMs.from : null, spanMs ? spanMs.to : null)

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
        toast.error('Could not save the overview map', { description: err instanceof Error ? err.message : String(err) })
      })
      .finally(() => {
        insetWrites.current -= 1
        if (insetWrites.current === 0) setPendingInset(null)
      })
  }
  const resolved = useMemo(
    () => resolveShots(shots, timeline).filter((r) => timeline.entries.find((e) => e.id === r.entryId)?.segments.some((s) => s.length > 0)),
    [shots, timeline],
  )
  const spans = useMemo(() => paceSpans(resolved), [resolved])
  const lengthS = replaySeconds(speed)
  const replay = useReplay(timeline.total, lengthS, spans)

  // Signs, like shots, belong to real time: the ones inside the replayed
  // passages are the ones placed; a trimmed-out sign is kept but not shown.
  const legRanges = built.items.map((i) => ({ id: i.id, start: new Date(i.start * 1000).toISOString(), end: new Date(i.end * 1000).toISOString() }))
  const { legs, signs } = usePassageLegs(legRanges)
  const placedSigns = useMemo(() => resolveSigns(signs, timeline), [signs, timeline])
  // A join still waiting for its name is listed but draws nothing on the map.
  const labelledSigns = useMemo(() => placedSigns.filter((r) => r.sign.title !== ''), [placedSigns])

  const here = locate(timeline, replay.clock)
  const boat = here ? positionAt(here.entry.segments, here.at) : null
  const hull = useBoatHull()
  // The bow follows the course sailed; worked out only when there is a model
  // to point. A held heading carries on only while playback moves forward.
  const [threeD] = use3dPreference()
  const lastHeading = useRef<HeadingState | null>(null)
  const heading = ((hull && threeD) || resolved.length > 0) && here ? nextHeading(lastHeading.current, { id: here.entry.id, at: here.at }, here.entry.segments) : null
  useEffect(() => { lastHeading.current = heading })
  // One smoothed heading serves the boat and the camera, so the model does not
  // swing against the screen. It is stepped in render because both need it in
  // the same render; the step is keyed on (clock, playing, passage, raw
  // heading), so a repeat render with the same key (StrictMode) returns the
  // cached value and cannot advance it twice. Pausing keeps the value as it
  // stands. A scrub or seek while paused, a new passage and reduced motion
  // show the exact heading. Resuming eases from the held value, timed from
  // the resume.
  const smoother = useRef<HeadingSmoother | null>(null)
  if (smoother.current === null) smoother.current = new HeadingSmoother()
  const smoothed = useRef<{ clock: number; playing: boolean; id: string | null; raw: number | null; out: number | null } | null>(null)
  const rawHeading = heading ? heading.heading : null
  const hereKey = here ? here.entry.id : null
  const sm = smoothed.current
  if (!sm || sm.clock !== replay.clock || sm.playing !== replay.playing || sm.id !== hereKey || sm.raw !== rawHeading) {
    const now = performance.now()
    const idChanged = !!sm && sm.id !== hereKey
    const moved = !sm || sm.clock !== replay.clock || sm.raw !== rawHeading
    if (!replay.playing || (sm && !sm.playing)) smoother.current.hold(now)
    // Only a move made while already paused is a scrub; the last tick of a
    // replay moves the clock and stops in the same render, and keeps its ease.
    const snap = reducedMotion || idChanged || (!replay.playing && moved && (!sm || !sm.playing))
    smoothed.current = {
      clock: replay.clock, playing: replay.playing, id: hereKey, raw: rawHeading,
      out: smoother.current.step(rawHeading, now, { snap }),
    }
  }
  const shownHeading = smoothed.current ? smoothed.current.out : null
  useEffect(() => {
    if (cursorRef) cursorRef.current = here ? { id: here.entry.id, at: here.at } : null
  })

  // Play from the very start (or from the end, which starts over) opens with
  // the zoom-in; resuming from a pause or a scrub just plays.
  const handlePlay = async () => {
    if (replay.playing) { replay.toggle(); return }
    // An opened pin is a paused preview; playing hands the cards back to each sign's own window.
    setOpenPinId(null)
    setResume((n) => n + 1)
    if (replay.clock <= 0 || replay.clock >= timeline.total) {
      setFlying(true)
      try {
        await mapHandle.current?.flyIn()
      } finally {
        setFlying(false)
      }
    }
    replay.toggle()
  }

  // Once the replay has moved off the start, each track is drawn up to the
  // boat: earlier passages whole, later ones not at all. The passage the boat
  // is on ends exactly where the marker stands.
  const hereId = here?.entry.id
  const boatLat = boat?.lat
  const boatLon = boat?.lon
  const sailedTo = useMemo(() => {
    if (replay.clock <= 0) return null
    const at = new Map<string, { at: number; end?: { lat: number; lon: number } }>()
    for (const e of timeline.entries) {
      const end = e.id === hereId && boatLat !== undefined && boatLon !== undefined ? { lat: boatLat, lon: boatLon } : undefined
      at.set(e.id, { at: sailedAt(e, replay.clock), end })
    }
    return at
  }, [timeline, replay.clock, hereId, boatLat, boatLon])

  // The camera: asked for by clock position; the map applies it per frame.
  const wallAt = useCallback(
    (clock: number) => wallAtTimeline(timeline.total, lengthS, spans, clock),
    [timeline.total, lengthS, spans],
  )
  const cameraDriver: CameraDriver | null = resolved.length > 0 && !flying
    ? {
        clock: replay.clock,
        reduced: reducedMotion,
        resume,
        resolve: ({ flat, overview, current }) => {
          if (!boat) return null
          return director.frame(
            { overview, flat, shotStart: (rs) => shotStartState(timeline, rs), elapsedAt: (rs, clock) => wallAt(clock) - wallAt(rs.clock) },
            {
              resolved,
              clock: replay.clock,
              playing: replay.playing,
              nowMs: performance.now(),
              reduced: reducedMotion,
              resume,
              boat: { lat: boat.lat, lon: boat.lon, heading: shownHeading },
              current,
            },
          )
        },
      }
    : null

  // Cards come and go on watched (wall-clock) time. Before the replay has
  // moved off the start every sign is a pin, and the pins show while the sign
  // editor is open.
  const replaying = replay.clock > 0
  const cards = useMemo(
    () => (replaying ? visibleSigns(labelledSigns, wallAt(replay.clock), wallAt) : []),
    [replaying, labelledSigns, wallAt, replay.clock],
  )
  const pins = !replaying || signsOpen ? labelledSigns : []
  // A pin pressed mid-play or mid-fly-in must not open a card that outlasts its window.
  const openId = !replay.playing && !flying && pins.some((p) => p.sign.id === openPinId) ? openPinId : null
  const photoSign = signs.find((s) => s.id === photoSignId) ?? null

  const selectedSign = signs.find((s) => s.id === selectedSignId && placedSigns.some((r) => r.sign.id === s.id)) ?? null
  const selectSign = (id: string | null) => {
    if (id !== selectedSignId) setSignNotice(null)
    setSelectedSignId(id)
    setSignError(null)
  }
  // One write at a time, in the order the operator made them, so a slow upload
  // and a title saved a moment later cannot cross. Each write reports its own
  // failure; a later success does not wipe it. The old error is dropped only
  // when a new action starts with nothing else in flight.
  const runSignWrite = (write: () => Promise<void>) => {
    if (signWritesPending.current === 0) setSignError(null)
    signWritesPending.current += 1
    setSignWrites((n) => n + 1)
    signQueue.current = signQueue.current.then(async () => {
      try {
        await write()
      } catch (err) {
        setSignError(err instanceof Error ? err.message : String(err))
      } finally {
        signWritesPending.current -= 1
        setSignWrites((n) => n - 1)
      }
    })
    return signQueue.current
  }
  const seekToSign = (at: number) => {
    const entry = timeline.entries.find((e) => at >= e.start && at <= e.end)
    if (!entry) return
    replay.setPlaying(false)
    replay.setClock(entry.offset + (at - entry.start))
    setResume((n) => n + 1)
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
      const placement = placeByCapture(await getCapture(), vesselZone(lon).offsetHours, timeline, cursorAt)
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
    if (selectedSign) void runSignWrite(async () => { await replaySignsApi.patch(selectedSign.id, change) })
  }
  const replacePhoto = (file: File) => {
    if (!selectedSign) return
    setSignNotice(null)
    void runSignWrite(async () => { await replaySignsApi.patch(selectedSign.id, { document_id: await uploadSignPhoto(file) }) })
  }
  const replaceFromLibrary = (doc: DocumentLinkPickerResult) => {
    if (!selectedSign) return
    setSignNotice(null)
    void runSignWrite(async () => { await replaySignsApi.patch(selectedSign.id, { document_id: doc.document_id }) })
  }
  const removePhoto = () => {
    setSignNotice(null)
    if (selectedSign) void runSignWrite(async () => { await replaySignsApi.patch(selectedSign.id, { document_id: null }) })
  }
  const deleteSign = () => {
    if (!selectedSign) return
    setSignNotice(null)
    void runSignWrite(async () => {
      await replaySignsApi.remove(selectedSign.id)
      setSelectedSignId(null)
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
    label: `Sign ${r.sign.title || AWAITING_NAME} at ${formatLocalDateTime(r.at, lon)}`,
    selected: r.sign.id === selectedSign?.id,
  }))

  // The chips show the time alone, with the date only when the signs span days;
  // the full date, time and zone stay in each chip's label.
  const selectedSignId2 = selectedSign?.id
  const signChips = useMemo(() => {
    const when = placedSigns.map((r) => ({ r, parts: localDateTimeParts(r.at, lon) }))
    const oneDay = new Set(when.map(({ parts }) => parts.date)).size <= 1
    return when.map(({ r, parts }) => ({
      id: r.sign.id,
      title: r.sign.title || AWAITING_NAME,
      timeLabel: `${parts.date} ${parts.time} ${parts.zone}`,
      shortTime: oneDay ? parts.time : `${parts.date} ${parts.time}`,
      photoSrc: signPhotoSrc(r.sign),
      selected: r.sign.id === selectedSignId2,
    }))
  }, [placedSigns, lon, selectedSignId2])

  const selectShot = (id: string | null) => {
    setSelectedShotId(id)
    setShotError(null)
  }
  const selectedShot = shots.find((s) => s.id === selectedShotId && resolved.some((r) => r.shot.id === s.id)) ?? null
  const runShotWrite = async (write: () => Promise<void>) => {
    setShotBusy(true)
    try {
      await write()
      setShotError(null)
    } catch (err) {
      setShotError(err instanceof Error ? err.message : String(err))
    } finally {
      setShotBusy(false)
    }
  }
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
    if (selectedShot) void runShotWrite(async () => { await cameraShotsApi.patch(selectedShot.id, change) })
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
    void runShotWrite(async () => { await cameraShotsApi.patch(selectedShot.id, change) })
  }
  // Times alone on a one-day replay; the date too once the shots span days.
  const selectedShotId2 = selectedShot?.id
  const { shotTicks, shotHeading } = useMemo(() => {
    const parts = resolved.map((r) => localDateTimeParts(r.at, lon))
    const oneDay = new Set(parts.map((p) => p.date)).size <= 1
    const short = (p: { date: string; time: string }) => (oneDay ? p.time : `${p.date} ${p.time}`)
    const ticks = resolved.map((r, i) => ({
      id: r.shot.id,
      fraction: timeline.total > 0 ? r.clock / timeline.total : 0,
      label: `Camera shot ${i + 1}, ${MODE_NAMES[r.shot.mode]}, at ${formatLocalDateTime(r.at, lon)}`,
      title: `Shot ${i + 1} · ${short(parts[i])}`,
      selected: r.shot.id === selectedShotId2,
    }))
    const idx = resolved.findIndex((r) => r.shot.id === selectedShotId2)
    if (idx < 0) return { shotTicks: ticks, shotHeading: null }
    // The shot ends at the next shot or where its own passage ends, whichever
    // is first; "end" is only for the last shot of the last passage.
    const entry = timeline.entries.find((e) => e.id === resolved[idx].entryId)
    const nextAt = idx + 1 < resolved.length ? resolved[idx + 1].at : null
    const lastPassage = entry !== undefined && entry === timeline.entries[timeline.entries.length - 1]
    let to: string | null = null
    if (nextAt !== null && (!entry || nextAt <= entry.end)) to = short(parts[idx + 1])
    else if (entry && !(nextAt === null && lastPassage)) to = short(localDateTimeParts(entry.end, lon))
    return { shotTicks: ticks, shotHeading: { number: idx + 1, total: resolved.length, from: short(parts[idx]), to } }
  }, [resolved, lon, timeline, selectedShotId2])

  const mapPassages = useMemo(() => built.items.map((i) => ({ id: i.id, segments: i.line })), [built.items])

  const playedIds = useMemo(
    () => new Set(timeline.entries.filter((e) => e.offset <= replay.clock).map((e) => e.id)),
    [timeline, replay.clock],
  )

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
      <div className="relative h-[26rem] min-h-0">
        <PassageMap
          className="h-full"
          ref={mapHandle}
          passages={mapPassages}
          legs={legs}
          playedIds={playedIds}
          sailedTo={sailedTo}
          boat={boat}
          boatHeading={shownHeading}
          hull={hull}
          cameraDriver={cameraDriver}
          onFlatChange={setMapFlat}
          markers={
            <SignMarkers
              cards={cards}
              pins={pins}
              openId={openId}
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
      </div>
      <PassageReplayBar
        clock={replay.clock}
        total={timeline.total}
        playing={replay.playing}
        readout={here ? formatLocalDateTime(here.at, lon) : '--'}
        speed={speed}
        insetOn={insetOn}
        disabled={timeline.total <= 0 || flying}
        onToggle={() => void handlePlay()}
        onScrub={(c) => { replay.setPlaying(false); replay.setClock(c); setResume((n) => n + 1) }}
        onSpeed={setSpeed}
        onInsetChange={setInsetOn}
        insetView={insetView ?? undefined}
        insetViewError={insetError}
        onInsetViewRetry={() => void retryInset()}
        onInsetViewChange={changeInsetView}
        shotTicks={shotTicks}
        cameraOpen={cameraOpen}
        onCameraToggle={() => setCameraOpen((o) => !o)}
        onTickClick={(id) => {
          const rs = resolved.find((r) => r.shot.id === id)
          if (!rs) return
          replay.setPlaying(false)
          replay.setClock(rs.clock)
          setResume((n) => n + 1)
          selectShot(id)
          setCameraOpen(true)
        }}
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
            whenLabel={selectedSign ? formatLocalDateTime(selectedSign.t_ms / 1000, lon) : null}
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
          />
        }
        editor={
          <CameraEditor
            shot={selectedShot}
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
