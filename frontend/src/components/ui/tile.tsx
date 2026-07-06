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
    <Card className={cn('gap-0 py-4', className)}>
      <CardHeader className="flex-row items-center gap-2 space-y-0 px-4 pb-3">
        <CardTitle
          className={cn(
            'inline-flex shrink-0 items-center gap-1 whitespace-nowrap font-display text-xs font-normal uppercase tracking-[0.22em] text-muted-foreground',
            titleClassName,
          )}
        >
          {icon ? <span className="inline-flex h-3.5 w-3.5 items-center justify-center">{icon}</span> : null}
          <span>{title}</span>
        </CardTitle>
        <div className="h-px flex-1 bg-border/70" />
        {titleExtra && <CardAction className="static shrink-0">{titleExtra}</CardAction>}
      </CardHeader>

      <CardContent className="px-4">{children}</CardContent>
    </Card>
  )
}
