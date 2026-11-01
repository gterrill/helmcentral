// Encoding the frames to H.264 with the browser's own VideoEncoder (WebCodecs)
// and writing the MP4 with mediabunny. No audio. Nothing leaves the browser.

import { PassageVideoError, UNSUPPORTED_MESSAGE } from '@/lib/passage-video/errors'
import { FPS, frameDurationUs, frameTimestampUs, type VideoSize } from '@/lib/passage-video/schedule'

/** H.264 High profile, level 4.0: 1920 by 1088 at 30 frames a second fits it exactly (the same, turned round, for portrait). */
export const VIDEO_CODEC = 'avc1.640028'
/** Enough that the fine detail of a chart and a photo survives, in a file a phone will still share. */
export const VIDEO_BITRATE = 10_000_000
/** A key frame this often, so the file can be seeked in. */
export const KEYFRAME_EVERY = 60
/** How many frames may wait in the encoder before the loop waits for it. */
export const MAX_ENCODE_QUEUE = 8

export function encoderConfig(size: Pick<VideoSize, 'width' | 'height'>): VideoEncoderConfig {
  return {
    codec: VIDEO_CODEC,
    width: size.width,
    height: size.height,
    bitrate: VIDEO_BITRATE,
    framerate: FPS,
    latencyMode: 'quality',
    avc: { format: 'avc' },
  }
}

/** What the encoder and the muxer are made from. The browser's and
 * mediabunny's own by default; a test hands in stand-ins. */
export interface EncoderDeps {
  VideoEncoder: typeof VideoEncoder | undefined
  VideoFrame: typeof VideoFrame
  mediabunny: Pick<typeof import('mediabunny'), 'Output' | 'Mp4OutputFormat' | 'BufferTarget' | 'EncodedVideoPacketSource' | 'EncodedPacket'>
}

async function browserDeps(): Promise<EncoderDeps> {
  return { VideoEncoder, VideoFrame, mediabunny: await import('mediabunny') }
}

/** Asks the browser whether it can encode H.264 at the size, before any work is done. */
export async function checkEncoderSupport(size: Pick<VideoSize, 'width' | 'height'>, deps?: Pick<EncoderDeps, 'VideoEncoder'>): Promise<VideoEncoderConfig> {
  const Encoder = deps ? deps.VideoEncoder : (globalThis as { VideoEncoder?: typeof VideoEncoder }).VideoEncoder
  const config = encoderConfig(size)
  if (!Encoder) throw new PassageVideoError('unsupported', UNSUPPORTED_MESSAGE)
  let support: VideoEncoderSupport
  try {
    support = await Encoder.isConfigSupported(config)
  } catch {
    throw new PassageVideoError('unsupported', UNSUPPORTED_MESSAGE)
  }
  if (!support.supported) throw new PassageVideoError('unsupported', UNSUPPORTED_MESSAGE)
  return config
}

export interface VideoSink {
  /** Encodes the picture as frame `index`, and `count` frames in all when it
   * holds still: the picture is read once and each later frame is a copy of
   * it with its own timestamp. Waits when the encoder is behind. */
  add(source: CanvasImageSource, index: number, count?: number): Promise<void>
  /** Everything encoded, as the finished MP4. */
  finish(): Promise<Blob>
  /** Lets go of the encoder and the output. Safe to call at any point, and twice. */
  cancel(): void
}

/** The sink for a config that `checkEncoderSupport` has said yes to. */
export async function createVideoSink(config: VideoEncoderConfig, depsIn?: EncoderDeps): Promise<VideoSink> {
  const deps = depsIn ?? await browserDeps()
  const { Output, Mp4OutputFormat, BufferTarget, EncodedVideoPacketSource, EncodedPacket } = deps.mediabunny
  const target = new BufferTarget()
  const output = new Output({ format: new Mp4OutputFormat({ fastStart: 'in-memory' }), target })
  const source = new EncodedVideoPacketSource('avc')
  output.addVideoTrack(source, { frameRate: FPS })
  await output.start()

  // Encoded chunks arrive in order, one call each; the muxer takes them one at a time.
  let written: Promise<void> = Promise.resolve()
  let failure: Error | null = null
  // Whatever is waiting for the encoder to drain, which a failure also ends.
  const waiting = new Set<() => void>()
  const stopped = (e: unknown) => {
    failure ??= e instanceof Error ? e : new Error(String(e))
    waiting.forEach((wake) => wake())
  }
  const encoder = new deps.VideoEncoder!({
    output: (chunk, meta) => {
      written = written.then(() => source.add(EncodedPacket.fromEncodedChunk(chunk), meta)).catch(stopped)
    },
    error: stopped,
  })
  encoder.configure(config)
  let closed = false
  const fail = () => new PassageVideoError('encode', `The video encoder stopped: ${failure?.message ?? 'unknown error'}.`)
  /** Runs a call on the encoder or the muxer, and says what stopped it as the operator is told. */
  const guard = async <T>(call: () => T | Promise<T>): Promise<T> => {
    try {
      return await call()
    } catch (e) {
      stopped(e)
      throw fail()
    }
  }

  const drained = () => new Promise<void>((resolve) => {
    const done = () => { encoder.removeEventListener('dequeue', done); waiting.delete(done); resolve() }
    waiting.add(done)
    encoder.addEventListener('dequeue', done)
  })

  return {
    async add(image, index, count = 1) {
      if (failure) throw fail()
      const timing = (i: number) => ({ timestamp: frameTimestampUs(i), duration: frameDurationUs(i) })
      let first: VideoFrame | null = null
      try {
        for (let i = index; i < index + count; i++) {
          // Backpressure: the loop is faster than the encoder, and a frame is 8 MB.
          while (encoder.encodeQueueSize > MAX_ENCODE_QUEUE) {
            await drained()
            if (failure) throw fail()
          }
          const frame: VideoFrame = first ? new deps.VideoFrame(first, timing(i)) : (first = new deps.VideoFrame(image, timing(i)))
          try {
            await guard(() => encoder.encode(frame, { keyFrame: i % KEYFRAME_EVERY === 0 }))
          } finally {
            if (frame !== first) frame.close()
          }
          if (failure) throw fail()
        }
      } finally {
        first?.close()
      }
    },
    async finish() {
      await guard(() => encoder.flush())
      await written
      if (failure) throw fail()
      encoder.close()
      closed = true
      await guard(() => output.finalize())
      return new Blob([target.buffer as ArrayBuffer], { type: 'video/mp4' })
    },
    cancel() {
      if (!closed) {
        closed = true
        if (encoder.state !== 'closed') encoder.close()
      }
      void output.cancel().catch(() => {})
    },
  }
}
