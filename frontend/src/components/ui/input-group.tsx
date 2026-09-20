import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"

function InputGroup({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="input-group"
      role="group"
      className={cn(
        // No shadow: this project's plain Input carries none, and addon fields
        // sit directly beside plain ones in the settings grids.
        "group/input-group border-input bg-background ring-offset-background relative flex w-full items-center rounded-md border outline-hidden transition-[color,box-shadow]",
        // The Mate composer (assistant-thread.tsx) puts a textarea in here
        // instead of a plain input - a fixed h-10 would clip it the moment
        // it grows past one line, so a textarea child frees the group to
        // size to its content instead.
        "h-10 has-[>textarea]:h-auto",
        "has-[>[data-align=inline-start]]:[&>input]:pl-2",
        "has-[>[data-align=inline-end]]:[&>input]:pr-2",
        "has-[>[data-align=block-start]]:h-auto has-[>[data-align=block-start]]:flex-col has-[>[data-align=block-start]]:[&>input]:pb-3",
        "has-[>[data-align=block-end]]:h-auto has-[>[data-align=block-end]]:flex-col has-[>[data-align=block-end]]:[&>input]:pt-3",
        "has-[[data-slot=input-group-control]:focus-visible]:ring-2 has-[[data-slot=input-group-control]:focus-visible]:ring-ring has-[[data-slot=input-group-control]:focus-visible]:ring-offset-2",
        "has-[[data-slot][aria-invalid=true]]:ring-destructive/20 has-[[data-slot][aria-invalid=true]]:border-destructive dark:has-[[data-slot][aria-invalid=true]]:ring-destructive/40",
        className
      )}
      {...props}
    />
  )
}

const inputGroupAddonVariants = cva(
  "text-muted-foreground flex h-auto cursor-text select-none items-center justify-center gap-2 py-1.5 text-sm font-medium group-data-[disabled=true]/input-group:opacity-50 [&>svg:not([class*='size-'])]:size-4",
  {
    variants: {
      align: {
        "inline-start": "order-first pl-3",
        "inline-end": "order-last pr-3",
        "block-start": "[.border-b]:pb-3 order-first w-full justify-start px-3 pt-3 group-has-[>input]/input-group:pt-2.5",
        "block-end": "[.border-t]:pt-3 order-last w-full justify-start px-3 pb-3 group-has-[>input]/input-group:pb-2.5",
      },
    },
    defaultVariants: { align: "inline-start" },
  }
)

function InputGroupAddon({
  className,
  align = "inline-start",
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof inputGroupAddonVariants>) {
  return (
    <div
      role="group"
      data-slot="input-group-addon"
      data-align={align}
      className={cn(inputGroupAddonVariants({ align }), className)}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("button")) return
        // The Mate composer's block-start addon (the staged-attachment
        // chips) sits above an InputGroupTextarea, not an InputGroupInput -
        // clicking the addon's empty space has to reach either control.
        e.currentTarget.parentElement?.querySelector<HTMLElement>("input, textarea")?.focus()
      }}
      {...props}
    />
  )
}

function InputGroupText({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      className={cn(
        "text-muted-foreground flex items-center gap-2 text-base md:text-sm [&_svg:not([class*='size-'])]:size-4 [&_svg]:pointer-events-none",
        className
      )}
      {...props}
    />
  )
}

function InputGroupInput({ className, ...props }: React.ComponentProps<"input">) {
  return (
    <Input
      data-slot="input-group-control"
      className={cn(
        "flex-1 rounded-none border-0 bg-transparent shadow-none focus-visible:ring-0 focus-visible:ring-offset-0 dark:bg-transparent",
        className
      )}
      {...props}
    />
  )
}

// The Mate composer (assistant-thread.tsx, shadcn's 2026-06 chat components
// composer shape) is the first multi-line field to sit inside an
// InputGroup, so this project's trimmed copy didn't need it until now.
// Same treatment as InputGroupInput above, minus the parts that only make
// sense for a single line: no fixed height to strip (a textarea sets its
// own via `rows`), and the ring-offset kill still applies since this
// project's focus ring uses ring-offset-2 everywhere (button.tsx,
// textarea.tsx) rather than upstream's offset-less ring.
function InputGroupTextarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <Textarea
      data-slot="input-group-control"
      className={cn(
        "flex-1 resize-none rounded-none border-0 bg-transparent shadow-none focus-visible:ring-0 focus-visible:ring-offset-0 dark:bg-transparent",
        className
      )}
      {...props}
    />
  )
}

// Upstream's InputGroupButton carries its own size variants (xs/sm/icon-xs/
// icon-sm) that shrink a button down to h-6/h-8/size-6/size-8 - a compact
// look that assumes nothing in the group needs to be a real touch target.
// DESIGN.md's 40px control floor says otherwise (`min-h-10 min-w-10` on
// every button, for touch use on a moving boat) and this project's Button
// already bakes that floor into its base class regardless of size - so
// reproducing upstream's shrinking sizes here would just fight the floor
// and leave dead classes behind. `size` passes straight through to Button
// instead; its own "icon-sm" is already the right 40x40 square for an addon
// button (see button.tsx).
function InputGroupButton({
  className,
  type = "button",
  variant = "ghost",
  ...props
}: React.ComponentProps<typeof Button>) {
  return (
    <Button
      type={type}
      variant={variant}
      className={cn("shadow-none", className)}
      {...props}
    />
  )
}

export {
  InputGroup,
  InputGroupAddon,
  InputGroupText,
  InputGroupInput,
  InputGroupTextarea,
  InputGroupButton,
}
