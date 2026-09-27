/**
 * Measures the z-<n> utility class each stacking surface actually renders,
 * instead of the stacking tests copying the ladder into their own literal
 * consts. A literal is a description, not a check: it stays green if a
 * surface's real z-index drifts, which is the same failure mode the ladder
 * exists to catch (ADR 0139's 2026-09-28 amendment).
 *
 * Sheet, Dialog and Tooltip are rendered in isolation here and unmounted
 * immediately after their z is read, via @testing-library/react's cleanup(),
 * so measuring the ladder inside one test doesn't leave stray portalled
 * nodes for the assertions that follow.
 *
 * The live alarm banner (App.tsx) is not a component of its own - it's an
 * inline div inside the top-level App tree - so mounting it here would mean
 * rendering all of App (its telemetry streams, map, dashboard grid) just to
 * read one className. Its z is read from App.tsx's source text instead,
 * keyed on the div's data-testid so a renamed or removed z-<n> class still
 * fails loudly rather than silently reporting a stale number.
 */
import { readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { render, screen, cleanup } from "@testing-library/react"

import { Sheet, SheetContent } from "@/components/ui/sheet"
import { Dialog, DialogContent } from "@/components/ui/dialog"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"

export function zIndexOf(className: string | null | undefined): number {
  const match = className?.match(/\bz-(\d+)\b/)
  if (!match) throw new Error(`no z-<n> utility class found in "${className}"`)
  return Number(match[1])
}

async function measureSheetZ(): Promise<number> {
  render(
    <Sheet open modal={false}>
      <SheetContent data-testid="z-ladder-sheet">Sheet body</SheetContent>
    </Sheet>,
  )
  const el = await screen.findByTestId("z-ladder-sheet")
  const z = zIndexOf(el.className)
  cleanup()
  return z
}

async function measureDialogZ(): Promise<number> {
  render(
    <Dialog open modal={false}>
      <DialogContent data-testid="z-ladder-dialog">Dialog body</DialogContent>
    </Dialog>,
  )
  const el = await screen.findByTestId("z-ladder-dialog")
  const z = zIndexOf(el.className)
  cleanup()
  return z
}

async function measureTooltipZ(): Promise<number> {
  render(
    <Tooltip open>
      <TooltipTrigger>Trigger</TooltipTrigger>
      <TooltipContent>z-ladder tooltip text</TooltipContent>
    </Tooltip>,
  )
  const el = await screen.findByText("z-ladder tooltip text")
  const z = zIndexOf(el.className)
  cleanup()
  return z
}

// Not `new URL(".", import.meta.url)`: happy-dom's URL polyfill (this
// suite's test environment) resolves a relative base against its own
// http://localhost:3000 stand-in location rather than the real file: URL,
// so that construction silently produces the wrong path. node:url's
// fileURLToPath on the raw import.meta.url does not go through that
// polyfill and resolves correctly.
const APP_TSX_PATH = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../App.tsx")
const ALARM_BANNER_MARKER = 'data-testid="alarm-banner-stack"'

function measureAlarmBannerZ(): number {
  const source = readFileSync(APP_TSX_PATH, "utf8")
  const markerIndex = source.indexOf(ALARM_BANNER_MARKER)
  if (markerIndex === -1) {
    throw new Error(
      `could not find ${ALARM_BANNER_MARKER} in App.tsx - the alarm banner div may have moved or been renamed`,
    )
  }
  const tagStart = source.lastIndexOf("<div", markerIndex)
  const tagEnd = source.indexOf(">", markerIndex)
  const tag = source.slice(tagStart, tagEnd)
  const match = tag.match(/\bz-(\d+)\b/)
  if (!match) {
    throw new Error("no z-<n> utility class found on the alarm banner div in App.tsx")
  }
  return Number(match[1])
}

export interface ZLadder {
  alarmZ: number
  sheetZ: number
  dialogZ: number
  tooltipZ: number
}

/** Renders each surface once, reads its real z-<n>, and returns the ladder. */
export async function measureZLadder(): Promise<ZLadder> {
  const alarmZ = measureAlarmBannerZ()
  const sheetZ = await measureSheetZ()
  const dialogZ = await measureDialogZ()
  const tooltipZ = await measureTooltipZ()
  return { alarmZ, sheetZ, dialogZ, tooltipZ }
}
