// A small in-memory IndexedDB for tests. fake-indexeddb is not installed, and the
// queue only needs open/upgrade, one object store with a key path, add, put,
// get, getAll, delete and clear inside a transaction. Values are kept by
// reference (jsdom's Blob cannot be structured-cloned). A transaction commits
// all its writes or none: a failed request aborts it and undoes them.

type Listener = ((event: unknown) => void) | null

class FakeRequest<T> {
  result!: T
  error: Error | null = null
  onsuccess: Listener = null
  onerror: Listener = null
}

export interface FakeIdbOptions {
  /** Makes the nth write request from now (1 is the next) fail with this error, aborting its transaction. */
  failWrite?: { n: number; error: Error }
  /** Makes the next open report `blocked` (another connection held up an upgrade) before it succeeds. */
  blockOpen?: boolean
  /** Makes the next open report `blocked` and then never answer (another connection never lets go). */
  blockOpenForever?: boolean
  /** Makes `db.transaction` throw this error, as a connection that is closing does. */
  transactionError?: Error
}

class FakeStore {
  rows = new Map<string, Record<string, unknown>>()
  constructor(readonly keyPath: string) {}
}

class FakeTransaction {
  oncomplete: Listener = null
  onerror: Listener = null
  onabort: Listener = null
  error: Error | null = null
  private undo: (() => void)[] = []
  private pending = 0
  private done = false
  private failed = false

  constructor(private readonly db: FakeDatabase, private readonly names: string[], readonly mode: string) {
    setTimeout(() => this.settle(), 0)
  }

  objectStore(name: string) {
    if (!this.names.includes(name)) throw new Error(`NotFoundError: ${name} is not in this transaction`)
    const store = this.db.stores.get(name)
    if (!store) throw new Error(`NotFoundError: no store ${name}`)
    return new FakeObjectStore(this, store)
  }

  abort() {
    if (this.done) throw new Error('InvalidStateError: transaction finished')
    this.fail(new Error('AbortError'))
  }

  /** @internal */
  request<T>(run: () => T, write: boolean): FakeRequest<T> {
    const req = new FakeRequest<T>()
    this.pending += 1
    queueMicrotask(() => {
      this.pending -= 1
      if (this.done) return
      try {
        if (write) {
          if (this.mode !== 'readwrite') throw new Error('ReadOnlyError')
          const plan = this.db.factory.options.failWrite
          if (plan) {
            plan.n -= 1
            if (plan.n === 0) throw plan.error
          }
        }
        req.result = run()
        req.onsuccess?.({ target: req })
      } catch (err) {
        req.error = err as Error
        req.onerror?.({ target: req })
        this.fail(err as Error)
        return
      }
      setTimeout(() => this.settle(), 0)
    })
    return req
  }

  /** @internal */
  record(undo: () => void) {
    this.undo.push(undo)
  }

  private settle() {
    if (this.done || this.pending > 0) return
    this.done = true
    this.oncomplete?.({ target: this })
  }

  private fail(err: Error) {
    if (this.done || this.failed) return
    this.failed = true
    this.done = true
    for (const undo of this.undo.reverse()) undo()
    this.error = err
    this.onerror?.({ target: this })
    this.onabort?.({ target: this })
  }
}

class FakeObjectStore {
  constructor(private readonly tx: FakeTransaction, private readonly store: FakeStore) {}

  private write(value: Record<string, unknown>, mustBeNew: boolean) {
    const key = String(value[this.store.keyPath])
    const prior = this.store.rows.get(key)
    if (mustBeNew && prior) throw new Error(`ConstraintError: key ${key} exists`)
    this.store.rows.set(key, { ...value })
    this.tx.record(() => {
      if (prior) this.store.rows.set(key, prior)
      else this.store.rows.delete(key)
    })
    return key
  }

  add(value: Record<string, unknown>) {
    return this.tx.request(() => this.write(value, true), true)
  }

  put(value: Record<string, unknown>) {
    return this.tx.request(() => this.write(value, false), true)
  }

  get(key: string) {
    return this.tx.request(() => {
      const row = this.store.rows.get(key)
      return row ? { ...row } : undefined
    }, false)
  }

