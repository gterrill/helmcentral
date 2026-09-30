import type { ReactNode } from 'react'

import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

// ADR 0142: one block of a details form - equipment-editor.tsx's
// "Specifications & IDs", "Documents", "Maintenance"... each becomes one of
// these. The heading styling matches FieldSet/FieldLegend's existing
// `variant="label"` look (uppercase, tracked, muted) so a page built from
// FormSection sits beside one still using FieldSet directly without reading
// as two different systems. Fields themselves are the caller's own
// `ui/field` markup (Field/FieldGroup/FieldLabel/...) passed as children -
// this only owns the card chrome and the heading row, not the form fields.

export interface FormSectionProps {
  title: ReactNode
  description?: ReactNode
  /** A small control in the header row - e.g. Documents' "Add document" button. */
  action?: ReactNode
  children: ReactNode
  className?: string
}

export function FormSection({ title, description, action, children, className }: FormSectionProps) {
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle
          as="h2"
          className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground"
        >
          {title}
        </CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
        {action && <CardAction>{action}</CardAction>}
      </CardHeader>
      <CardContent className={cn('flex flex-col gap-4')}>{children}</CardContent>
    </Card>
  )
}
