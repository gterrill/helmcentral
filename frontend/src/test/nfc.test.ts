import { afterEach, describe, expect, it, vi } from 'vitest'
import { nfcSupported, scanTags, writeUrlTag } from '@/lib/nfc'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('nfcSupported', () => {
  it('is false when window.NDEFReader does not exist', () => {
    expect(nfcSupported()).toBe(false)
  })

  it('is true when window.NDEFReader exists', () => {
    vi.stubGlobal('NDEFReader', class {})
    expect(nfcSupported()).toBe(true)
  })
})

describe('writeUrlTag', () => {
  it('throws when Web NFC is not supported', async () => {
    await expect(writeUrlTag('https://boat.example/inventory/bins/LAZ-02')).rejects.toThrow(/not supported/)
  })

  it('writes the url as a single NDEF record via the reader', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('NDEFReader', class {
      write = write
    })

    await writeUrlTag('https://boat.example/inventory/bins/LAZ-02')

    expect(write).toHaveBeenCalledWith('https://boat.example/inventory/bins/LAZ-02', { signal: undefined })
  })

  it('propagates the reader\'s own thrown error rather than a generic one', async () => {
    vi.stubGlobal('NDEFReader', class {
      write = vi.fn().mockRejectedValue(new Error('NotAllowedError: permission denied'))
    })

    await expect(writeUrlTag('https://boat.example/inventory/bins/LAZ-02')).rejects.toThrow(/permission denied/)
  })
})

describe('scanTags', () => {
  it('throws when Web NFC is not supported', async () => {
    await expect(scanTags(() => {})).rejects.toThrow(/not supported/)
  })

  it('calls onUrl for each url record read while scanning', async () => {
    let capturedOnReading: ((event: unknown) => void) | null = null
    const scan = vi.fn().mockImplementation(async () => {})
    vi.stubGlobal('NDEFReader', class {
      scan = scan
      set onreading(handler: (event: unknown) => void) {
        capturedOnReading = handler
      }
    })

    const onUrl = vi.fn()
    await scanTags(onUrl)

    expect(scan).toHaveBeenCalled()
    expect(capturedOnReading).not.toBeNull()

    const decoder = new TextEncoder()
    capturedOnReading!({
      message: {
        records: [
          { recordType: 'url', data: new DataView(decoder.encode('https://boat.example/inventory/bins/LAZ-02').buffer) },
          { recordType: 'text', data: new DataView(decoder.encode('ignored').buffer) },
        ],
      },
    })

    expect(onUrl).toHaveBeenCalledTimes(1)
    expect(onUrl).toHaveBeenCalledWith('https://boat.example/inventory/bins/LAZ-02')
  })
})
