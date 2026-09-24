// ADR 0127: Web NFC - the browser API that reads and writes physical NFC
// tags - exists only in Chrome on Android at the time of writing. It is a
// working-draft API with no ambient TypeScript declaration in this
// project's lib set, so the three interfaces below are the minimal shape
// this file actually calls, kept local rather than reaching for a
// DefinitelyTyped package for three methods. tag-row.tsx is the only
// caller; everywhere else (iPhone, desktop) nfcSupported() is false and the
// row falls back to Copy, never a silent, broken "Write tag" button.

interface NDEFReadingEventLike extends Event {
  message: { records: { recordType: string; data?: DataView }[] }
}

interface NDEFMessageInitLike {
  records: { recordType: string; data: string }[]
}

interface NDEFReaderLike extends EventTarget {
  scan(options?: { signal?: AbortSignal }): Promise<void>
  write(message: NDEFMessageInitLike, options?: { signal?: AbortSignal }): Promise<void>
  onreading: ((event: NDEFReadingEventLike) => void) | null
}

interface NDEFReaderConstructor {
  new (): NDEFReaderLike
}

// Read off window on every call, not cached at module load - the same
// reasoning hasWebGL2 probes lazily (lib/webgl.ts): the module can load
// long before (or in a test, without) the browser feature it is asking
// about ever being present.
function ndefReaderConstructor(): NDEFReaderConstructor | undefined {
  if (typeof window === 'undefined') return undefined
  return (window as unknown as { NDEFReader?: NDEFReaderConstructor }).NDEFReader
}

export function nfcSupported(): boolean {
  return ndefReaderConstructor() !== undefined
}

/**
 * Writes url to the tag the operator taps, as a single NDEF URL record (ADR
 * 0127 §1: "An NDEF URL record, and nothing else"). Resolves once the write
 * completes (the browser's own "hold the phone to the tag" prompt runs
 * first); throws whatever the browser threw - a permission refusal, no tag
 * presented before signal aborts, or Web NFC genuinely unsupported - so
 * tag-row.tsx shows the real reason rather than a generic failure
 * (AGENTS.md fallback policy).
 */
export async function writeUrlTag(url: string, signal?: AbortSignal): Promise<void> {
  const Ctor = ndefReaderConstructor()
  if (!Ctor) {
    throw new Error('Web NFC is not supported in this browser')
  }
  const reader = new Ctor()
  // A bare string here writes a TEXT record, not a URL record - Web NFC's
  // own overload for "write this exact string" (fine for a text tag, wrong
  // for this one): a phone tapping the tag then opens nothing, and
  // scanTags' own recordType === 'url' filter below ignores it too. The
  // explicit NDEFMessageInit shape is what actually produces "An NDEF URL
  // record, and nothing else" (ADR 0127 §1).
  await reader.write({ records: [{ recordType: 'url', data: url }] }, { signal })
}

/**
 * Starts a continuous NFC scan, calling onUrl once per URL record read
 * (stocktake-section.tsx's own scan loop - every tap while scanning is
 * live). Resolves once scanning has STARTED, not when it stops - matching
 * Web NFC's own scan() contract - so a caller awaiting this only learns
 * whether the scan could begin at all (permission, hardware); reads
 * continue arriving through onUrl until signal aborts.
 */
export async function scanTags(onUrl: (url: string) => void, signal?: AbortSignal): Promise<void> {
  const Ctor = ndefReaderConstructor()
  if (!Ctor) {
    throw new Error('Web NFC is not supported in this browser')
  }
  const reader = new Ctor()
  reader.onreading = (event) => {
    for (const record of event.message.records) {
      if (record.recordType === 'url' && record.data) {
        onUrl(new TextDecoder().decode(record.data))
      }
    }
  }
  await reader.scan({ signal })
}
