import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { apiBaseUrl } from '@/config/api'
import { sameJSON } from '@/lib/same-value'
import { useSelection } from '@/lib/selection'

// One request helper and one keyed read cache under the per-resource hooks
// (ADR 0173). The names (useApiQuery, setQueryData, invalidateQueries,
// refetchInterval, enabled, select) deliberately follow TanStack Query so a
// later move to it is mechanical. There is no provider: the store is module
// level, and tests clear it with resetQueryStore().

// ---------------------------------------------------------------- requests

export class ApiError extends Error {
  status: number
  fieldErrors?: Record<string, string>
  // The parsed error body, for the few callers that read more than the
  // message (a 409's `affected` list, path-keyed validation errors).
  body?: unknown
  constructor(message: string, status: number, fieldErrors?: Record<string, string>, body?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.fieldErrors = fieldErrors
    this.body = body
  }
}

interface FieldMessage {
  field: string
  message: string
}

// Three body shapes reach here and all three are the server's own words:
//   {"field":"f","message":"..."}          one rejected field
//   {"errors":[{field,message}, ...]}      several at once
//   {"error":"zone is in use"}             a failure of the record as a whole
// The status code is the last resort, only for a body that is none of them
// (not JSON, empty, or an empty `error`), never a generic message standing in
// for one the server sent.
async function toApiError(res: Response): Promise<ApiError> {
  const payload = (await res.json().catch(() => ({}))) as {
    error?: unknown
    errors?: unknown
    field?: unknown
    message?: unknown
  } | null
  const body = payload ?? {}
  if (Array.isArray(body.errors) && body.errors.length > 0) {
    const list = (body.errors as FieldMessage[]).filter(
      (e) => e && typeof e.field === 'string' && typeof e.message === 'string',
    )
    if (list.length > 0) {
      return new ApiError(
        list.map((e) => e.message).join('; '),
        res.status,
        Object.fromEntries(list.map((e) => [e.field, e.message])),
        payload,
      )
    }
  }
  if (typeof body.field === 'string' && typeof body.message === 'string') {
    return new ApiError(body.message, res.status, { [body.field]: body.message }, payload)
  }
  if (typeof body.error === 'string' && body.error.length > 0) {
    return new ApiError(body.error, res.status, undefined, payload)
  }
  return new ApiError(`HTTP ${res.status}`, res.status, undefined, payload ?? undefined)
}

