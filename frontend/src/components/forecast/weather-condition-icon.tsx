import { Cloud, CloudRain, Sun } from 'lucide-react'

// Simple weather icon selector
export function WeatherConditionIcon({ condition, size = 40 }: { condition: string; size?: number }) {
  const iconProps = { size }

  if (condition.toLowerCase().includes('clear') || condition.toLowerCase().includes('sunny')) {
    return <Sun {...iconProps} className="text-gauge-primary" />
  }
  if (condition.toLowerCase().includes('cloud')) {
    return <Cloud {...iconProps} className="text-muted-foreground" />
  }
  if (condition.toLowerCase().includes('rain') || condition.toLowerCase().includes('drizzle')) {
    return <CloudRain {...iconProps} className="text-chart-precip" />
  }

  return <Cloud {...iconProps} className="text-muted-foreground" />
}
