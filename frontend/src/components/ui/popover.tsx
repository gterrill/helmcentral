import * as React from "react"
import { Popover as PopoverPrimitive } from "@base-ui/react/popover"

import { cn } from "@/lib/utils"

const Popover = PopoverPrimitive.Root

const PopoverTrigger = PopoverPrimitive.Trigger

const PopoverContent = React.forwardRef<
  React.ComponentRef<typeof PopoverPrimitive.Popup>,
  React.ComponentProps<typeof PopoverPrimitive.Popup> & {
    sideOffset?: number
    align?: React.ComponentProps<typeof PopoverPrimitive.Positioner>["align"]
    side?: React.ComponentProps<typeof PopoverPrimitive.Positioner>["side"]
    // A virtual element (or ref/getter for one) to position against instead
    // of the trigger - ask-mate-selection.tsx has no trigger element at all,
    // only a text selection's Range, so it anchors here against that
    // Range's own getBoundingClientRect(). Optional and forwarded straight
    // through; every existing caller anchors to its PopoverTrigger as before.
    anchor?: React.ComponentProps<typeof PopoverPrimitive.Positioner>["anchor"]
  }
>(({ className, children, sideOffset = 4, align = "center", side, anchor, ...props }, ref) => (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Positioner
      className="isolate z-60"
      sideOffset={sideOffset}
      align={align}
      side={side}
      anchor={anchor}
    >
      <PopoverPrimitive.Popup
        ref={ref}
        className={cn(
          "relative z-60 rounded-md border bg-popover text-popover-foreground shadow-md transition-[opacity,transform] data-starting-style:opacity-0 data-starting-style:scale-95 data-ending-style:opacity-0 data-ending-style:scale-95 origin-(--transform-origin)",
          className
        )}
        {...props}
      >
        {children}
      </PopoverPrimitive.Popup>
    </PopoverPrimitive.Positioner>
  </PopoverPrimitive.Portal>
))
PopoverContent.displayName = "PopoverContent"

export {
  Popover,
  PopoverTrigger,
  PopoverContent,
}
