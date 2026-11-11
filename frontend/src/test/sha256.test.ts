import { createHash } from 'node:crypto'
import { describe, expect, it } from 'vitest'

import { Sha256, sha256HexOfBlob } from '@/lib/sha256'

const enc = (s: string) => new TextEncoder().encode(s)
const hex = (data: Uint8Array) => {
  const h = new Sha256()
  h.update(data)
  return h.digestHex()
}

describe('Sha256', () => {
  it('matches the NIST vectors', () => {
    expect(hex(enc(''))).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
    expect(hex(enc('abc'))).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
    expect(hex(enc('abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq'))).toBe('248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1')
  })

  it('hashes a million a characters', () => {
    const h = new Sha256()
    const block = enc('a'.repeat(1000))
    for (let i = 0; i < 1000; i++) h.update(block)
    expect(h.digestHex()).toBe('cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0')
  })

  it('gives the same answer however the input is cut', () => {
    const data = new Uint8Array(1000).map((_, i) => (i * 31 + 7) & 255)
    const want = createHash('sha256').update(data).digest('hex')
    for (const cut of [1, 3, 55, 56, 63, 64, 65, 127, 999]) {
      const h = new Sha256()
      for (let at = 0; at < data.length; at += cut) h.update(data.subarray(at, at + cut))
      expect(h.digestHex(), `cut ${cut}`).toBe(want)
    }
  })

  it('agrees with a reference at every length around a block boundary', () => {
    for (let n = 0; n < 200; n++) {
      const data = new Uint8Array(n).fill(n & 255)
      expect(hex(data), `length ${n}`).toBe(createHash('sha256').update(data).digest('hex'))
    }
  })
})

describe('sha256HexOfBlob', () => {
  it('hashes a file in chunks to the same answer as hashing it whole', async () => {
    const data = new Uint8Array(300_000).map((_, i) => (i * 17 + 3) & 255)
    const want = createHash('sha256').update(data).digest('hex')
    const file = new File([data as BlobPart], 'big.jpg')
    expect(await sha256HexOfBlob(file, 64 * 1024)).toBe(want)
    expect(await sha256HexOfBlob(file, 100_000)).toBe(want)
    expect(await sha256HexOfBlob(file)).toBe(want)
  })

  it('hashes an empty file', async () => {
    expect(await sha256HexOfBlob(new File([], 'empty'))).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
  })
})
