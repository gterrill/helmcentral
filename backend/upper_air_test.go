package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func upperAirDay(dayKey string, heightM, thicknessM, jetMS float64) upperAirDayInput {
	return upperAirDayInput{DayKey: dayKey, Height500M: heightM, ThicknessM: thicknessM, PeakWind500MS: jetMS, Present: true}
}

// Surviving the Storm gives no numeric 500mb thresholds; its method is reading
// successive charts. So a day is judged against the rest of the forecast window
// at this position rather than against a constant somebody invented. The boat
// carries this from the tropics to the Southern Ocean with no retuning.
func TestUpperAirOutlook_FlagsTheLowestFallingDays(t *testing.T) {
	// Heights ramp down across the window: the late days are both lowest and
	// still falling, which is an upper trough moving in.
	var days []upperAirDayInput
	for i := 0; i < 10; i++ {
		days = append(days, upperAirDay(dayKeyAt(i), 5900-float64(i)*10, 5700, 20))
	}

	out := upperAirOutlook(days)
	if len(out) != 10 {
		t.Fatalf("expected 10 days back, got %d", len(out))
	}

	if out[0].TroughSupport {
		t.Fatalf("the highest day of the window must not flag: %+v", out[0])
	}
	if !out[9].TroughSupport {
		t.Fatalf("the lowest, still-falling day must flag: %+v", out[9])
	}
	if out[9].HeightPercentile > 0.2 {
		t.Fatalf("last day percentile = %.2f, want in the lowest quintile", out[9].HeightPercentile)
	}
	if out[9].Tendency24hM >= 0 {
		t.Fatalf("last day tendency = %.1f, want negative", out[9].Tendency24hM)
	}
}

// A flat window has a lowest day by definition. It is not a trough, and
// flagging it would put a warning on the strip every single week.
func TestUpperAirOutlook_FlatWindowFlagsNothing(t *testing.T) {
	var days []upperAirDayInput
	for i := 0; i < 10; i++ {
		days = append(days, upperAirDay(dayKeyAt(i), 5880, 5700, 15))
	}

	for _, day := range upperAirOutlook(days) {
		if day.TroughSupport {
			t.Fatalf("a flat window must flag nothing, got %+v", day)
		}
	}
}

// Low heights that are already recovering are the back of a trough, not the
// front of one. The book's concern is what is arriving.
func TestUpperAirOutlook_RisingHeightsDoNotFlag(t *testing.T) {
	var days []upperAirDayInput
	for i := 0; i < 10; i++ {
		days = append(days, upperAirDay(dayKeyAt(i), 5800+float64(i)*10, 5700, 20))
	}

	out := upperAirOutlook(days)
	if out[0].Tendency24hM != 0 {
		t.Fatalf("the first day has nothing before it, so no tendency: %+v", out[0])
	}
	for i, day := range out {
		if day.TroughSupport {
			t.Fatalf("day %d flagged on rising heights: %+v", i, day)
		}
	}
}

// Absence must stay absent. A day the provider sent no upper air for reports
// nothing rather than a 0m geopotential height, which is not a real reading of
// anything.
func TestUpperAirOutlook_AbsentDaysStayAbsent(t *testing.T) {
	days := []upperAirDayInput{
		upperAirDay(dayKeyAt(0), 5900, 5700, 20),
		{DayKey: dayKeyAt(1), Present: false},
		upperAirDay(dayKeyAt(2), 5850, 5700, 20),
	}

	out := upperAirOutlook(days)
	if out[1].Present {
		t.Fatalf("a day with no upper-air data must not report any: %+v", out[1])
	}
	if out[1].Height500M != 0 || out[1].TroughSupport {
		t.Fatalf("absent day carried values: %+v", out[1])
	}
	// And an absent day must not be counted as a height of zero when working
	// out where the other days sit. If it were, both real days would sit at
	// the top of the window and the lower one would come out at 0.5 rather
	// than at the bottom where it belongs.
	if out[2].HeightPercentile != 0 {
		t.Fatalf("the lower of the two present days should sit at the bottom, got %.2f", out[2].HeightPercentile)
	}
	if out[0].HeightPercentile != 1 {
		t.Fatalf("the higher of the two present days should sit at the top, got %.2f", out[0].HeightPercentile)
	}
}

