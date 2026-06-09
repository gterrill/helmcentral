interface SettingsSelectProps<T extends string> {
  label: string
  value: T
  onChange: (value: T) => void
  options: { value: T; label: string }[]
  ariaLabel?: string
  className?: string
  isMultiCol?: boolean
}

export function SettingsSelect<T extends string>({
  label,
  value,
  onChange,
  options,
  ariaLabel,
  className = '',
  isMultiCol = false,
}: SettingsSelectProps<T>) {
  return (
    <label
      className={`flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em] ${isMultiCol ? 'md:col-span-2' : ''} ${className}`}
    >
      <span className="text-muted-foreground">{label}</span>
      <select
        className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
        value={value}
        onChange={(event) => onChange(event.target.value as T)}
        aria-label={ariaLabel ?? label}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  )
}