export async function apiRequest<T>(
  path: string,
  init?: RequestInit & { json?: unknown },
): Promise<T> {
  const { json, ...rest } = init ?? {}
  const headers = new Headers(rest.headers)
  let body = rest.body
  if (json !== undefined) {
    body = JSON.stringify(json)
    headers.set('Content-Type', 'application/json')
  }
  // A network failure rejects here and propagates as-is.
  const res = await fetch(`${apiBaseUrl}${path}`, { ...rest, headers, body })
  if (!res.ok) throw await toApiError(res)
  // Every bodiless success the server sends is a 204; anything else is JSON,
  // and a body that is not is a server bug worth failing on.
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

// ------------------------------------------------------------------- store

export const GC_TIME_MS = 5 * 60_000

type QueryKey = readonly unknown[]

interface Snapshot {
  data: unknown
  error: Error | null
  // 'pending' until the first answer (or setQueryData) lands. A 204 answers
  // with no data, so data alone cannot tell loaded from loading.
  status: 'pending' | 'success' | 'error'
  isFetching: boolean
}

interface Entry {
  key: QueryKey
  path: string | undefined
  data: unknown
  error: Error | null
  status: 'pending' | 'success' | 'error'
  inflight: Promise<void> | undefined
  requestId: number
  subscribers: Set<Subscription>
  listeners: Set<() => void>
  gcTimer: ReturnType<typeof setTimeout> | undefined
  interval: ReturnType<typeof setInterval> | undefined
  intervalMs: number | undefined
  snapshot: Snapshot
  declared: { version: number; value: Declaration | undefined } | undefined
}

const EMPTY: Snapshot = { data: undefined, error: null, status: 'pending', isFetching: false }
const entries = new Map<string, Entry>()

function stableStringify(value: unknown): string {
  if (value === undefined) return 'null'
  if (value === null || typeof value !== 'object') return JSON.stringify(value) ?? 'null'
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`
  const obj = value as Record<string, unknown>
  return `{${Object.keys(obj)
    .sort()
    .map((k) => `${JSON.stringify(k)}:${stableStringify(obj[k])}`)
    .join(',')}}`
}

function hashKey(key: QueryKey): string {
  return stableStringify(key)
}

function commit(entry: Entry) {
  entry.snapshot = {
    data: entry.data,
    error: entry.error,
    status: entry.status,
    isFetching: entry.inflight !== undefined,
  }
  entry.listeners.forEach((l) => l())
}

function ensureEntry(key: QueryKey): Entry {
  const hash = hashKey(key)
  let entry = entries.get(hash)
  if (!entry) {
    entry = {
      key,
      path: undefined,
      data: undefined,
      error: null,
      status: 'pending',
      inflight: undefined,
      requestId: 0,
      subscribers: new Set(),
      listeners: new Set(),
      gcTimer: undefined,
      interval: undefined,
      intervalMs: undefined,
      snapshot: EMPTY,
      declared: undefined,
    }
    entries.set(hash, entry)
  }
  return entry
}

function dropEntry(entry: Entry) {
  if (entry.gcTimer !== undefined) clearTimeout(entry.gcTimer)
  if (entry.interval !== undefined) clearInterval(entry.interval)
  entry.gcTimer = undefined
  entry.interval = undefined
  entry.requestId += 1
  entry.inflight = undefined
  const hash = hashKey(entry.key)
  if (entries.get(hash) === entry) entries.delete(hash)
}

function scheduleGc(entry: Entry) {
  if (entry.gcTimer !== undefined) clearTimeout(entry.gcTimer)
  entry.gcTimer = setTimeout(() => {
    if (entry.subscribers.size === 0) dropEntry(entry)
  }, GC_TIME_MS)
}

// Starts a request that supersedes any in flight. Only the response of the
// latest request for the entry lands (the guard every hand-rolled seqRef was
// for); an older one is dropped when it arrives, and whoever awaited it waits
// on the request that replaced it instead.
function startFetch(entry: Entry, path: string): Promise<void> {
  const id = (entry.requestId += 1)
  const run = async (): Promise<void> => {
    try {
      const data = await apiRequest<unknown>(path)
      if (entry.requestId !== id) return entry.inflight
      // An answer equal by value to the data the entry holds keeps that
      // object, when the key declares it (declareQuery), so every reader,
      // every selector and getQueryData see no change. Compared once per
      // answer, here, not once per reader, and whatever is mounted.
      entry.data = entry.data !== undefined && declarationOf(hashKey(entry.key), entry.key)?.structuralSharing === true && sameJSON(entry.data, data) ? entry.data : data
      entry.error = null
      entry.status = 'success'
    } catch (err) {
      if (entry.requestId !== id) return entry.inflight
      entry.error = err instanceof Error ? err : new Error(String(err))
      entry.status = 'error'
    }
    entry.inflight = undefined
    commit(entry)
  }
  const promise = run()
  entry.inflight = promise
  commit(entry)
  return promise
}

// ------------------------------------------------------------ declarations

/**
 * What a query key says about its own data, declared once beside the key
 * rather than by each reader (ADR 0215). Both are off unless declared, and
 * neither depends on which readers are mounted.
 */
export interface QueryDeclaration<Raw = never, Parsed = unknown> {
  /**
   * Keep the entry's data object when a request answers with data equal by
   * value, so no reader of the key sees a change. Named after TanStack
   * Query's option, which does more: it also keeps the unchanged parts of
   * changed data. Off unless declared, because a reader with an effect on
   * `data` would no longer see a refetch that changed nothing.
   */
  structuralSharing?: boolean
  /**
   * Turns one answer into the value `useApiQuerySelector`'s `select` sees.
   * It runs once per answer, however many readers there are, and not at all
   * for an answer nobody selects from. It is called with undefined while no
   * answer has landed. `useApiQuery`, `getQueryData` and `setQueryData` deal
   * in the raw answer.
   */
  parse?: (raw: Raw | undefined) => Parsed
}

interface Declaration {
  prefix: QueryKey
  structuralSharing: boolean
  parse: ((raw: unknown) => unknown) | undefined
  // Parse results by the raw answer they came from (an object), so a second
  // reader of the same answer finds the first one's result.
  memo: WeakMap<object, { value: unknown } | { error: unknown }>
  absent: { value: unknown } | { error: unknown } | undefined
}

const declarations = new Map<string, Declaration>()
// Bumped by every new declaration, so an entry's cached lookup knows it is stale.
let declarationVersion = 0

function overlaps(a: QueryKey, b: QueryKey): boolean {
  const n = Math.min(a.length, b.length)
  for (let i = 0; i < n; i++) if (stableStringify(a[i]) !== stableStringify(b[i])) return false
  return true
}

/**
 * Declares what every reader of the keys that start with `prefix` gets.
 *
 * Call it at module level in the module that exports the key (the constant or
 * the hook that builds it), so that importing the key imports its declaration.
 * A declaration is looked up when an answer lands and when a selector reads,
 * with no fallback: a key read or fetched before its declaring module has
 * loaded would get neither parse nor sharing, so never put a declaration
 * somewhere a reader of the key can skip.
 *
 * Declarations may not overlap. A second one that is the same prefix, or a
 * prefix of, or prefixed by, an existing one with different options throws,
 * because which would win would depend on import order. Declaring the same
 * thing again (a module imported twice, hot reload) is a no-op.
 */
export function declareQuery<Raw = never, Parsed = unknown>(prefix: QueryKey, options: QueryDeclaration<Raw, Parsed>): void {
  const sharing = options.structuralSharing === true
  const parse = options.parse as ((raw: unknown) => unknown) | undefined
  const hash = hashKey(prefix)
  for (const [otherHash, other] of declarations) {
    if (!overlaps(prefix, other.prefix)) continue
    // A re-evaluated module has a new function with the same source.
    const sameParse = other.parse === parse || (other.parse !== undefined && parse !== undefined && other.parse.toString() === parse.toString())
    if (other.structuralSharing === sharing && sameParse) {
      if (otherHash === hash) other.parse = parse
      continue
    }
    throw new Error(`declareQuery: ${hash} overlaps the declaration for ${otherHash} with different options`)
  }
  if (declarations.has(hash)) return
  declarations.set(hash, { prefix, structuralSharing: sharing, parse, memo: new WeakMap(), absent: undefined })
  declarationVersion += 1
}

function resolveDeclaration(key: QueryKey): Declaration | undefined {
  let best: Declaration | undefined
  for (const d of declarations.values()) {
    if (d.prefix.length > key.length || (best && d.prefix.length <= best.prefix.length)) continue
    if (overlaps(d.prefix, key)) best = d
  }
  return best
}

// The declaration for an entry's key, resolved once per declaration change.
function declarationOf(hash: string, key: QueryKey): Declaration | undefined {
  const entry = entries.get(hash)
  if (!entry) return resolveDeclaration(key)
  if (entry.declared?.version !== declarationVersion) entry.declared = { version: declarationVersion, value: resolveDeclaration(key) }
  return entry.declared.value
}

// The declared parse of one answer, computed once per answer. A parse that
// throws is remembered too, so a bad body fails once, not once per reader.
function parsedAnswer(d: Declaration, raw: unknown): unknown {
  const cacheable = typeof raw === 'object' && raw !== null
  let hit = cacheable ? d.memo.get(raw) : raw === undefined ? d.absent : undefined
  if (!hit) {
    try {
      hit = { value: d.parse!(raw) }
    } catch (error) {
      hit = { error }
    }
    if (cacheable) d.memo.set(raw, hit)
    else if (raw === undefined) d.absent = hit
  }
  if ('error' in hit) throw hit.error
  return hit.value
}

// Resolves when the request settles. Never rejects: a failure lands in the
// entry's error. Requests for the same key share the one in flight, which is
// right for a mount or a poll: any answer from now on will do.
export function fetchQuery(key: QueryKey, path: string): Promise<void> {
  const entry = ensureEntry(key)
  entry.path = path
  if (entry.subscribers.size === 0) scheduleGc(entry)
  return entry.inflight ?? startFetch(entry, path)
}

// An explicit refetch always starts a new request. A read already in flight
// may have left before a write the caller just made, so sharing it could hand
// back the state from before that write.
export function refetchQuery(key: QueryKey, path: string): Promise<void> {
  const entry = ensureEntry(key)
  entry.path = path
  if (entry.subscribers.size === 0) scheduleGc(entry)
  return startFetch(entry, path)
}

function updatePolling(entry: Entry) {
  let min: number | undefined
  entry.subscribers.forEach((s) => {
    if (s.interval !== undefined && s.interval > 0 && (min === undefined || s.interval < min)) {
      min = s.interval
    }
  })
  if (min === entry.intervalMs) return
  if (entry.interval !== undefined) clearInterval(entry.interval)
  entry.interval = undefined
  entry.intervalMs = min
  if (min !== undefined) {
    entry.interval = setInterval(() => {
      if (entry.path !== undefined) void fetchQuery(entry.key, entry.path)
    }, min)
  }
}

interface Subscription {
  interval: number | undefined
}

function subscribeEntry(
  key: QueryKey,
  path: string,
  sub: Subscription,
  onChange: () => void,
): () => void {
  const entry = ensureEntry(key)
  entry.path = path
  if (entry.gcTimer !== undefined) clearTimeout(entry.gcTimer)
  entry.gcTimer = undefined
  entry.subscribers.add(sub)
  entry.listeners.add(onChange)
  updatePolling(entry)
  // Always revalidate on mount; a request already in flight is shared.
  void fetchQuery(key, path)
  return () => {
    entry.subscribers.delete(sub)
    entry.listeners.delete(onChange)
    updatePolling(entry)
    if (entry.subscribers.size === 0 && entries.get(hashKey(entry.key)) === entry) scheduleGc(entry)
  }
}

// A changed refetchInterval retunes the poll in place. Resubscribing would
// count as a fresh mount and fetch again.
function retunePolling(key: QueryKey, sub: Subscription, interval: number | undefined) {
  sub.interval = interval
  const entry = entries.get(hashKey(key))
  if (entry && entry.subscribers.has(sub)) updatePolling(entry)
}

export function getQueryData<T>(key: QueryKey): T | undefined {
  return entries.get(hashKey(key))?.data as T | undefined
}

/**
 * Replaces the entry's data with a value, or with what an updater makes of the
 * data held. An updater must return a new object and leave its argument alone:
 * a key's declared parse is remembered by the answer object, so a changed-in-
 * place argument handed back would keep its old parse. In development an
 * updater that returns the object it was given throws.
 */
export function setQueryData<T>(
  key: QueryKey,
  updater: T | ((prev: T | undefined) => T),
): void {
  const entry = ensureEntry(key)
  // A read that left before this write would answer with the state from before
  // it, so it is dropped when it lands.
  entry.requestId += 1
  entry.inflight = undefined
  // With no data an updater would build its value from `undefined` (nothing
  // loaded yet, or the load failed), so the result is only a fragment of the
  // answer: one new row, not the list. It is not stored, because a failed read
  // after it would leave the fragment standing as the real list. The list comes
  // only from a read made after the write. A plain value is the caller's whole
  // answer and stands.
  if (entry.data === undefined && typeof updater === 'function') {
    if (entry.subscribers.size > 0 && entry.path !== undefined) {
      void startFetch(entry, entry.path)
      return
    }
    scheduleGc(entry)
    commit(entry)
    return
  }
  let next: T
  if (typeof updater === 'function') {
    const prev = entry.data as T | undefined
    next = (updater as (prev: T | undefined) => T)(prev)
    // A parse is remembered by the answer object it read, so an updater that
    // changes its argument in place and hands it back would leave a stale parse.
    if (import.meta.env.DEV && prev !== undefined && prev === next && typeof next === 'object' && next !== null) {
      throw new Error('setQueryData: the updater returned the object it was given. Return a new object.')
    }
  } else {
    next = updater
  }
  entry.data = next
  entry.status = 'success'
  entry.error = null
  if (entry.subscribers.size === 0) scheduleGc(entry)
  commit(entry)
}

// Every key that starts with `prefix`: one with subscribers refetches now; one
// without drops any response still in flight and refetches on its next mount.
// The result settles when the refetches it started have, for a caller that
// wants fresh data on screen before it goes on and would otherwise have to
// refetch the same key a second time. It never rejects.
//
// `skip` leaves a matching key alone, for a key whose record is gone and would
// only answer 404.
export function invalidateQueries(prefix: QueryKey, skip?: (key: QueryKey) => boolean): Promise<void> {
  const started: Promise<void>[] = []
  const wanted = prefix.map(stableStringify)
  for (const entry of Array.from(entries.values())) {
    if (entry.key.length < wanted.length) continue
    if (!wanted.every((w, i) => stableStringify(entry.key[i]) === w)) continue
    if (skip?.(entry.key)) continue
    if (entry.subscribers.size > 0 && entry.path !== undefined) {
      started.push(startFetch(entry, entry.path))
    } else {
      entry.requestId += 1
      entry.inflight = undefined
      commit(entry)
    }
  }
  return Promise.all(started).then(() => undefined)
}

// For tests: forget everything and stop every timer.
export function resetQueryStore(): void {
  for (const entry of Array.from(entries.values())) {
    const listeners = Array.from(entry.listeners)
    dropEntry(entry)
    entry.snapshot = EMPTY
    listeners.forEach((l) => l())
  }
  entries.clear()
}

// -------------------------------------------------------------------- hook

export interface UseApiQueryOptions<T> {
  queryKey: QueryKey
  path: string
  refetchInterval?: number
  enabled?: boolean
  select?: (raw: never) => T
  /**
   * Re-render when a request leaves and when it lands, even if the data,
   * error and status did not change. Off by default: a reader re-renders only
   * when one of those changes, and `isFetching` is current whenever it
   * renders for any reason. Pass it only to show a request in flight.
   */
  trackFetching?: boolean
}

export interface UseApiQueryResult<T> {
  data: T | undefined
  error: Error | null
  isLoading: boolean
  isFetching: boolean
  refetch: () => Promise<void>
}

const noopSubscribe = () => () => {}

// One hook instance's subscription to an entry: it mounts the entry, polls at
// refetchInterval, and retunes the poll in place when the interval changes.
function useEntrySubscription(
  queryKey: QueryKey,
  path: string,
  refetchInterval: number | undefined,
  enabled: boolean,
): { hash: string; key: QueryKey; subscribe: (onChange: () => void) => () => void } {
  const hash = hashKey(queryKey)

  // The key array is a fresh literal on most renders; its hash is the identity.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const key = useMemo(() => queryKey, [hash])

  // One subscription record per hook instance; the interval lives on it so a
  // change does not resubscribe.
  const subRef = useRef<Subscription>({ interval: refetchInterval })
  const subscribe = useCallback(
    (onChange: () => void) => {
      if (!enabled) return noopSubscribe()
      subRef.current = { interval: subRef.current.interval }
      return subscribeEntry(key, path, subRef.current, onChange)
    },
    [key, path, enabled],
  )
  useEffect(() => {
    retunePolling(key, subRef.current, refetchInterval)
  }, [key, refetchInterval])
  return { hash, key, subscribe }
}

/**
 * One slice of a query's data, for a reader that should re-render only when
 * that slice changes (ADR 0215). It re-renders only when `select` returns
 * something `isEqual` calls different. It fetches and polls exactly as
 * useApiQuery does, sharing the same entry.
 *
 * `select` sees the data (undefined until the first answer lands; the key's
 * declared `parse` of it, if it declares one), the
 * query's error, and the selection it returned last time (undefined on the
 * first call), so a reader can hand back the parts that did not change.
 * Like useApiQuery's own select, a body `select` cannot read becomes the
 * error rather than a throw during render: `select` is called again with no
 * data and that error, so the reader shows what it shows for a failed query.
 * `select` may be a fresh function on every render.
 */
export function useApiQuerySelector<T, S>(
  { queryKey, path, refetchInterval, enabled = true }: Omit<UseApiQueryOptions<T>, 'select' | 'trackFetching'>,
  select: (data: T | undefined, error: Error | null, previous: S | undefined) => S,
  isEqual: (a: S, b: S) => boolean = Object.is,
): S {
  const { hash, key, subscribe } = useEntrySubscription(queryKey, path, refetchInterval, enabled)
  const getSnapshot = useCallback((): Snapshot => (enabled ? (entries.get(hash)?.snapshot ?? EMPTY) : EMPTY), [hash, enabled])
  const selectSnapshot = useCallback(
    (snapshot: Snapshot, previous: S | undefined): S => {
      const declared = declarationOf(hash, key)
      const read = (data: unknown, error: Error | null): S => select(data as T | undefined, error, previous)
      try {
        return read(declared?.parse ? parsedAnswer(declared, snapshot.data) : snapshot.data, snapshot.error)
      } catch (err) {
        // The reader shows what it shows for a failed query: no answer, as
        // the key's parse reads it, and the error.
        const none = declared?.parse ? parsedAnswer(declared, undefined) : undefined
        return read(none, err instanceof Error ? err : new Error(String(err)))
      }
    },
    [hash, key, select],
  )
  return useSelection({ subscribe, getSnapshot, select: selectSnapshot, isEqual, sameSnapshot: sameAnswer })
}

// Snapshots that differ only in isFetching or status feed select the same input.
const sameAnswer = (a: Snapshot, b: Snapshot) => a.data === b.data && a.error === b.error

export function useApiQuery<T>({
  queryKey,
  path,
  refetchInterval,
  enabled = true,
  select,
  trackFetching = false,
}: UseApiQueryOptions<T>): UseApiQueryResult<T> {
  const { hash, key, subscribe } = useEntrySubscription(queryKey, path, refetchInterval, enabled)

  // A request leaving and landing flips isFetching twice. Only a reader that
  // asked (trackFetching) re-renders on those flips (ADR 0215): every other
  // reader keeps the snapshot it holds while the data, the error and the
  // status are the same, and so is not notified.
  const held = useRef<Snapshot | null>(null)
  const getSnapshot = useCallback((): Snapshot => {
    const source = enabled ? (entries.get(hash)?.snapshot ?? EMPTY) : EMPTY
    const last = held.current
    if (!trackFetching && last && last.data === source.data && last.error === source.error && last.status === source.status) return last
    held.current = source
    return source
  }, [hash, enabled, trackFetching])
  const snap = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  const refetch = useCallback(
    () => (enabled ? refetchQuery(key, path) : Promise.resolve()),
    [key, path, enabled],
  )

  // select is read when the data changes, so an inline function does not
  // hand callers a new object every render. A body select cannot read (a
  // null, a missing field) becomes the query's error rather than a throw
  // during render.
  const selected = useMemo((): { data: T | undefined; error: Error | null } => {
    if (snap.data === undefined) return { data: undefined, error: null }
    if (!select) return { data: snap.data as T, error: null }
    try {
      return { data: select(snap.data as never), error: null }
    } catch (err) {
      return { data: undefined, error: err instanceof Error ? err : new Error(String(err)) }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [snap.data])

  const error = snap.error ?? selected.error
  return {
    data: selected.data,
    error,
    isLoading: enabled && snap.status === 'pending' && error === null,
    // Read from the entry as this render runs, not from the snapshot held: a
    // reader that does not track it holds one from before the last flip.
    isFetching: enabled ? (entries.get(hash)?.snapshot.isFetching ?? false) : false,
    refetch,
  }
}
