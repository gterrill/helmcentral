import { Search } from 'lucide-react'
import { useState } from 'react'

import { useTideStations, type TideStation } from '@/hooks/use-tide-stations'

interface TideStationPickerProps {
  provider: string
  onSelect: (station: TideStation) => void
}

export function TideStationPicker({ provider, onSelect }: TideStationPickerProps) {
  const [query, setQuery] = useState('')
  const { stations, loading } = useTideStations(provider, query)
  const trimmedQuery = query.trim()

  return (
    <div className="space-y-2">
      <label className="flex items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
        <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
        <input
          type="text"
          className="min-w-0 w-full bg-transparent text-sm font-semibold normal-case text-foreground outline-none"
          placeholder="Search tide stations (e.g. Sydney)"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          aria-label="Search tide stations"
        />
      </label>

      {trimmedQuery.length > 0 && trimmedQuery.length < 2 && (
        <p className="px-1 text-xs text-muted-foreground">Keep typing to search...</p>
      )}

      {loading && (
        <p className="px-1 text-xs text-muted-foreground">Searching...</p>
      )}

      {!loading && trimmedQuery.length >= 2 && stations.length === 0 && (
        <p className="px-1 text-xs text-muted-foreground">No stations found</p>
      )}

      {stations.length > 0 && (
        <div className="max-h-64 overflow-y-auto rounded-md border bg-background/60">
          {stations.map((station) => (
            <button
              key={station.stationId}
              type="button"
              className="flex w-full flex-col items-start gap-0.5 border-b border-border/50 px-3 py-2 text-left last:border-b-0 hover:bg-muted/40"
              onClick={() => {
                setQuery('')
                onSelect(station)
              }}
            >
              <span className="text-sm font-semibold text-foreground">{station.name}</span>
              {station.state && (
                <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">{station.state}</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
