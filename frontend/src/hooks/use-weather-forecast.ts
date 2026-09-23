import { useCallback, useEffect, useRef, useState } from 'react';

import type { NowcastPoint } from '@/lib/nowcast';

export interface WeatherHourlyWindPoint {
  label: string;
  hourOfDay: number;
  windSpeed: number;
  windGust: number;
  windDirection: string;
  windDirectionDeg: number;
}

export interface WeatherHourlyPrecipPoint {
  label: string;
  hourOfDay: number;
  /** null when the provider reported no chance-of-precipitation data at all - distinct from a real 0%. */
  precipChancePct: number | null;
  precipIntensityMm: number;
}

/**
 * The backend sends -1 for "the provider reported nothing here" on several
 * fields - precipitation chance, humidity, visibility - because 0 is itself a
 * legitimate reading on every one of them (0% precip, 0% humidity, and most
 * gravely 0.0nm visibility in real fog) and so cannot double as the absent
 * marker (see sentinelPrecipitationPct/sentinelHumidityPct/sentinelVisibilityNm
 * in backend/weather_providers.go). Absence becomes null so the UI can render
 * "unavailable" instead of a confident-looking number.
 */
function sentinelValueOrNull(value: number | undefined): number | null {
  if (typeof value !== 'number' || value < 0) {
    return null;
  }
  return value;
}

export interface WeatherHourlyUVPoint {
  label: string;
  uvIndex: number;
}

export interface WeatherHourlyCloudPoint {
  label: string;
  hourOfDay: number;
  condition: string;
  temperatureF: number;
  isDaylight: boolean;
  /** null when the provider reported no humidity/visibility data for this hour - distinct from a real 0. */
  humidityPct: number | null;
  visibilityNm: number | null;
}

export interface WeatherForecastDay {
  dayKey: string;
  date: string;
  dayName: string;
  condition: string;
  high: number;
  low: number;
  windSpeed: number;
  windGust: number;
  windDirection: string;
  windSummary: string | null;
  precipitationSummary: string | null;
  /** null when the provider reported no chance-of-precipitation data at all - distinct from a real 0%. */
  precipitation: number | null;
  /** null when the provider reported no humidity/visibility data at all - distinct from a real 0. */
  humidityPct: number | null;
  visibilityNm: number | null;
  sunriseTime: string | null;
  sunsetTime: string | null;
  moonPhase: string | null;
  hourlyWind: WeatherHourlyWindPoint[];
  hourlyPrecip: WeatherHourlyPrecipPoint[];
  hourlyUV: WeatherHourlyUVPoint[];
  hourlyCloud: WeatherHourlyCloudPoint[];
}

export interface WeatherHourlyEntry {
  label: string;
  condition: string;
  temperatureF: number;
  windSpeedKts: number;
  windGustKts: number;
  windDirection: string;
  windDirectionDeg: number;
  kind: 'forecast' | 'sunset' | string;
  /**
   * The provider's own per-hour daylight reading (same field the cloud
   * chart's isDaylight already carries) - not derived from whether a sunset
   * marker has appeared earlier in the strip. A 24-hour window can cross
   * both sunset and the following sunrise, and a "seen a sunset yet"
   * latch never turns back off; this per-hour flag does.
   */
  isDaylight: boolean;
}

interface WeatherHourlyWindApi {
  label?: string;
  hour_of_day?: number;
  wind_speed_kts?: number;
  wind_gust_kts?: number;
  wind_direction?: string;
  wind_direction_deg?: number;
}

interface WeatherHourlyPrecipApi {
  label?: string;
  hour_of_day?: number;
  precipitation_chance_pct?: number;
  precipitation_intensity_mm?: number;
}

interface WeatherHourlyUVApi {
  label?: string;
  uv_index?: number;
}

interface WeatherHourlyCloudApi {
  label?: string;
  hour_of_day?: number;
  condition?: string;
  temperature_f?: number;
  is_daylight?: boolean;
  humidity_pct?: number;
  visibility_nm?: number;
}

