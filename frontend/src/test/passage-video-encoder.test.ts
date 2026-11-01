import { describe, expect, it } from 'vitest'

import {
  KEYFRAME_EVERY, MAX_ENCODE_QUEUE, VIDEO_BITRATE, VIDEO_CODEC, checkEncoderSupport, createVideoSink, encoderConfig, type EncoderDeps,
} from '@/lib/passage-video/encoder'
import { PassageVideoError, UNSUPPORTED_MESSAGE } from '@/lib/passage-video/errors'
import { VIDEO_SIZES } from '@/lib/passage-video/schedule'

const LAND = VIDEO_SIZES.landscape
const PORT = VIDEO_SIZES.portrait

describe('encoderConfig', () => {
  it('asks for H.264 High at a level that fits 1080p30, at about ten megabits', () => {
    const c = encoderConfig(LAND)
    expect(c.codec).toBe(VIDEO_CODEC)
    expect(VIDEO_CODEC).toBe('avc1.640028')
    expect(c).toMatchObject({ width: 1920, height: 1080, framerate: 30, avc: { format: 'avc' } })
    expect(VIDEO_BITRATE).toBeGreaterThanOrEqual(8_000_000)
    expect(VIDEO_BITRATE).toBeLessThanOrEqual(12_000_000)
    expect(c.bitrate).toBe(VIDEO_BITRATE)
  })

  it('is the same for portrait with the sides the other way', () => {
    expect(encoderConfig(PORT)).toMatchObject({ width: 1080, height: 1920, codec: VIDEO_CODEC })
  })
})

describe('checkEncoderSupport', () => {
  const withSupport = (isConfigSupported: (c: unknown) => Promise<{ supported?: boolean; config?: unknown }>) =>
    ({ VideoEncoder: { isConfigSupported } }) as unknown as EncoderDeps

  it('says what the browser agreed to', async () => {
    const config = await checkEncoderSupport(LAND, withSupport(async (c) => ({ supported: true, config: c })))
    expect(config).toMatchObject({ codec: VIDEO_CODEC, width: 1920 })
  })

  it('stops with the plain message when the browser has no video encoder', async () => {
    const err = await checkEncoderSupport(LAND, { VideoEncoder: undefined } as unknown as EncoderDeps).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(PassageVideoError)
    expect((err as PassageVideoError).kind).toBe('unsupported')
    expect((err as PassageVideoError).message).toBe(UNSUPPORTED_MESSAGE)
    expect(UNSUPPORTED_MESSAGE).toBe("This browser can't make H.264 video.")
  })

  it('stops with the same message when H.264 is not supported at the size', async () => {
    const err = await checkEncoderSupport(PORT, withSupport(async () => ({ supported: false }))).catch((e: unknown) => e)
    expect((err as PassageVideoError).message).toBe(UNSUPPORTED_MESSAGE)
  })

  it('and when asking throws', async () => {
    const err = await checkEncoderSupport(LAND, withSupport(async () => { throw new TypeError('bad config') })).catch((e: unknown) => e)
    expect((err as PassageVideoError).kind).toBe('unsupported')
  })

  it('asks about the size it will be used at, not a stand-in', async () => {
    let asked: { width: number; height: number } | null = null
    await checkEncoderSupport(PORT, withSupport(async (c) => { asked = c as { width: number; height: number }; return { supported: true, config: c } }))
    expect(asked).toMatchObject({ width: 1080, height: 1920 })
  })
})

// ── a sink, with the browser's encoder and the muxer stood in for ─────────

