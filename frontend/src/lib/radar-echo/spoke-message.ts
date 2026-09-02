/**
 * Hand-rolled protobuf reader for mayara's spoke stream. No protobuf
 * dependency: the wire schema is 2 messages and 7 fields total, captured
 * from mayara's own RadarMessage.proto (echoed in
 * backend/testdata/mayara/spokes-fur6424A.md) and confirmed against the
 * Phase 0 capture, so pulling in a full protobuf runtime for this one
 * message shape would be a large dependency for a small, stable target.
 *
 *   message RadarMessage {
 *     message Spoke {
 *       uint32 angle = 1;              // [0..spokesPerRevolution), from bow
 *       optional uint32 bearing = 2;   // [0..spokesPerRevolution), true north
 *       uint32 range = 3;              // metres, range of the last pixel in data
 *       optional uint64 time = 4;      // millis since epoch (unused here)
 *       bytes data = 5;
 *       optional double lat = 6;       // radar's own fix at generation time
 *       optional double lon = 7;
 *     }
 *     repeated Spoke spokes = 2;
 *   }
 *
 * Wire types: varint (0) for angle/bearing/range/time, length-delimited (2)
 * for data and the embedded Spoke message itself, 64-bit (1) for lat/lon as
 * little-endian float64. Every other field number or wire type is skipped
 * by its wire type rather than rejected outright, so a future mayara field
 * this reader doesn't know about degrades gracefully instead of dropping
 * every spoke in the message.
 *
 * Phase 0 measured `bearing`, `lat` and `lon` populated on all 48,056
 * captured spokes, so in practice these are never null on real hardware --
 * they stay optional in the type because the wire format itself marks them
 * `optional`, and a decoder that assumes otherwise is exactly the kind of
 * "documented value contradicted by the wire" mistake this integration has
 * made before.
 */

export interface RadarSpoke {
  angle: number
  bearing: number | null
  rangeM: number
  lat: number | null
  lon: number | null
  data: Uint8Array
}

const WIRE_VARINT = 0
const WIRE_64BIT = 1
const WIRE_LEN = 2
const WIRE_32BIT = 5

const FIELD_SPOKES = 2

const SPOKE_FIELD_ANGLE = 1
const SPOKE_FIELD_BEARING = 2
const SPOKE_FIELD_RANGE = 3
const SPOKE_FIELD_TIME = 4
const SPOKE_FIELD_DATA = 5
const SPOKE_FIELD_LAT = 6
const SPOKE_FIELD_LON = 7

/** A cursor over one shared byte/view pair. Never allocates per read. */
class WireReader {
  pos: number

  constructor(
    private readonly bytes: Uint8Array,
    private readonly view: DataView,
    start = 0,
  ) {
    this.pos = start
  }

  readVarint(): number {
    let result = 0
    let shift = 0
    for (;;) {
      const byte = this.bytes[this.pos]
      this.pos += 1
      result |= (byte & 0x7f) << shift
      if ((byte & 0x80) === 0) break
      shift += 7
    }
    // >>> 0 forces an unsigned reading. angle/bearing/range are all well
    // under 2^31 on real hardware, so this never loses information; it
    // only guards against the sign bit flipping on a stray high shift.
    return result >>> 0
  }

  readFloat64LE(): number {
    const value = this.view.getFloat64(this.pos, true)
    this.pos += 8
    return value
  }

  /** Length-delimited payload bounds, without copying. */
  readLenBounds(): { start: number; length: number } {
    const length = this.readVarint()
    const start = this.pos
    this.pos += length
    return { start, length }
  }

  /** Advance past one field's value without materialising it. */
  skip(wireType: number): void {
    switch (wireType) {
      case WIRE_VARINT:
        this.readVarint()
        return
      case WIRE_64BIT:
        this.pos += 8
        return
      case WIRE_LEN: {
        const length = this.readVarint()
        this.pos += length
        return
      }
      case WIRE_32BIT:
        this.pos += 4
        return
      default:
        throw new Error(`radar-echo: unsupported protobuf wire type ${wireType}`)
    }
  }
}

function readTag(reader: WireReader): { field: number; wireType: number } {
  const tag = reader.readVarint()
  return { field: tag >>> 3, wireType: tag & 0x7 }
}

/**
 * Decode one Spoke submessage. Returns null when the spoke is masked out by
 * `angleMask` or is missing its data payload -- either way there is nothing
 * to render for it.
 *
 * Protobuf field encoders (including mayara's Rust `prost`) emit fields in
 * declaration order, so `angle` (tag 1) always arrives before `data` (tag
 * 5) on this wire. That lets a rejected spoke skip past the 1024-byte data
 * field by length alone, without ever materialising it -- the only lever
 * available against a stream that cannot be asked to send less.
 */
function decodeSpoke(bytes: Uint8Array, view: DataView, start: number, length: number, angleMask: number): RadarSpoke | null {
  const reader = new WireReader(bytes, view, start)
  const end = start + length

  let angle = 0
  let bearing: number | null = null
  let rangeM = 0
  let lat: number | null = null
  let lon: number | null = null
  let data: Uint8Array | null = null
  let rejected = false

  while (reader.pos < end) {
    const { field, wireType } = readTag(reader)
    switch (field) {
      case SPOKE_FIELD_ANGLE:
        angle = reader.readVarint()
        // angleMask === 0 keeps everything: angle & 0 is always 0.
        if ((angle & angleMask) !== 0) rejected = true
        break
      case SPOKE_FIELD_BEARING:
        bearing = reader.readVarint()
        break
      case SPOKE_FIELD_RANGE:
        rangeM = reader.readVarint()
        break
      case SPOKE_FIELD_DATA:
        if (rejected) {
          // Advance past the payload's length without copying it.
          reader.skip(wireType)
        } else {
          const { start: dataStart, length: dataLength } = reader.readLenBounds()
          // .slice() copies. The frame this view was built over is
          // discarded by the caller right after decodeRadarMessage
          // returns, so a retained subarray view would be a use-after-free
          // that only shows up under GC pressure -- never in a quick test.
          data = bytes.slice(dataStart, dataStart + dataLength)
        }
        break
      case SPOKE_FIELD_LAT:
        lat = reader.readFloat64LE()
        break
      case SPOKE_FIELD_LON:
        lon = reader.readFloat64LE()
        break
      case SPOKE_FIELD_TIME:
      default:
        reader.skip(wireType)
        break
    }
  }

  if (rejected || data === null) return null
  return { angle, bearing, rangeM, lat, lon, data }
}

/**
 * Decode one binary WebSocket frame (one RadarMessage) into its spokes.
 *
 * `angleMask`: keep a spoke only when `(angle & angleMask) === 0`. This is
 * the only server-independent lever against bandwidth: mayara cannot be
 * asked to send fewer spokes, so a masked-out spoke's data field is skipped
 * by length instead of copied. Default 0 keeps every spoke.
 */
export function decodeRadarMessage(frame: ArrayBuffer | Uint8Array, angleMask = 0): RadarSpoke[] {
  const bytes = frame instanceof Uint8Array ? frame : new Uint8Array(frame)
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const reader = new WireReader(bytes, view)
  const spokes: RadarSpoke[] = []

  while (reader.pos < bytes.length) {
    const { field, wireType } = readTag(reader)
    if (field === FIELD_SPOKES && wireType === WIRE_LEN) {
      const { start, length } = reader.readLenBounds()
      const spoke = decodeSpoke(bytes, view, start, length, angleMask)
      if (spoke !== null) spokes.push(spoke)
    } else {
      reader.skip(wireType)
    }
  }

  return spokes
}
