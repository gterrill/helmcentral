import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'

import { InsetMap, PassageMap, type PassageMapHandle } from '@/components/passages/passage-map'
import { use3dPreference } from '@/lib/passage-3d-preference'
import { useInsetPreference } from '@/lib/passage-inset-preference'
import { PassageReplayBar } from '@/components/passages/passage-replay-bar'
import { useBoatHull } from '@/hooks/use-boat-hull'
import { nextHeading, type HeadingState } from '@/lib/boat-model'
import { useReplay, type ReplaySpeedId } from '@/hooks/use-replay'
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

export function ReplayPanel({ passages, isDarkTheme, cursorRef }: ReplayPanelProps) {
  const [speed, setSpeed] = useState<ReplaySpeedId>('normal')
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
  const replay = useReplay(timeline.total, speed)

  const here = locate(timeline, replay.clock)
  const boat = here ? positionAt(here.entry.segments, here.at) : null
  const hull = useBoatHull()
  // The bow follows the course sailed; worked out only when there is a model
  // to point. A held heading carries on only while playback moves forward.
  const [threeD] = use3dPreference()
  const lastHeading = useRef<HeadingState | null>(null)
  const heading = hull && threeD && here ? nextHeading(lastHeading.current, { id: here.entry.id, at: here.at }, here.entry.segments) : null
  useEffect(() => { lastHeading.current = heading })
  useEffect(() => {
    if (cursorRef) cursorRef.current = here ? { id: here.entry.id, at: here.at } : null
  })

  // Play from the very start (or from the end, which starts over) opens with
  // the zoom-in; resuming from a pause or a scrub just plays.
  const handlePlay = async () => {
    if (replay.playing) { replay.toggle(); return }
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
        onScrub={(c) => { replay.setPlaying(false); replay.setClock(c) }}
        onSpeed={setSpeed}
        onInsetChange={setInsetOn}
      />
    </div>
  )
}
