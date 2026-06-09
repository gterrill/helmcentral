interface SettingsInputProps {
  label: string
  value: string
  onChange: (value: string) => void
  ariaLabel?: string
  className?: string
  type?: 'text' | 'number'
}

export function SettingsInput({
  label,
  value,
  onChange,
  ariaLabel,
  className = '',
  type = 'text',
}: SettingsInputProps) {
  return (
    <label
      className={`flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em] ${className}`}
    >
      <span className="text-muted-foreground">{label}</span>
      <input
        type={type}
        className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-label={ariaLabel ?? label}
      />
    </label>
  )
}