func TestUpperAirOutlook_NoDataAtAll(t *testing.T) {
	if got := upperAirOutlook(nil); len(got) != 0 {
		t.Fatalf("expected nothing back, got %d entries", len(got))
	}
}

// The jet figure reaches the wire in knots, since that is what every other
// wind number on this page is in.
func TestUpperAirOutlook_ConvertsJetToKnots(t *testing.T) {
	out := upperAirOutlook([]upperAirDayInput{upperAirDay(dayKeyAt(0), 5880, 5700, 30)})
	if math.Abs(out[0].PeakWind500Kts-30*metersPerSecondToKnots) > 0.01 {
		t.Fatalf("jet = %.2f kts, want %.2f", out[0].PeakWind500Kts, 30*metersPerSecondToKnots)
	}
}

func dayKeyAt(offset int) string {
	return time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC).AddDate(0, 0, offset).Format("2006-01-02")
}

// --- provider category (ADR 0071) ---

func withCleanUpperAirProviderRegistry(t *testing.T) {
	t.Helper()
	origRegistry := upperAirProviderRegistry
	origOrder := upperAirProviderOrder
	upperAirProviderRegistry = map[string]upperAirProvider{}
	upperAirProviderOrder = nil
	t.Cleanup(func() {
		upperAirProviderRegistry = origRegistry
		upperAirProviderOrder = origOrder
	})
}

type stubUpperAirProvider struct {
	id     string
	bundle upperAirBundle
	err    error
}

func (s *stubUpperAirProvider) ID() string          { return s.id }
func (s *stubUpperAirProvider) Name() string        { return "Stub" }
func (s *stubUpperAirProvider) Description() string { return "Stub upper-air provider for tests" }
func (s *stubUpperAirProvider) TTLSeconds() int64   { return 3600 }
func (s *stubUpperAirProvider) FetchUpperAir(lat, lon float64, days int) (upperAirBundle, error) {
	if s.err != nil {
		return upperAirBundle{}, s.err
	}
	return s.bundle, nil
}

func writeUpperAirSettings(t *testing.T, provider, signalkURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := fmt.Sprintf("signalk:\n  address: %s\n  port: 0\nui:\n  upper_air_provider: %s\n", signalkURL, provider)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings: %v", err)
	}
	return path
}

// A boat with no upper-air plugin installed is a normal configuration, not a
// fault. It gets an empty answer rather than a 502, because the forecast page
// simply shows no upper-air section.
func TestUpperAirForecast_NoProviderInstalledIsNotAnError(t *testing.T) {
	withCleanUpperAirProviderRegistry(t)

	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  tank_labels: {}\n"), 0o644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}
	t.Setenv("SETTINGS_FILE", path)

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := upperAirForecast(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/upper-air", nil), rec)); err != nil {
		t.Fatalf("upperAirForecast returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no provider installed, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload upperAirResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if len(payload.Days) != 0 {
		t.Fatalf("expected no days, got %d", len(payload.Days))
	}
}

// Naming a provider that is not installed is a real mistake and says so,
// rather than silently serving nothing forever.
func TestUpperAirForecast_ConfiguredButMissingProviderIs502(t *testing.T) {
	withCleanUpperAirProviderRegistry(t)
	t.Setenv("SETTINGS_FILE", writeUpperAirSettings(t, "not-installed", "127.0.0.1"))

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := upperAirForecast(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/upper-air", nil), rec)); err != nil {
		t.Fatalf("upperAirForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for a configured-but-absent provider, got %d", rec.Code)
	}
}

func TestUpperAirForecast_ServesTheOutlook(t *testing.T) {
	withCleanUpperAirProviderRegistry(t)

	loc := vesselLocalLocation(153.0)
	nowLocal := time.Now().In(loc)
	midnight := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)

	var hours []upperAirHourPoint
	for i := 0; i < 6; i++ {
		hours = append(hours, upperAirHourPoint{
			Time:                    midnight.AddDate(0, 0, i).Add(time.Hour),
			GeopotentialHeight500M:  5900 - float64(i)*20,
			GeopotentialHeight1000M: 190,
			WindSpeed500MS:          25,
			Temperature500C:         -8,
		})
	}
	registerUpperAirProvider(&stubUpperAirProvider{id: "open-meteo-upper",
		bundle: upperAirBundle{Hourly: hours, CachedAt: time.Now().UTC()}})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeUpperAirSettings(t, "open-meteo-upper", server.URL))

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := upperAirForecast(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/upper-air", nil), rec)); err != nil {
		t.Fatalf("upperAirForecast returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload upperAirResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if len(payload.Days) != 6 {
		t.Fatalf("expected 6 days, got %d", len(payload.Days))
	}
	if !payload.Days[0].Outlook.Present {
		t.Fatalf("expected an outlook on day 0: %+v", payload.Days[0])
	}
	if payload.Days[0].Outlook.TroughSupport {
		t.Fatalf("the highest day must not be marked: %+v", payload.Days[0].Outlook)
	}
	if !payload.Days[5].Outlook.TroughSupport {
		t.Fatalf("the lowest, still-falling day must be marked: %+v", payload.Days[5].Outlook)
	}
	if payload.Days[0].DayName == "" || payload.Days[0].Date == "" {
		t.Fatalf("expected day naming, got %+v", payload.Days[0])
	}
}

