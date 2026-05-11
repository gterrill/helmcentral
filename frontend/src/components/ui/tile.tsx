import * as React from 'react'

import { cn } from '@/lib/utils'

interface TileProps {
  title: string
  icon?: React.ReactNode
  className?: string
  titleClassName?: string
  children: React.ReactNode
}

export function Tile({ title, icon, className, titleClassName, children }: TileProps) {
  return (
    <section className={cn('relative rounded-lg border bg-card p-4 pt-5', className)}>
      <div className="pointer-events-none absolute inset-x-3 top-0 flex -translate-y-1/2 items-center gap-2">
        <div
          className={cn(
            'inline-flex items-center gap-1 bg-card px-2 font-display text-xs uppercase tracking-[0.22em] text-muted-foreground',
            titleClassName,
          )}
        >
          {icon ? <span className="inline-flex h-3.5 w-3.5 items-center justify-center">{icon}</span> : null}
          <span>{title}</span>
        </div>
        <div className="h-px flex-1 bg-border/70" />
      </div>

      {children}
    </section>
  )
}
