"use client"

import * as React from "react"
import { Dialog as SheetPrimitive } from "@base-ui/react/dialog"
import { cva, type VariantProps } from "class-variance-authority"
import { X } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

const Sheet = SheetPrimitive.Root

const SheetTrigger = SheetPrimitive.Trigger

const SheetClose = SheetPrimitive.Close

const SheetPortal = SheetPrimitive.Portal

const SheetOverlay = React.forwardRef<
  React.ComponentRef<typeof SheetPrimitive.Backdrop>,
  React.ComponentProps<typeof SheetPrimitive.Backdrop>
>(({ className, ...props }, ref) => (
  <SheetPrimitive.Backdrop
    className={cn(
      // A light, theme-aware scrim rather than the primitive's default 80%
      // black - that flat black dimmed a live, unacknowledged alarm on the
      // page behind the sheet (impeccable critique 2026-09-12, P1). The
      // banner itself is lifted above this overlay separately (see App.tsx).
      "fixed inset-0 z-50 bg-background/60 transition-opacity data-starting-style:opacity-0 data-ending-style:opacity-0",
      className
    )}
    {...props}
    ref={ref}
  />
))
SheetOverlay.displayName = "SheetOverlay"

const sheetVariants = cva(
  "fixed z-70 gap-4 bg-background p-6 shadow-lg transition-transform ease-in-out data-ending-style:duration-300 data-starting-style:duration-500",
  {
    variants: {
      side: {
        top: "inset-x-0 top-0 border-b data-ending-style:-translate-y-full data-starting-style:-translate-y-full",
        bottom:
          "inset-x-0 bottom-0 border-t data-ending-style:translate-y-full data-starting-style:translate-y-full",
        left: "inset-y-0 left-0 h-full w-3/4 border-r data-ending-style:-translate-x-full data-starting-style:-translate-x-full sm:max-w-sm",
        right:
          "inset-y-0 right-0 h-full w-3/4  border-l data-ending-style:translate-x-full data-starting-style:translate-x-full sm:max-w-sm",
      },
    },
    defaultVariants: {
      side: "right",
    },
  }
)

interface SheetContentProps
  extends React.ComponentProps<typeof SheetPrimitive.Popup>,
    VariantProps<typeof sheetVariants> {}

const SheetContent = React.forwardRef<
  React.ComponentRef<typeof SheetPrimitive.Popup>,
  SheetContentProps
>(({ side = "right", className, children, ...props }, ref) => (
  <SheetPortal>
    <SheetOverlay />
    <SheetPrimitive.Popup
      ref={ref}
      className={cn(sheetVariants({ side }), className)}
      {...props}
    >
      {children}
      {/* AGENTS.md's 40 px control floor (a moving boat, wet hands) applies here
          same as any other control - rendered through the shared Button so this
          gets the same size-icon dimensions and focus-visible ring as every other
          icon button, rather than a hand-styled 16 px target of its own. */}
      <SheetPrimitive.Close
        render={
          <Button
            variant="ghost"
            size="icon"
            className="absolute right-2 top-2 opacity-70 ring-offset-background transition-opacity hover:opacity-100 disabled:pointer-events-none"
          >
            <X className="h-4 w-4" />
            <span className="sr-only">Close</span>
          </Button>
        }
      />
    </SheetPrimitive.Popup>
  </SheetPortal>
))
SheetContent.displayName = "SheetContent"

const SheetHeader = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      // Both current consumers (mate-sheet.tsx's flex-row header, sidebar.tsx's
      // sr-only mobile header) override or hide this text-align entirely, so
      // it stayed dead in both; drop it here rather than let a future
      // consumer inherit centred/left text-align by accident.
      "flex flex-col space-y-2",
      className
    )}
    {...props}
  />
)
SheetHeader.displayName = "SheetHeader"

const SheetFooter = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2",
      className
    )}
    {...props}
  />
)
SheetFooter.displayName = "SheetFooter"

const SheetTitle = React.forwardRef<
  React.ComponentRef<typeof SheetPrimitive.Title>,
  React.ComponentProps<typeof SheetPrimitive.Title>
>(({ className, ...props }, ref) => (
  <SheetPrimitive.Title
    ref={ref}
    className={cn("text-lg font-semibold text-foreground", className)}
    {...props}
  />
))
SheetTitle.displayName = "SheetTitle"

const SheetDescription = React.forwardRef<
  React.ComponentRef<typeof SheetPrimitive.Description>,
  React.ComponentProps<typeof SheetPrimitive.Description>
>(({ className, ...props }, ref) => (
  <SheetPrimitive.Description
    ref={ref}
    className={cn("text-sm text-muted-foreground", className)}
    {...props}
  />
))
SheetDescription.displayName = "SheetDescription"

export {
  Sheet,
  SheetPortal,
  SheetOverlay,
  SheetTrigger,
  SheetClose,
  SheetContent,
  SheetHeader,
  SheetFooter,
  SheetTitle,
  SheetDescription,
}