// Found against live data: the trough bottom flagged on a 24-hour tendency of
// -0.167m, which is indistinguishable from flat. Had the model produced
// +0.167 instead, the lowest day of the fortnight would not have been marked
// at all. A flag that turns on the sign of a meaningless number is a coin
// toss, so what has to be real is the fall INTO the day, measured across two
// days rather than one.
func TestUpperAirOutlook_TroughBottomFlagsDespiteAFlatFinalDay(t *testing.T) {
	heights := []float64{5900, 5898, 5895, 5890, 5874, 5858, 5835, 5834.8, 5836, 5839}
	var days []upperAirDayInput
	for i, h := range heights {
		days = append(days, upperAirDay(dayKeyAt(i), h, 5700, 20))
	}

	out := upperAirOutlook(days)

	// The steep approach.
	if !out[6].TroughSupport {
		t.Fatalf("the day of the big fall must flag: %+v", out[6])
	}
	// The bottom, whose own 24h tendency is -0.2m but which sits at the end of
	// a 39m fall.
	if !out[7].TroughSupport {
		t.Fatalf("the trough bottom must flag despite a flat final day: %+v", out[7])
	}
	// And the recovery must not, even though it is still low in the window.
	if out[8].TroughSupport || out[9].TroughSupport {
		t.Fatalf("recovering days must not flag: %+v / %+v", out[8], out[9])
	}
}

// A day the heights merely wobble down to is not a trough. The fall has to be
// a real share of the window's own range, which keeps this self-calibrating.
func TestUpperAirOutlook_IgnoresNoiseLevelFalls(t *testing.T) {
	// A 60m window, then a final day 1m below its neighbour: lowest, falling,
	// and meaningless.
	heights := []float64{5900, 5880, 5870, 5860, 5850, 5845, 5842, 5841, 5840.5, 5840}
	var days []upperAirDayInput
	for i, h := range heights {
		days = append(days, upperAirDay(dayKeyAt(i), h, 5700, 20))
	}

	out := upperAirOutlook(days)
	if out[9].TroughSupport {
		t.Fatalf("a 1m drift to the bottom is not an upper trough: %+v", out[9])
	}
}

// The first two days of a window have no history behind them, so whether
// heights are arriving or leaving is unknowable. Saying nothing is the honest
// answer.
func TestUpperAirOutlook_FirstDaysCannotFlag(t *testing.T) {
	heights := []float64{5830, 5835, 5860, 5880, 5900, 5910}
	var days []upperAirDayInput
	for i, h := range heights {
		days = append(days, upperAirDay(dayKeyAt(i), h, 5700, 20))
	}

	out := upperAirOutlook(days)
	if out[0].TroughSupport || out[1].TroughSupport {
		t.Fatalf("days with no run-up behind them must not flag: %+v / %+v", out[0], out[1])
	}
}