interface WeatherForecastDayApi {
  day_key?: string;
  date?: string;
  day_name?: string;
  condition?: string;
  high_temp_f?: number;
  low_temp_f?: number;
  wind_speed_kts?: number;
  wind_gust_kts?: number;
  wind_direction?: string;
  wind_summary?: string;
  precipitation_summary?: string;
  precipitation_pct?: number;
  humidity_pct?: number;
  visibility_nm?: number;
  sunrise_time?: string;
  sunset_time?: string;
  moon_phase?: string;
  hourly_wind?: WeatherHourlyWindApi[];
  hourly_precip?: WeatherHourlyPrecipApi[];
  hourly_uv?: WeatherHourlyUVApi[];
  hourly_cloud?: WeatherHourlyCloudApi[];
}

interface WeatherNextHourPointApi {
  time?: string;
  chance_pct?: number;
  mm_per_h?: number;
}

interface WeatherNextHourApi {
  start?: string;
  step_minutes?: number;
  source?: string;
  points?: WeatherNextHourPointApi[];
}

interface WeatherForecastEnvelopeApi {
  provider?: string;
  days?: WeatherForecastDayApi[];
  hourly_today?: Array<{
    label?: string;
    condition?: string;
    temperature_f?: number;
    wind_speed_kts?: number;
    wind_gust_kts?: number;
    wind_direction?: string;
    wind_direction_deg?: number;
    kind?: string;
    is_daylight?: boolean;
  }>;
  next_hour?: WeatherNextHourApi;
  summary?: string;
  cached?: boolean;
  updated_at?: string;
  ttl_seconds?: number;
}

/** The nowcast lib/nowcast.ts consumes - see that file and backend/weather_providers.go's top doc comment for the next_hour contract. */
export interface WeatherNextHour {
  stepMinutes: number;
  points: NowcastPoint[];
  /**
   * "nowcast" (genuine short-range data) or "hourly" (interpolated from the
   * hourly model, e.g. Open-Meteo's minutely_15 outside its native-
   * resolution regions - ADR 0126 addendum). The backend already validates
   * this (mapWasmFetchForecastOutput hard-errors on anything else, so
   * next_hour is never sent with a missing/unrecognized source) - a wire
   * value other than these two exact strings reaching here means something
   * broke that contract, not a shape to quietly repair, so mapNextHour
   * below discards the whole next_hour rather than guessing "hourly".
   */
  source: 'nowcast' | 'hourly';
}

/**
 * Maps the wire next_hour envelope to typed NowcastPoint[]. The backend
 * already validates this contract end-to-end (weather_providers.go:
 * next_hour_source is a hard error if missing/unrecognized whenever points
 * are present, and buildWeatherNextHourResponse never emits an unusable
 * step - it omits next_hour entirely instead), so a malformed payload
 * reaching here means something upstream broke the contract, not a shape
 * the frontend should quietly repair. Any structural problem - a bad or
 * missing timestamp, a missing mm_per_h, a non-positive/missing
 * step_minutes, or an unrecognized source - discards the *whole* next_hour
 * (never a partially-patched one) and console.errors what was wrong, so a
 * broken nowcast shows as "no nowcast" (the existing, well-tested
 * hourly/daily fallback) rather than a silently-repaired strip, and the
 * break is diagnosable instead of silent (AGENTS.md fallback policy).
 */
function mapNextHour(raw: WeatherNextHourApi | undefined): WeatherNextHour | null {
  if (!raw) return null;
  if (!Array.isArray(raw.points) || raw.points.length === 0) {
    return null;
  }

  if (typeof raw.step_minutes !== 'number' || raw.step_minutes <= 0) {
    console.error('next_hour: malformed step_minutes, dropping the nowcast', raw.step_minutes);
    return null;
  }
  if (raw.source !== 'nowcast' && raw.source !== 'hourly') {
    console.error('next_hour: missing/unrecognized source, dropping the nowcast', raw.source);
    return null;
  }

  const points: NowcastPoint[] = [];
  for (const p of raw.points) {
    if (typeof p.time !== 'string') {
      console.error('next_hour: point missing a time, dropping the nowcast', p);
      return null;
    }
    const time = new Date(p.time);
    if (Number.isNaN(time.getTime())) {
      console.error('next_hour: point has an unparseable time, dropping the nowcast', p.time);
      return null;
    }
    if (typeof p.mm_per_h !== 'number') {
      console.error('next_hour: point missing mm_per_h, dropping the nowcast', p);
      return null;
    }
    points.push({
      time,
      chancePct: sentinelValueOrNull(p.chance_pct),
      mmPerH: p.mm_per_h,
    });
  }

  return {
    stepMinutes: raw.step_minutes,
    points,
    source: raw.source,
  };
}

