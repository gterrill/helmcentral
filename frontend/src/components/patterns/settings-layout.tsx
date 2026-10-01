import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'
import { FormSection } from './form-section'
import { Page } from './page'

// ADR 0142: the Settings template, modelled on Polaris' settings template
// (shopify.dev/docs/api/app-home/latest/patterns/templates/settings). A page
// heading over ONE narrow, centred column (Polaris' `inlineSize="small"`) of
// headed sections, each a FormSection holding its fields and their own help
// text. There is no aside and no second column: a settings page is read top
// to bottom and edited in place.
//
// The page is one form, and the caller owns the draft. Unsaved changes drive
// the contextual SaveBar (save / discard) in the app header, exactly as a
// details page does - this layout draws no save control of its own.
//
// `tools` is Polaris' trailing group for secondary actions that are not part
// of the form's draft: reset, export, clear. It renders as a final section
// headed "Tools". Destructive entries inside it take the critical tone
// (an outline Button with `border-destructive/40 text-destructive`); nothing in it counts toward "dirty".

export interface SettingsLayoutProps {
  title: ReactNode
  description?: ReactNode
  /** FormSection blocks, in reading order. */
  children: ReactNode
  /** Secondary and destructive actions, rendered as a final "Tools" section. */
  tools?: ReactNode
  className?: string
}

export function SettingsLayout({ title, description, children, tools, className }: SettingsLayoutProps) {
  return (
    <Page title={title} className={cn('mx-auto w-full max-w-3xl min-w-0', className)}>
      {description && <p className="-mt-2 text-sm text-muted-foreground">{description}</p>}
      {children}
      {tools && <FormSection title="Tools"><div className="flex flex-wrap items-center gap-2">{tools}</div></FormSection>}
    </Page>
  )
}
