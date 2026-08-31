"use client"

import * as React from "react"
import { Tabs as TabsPrimitive } from "@base-ui/react/tabs"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const Tabs = TabsPrimitive.Root

/**
 * Follows shadcn's Base UI tabs (ui.shadcn.com/docs/components/base/tabs),
 * translated to this project's Tailwind v3 syntax — `data-[active]:` rather
 * than v4's `data-active:` — and with the vertical-orientation plumbing left
 * out, since nothing here renders tabs vertically. `data-active` is the
 * attribute Base UI actually sets on the selected tab; `data-selected` is not.
 *
 * The `line` variant is the one that survives the instrument skin. shadcn's
 * default pill assumes a light theme where --muted is darker than
 * --background, so the selected tab lifts off its track. The instrument skin
 * inverts that: --muted is 18% lightness, --background 8%, and
 * --sidebar-background is var(--card), the same 8%. A `bg-background` pill on
 * a sidebar is therefore invisible — it matches the panel behind the strip
 * exactly, and the *unselected* tabs are what stands out. An underline
 * indicator doesn't depend on that relationship holding.
 */
const tabsListVariants = cva(
  "group/tabs-list inline-flex items-center justify-center text-muted-foreground",
  {
    variants: {
      variant: {
        default: "h-10 rounded-md bg-muted p-1",
        line: "h-9 w-full gap-1 border-b bg-transparent",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

const TabsList = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.List>,
  React.ComponentProps<typeof TabsPrimitive.List> & VariantProps<typeof tabsListVariants>
>(({ className, variant = "default", ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    data-variant={variant}
    className={cn(tabsListVariants({ variant }), className)}
    {...props}
  />
))
TabsList.displayName = "TabsList"

const TabsTrigger = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.Tab>,
  React.ComponentProps<typeof TabsPrimitive.Tab>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Tab
    ref={ref}
    className={cn(
      // The border is always present, transparent when inactive, so gaining
      // the active outline doesn't shift the row by a pixel.
      "relative inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-sm border border-transparent px-3 py-1.5 text-sm font-medium text-foreground/60 transition-all",
      "hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
      "disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50",
      "data-[active]:border-input data-[active]:bg-background data-[active]:text-foreground data-[active]:shadow-sm",
      // line: no pill at all, just full-contrast text over an accent underline
      // that sits on the list's own bottom rule.
      "group-data-[variant=line]/tabs-list:h-full group-data-[variant=line]/tabs-list:flex-1 group-data-[variant=line]/tabs-list:rounded-none",
      "group-data-[variant=line]/tabs-list:data-[active]:border-transparent group-data-[variant=line]/tabs-list:data-[active]:bg-transparent group-data-[variant=line]/tabs-list:data-[active]:shadow-none",
      "after:absolute after:inset-x-0 after:-bottom-px after:h-0.5 after:bg-primary after:opacity-0 after:transition-opacity",
      "group-data-[variant=line]/tabs-list:data-[active]:after:opacity-100",
      className
    )}
    {...props}
  />
))
TabsTrigger.displayName = "TabsTrigger"

const TabsContent = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.Panel>,
  React.ComponentProps<typeof TabsPrimitive.Panel>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Panel
    ref={ref}
    className={cn(
      "mt-2 ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
      className
    )}
    {...props}
  />
))
TabsContent.displayName = "TabsContent"

export { Tabs, TabsList, TabsTrigger, TabsContent, tabsListVariants }
