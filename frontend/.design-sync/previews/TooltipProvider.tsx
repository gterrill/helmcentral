import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from 'helmcentral-dashboard'
import { Anchor, Battery } from 'lucide-react'

export function Default() {
  return (
    <TooltipProvider>
      <div className="flex items-center gap-3">
        <Tooltip defaultOpen defaultTriggerId="tooltip-provider-anchor">
          <TooltipTrigger
            id="tooltip-provider-anchor"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background shadow-sm"
          >
            <Anchor className="h-4 w-4" />
          </TooltipTrigger>
          <TooltipContent side="top">Anchor watch active</TooltipContent>
        </Tooltip>
        <Tooltip defaultTriggerId="tooltip-provider-battery">
          <TooltipTrigger
            id="tooltip-provider-battery"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background shadow-sm"
          >
            <Battery className="h-4 w-4" />
          </TooltipTrigger>
          <TooltipContent side="top">House bank 86%</TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  )
}

export function CustomDelay() {
  return (
    <TooltipProvider delay={0} closeDelay={0}>
      <div className="flex items-center gap-3">
        <Tooltip defaultOpen defaultTriggerId="tooltip-provider-fast-1">
          <TooltipTrigger
            id="tooltip-provider-fast-1"
            className="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm shadow-sm"
          >
            Generator
          </TooltipTrigger>
          <TooltipContent side="top">Running, 4.2kW load</TooltipContent>
        </Tooltip>
        <Tooltip defaultOpen defaultTriggerId="tooltip-provider-fast-2">
          <TooltipTrigger
            id="tooltip-provider-fast-2"
            className="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm shadow-sm"
          >
            Tanks
          </TooltipTrigger>
          <TooltipContent side="top">Fresh water 62% · Waste 18%</TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  )
}
