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
  }
>(({ className, children, sideOffset = 4, align = "center", ...props }, ref) => (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Positioner
      className="isolate z-50"
      sideOffset={sideOffset}
      align={align}
    >
      <PopoverPrimitive.Popup
        ref={ref}
        className={cn(
          "relative z-50 rounded-md border bg-popover text-popover-foreground shadow-md transition-[opacity,transform] data-[starting-style]:opacity-0 data-[starting-style]:scale-95 data-[ending-style]:opacity-0 data-[ending-style]:scale-95 origin-[--transform-origin]",
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
