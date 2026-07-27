import * as React from 'react'

import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

interface TileProps {
  title: string
  icon?: React.ReactNode
  className?: string
  titleClassName?: string
  titleExtra?: React.ReactNode
  children: React.ReactNode
}

export function Tile({ title, icon, className, titleClassName, titleExtra, children }: TileProps) {
  return (
    <Card className={cn('h-full gap-0 py-4', className)}>
      {/* Padding and letter-spacing tighten before anything else at phone width. The
          0.22em tracking costs more width than the horizontal padding does, so it is
          the first thing to give. */}
      <CardHeader className="flex-row items-center gap-2 space-y-0 px-3 pb-3 sm:px-4">
        <CardTitle
          className={cn(
            'inline-flex min-w-0 items-center gap-1 font-display text-xs font-normal uppercase tracking-[0.14em] text-muted-foreground sm:tracking-[0.22em]',
            titleClassName,
          )}
        >
          {icon ? <span className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center">{icon}</span> : null}
          {/* Truncates rather than stretching the card: an operator-supplied embed
              title is arbitrary length and used to have no way to yield. */}
          <span className="truncate">{title}</span>
        </CardTitle>
        <div className="h-px flex-1 bg-border/70" />
        {titleExtra && <CardAction className="static shrink-0">{titleExtra}</CardAction>}
      </CardHeader>

      <CardContent className="flex-1 px-3 sm:px-4">{children}</CardContent>
    </Card>
  )
}
