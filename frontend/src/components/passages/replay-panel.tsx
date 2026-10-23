import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'

import { CameraEditor, type CameraShotChange } from '@/components/passages/camera-editor'
import { InsetMap, PassageMap, type CameraDriver, type PassageMapHandle } from '@/components/passages/passage-map'
import { use3dPreference } from '@/lib/passage-3d-preference'
import { useInsetPreference } from '@/lib/passage-inset-preference'
import { PassageReplayBar } from '@/components/passages/passage-replay-bar'
import { useBoatHull } from '@/hooks/use-boat-hull'
import { useCameraShots } from '@/hooks/use-camera-shots'
import { usePrefersReducedMotion } from '@/hooks/use-prefers-reduced-motion'
import { nextHeading, type HeadingState } from '@/lib/boat-model'
import { cameraShotsApi } from '@/lib/camera-shots-api'
import { CameraDirector, shotStartState } from '@/lib/replay-camera'
import { paceSpans, resolveShots, wallAtTimeline, type CameraMode } from '@/lib/replay-clock'
import { REPLAY_3D_PITCH } from '@/lib/terrain'
import { replaySeconds, useReplay, type ReplaySpeedId } from '@/hooks/use-replay'
import {
  buildTimeline,
  formatLocalDateTime,
  locate,
  positionAt,
  sailedAt,
  segmentBounds,
  timedSegments,
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
  const [speed, setSpeed] = useState<ReplaySpeedId>('normal')
  const [cameraOpen, setCameraOpen] = useState(false)
  const [selectedShotId, setSelectedShotId] = useState<string | null>(null)
  const [shotBusy, setShotBusy] = useState(false)
  const [shotError, setShotError] = useState<string | null>(null)
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
  const resolved = useMemo(
    () => resolveShots(shots, timeline).filter((r) => timeline.entries.find((e) => e.id === r.entryId)?.segments.some((s) => s.length > 0)),
    [shots, timeline],
  )
  const spans = useMemo(() => paceSpans(resolved), [resolved])
  const lengthS = replaySeconds(speed)
  const replay = useReplay(timeline.total, lengthS, spans)

  const here = locate(timeline, replay.clock)
  const boat = here ? positionAt(here.entry.segments, here.at) : null
  const hull = useBoatHull()
  // The bow follows the course sailed; worked out only when there is a model
  // to point. A held heading carries on only while playback moves forward.
  const [threeD] = use3dPreference()
  const lastHeading = useRef<HeadingState | null>(null)
  const heading = ((hull && threeD) || resolved.length > 0) && here ? nextHeading(lastHeading.current, { id: here.entry.id, at: here.at }, here.entry.segments) : null
  useEffect(() => { lastHeading.current = heading })
  useEffect(() => {
    if (cursorRef) cursorRef.current = here ? { id: here.entry.id, at: here.at } : null
  })

  // Play from the very start (or from the end, which starts over) opens with
  // the zoom-in; resuming from a pause or a scrub just plays.
  const handlePlay = async () => {
    if (replay.playing) { replay.toggle(); return }
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
              boat: { lat: boat.lat, lon: boat.lon, heading: heading ? heading.heading : null },
              current,
            },
          )
        },
      }
    : null

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
  const shotTicks = resolved.map((r) => ({
    id: r.shot.id,
    fraction: timeline.total > 0 ? r.clock / timeline.total : 0,
    label: `Camera shot ${MODE_NAMES[r.shot.mode]} at ${formatLocalDateTime(r.at, lon)}`,
    selected: r.shot.id === selectedShot?.id,
  }))

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
          playedIds={playedIds}
          sailedTo={sailedTo}
          boat={boat}
          boatHeading={heading ? heading.heading : null}
          hull={hull}
          cameraDriver={cameraDriver}
          onFlatChange={setMapFlat}
          isDarkTheme={isDarkTheme}
          overlay={
            insetOn && !flying ? (
              <InsetMap
                className="h-32 w-44 overflow-hidden rounded-md border border-border shadow-sm"
                bounds={bounds}
                isDarkTheme={isDarkTheme}
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
        editor={
          <CameraEditor
            shot={selectedShot}
            shotCount={resolved.length}
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
    </div>
  )
}