function fakes(opts: { queue?: () => number; failOnFrame?: number; throwOnEncode?: Error; throwOnFlush?: Error } = {}) {
  const log: string[] = []
  const frames: { src: unknown; timestamp: number; duration: number; closed: boolean }[] = []
  const encoded: { index: number; keyFrame: boolean; timestamp: number }[] = []
  const packets: { timestamp: number; first: boolean }[] = []
  let finalized = false
  let cancelled = false

  class FakeVideoFrame {
    timestamp: number
    duration: number
    closed = false
    constructor(public src: unknown, init: { timestamp: number; duration: number }) {
      this.timestamp = init.timestamp
      this.duration = init.duration
      frames.push(this)
    }
    close() { this.closed = true }
  }
  class FakeVideoEncoder {
    static async isConfigSupported(c: unknown) { return { supported: true, config: c } }
    state = 'unconfigured'
    private listeners = new Set<() => void>()
    private count = 0
    constructor(private init: { output: (chunk: unknown, meta?: unknown) => void; error: (e: Error) => void }) {}
    get encodeQueueSize() { return opts.queue ? opts.queue() : 0 }
    configure(c: unknown) { log.push(`configure ${(c as { codec: string }).codec}`); this.state = 'configured' }
    encode(frame: FakeVideoFrame, o?: { keyFrame?: boolean }) {
      if (opts.throwOnEncode) throw opts.throwOnEncode
      if (opts.failOnFrame === this.count) { this.init.error(new Error('encoder fell over')); this.count++; return }
      encoded.push({ index: this.count, keyFrame: !!o?.keyFrame, timestamp: frame.timestamp })
      this.init.output({ timestamp: frame.timestamp }, this.count === 0 ? { decoderConfig: {} } : undefined)
      this.count++
    }
    async flush() { log.push('flush'); if (opts.throwOnFlush) throw opts.throwOnFlush }
    fail(e: Error) { this.init.error(e) }
    close() { this.state = 'closed'; log.push('close') }
    addEventListener(_t: string, f: () => void) { this.listeners.add(f) }
    removeEventListener(_t: string, f: () => void) { this.listeners.delete(f) }
    dequeue() { for (const l of [...this.listeners]) l() }
  }
  const made: FakeVideoEncoder[] = []
  const Enc = class extends FakeVideoEncoder {
    constructor(init: ConstructorParameters<typeof FakeVideoEncoder>[0]) { super(init); made.push(this) }
  }
  const mediabunny = {
    Mp4OutputFormat: class { constructor(public options: unknown) {} },
    BufferTarget: class { buffer: ArrayBuffer | null = null },
    EncodedVideoPacketSource: class {
      constructor(public codec: string) {}
      async add(packet: { timestamp: number }, meta?: unknown) { packets.push({ timestamp: packet.timestamp, first: !!meta }) }
    },
    EncodedPacket: { fromEncodedChunk: (chunk: { timestamp: number }) => ({ timestamp: chunk.timestamp / 1_000_000 }) },
    Output: class {
      state = 'pending'
      constructor(public options: { target: { buffer: ArrayBuffer | null } }) {}
      addVideoTrack(source: unknown, meta: unknown) { log.push(`track ${JSON.stringify(meta)}`); return source }
      async start() { log.push('start') }
      async finalize() { finalized = true; this.options.target.buffer = new ArrayBuffer(8) }
      async cancel() { cancelled = true }
    },
  }
  const deps = { VideoEncoder: Enc, VideoFrame: FakeVideoFrame, mediabunny } as unknown as EncoderDeps
  return { deps, log, frames, encoded, packets, encoder: () => made[0], wasFinalized: () => finalized, wasCancelled: () => cancelled }
}

