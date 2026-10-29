package main

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// forecastPoint is where a forecast endpoint is asked about. Without lat and
// lon query parameters it is the boat's own position from SignalK, as it has
// always been. With both it is the place the operator named (a planned stay),
// and SignalK is not consulted at all: the day boundaries and time zone are
// the place's own (zoneAt) and "now" is the server clock.
type forecastPoint struct {
	Lat, Lon  float64
	Zone      placeZone
	Reference time.Time
	Explicit  bool
}

// explicitForecastPoint reads the optional lat/lon pair. Both absent (or
// empty) means own-ship. Half a pair, a value that is not a finite number, or
// one outside the globe is an error: a half-supplied pair silently falling
// back to the boat would show the wrong place's weather under the right
// place's name.
func explicitForecastPoint(c echo.Context) (lat, lon float64, present bool, err error) {
	rawLat := strings.TrimSpace(c.QueryParam("lat"))
	rawLon := strings.TrimSpace(c.QueryParam("lon"))
	if rawLat == "" && rawLon == "" {
		return 0, 0, false, nil
	}
	if rawLat == "" || rawLon == "" {
		return 0, 0, false, fmt.Errorf("lat and lon must be given together")
	}
	lat, latErr := strconv.ParseFloat(rawLat, 64)
	lon, lonErr := strconv.ParseFloat(rawLon, 64)
	if latErr != nil || lonErr != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return 0, 0, false, fmt.Errorf("lat and lon must be numbers")
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false, fmt.Errorf("lat must be within -90..90 and lon within -180..180")
	}
	return lat, lon, true, nil
}

// resolveForecastPoint picks the point for a forecast handler. On failure it
// returns the HTTP status and message to answer with. checkSignalKURL keeps
// the own-ship "SignalK URL is not configured" guard for the handlers that
// always had it; it is irrelevant for an explicit place.
func resolveForecastPoint(c echo.Context, settingsPath string, checkSignalKURL bool) (forecastPoint, int, string) {
	lat, lon, explicit, err := explicitForecastPoint(c)
	if err != nil {
		return forecastPoint{}, http.StatusBadRequest, err.Error()
	}
	if explicit {
		zone, zerr := zoneAt(lat, lon)
		if zerr != nil {
			return forecastPoint{}, http.StatusBadGateway, zerr.Error()
		}
		return forecastPoint{Lat: lat, Lon: lon, Zone: zone, Reference: time.Now().UTC(), Explicit: true}, 0, ""
	}

	if checkSignalKURL {
		address, port, loadErr := loadSignalKSettings(settingsPath)
		if loadErr != nil {
			address = defaultSignalKAddress
			port = defaultSignalKPort
		}
		if buildSignalKURL(address, port) == "" {
			return forecastPoint{}, http.StatusBadGateway, "SignalK URL is not configured"
		}
	}

	vesselState, vesselErr := fetchSignalKVesselState()
	if vesselErr != nil {
		return forecastPoint{}, http.StatusBadGateway, fmt.Sprintf("failed to fetch vessel state: %v", vesselErr)
	}
	if !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return forecastPoint{}, http.StatusBadGateway, "invalid vessel coordinates from SignalK"
	}
	// A critical GNSS alert leaves the state at the last trusted fix, so its
	// zone is the remembered one and the forecast is still for that place.
	boat := boatTimeForState(vesselState, nil)
	if boat.NoFix {
		return forecastPoint{}, http.StatusBadGateway, "no time zone could be found for the vessel position"
	}
	reference := vesselState.Datetime
	if reference.IsZero() {
		reference = time.Now().UTC()
	}
	return forecastPoint{Lat: vesselState.Latitude, Lon: vesselState.Longitude, Zone: boat.placeZone, Reference: reference}, 0, ""
}
