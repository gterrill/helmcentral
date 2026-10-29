import { useState } from "react";

import { CalendarRange, Pencil } from "lucide-react";

import {
  ForecastDrawer,
  type ForecastDrawerProps,
} from "@/components/forecast-drawer";
import { ForecastStaySheet } from "@/components/forecast-stay-sheet";
import { Button } from "@/components/ui/button";
import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { usePlannedStay } from "@/hooks/use-planned-stay";
import { useForecastWarnings } from "@/hooks/use-forecast-warnings";
import { useUpperAir } from "@/hooks/use-upper-air";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import {
  buildForecastDrawerProps,
  type ForecastBoatProps,
} from "@/lib/forecast-drawer-props";
import { unknownZoneMessage, zoneClocksDiffer, zoneOffsetLabel } from "@/lib/vessel-time";
import { formatStayRange } from "@/lib/stay";

interface ForecastPageProps {
  boat: ForecastBoatProps;
  /** The boat's IANA zone, to tell whether the stay keeps a different clock. */
  boatTimeZone: string | undefined;
  canWrite: boolean;
}

/**
 * The Forecast page: the boat's forecast, and beside it the planned stay's.
 *
 * It opens on the boat. A chip carrying the place and dates switches to the
 * stay and a Boat button returns, one tap each way; the header always names
 * which place is on screen. The stay's feeds are their own queries, held back
 * until the stay is showing, so the dashboard tiles, the clock's sunrise and
 * sunset, Auto theme and the Rode Planner keep reading the boat's.
 */
export function ForecastPage({
  boat,
  boatTimeZone,
  canWrite,
}: ForecastPageProps) {
  const { stay, save, clear } = usePlannedStay();
  const [view, setView] = useState<"boat" | "stay">("boat");
  const [sheetOpen, setSheetOpen] = useState(false);

  // A stay that was cleared, or has ended, while open drops the view back to
  // the boat; nothing is left showing a place that is no longer planned.
  const showingStay = view === "stay" && stay !== null;
  const place = stay ? { lat: stay.lat, lon: stay.lon } : undefined;

  const weather = useWeatherForecast(FORECAST_REFRESH_SECONDS, {
    place,
    enabled: showingStay,
  });
  const waves = useWaveForecast(FORECAST_REFRESH_SECONDS, {
    place,
    enabled: showingStay,
  });
  const upperAir = useUpperAir(undefined, { place, enabled: showingStay });
  const warnings = useForecastWarnings(FORECAST_REFRESH_SECONDS, {
    place,
    enabled: showingStay,
  });

  const stayProps: ForecastDrawerProps | null =
    showingStay && stay
      ? {
          ...buildForecastDrawerProps({
            weather,
            waves,
            upperAir,
            warnings,
            unit: boat.unit,
          }),
          stay: {
            placeName: stay.name,
            lat: stay.lat,
            lon: stay.lon,
            arrive: stay.arrive,
            depart: stay.depart,
            timezone: stay.timezone,
          },
        }
      : null;

  // A zone this browser does not know is said plainly; the comparison of
  // clocks is skipped for it rather than thrown.
  const zoneProblem = stay
    ? (unknownZoneMessage(stay.timezone) ?? unknownZoneMessage(boatTimeZone))
    : null;
  const clockDiffers =
    stay !== null &&
    !zoneProblem &&
    boatTimeZone !== undefined &&
    zoneClocksDiffer(stay.timezone, boatTimeZone, Date.now());

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <p
          data-testid="forecast-place-label"
          className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground"
        >
          {showingStay && stay
            ? `Forecast for ${stay.name} — not the boat's position`
            : "Forecast for the boat's position"}
        </p>

        <div className="flex flex-wrap items-center gap-2">
          {stay ? (
            <>
              <div
                className="inline-flex rounded-md border border-border bg-card p-0.5"
                role="group"
                aria-label="Forecast place"
              >
                <Button
                  type="button"
                  size="sm"
                  variant={showingStay ? "ghost" : "secondary"}
                  className="h-8 text-xs"
                  aria-pressed={!showingStay}
                  onClick={() => setView("boat")}
                >
                  Boat
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={showingStay ? "secondary" : "ghost"}
                  className="h-8 min-w-0 max-w-64 text-xs"
                  aria-pressed={showingStay}
                  onClick={() => setView("stay")}
                >
                  <CalendarRange
                    size={13}
                    aria-hidden
                    className="mr-1 shrink-0"
                  />
                  <span className="truncate">{`${stay.name} · ${formatStayRange(stay.arrive, stay.depart)}`}</span>
                </Button>
              </div>
              {canWrite && (
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-8 text-xs"
                  onClick={() => setSheetOpen(true)}
                >
                  <Pencil size={13} aria-hidden className="mr-1" />
                  Change stay
                </Button>
              )}
            </>
          ) : (
            canWrite && (
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="h-8 text-xs"
                onClick={() => setSheetOpen(true)}
              >
                <CalendarRange size={13} aria-hidden className="mr-1" />
                Plan a stay
              </Button>
            )
          )}
        </div>
      </div>

      {zoneProblem && (
        <p
          data-testid="forecast-stay-zone-error"
          className="text-xs text-destructive"
        >
          {zoneProblem}
        </p>
      )}

      {clockDiffers && stay && (
        <p
          data-testid="forecast-stay-clock-note"
          className="text-xs text-muted-foreground"
        >
          Stay times are {zoneOffsetLabel(stay.timezone, Date.now())}, local to {stay.name}, not
          the boat&apos;s clock.
        </p>
      )}

      <ForecastDrawer
        key={showingStay ? "stay" : "boat"}
        {...(stayProps ?? boat)}
      />

      {canWrite && (
        <ForecastStaySheet
          open={sheetOpen}
          onOpenChange={setSheetOpen}
          stay={stay}
          onSave={save}
          onClear={clear}
        />
      )}
    </div>
  );
}
