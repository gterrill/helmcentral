import { BREAKPOINTS, useMinWidth } from "@/lib/breakpoints"

export function useIsMobile() {
  return !useMinWidth(BREAKPOINTS.md)
}
