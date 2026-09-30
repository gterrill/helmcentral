import { Search } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'

// ADR 0142: the search box plus filter selects above an IndexTable
// (equipment-index.tsx's own toolbar, generalised). Base UI's Select cannot
// carry a literal empty-string item value, which is exactly the value this
// project already uses everywhere else for "no filter applied"
// (EquipmentFilter's own category/system/status/zone) - equipment-index.tsx
// worked around that with its own local ALL_VALUE sentinel per select; this
// does the same thing once, centrally, so every filter's `value` stays a
// plain '' for "All" from the caller's point of view.

const ALL_SENTINEL = '__index-filters-all__'

export interface IndexFilterOption {
  value: string
  label: string
}

export interface IndexFilter {
  id: string
  /** aria-label on the select trigger, e.g. "Filter by category". */
  label: string
  /** '' means "All" - the same convention this app's own filter state
   * (EquipmentFilter) already uses. */
  value: string
  allLabel?: string
  options: IndexFilterOption[]
  onChange: (value: string) => void
}

export interface IndexFiltersProps {
  searchValue: string
  onSearchChange: (value: string) => void
  searchLabel?: string
  searchPlaceholder?: string
  filters?: IndexFilter[]
  onClearAll?: () => void
  clearAllLabel?: string
  className?: string
}

export function IndexFilters({
  searchValue,
  onSearchChange,
  searchLabel = 'Search',
  searchPlaceholder,
  filters = [],
  onClearAll,
  clearAllLabel = 'Clear all',
  className,
}: IndexFiltersProps) {
  const activeCount = filters.filter((filter) => filter.value !== '').length

  return (
    <div className={cn('flex flex-wrap items-end gap-2', className)}>
      <div className="flex min-w-40 flex-col gap-1">
        <label
          htmlFor={`index-filters-search-${searchLabel}`}
          className="text-[10px] font-medium uppercase tracking-[0.16em] text-muted-foreground"
        >
          {searchLabel}
        </label>
        <InputGroup className="h-9">
          <InputGroupAddon>
            <Search className="h-4 w-4" aria-hidden="true" />
          </InputGroupAddon>
          <InputGroupInput
            id={`index-filters-search-${searchLabel}`}
            aria-label={searchLabel}
            placeholder={searchPlaceholder}
            value={searchValue}
            onChange={(e) => onSearchChange(e.target.value)}
          />
        </InputGroup>
      </div>

      {filters.map((filter) => (
        <Select
          key={filter.id}
          value={filter.value === '' ? ALL_SENTINEL : filter.value}
          onValueChange={(value) => filter.onChange(value === ALL_SENTINEL ? '' : (value ?? ''))}
        >
          <SelectTrigger aria-label={filter.label} className="h-9 w-auto min-w-32">
            <SelectValue>
              {(value: string) =>
                value === ALL_SENTINEL
                  ? (filter.allLabel ?? 'All')
                  : (filter.options.find((option) => option.value === value)?.label ?? value)
              }
            </SelectValue>
          </SelectTrigger>
          <SelectPopup>
            <SelectItem value={ALL_SENTINEL}>{filter.allLabel ?? 'All'}</SelectItem>
            {filter.options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
      ))}

      {activeCount > 0 && (
        <div className="flex items-center gap-2">
          <Badge variant="secondary">{activeCount}</Badge>
          <Button type="button" variant="ghost" size="sm" onClick={onClearAll}>
            {clearAllLabel}
          </Button>
        </div>
      )}
    </div>
  )
}