describe('createVideoSink', () => {
  it('opens an MP4 with one H.264 track at 30 frames a second, no audio', async () => {
    const f = fakes()
    await createVideoSink(encoderConfig(LAND), f.deps)
    expect(f.log).toContain(`configure ${VIDEO_CODEC}`)
    expect(f.log.find((l) => l.startsWith('track'))).toBe('track {"frameRate":30}')
    expect(f.log).toContain('start')
  })

  it('encodes each frame with its own timestamp and duration, and closes it at once', async () => {
    const f = fakes()
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    for (let i = 0; i < 4; i++) await sink.add({} as CanvasImageSource, i)
    expect(f.frames.map((x) => x.timestamp)).toEqual([0, 33_333, 66_666, 100_000])
    expect(f.frames.map((x) => x.duration)).toEqual([33_333, 33_333, 33_334, 33_333])
    expect(f.frames.every((x) => x.closed)).toBe(true)
  })

  it('reads a held picture once and copies it for the frames after, each with its own timestamp', async () => {
    const f = fakes()
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    const picture = {} as CanvasImageSource
    await sink.add(picture, 5, 3)
    expect(f.frames.map((x) => x.timestamp)).toEqual([166_666, 200_000, 233_333])
    // Only the first is made from the picture; the others are copies of that frame.
    expect(f.frames[0].src).toBe(picture)
    expect(f.frames[1].src).toBe(f.frames[0])
    expect(f.frames[2].src).toBe(f.frames[0])
    expect(f.frames.every((x) => x.closed)).toBe(true)
    expect(f.encoded.map((e) => e.timestamp)).toEqual([166_666, 200_000, 233_333])
  })

  it('makes a key frame at the start and every so often', async () => {
    const f = fakes()
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    for (let i = 0; i <= KEYFRAME_EVERY + 1; i++) await sink.add({} as CanvasImageSource, i)
    expect(f.encoded.filter((e) => e.keyFrame).map((e) => e.index)).toEqual([0, KEYFRAME_EVERY])
  })

  it('waits for the encoder when its queue is long, and goes on when it drains', async () => {
    let queue = MAX_ENCODE_QUEUE + 3
    const f = fakes({ queue: () => queue })
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    let added = false
    const pending = sink.add({} as CanvasImageSource, 0).then(() => { added = true })
    await Promise.resolve()
    await Promise.resolve()
    expect(added).toBe(false)
    expect(f.frames).toHaveLength(0)
    queue = 0
    f.encoder().dequeue()
    await pending
    expect(added).toBe(true)
    expect(f.frames).toHaveLength(1)
  })

  it('finishes with an MP4 blob once everything is flushed and finalised', async () => {
    const f = fakes()
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    await sink.add({} as CanvasImageSource, 0)
    await sink.add({} as CanvasImageSource, 1)
    const blob = await sink.finish()
    expect(blob.type).toBe('video/mp4')
    expect(f.log.indexOf('flush')).toBeLessThan(f.log.indexOf('close'))
    expect(f.wasFinalized()).toBe(true)
    expect(f.packets).toHaveLength(2)
    expect(f.packets[0].first).toBe(true)
  })

  it('says so, and stops, when the encoder fails', async () => {
    const f = fakes({ failOnFrame: 1 })
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    await sink.add({} as CanvasImageSource, 0)
    const err = await sink.add({} as CanvasImageSource, 1).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(PassageVideoError)
    expect((err as PassageVideoError).kind).toBe('encode')
    expect((err as PassageVideoError).message).toContain('encoder fell over')
    // And it stays stopped.
    expect(await sink.add({} as CanvasImageSource, 2).catch((e: unknown) => e)).toBeInstanceOf(PassageVideoError)
    expect(await sink.finish().catch((e: unknown) => e)).toBeInstanceOf(PassageVideoError)
  })

  it('stops a frame that is waiting for the encoder to drain when the encoder fails meanwhile', async () => {
    const f = fakes({ queue: () => MAX_ENCODE_QUEUE + 3 })
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    const pending = sink.add({} as CanvasImageSource, 0).catch((e: unknown) => e)
    await Promise.resolve()
    await Promise.resolve()
    ;(f.encoder() as unknown as { fail: (e: Error) => void }).fail(new Error('GPU process crashed'))
    const err = await pending
    expect(err).toBeInstanceOf(PassageVideoError)
    expect((err as PassageVideoError).kind).toBe('encode')
    expect((err as PassageVideoError).message).toBe('The video encoder stopped: GPU process crashed.')
    // The wait is gone, not left on the encoder.
    expect(f.frames).toHaveLength(0)
  })

  it('tells the operator when encode() itself throws', async () => {
    const f = fakes({ throwOnEncode: new DOMException('Encoder is closed', 'InvalidStateError') })
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    const err = await sink.add({} as CanvasImageSource, 0).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(PassageVideoError)
    expect((err as PassageVideoError).kind).toBe('encode')
    expect((err as PassageVideoError).message).toContain('The video encoder stopped: Encoder is closed')
    // The frame it was given is let go.
    expect(f.frames.every((x) => x.closed)).toBe(true)
  })

  it('tells the operator when flush() throws', async () => {
    const f = fakes({ throwOnFlush: new Error('flush failed') })
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    await sink.add({} as CanvasImageSource, 0)
    const err = await sink.finish().catch((e: unknown) => e)
    expect(err).toBeInstanceOf(PassageVideoError)
    expect((err as PassageVideoError).message).toBe('The video encoder stopped: flush failed.')
  })

  it('lets go of the encoder and the output when cancelled', async () => {
    const f = fakes()
    const sink = await createVideoSink(encoderConfig(LAND), f.deps)
    await sink.add({} as CanvasImageSource, 0)
    sink.cancel()
    expect(f.log).toContain('close')
    expect(f.wasCancelled()).toBe(true)
    // Cancelling again is harmless.
    sink.cancel()
  })
})