export function useWeatherForecast(refreshIntervalSeconds = 3600) {
  const [forecast, setForecast] = useState<WeatherForecastDay[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hourlyToday, setHourlyToday] = useState<WeatherHourlyEntry[]>([]);
  const [nextHour, setNextHour] = useState<WeatherNextHour | null>(null);
  const [summary, setSummary] = useState<string | null>(null);
  const [provider, setProvider] = useState<string | null>(null);
  const [isCached, setIsCached] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  const [ttlSeconds, setTtlSeconds] = useState<number | null>(null);
  const hasLoadedDataRef = useRef(false);

  const fetchForecast = useCallback(async () => {
      try {
        if (!hasLoadedDataRef.current) {
          setLoading(true);
        }

        const response = await fetch('/api/weather-forecast');
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }

        const payload = (await response.json()) as WeatherForecastDayApi[] | WeatherForecastEnvelopeApi;
        const rawDays = Array.isArray(payload) ? payload : payload.days;
        if (!Array.isArray(rawDays)) {
          throw new Error('Unexpected weather forecast payload format');
        }

        const mappedForecast = rawDays
          .slice(0, 10)
          .map((day, idx): WeatherForecastDay => {
            const fallbackDate = new Date();
            fallbackDate.setDate(fallbackDate.getDate() + idx);

            const windSpeed = typeof day.wind_speed_kts === 'number' ? day.wind_speed_kts : -1;
            const windGust = typeof day.wind_gust_kts === 'number' ? day.wind_gust_kts : windSpeed;

            return {
              dayKey: typeof day.day_key === 'string' && day.day_key !== '' ? day.day_key : fallbackDate.toISOString().slice(0, 10),
              date: day.date || fallbackDate.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
              dayName: day.day_name || fallbackDate.toLocaleDateString('en-US', { weekday: 'long' }),
              condition: day.condition || 'Unknown',
              high: typeof day.high_temp_f === 'number' ? day.high_temp_f : -1,
              low: typeof day.low_temp_f === 'number' ? day.low_temp_f : -1,
              windSpeed,
              windGust,
              windDirection: day.wind_direction || '—',
              windSummary: typeof day.wind_summary === 'string' && day.wind_summary !== '' ? day.wind_summary : null,
              precipitationSummary: typeof day.precipitation_summary === 'string' && day.precipitation_summary !== '' ? day.precipitation_summary : null,
              precipitation: sentinelValueOrNull(day.precipitation_pct),
              humidityPct: sentinelValueOrNull(day.humidity_pct),
              visibilityNm: sentinelValueOrNull(day.visibility_nm),
              sunriseTime: typeof day.sunrise_time === 'string' && day.sunrise_time !== '' ? day.sunrise_time : null,
              sunsetTime: typeof day.sunset_time === 'string' && day.sunset_time !== '' ? day.sunset_time : null,
              moonPhase: typeof day.moon_phase === 'string' && day.moon_phase !== '' ? day.moon_phase : null,
              hourlyWind: Array.isArray(day.hourly_wind)
                ? day.hourly_wind.map((entry) => ({
                    label: entry.label || '—',
                    hourOfDay: typeof entry.hour_of_day === 'number' ? entry.hour_of_day : -1,
                    windSpeed: typeof entry.wind_speed_kts === 'number' ? entry.wind_speed_kts : -1,
                    windGust: typeof entry.wind_gust_kts === 'number' ? entry.wind_gust_kts : -1,
                    windDirection: entry.wind_direction || '—',
                    windDirectionDeg: typeof entry.wind_direction_deg === 'number' ? entry.wind_direction_deg : -1,
                  }))
                : [],
              hourlyPrecip: Array.isArray(day.hourly_precip)
                ? day.hourly_precip.map((entry) => ({
                    label: entry.label || '—',
                    hourOfDay: typeof entry.hour_of_day === 'number' ? entry.hour_of_day : -1,
                    precipChancePct: sentinelValueOrNull(entry.precipitation_chance_pct),
                    precipIntensityMm: typeof entry.precipitation_intensity_mm === 'number' ? entry.precipitation_intensity_mm : 0,
                  }))
                : [],
              hourlyUV: Array.isArray(day.hourly_uv)
                ? day.hourly_uv.map((entry) => ({
                    label: entry.label || '—',
                    uvIndex: typeof entry.uv_index === 'number' ? entry.uv_index : 0,
                  }))
                : [],
              hourlyCloud: Array.isArray(day.hourly_cloud)
                ? day.hourly_cloud.map((entry) => ({
                    label: entry.label || '—',
                    hourOfDay: typeof entry.hour_of_day === 'number' ? entry.hour_of_day : -1,
                    condition: entry.condition || 'Unknown',
                    temperatureF: typeof entry.temperature_f === 'number' ? entry.temperature_f : -1,
                    isDaylight: Boolean(entry.is_daylight),
                    humidityPct: sentinelValueOrNull(entry.humidity_pct),
                    visibilityNm: sentinelValueOrNull(entry.visibility_nm),
                  }))
                : [],
            };
          });

        if (mappedForecast.length > 0) {
          hasLoadedDataRef.current = true;
        }

        setForecast(mappedForecast);
        if (!Array.isArray(payload)) {
          setHourlyToday(Array.isArray(payload.hourly_today)
            ? payload.hourly_today.map((entry) => ({
                label: entry.label || '—',
                condition: entry.condition || 'Unknown',
                temperatureF: typeof entry.temperature_f === 'number' ? entry.temperature_f : -1,
                windSpeedKts: typeof entry.wind_speed_kts === 'number' ? entry.wind_speed_kts : -1,
                windGustKts: typeof entry.wind_gust_kts === 'number' ? entry.wind_gust_kts : -1,
                windDirection: entry.wind_direction || '—',
                windDirectionDeg: typeof entry.wind_direction_deg === 'number' ? entry.wind_direction_deg : -1,
                kind: entry.kind || 'forecast',
                isDaylight: Boolean(entry.is_daylight),
              }))
            : []);
          setNextHour(mapNextHour(payload.next_hour));
          setSummary(typeof payload.summary === 'string' ? payload.summary : null);
          setProvider(typeof payload.provider === 'string' && payload.provider !== '' ? payload.provider : null);
          setIsCached(Boolean(payload.cached));
          setUpdatedAt(typeof payload.updated_at === 'string' ? payload.updated_at : null);
          setTtlSeconds(typeof payload.ttl_seconds === 'number' ? payload.ttl_seconds : null);
        } else {
          setHourlyToday([]);
          setNextHour(null);
          setSummary(null);
          setProvider(null);
          setIsCached(false);
          setUpdatedAt(null);
          setTtlSeconds(null);
        }
        setError(null);
      } catch (error) {
        console.error('Error fetching weather forecast:', error);
        setError(error instanceof Error ? error.message : 'Failed to load forecast data');
      } finally {
        setLoading(false);
      }
    }, []);

  useEffect(() => {
    fetchForecast();
    const interval = setInterval(fetchForecast, refreshIntervalSeconds * 1000);
    return () => clearInterval(interval);
  }, [fetchForecast, refreshIntervalSeconds]);

  return { forecast, hourlyToday, nextHour, summary, provider, loading, error, isCached, updatedAt, ttlSeconds, refetch: fetchForecast };
}
