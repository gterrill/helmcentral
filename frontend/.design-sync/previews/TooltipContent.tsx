import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from 'helmcentral-dashboard'
import { Anchor } from 'lucide-react'

export function Default() {
  return (
    <TooltipProvider>
      <Tooltip defaultOpen defaultTriggerId="tooltip-content-default">
        <TooltipTrigger
          id="tooltip-content-default"
          className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background text-foreground shadow-sm"
        >
          <Anchor className="h-4 w-4" />
        </TooltipTrigger>
        <TooltipContent side="top">Anchor holding at 12m radius</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

export function Sides() {
  return (
    <TooltipProvider>
      <div className="flex items-center gap-10 p-8">
        <Tooltip defaultOpen defaultTriggerId="tooltip-content-right">
          <TooltipTrigger
            id="tooltip-content-right"
            className="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm shadow-sm"
          >
            Tide
          </TooltipTrigger>
          <TooltipContent side="right">Next high tide 14:32</TooltipContent>
        </Tooltip>
        <Tooltip defaultOpen defaultTriggerId="tooltip-content-bottom">
          <TooltipTrigger
            id="tooltip-content-bottom"
            className="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm shadow-sm"
          >
            Wind
          </TooltipTrigger>
          <TooltipContent side="bottom">12kts SW, gusting 18kts</TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  )
}
