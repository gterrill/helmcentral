import { useEffect, useState } from 'react'

interface SolarControllerState {
  id: string
  label: string
  current_w: number
  today_kwh: number
  yesterday_kwh: number
  mode: string
  error: string
  last_update_age_s: number
  contribution_pct: number
}

interface SolarState {
  datetime: string
  source: string
  current_w: number
  today_kwh: number
  yesterday_kwh: number
  peak_today_w: number
  controllers: SolarControllerState[]
}

export interface SolarController {
  id: string
  label: string
  currentW: number | null
  todayKWh: number | null
  yesterdayKWh: number | null
  mode: string | null
  error: string | null
  lastUpdateAgeS: number | null
  contributionPct: number | null
}

function normalizeNumber(value: unknown): number | null {
  return typeof value === 'number' && value >= 0 ? value : null
}

function normalizeText(value: unknown): string | null {
  return typeof value === 'string' && value.trim().length > 0 ? value.trim() : null
}

export function useSolarState(refreshInterval: number) {
  const [currentW, setCurrentW] = useState<number | null>(null)
  const [todayKWh, setTodayKWh] = useState<number | null>(null)
  const [yesterdayKWh, setYesterdayKWh] = useState<number | null>(null)
  const [peakTodayW, setPeakTodayW] = useState<number | null>(null)
  const [controllers, setControllers] = useState<SolarController[]>([])

  useEffect(() => {
    const fetchSolarState = async () => {
      try {
        const response = await fetch('/api/solar-state')
        if (!response.ok) {
          throw new Error('Failed to fetch solar state')
        }

        const data = (await response.json()) as SolarState

        setCurrentW(normalizeNumber(data.current_w))
        setTodayKWh(normalizeNumber(data.today_kwh))
        setYesterdayKWh(normalizeNumber(data.yesterday_kwh))
        setPeakTodayW(normalizeNumber(data.peak_today_w))

        const nextControllers = Array.isArray(data.controllers)
          ? data.controllers.map((controller) => ({
            id: normalizeText(controller.id) ?? 'unknown',
            label: normalizeText(controller.label) ?? 'Array',
            currentW: normalizeNumber(controller.current_w),
            todayKWh: normalizeNumber(controller.today_kwh),
            yesterdayKWh: normalizeNumber(controller.yesterday_kwh),
            mode: normalizeText(controller.mode),
            error: normalizeText(controller.error),
            lastUpdateAgeS: normalizeNumber(controller.last_update_age_s),
            contributionPct: normalizeNumber(controller.contribution_pct),
          }))
          : []

        setControllers(nextControllers)
      } catch (err) {
        console.error('Failed to fetch solar state:', err)
      }
    }

    void fetchSolarState()
    const timer = window.setInterval(() => {
      void fetchSolarState()
    }, refreshInterval * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [refreshInterval])

  return {
    currentW,
    todayKWh,
    yesterdayKWh,
    peakTodayW,
    controllers,
  }
}
