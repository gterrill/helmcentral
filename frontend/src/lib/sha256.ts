// SHA-256 in plain TypeScript. Web Crypto (crypto.subtle) is only there on a
// secure page, and the boat is often opened over plain http on the LAN, so the
// Inbox cannot lean on it. This is the FIPS 180-4 algorithm, fed in pieces so a
// large photo is hashed without holding it all in memory.

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

const rotr = (x: number, n: number) => (x >>> n) | (x << (32 - n))

export class Sha256 {
  private h = new Uint32Array([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19])
  private block = new Uint8Array(64)
  private filled = 0
  private length = 0
  private w = new Uint32Array(64)

  update(data: Uint8Array): void {
    this.length += data.length
    let at = 0
    if (this.filled > 0) {
      const take = Math.min(64 - this.filled, data.length)
      this.block.set(data.subarray(0, take), this.filled)
      this.filled += take
      at = take
      if (this.filled < 64) return
      this.compress(this.block, 0)
      this.filled = 0
    }
    for (; at + 64 <= data.length; at += 64) this.compress(data, at)
    if (at < data.length) {
      this.block.set(data.subarray(at), 0)
      this.filled = data.length - at
    }
  }

  /** The digest as 64 lowercase hex characters. Ends this hash: do not update it afterwards. */
  digestHex(): string {
    const bits = this.length * 8
    const pad = new Uint8Array(((this.filled + 9 + 63) & ~63) - this.filled)
    pad[0] = 0x80
    const view = new DataView(pad.buffer)
    view.setUint32(pad.length - 8, Math.floor(bits / 2 ** 32))
    view.setUint32(pad.length - 4, bits >>> 0)
    const length = this.length
    this.update(pad)
    this.length = length
    return Array.from(this.h, (v) => v.toString(16).padStart(8, '0')).join('')
  }

  private compress(data: Uint8Array, at: number): void {
    const w = this.w
    for (let i = 0; i < 16; i++) {
      const j = at + i * 4
      w[i] = (data[j] << 24) | (data[j + 1] << 16) | (data[j + 2] << 8) | data[j + 3]
    }
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3)
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10)
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0
    }
    let [a, b, c, d, e, f, g, h] = this.h
    for (let i = 0; i < 64; i++) {
      const t1 = (h + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ((e & f) ^ (~e & g)) + K[i] + w[i]) | 0
      const t2 = ((rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) | 0
      h = g
      g = f
      f = e
      e = (d + t1) | 0
      d = c
      c = b
      b = a
      a = (t1 + t2) | 0
    }
    const s = this.h
    s[0] += a
    s[1] += b
    s[2] += c
    s[3] += d
    s[4] += e
    s[5] += f
    s[6] += g
    s[7] += h
  }
}

const CHUNK_BYTES = 1024 * 1024

/** SHA-256 of a file as hex, read a chunk at a time so memory stays flat. */
export async function sha256HexOfBlob(blob: Blob, chunkBytes: number = CHUNK_BYTES): Promise<string> {
  const hash = new Sha256()
  for (let at = 0; at < blob.size; at += chunkBytes) {
    hash.update(new Uint8Array(await blob.slice(at, at + chunkBytes).arrayBuffer()))
  }
  return hash.digestHex()
}
