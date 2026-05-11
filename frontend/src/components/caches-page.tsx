import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'

interface CacheInfo {
  name: string
  file_path: string
  ttl_seconds: number
  exists: boolean
  size_bytes: number
  modified_at: string | null
  in_memory_entries: number
  cache_hits: number
  cache_misses: number
}

function formatBytes(bytes: number) {
  if (bytes <= 0) {
    return '0 B'
  }

  if (bytes < 1024) {
    return `${bytes} B`
  }

  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`
  }

  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatTTL(ttlSeconds: number) {
  if (ttlSeconds <= 0) {
    return '—'
  }

  const hours = Math.floor(ttlSeconds / 3600)
  const minutes = Math.floor((ttlSeconds % 3600) / 60)
  const seconds = ttlSeconds % 60

  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`
  }

  if (minutes > 0) {
    return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`
  }

  return `${seconds}s`
}

export function AdminPage() {
  const [caches, setCaches] = useState<CacheInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [invalidating, setInvalidating] = useState<string | null>(null)

  const loadCaches = async () => {
    try {
      setLoading(true)
      setError(null)
      const response = await fetch('/api/caches')
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`)
      }

      const data = await response.json()
      if (!Array.isArray(data)) {
        throw new Error('Invalid response format')
      }

      setCaches(data)
    } catch (fetchError) {
      setError(fetchError instanceof Error ? fetchError.message : 'Failed to load caches')
    } finally {
      setLoading(false)
    }
  }

  const invalidateCache = async (name: string) => {
    try {
      setInvalidating(name)
      setError(null)
      const response = await fetch(`/api/caches/${name}/invalidate`, { method: 'POST' })
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`)
      }

      await loadCaches()
    } catch (invalidateError) {
      setError(invalidateError instanceof Error ? invalidateError.message : 'Failed to invalidate cache')
    } finally {
      setInvalidating(null)
    }
  }

  useEffect(() => {
    loadCaches()
  }, [])

  return (
    <div className="min-h-screen bg-background px-4 py-8">
      <div className="mx-auto max-w-6xl rounded-xl border bg-card/80 p-6 shadow-sm">
        <div className="mb-6 flex items-center justify-between gap-4">
          <div>
            <h1 className="font-display text-4xl text-foreground">Admin</h1>
            <p className="mt-1 text-sm text-muted-foreground">Operational tools for backend state and maintenance.</p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={loadCaches} disabled={loading}>
              Refresh
            </Button>
            <Button variant="outline" onClick={() => (window.location.href = '/')}>
              Dashboard
            </Button>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <Tile title="Cache Control" className="bg-background/60">
            <p className="mb-3 text-sm text-muted-foreground">JSON-backed cache files used by the backend.</p>

            {error && (
              <div className="mb-4 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-800">
                {error}
              </div>
            )}

            <div className="overflow-hidden rounded-lg border">
              <table className="w-full text-sm">
                <thead>
                  <tr className="bg-muted/30 text-left uppercase tracking-[0.12em] text-muted-foreground">
                    <th className="px-3 py-2">Cache</th>
                    <th className="px-3 py-2">File</th>
                    <th className="px-3 py-2">TTL</th>
                    <th className="px-3 py-2">State</th>
                    <th className="px-3 py-2">Entries</th>
                    <th className="px-3 py-2">Hit / Miss</th>
                    <th className="px-3 py-2">Updated</th>
                    <th className="px-3 py-2 text-right">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {loading ? (
                    <tr>
                      <td colSpan={8} className="px-3 py-6 text-center text-muted-foreground">
                        Loading caches...
                      </td>
                    </tr>
                  ) : caches.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="px-3 py-6 text-center text-muted-foreground">
                        No caches found.
                      </td>
                    </tr>
                  ) : (
                    caches.map(cache => (
                      <tr key={cache.name} className="border-t border-border/70">
                        <td className="px-3 py-3 font-semibold text-foreground">{cache.name}</td>
                        <td className="px-3 py-3 text-xs text-muted-foreground">{cache.file_path}</td>
                        <td className="px-3 py-3 text-muted-foreground">{formatTTL(cache.ttl_seconds)}</td>
                        <td className="px-3 py-3 text-muted-foreground">
                          {cache.exists ? `Present (${formatBytes(cache.size_bytes)})` : 'Missing'}
                        </td>
                        <td className="px-3 py-3 text-muted-foreground">{cache.in_memory_entries}</td>
                        <td className="px-3 py-3 text-muted-foreground">
                          {cache.cache_hits} / {cache.cache_misses}
                        </td>
                        <td className="px-3 py-3 text-muted-foreground">
                          {cache.modified_at ? new Date(cache.modified_at).toLocaleString() : '—'}
                        </td>
                        <td className="px-3 py-3 text-right">
                          <Button
                            size="sm"
                            variant="outline"
                            className="h-9"
                            disabled={invalidating === cache.name}
                            onClick={() => invalidateCache(cache.name)}
                          >
                            {invalidating === cache.name ? 'Invalidating...' : 'Invalidate'}
                          </Button>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </Tile>

          <Tile title="System" className="bg-background/60">
            <p className="text-sm text-muted-foreground">Additional admin tools can be added here.</p>
          </Tile>
        </div>
      </div>
    </div>
  )
}