  getAll() {
    return this.tx.request(() => [...this.store.rows.values()].map((r) => ({ ...r })), false)
  }

  count() {
    return this.tx.request(() => this.store.rows.size, false)
  }

  delete(key: string) {
    return this.tx.request(() => {
      const prior = this.store.rows.get(key)
      this.store.rows.delete(key)
      this.tx.record(() => { if (prior) this.store.rows.set(key, prior) })
      return undefined
    }, true)
  }

  clear() {
    return this.tx.request(() => {
      const prior = new Map(this.store.rows)
      this.store.rows.clear()
      this.tx.record(() => { for (const [k, v] of prior) this.store.rows.set(k, v) })
      return undefined
    }, true)
  }
}

class FakeDatabase {
  stores = new Map<string, FakeStore>()
  version = 0
  objectStoreNames = { contains: (name: string) => this.stores.has(name) }
  closed = false
  onversionchange: Listener = null
  onclose: Listener = null

  constructor(readonly name: string, readonly factory: FakeIdbFactory) {}

  createObjectStore(name: string, options: { keyPath: string }) {
    const store = new FakeStore(options.keyPath)
    this.stores.set(name, store)
    return store
  }

  transaction(names: string | string[], mode: string = 'readonly') {
    if (this.closed) throw new Error('InvalidStateError: the connection is closing')
    if (this.factory.options.transactionError) throw this.factory.options.transactionError
    return new FakeTransaction(this, Array.isArray(names) ? names : [names], mode)
  }

  close() { this.closed = true }
}

class FakeOpenRequest extends FakeRequest<FakeDatabase> {
  onupgradeneeded: Listener = null
  onblocked: Listener = null
}

/** The part of `indexedDB` the queue uses. */
export class FakeIdbFactory {
  readonly databases = new Map<string, FakeDatabase>()
  readonly opened: { name: string; version: number | undefined }[] = []
  options: FakeIdbOptions = {}

  open(name: string, version?: number) {
    this.opened.push({ name, version })
    const req = new FakeOpenRequest()
    queueMicrotask(() => {
      let db = this.databases.get(name)
      const fresh = !db
      if (!db) {
        db = new FakeDatabase(name, this)
        this.databases.set(name, db)
      }
      if (this.options.blockOpenForever) {
        this.options.blockOpenForever = false
        req.onblocked?.({ target: req })
        return
      }
      req.result = db
      db.closed = false
      if (this.options.blockOpen) {
        this.options.blockOpen = false
        req.onblocked?.({ target: req })
      }
      if (fresh || (version !== undefined && version > db.version)) {
        db.version = version ?? 1
        req.onupgradeneeded?.({ target: req })
      }
      req.onsuccess?.({ target: req })
    })
    return req
  }

  /** The browser dropping the connection (storage cleared, profile closed): `close` fires, the handle is dead. */
  forceClose(name: string) {
    const db = this.databases.get(name)
    if (!db) return
    db.closed = true
    db.onclose?.({ target: db })
  }

  /** Another tab upgrading the database: every open connection is asked to let go. */
  versionChange(name: string) {
    this.databases.get(name)?.onversionchange?.({ target: this.databases.get(name) })
  }

  /**
   * Writes rows straight into a store, creating the database at `version` with
   * its stores if it is not there yet: what another writer (the service worker)
   * leaves behind. It opens no connection, so `opened` is untouched.
   */
  seed(dbName: string, version: number, storeNames: string[], storeName: string, rows: Record<string, unknown>[]) {
    let db = this.databases.get(dbName)
    if (!db) {
      db = new FakeDatabase(dbName, this)
      db.version = version
      this.databases.set(dbName, db)
    }
    for (const name of storeNames) {
      if (!db.stores.has(name)) db.createObjectStore(name, { keyPath: 'id' })
    }
    for (const row of rows) db.stores.get(storeName)!.rows.set(String(row.id), { ...row })
  }

  /** The rows of a store, for a test to look at what is really saved. */
  rows(dbName: string, storeName: string): Record<string, unknown>[] {
    return [...(this.databases.get(dbName)?.stores.get(storeName)?.rows.values() ?? [])]
  }
}
