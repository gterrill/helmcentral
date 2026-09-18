import * as React from "react"
import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip"

import { cn } from "@/lib/utils"

const TooltipProvider = TooltipPrimitive.Provider

const Tooltip = TooltipPrimitive.Root

const TooltipTrigger = TooltipPrimitive.Trigger

const TooltipContent = React.forwardRef<
  React.ComponentRef<typeof TooltipPrimitive.Popup>,
  Pick<
    React.ComponentProps<typeof TooltipPrimitive.Positioner>,
    "side" | "sideOffset" | "align" | "alignOffset"
  > &
    React.ComponentProps<typeof TooltipPrimitive.Popup>
>(({ className, side, sideOffset = 4, align, alignOffset, ...props }, ref) => (
  <TooltipPrimitive.Portal>
    {/* z-90, the topmost layer: a tooltip can be anchored to a trigger inside
        a dialog (z-80), sheet (z-70), or popover/dropdown (z-60) and must
        still draw on top of them. This sits above the entire z-order hierarchy. */}
    <TooltipPrimitive.Positioner
      className="isolate z-90"
      side={side}
      sideOffset={sideOffset}
      align={align}
      alignOffset={alignOffset}
    >
      <TooltipPrimitive.Popup
        ref={ref}
        className={cn(
          "z-90 overflow-hidden rounded-md border bg-popover px-3 py-1.5 text-sm text-popover-foreground shadow-md transition-[opacity,transform] data-starting-style:opacity-0 data-starting-style:scale-95 data-ending-style:opacity-0 data-ending-style:scale-95 origin-(--transform-origin)",
          className
        )}
        {...props}
      />
    </TooltipPrimitive.Positioner>
  </TooltipPrimitive.Portal>
))
TooltipContent.displayName = "TooltipContent"

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider }
