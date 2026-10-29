package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

func testWatchChart() watchChart {
	from := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	var a, b [][2]float64
	for i := 0; i <= 48; i++ {
		t := from.Add(time.Duration(i) * 15 * time.Minute)
		a = append(a, [2]float64{float64(t.UnixMilli()), 1015 - float64(i)*0.2})
		b = append(b, [2]float64{float64(t.UnixMilli()), 1013 - float64(i)*0.1})
	}
	return watchChart{
		Title: "Pressure, last 12 hours", Unit: "hPa", Timezone: "UTC+12", UTCOffsetSeconds: 12 * 3600,
		Window:    watchChartWindow{From: from.Format(time.RFC3339), To: from.Add(12 * time.Hour).Format(time.RFC3339)},
		Series:    []watchChartSeries{{Label: "Pressure", Unit: "hPa", Points: a}, {Label: "Other", Unit: "hPa", Points: b}},
		Threshold: &watchChartThreshold{Label: "Fall limit", Value: 1008},
	}
}

func decodeChartPNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img
}

func countPixels(img image.Image, want color.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if uint8(r>>8) == want.R && uint8(g>>8) == want.G && uint8(bl>>8) == want.B {
				n++
			}
		}
	}
	return n
}

func TestRenderWatchChartPNG_SizeBackgroundAndSeriesColours(t *testing.T) {
	data, err := renderWatchChartPNG(testWatchChart())
	if err != nil {
		t.Fatal(err)
	}
	img := decodeChartPNG(t, data)
	if b := img.Bounds(); b.Dx() != 800 || b.Dy() != 400 {
		t.Fatalf("size = %dx%d, want 800x400", b.Dx(), b.Dy())
	}
	if r, g, b, _ := img.At(2, 2).RGBA(); uint8(r>>8) != 255 || uint8(g>>8) != 255 || uint8(b>>8) != 255 {
		t.Fatalf("corner pixel is not white: %v", img.At(2, 2))
	}
	for i, c := range watchChartColours[:2] {
		if countPixels(img, c) < 200 {
			t.Errorf("series %d colour %v is barely drawn (%d px)", i, c, countPixels(img, c))
		}
	}
	if countPixels(img, watchChartThresholdColour) < 100 {
		t.Errorf("threshold line is not drawn")
	}
}

func TestRenderWatchChartPNG_ThresholdIsDashed(t *testing.T) {
	data, err := renderWatchChartPNG(testWatchChart())
	if err != nil {
		t.Fatal(err)
	}
	img := decodeChartPNG(t, data)
	// Find the row with the most threshold pixels, then count the runs in it.
	bestRow, best := -1, 0
	for y := 0; y < 400; y++ {
		n := 0
		for x := 0; x < 800; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if uint8(r>>8) == watchChartThresholdColour.R && uint8(g>>8) == watchChartThresholdColour.G && uint8(b>>8) == watchChartThresholdColour.B {
				n++
			}
		}
		if n > best {
			bestRow, best = y, n
		}
	}
	runs, in := 0, false
	for x := 0; x < 800; x++ {
		r, g, b, _ := img.At(x, bestRow).RGBA()
		on := uint8(r>>8) == watchChartThresholdColour.R && uint8(g>>8) == watchChartThresholdColour.G && uint8(b>>8) == watchChartThresholdColour.B
		if on && !in {
			runs++
		}
		in = on
	}
	if runs < 8 {
		t.Fatalf("threshold row has %d dashes, want a dashed line", runs)
	}
}

func TestRenderWatchChartPNG_IsDeterministic(t *testing.T) {
	a, err := renderWatchChartPNG(testWatchChart())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := renderWatchChartPNG(testWatchChart())
	if !bytes.Equal(a, b) {
		t.Fatal("the same chart rendered two different PNGs")
	}
}

func TestRenderWatchChartPNG_TwoUnitsStackTwoPanels(t *testing.T) {
	c := testWatchChart()
	c.Threshold = nil
	c.Series[1].Unit = "A"
	data, err := renderWatchChartPNG(c)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeChartPNG(t, data)
	// Each series colour must stay in its own half of the plot.
	top, bottom := 0, 0
	for y := 60; y < 400; y++ { // below the legend
		for x := 0; x < 800; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if uint8(r>>8) == watchChartColours[1].R && uint8(g>>8) == watchChartColours[1].G && uint8(b>>8) == watchChartColours[1].B {
				if y < 200 {
					top++
				} else {
					bottom++
				}
			}
		}
	}
	if top != 0 || bottom < 200 {
		t.Fatalf("second unit is not in the lower panel only: top=%d bottom=%d", top, bottom)
	}
}

func TestRenderWatchChartPNG_RefusesAChartItCannotDraw(t *testing.T) {
	c := testWatchChart()
	c.Series = []watchChartSeries{{Label: "Pressure", Unit: "hPa", Points: [][2]float64{{1, 1}}}}
	if _, err := renderWatchChartPNG(c); err == nil {
		t.Fatal("a one point series was drawn")
	}
	c.Series = nil
	if _, err := renderWatchChartPNG(c); err == nil {
		t.Fatal("a chart with no series was drawn")
	}
}
