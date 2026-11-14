import * as React from "react"
import { Combobox as ComboboxPrimitive } from "@base-ui/react/combobox"
import { Check, ChevronDown } from "lucide-react"

import { inputClassName } from "@/components/ui/input"
import { cn } from "@/lib/utils"

const Combobox = ComboboxPrimitive.Root

// The input carries ui/input.tsx's own box, with room on the right for the
// chevron that opens the list.
const ComboboxInput = React.forwardRef<
  React.ComponentRef<typeof ComboboxPrimitive.Input>,
  React.ComponentProps<typeof ComboboxPrimitive.Input>
>(({ className, ...props }, ref) => (
  <div className="relative min-w-0 flex-1">
    <ComboboxPrimitive.Input
      ref={ref}
      className={cn(inputClassName, "pr-8", className)}
      {...props}
    />
    <ComboboxPrimitive.Trigger
      aria-label="Show matching items"
      className="absolute inset-y-0 right-0 flex w-8 items-center justify-center text-muted-foreground"
    >
      <ChevronDown className="h-4 w-4 opacity-50" aria-hidden="true" />
    </ComboboxPrimitive.Trigger>
  </div>
))
ComboboxInput.displayName = "ComboboxInput"

const ComboboxPopup = React.forwardRef<
  React.ComponentRef<typeof ComboboxPrimitive.Popup>,
  React.ComponentProps<typeof ComboboxPrimitive.Popup>
>(({ className, children, ...props }, ref) => (
  <ComboboxPrimitive.Portal>
    {/* z-85 for the same reason as select.tsx: the input usually lives in a
        dialog (z-80) and the list must draw above it, below tooltips. */}
    <ComboboxPrimitive.Positioner className="isolate z-85" sideOffset={4} align="start">
      <ComboboxPrimitive.Popup
        ref={ref}
        className={cn(
          "relative z-85 max-h-(--available-height) w-(--anchor-width) min-w-[8rem] overflow-y-auto overflow-x-hidden rounded-md border bg-popover text-popover-foreground shadow-md transition-[opacity,transform] data-starting-style:opacity-0 data-starting-style:scale-95 data-ending-style:opacity-0 data-ending-style:scale-95 origin-(--transform-origin)",
          className
        )}
        {...props}
      >
        <ComboboxPrimitive.List>{children}</ComboboxPrimitive.List>
      </ComboboxPrimitive.Popup>
    </ComboboxPrimitive.Positioner>
  </ComboboxPrimitive.Portal>
))
ComboboxPopup.displayName = "ComboboxPopup"

const ComboboxItem = React.forwardRef<
  React.ComponentRef<typeof ComboboxPrimitive.Item>,
  React.ComponentProps<typeof ComboboxPrimitive.Item>
>(({ className, children, ...props }, ref) => (
  <ComboboxPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex w-full cursor-default select-none items-center rounded-xs py-1.5 pl-8 pr-2 text-sm outline-hidden data-[highlighted]:bg-muted data-disabled:pointer-events-none data-disabled:opacity-50",
      className
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <ComboboxPrimitive.ItemIndicator>
        <Check className="h-4 w-4" />
      </ComboboxPrimitive.ItemIndicator>
    </span>
    {children}
  </ComboboxPrimitive.Item>
))
ComboboxItem.displayName = "ComboboxItem"

export { Combobox, ComboboxInput, ComboboxPopup, ComboboxItem }
