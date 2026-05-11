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
    <section className={cn('rounded-lg border-x border-b bg-card p-4 pt-5', className)}>
      <div className="mb-3 flex items-center gap-2">
        <div
          className={cn(
            'inline-flex items-center gap-1 whitespace-nowrap font-display text-xs uppercase tracking-[0.22em] text-muted-foreground',
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
