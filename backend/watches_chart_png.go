package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	xfixed "golang.org/x/image/math/fixed"
)

/*
The PNG of a watch chart (ADR 0228), drawn in pure Go for the email and the
phone notification. It is the same data the Watches page plots, so the two
agree. Lines are drawn without anti-aliasing so the output is the same on every
machine and a test can look at exact pixels.
*/

const (
	chartPNGWidth  = 800
	chartPNGHeight = 400
	// Text sizes are in pixels at 72 dpi, so the smallest is 13 px: readable on
	// a phone once the image is scaled to the screen.
	chartTitleSize = 18
	chartTextSize  = 13
)

var (
	chartInk      = color.RGBA{33, 37, 41, 255}
	chartMuted    = color.RGBA{90, 98, 107, 255}
	chartGridLine = color.RGBA{225, 228, 232, 255}
	chartAxisLine = color.RGBA{150, 156, 163, 255}
	chartWhite    = color.RGBA{255, 255, 255, 255}
)

var chartFonts = sync.OnceValues(func() (map[int]font.Face, error) {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("the chart font could not be read: %w", err)
	}
	faces := map[int]font.Face{}
	for _, size := range []int{chartTitleSize, chartTextSize} {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, fmt.Errorf("the chart font could not be sized: %w", err)
		}
		faces[size] = face
	}
	return faces, nil
})

type chartCanvas struct {
	img   *image.RGBA
	faces map[int]font.Face
}

func (c *chartCanvas) set(x, y int, col color.RGBA) {
	if image.Pt(x, y).In(c.img.Bounds()) {
		c.img.SetRGBA(x, y, col)
	}
}

func (c *chartCanvas) rect(x0, y0, x1, y1 int, col color.RGBA) {
	draw.Draw(c.img, image.Rect(x0, y0, x1, y1), &image.Uniform{col}, image.Point{}, draw.Src)
}

// line draws from (x0,y0) to (x1,y1) with a square brush of the given width.
// A dash of 0 is solid; otherwise it is dash pixels on, dash off.
func (c *chartCanvas) line(x0, y0, x1, y1 float64, width int, col color.RGBA, dash int) {
	steps := int(math.Ceil(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))))
	if steps == 0 {
		steps = 1
	}
	for i := 0; i <= steps; i++ {
		if dash > 0 && (i/dash)%2 == 1 {
			continue
		}
		f := float64(i) / float64(steps)
		x, y := int(math.Round(x0+(x1-x0)*f)), int(math.Round(y0+(y1-y0)*f))
		for dy := 0; dy < width; dy++ {
			for dx := 0; dx < width; dx++ {
				c.set(x+dx-width/2, y+dy-width/2, col)
			}
		}
	}
}

func (c *chartCanvas) text(s string, x, y, size int, col color.RGBA) {
	d := &font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: c.faces[size], Dot: xfixed.P(x, y)}
	d.DrawString(s)
}

func (c *chartCanvas) textWidth(s string, size int) int {
	return (&font.Drawer{Face: c.faces[size]}).MeasureString(s).Ceil()
}

// fitText is s, cut short with an ellipsis if it is wider than max pixels.
func (c *chartCanvas) fitText(s string, max, size int) string {
	if c.textWidth(s, size) <= max {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && c.textWidth(string(runes)+"…", size) > max {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimSpace(string(runes)) + "…"
}

// chartPanel is the series that share one unit and one y axis.
type chartPanel struct {
	unit   string
	series []int
}

func chartPanels(c watchChart) []chartPanel {
	var panels []chartPanel
	for i, s := range c.Series {
		placed := false
		for p := range panels {
			if panels[p].unit == s.Unit {
				panels[p].series = append(panels[p].series, i)
				placed = true
				break
			}
		}
		if !placed {
			panels = append(panels, chartPanel{unit: s.Unit, series: []int{i}})
		}
	}
	return panels
}

// renderWatchChartPNG draws a chart as an 800 by 400 PNG. It refuses a chart
// with nothing to draw rather than drawing an empty frame.
func renderWatchChartPNG(c watchChart) ([]byte, error) {
	if len(c.Series) == 0 {
		return nil, errors.New("the chart has no series")
	}
	if len(c.Series) > len(watchChartColours) {
		return nil, fmt.Errorf("the chart has %d series and the renderer draws %d", len(c.Series), len(watchChartColours))
	}
	for _, s := range c.Series {
		if len(s.Points) < 2 {
			return nil, fmt.Errorf("the series %q has %d points, so there is no line to draw", s.Label, len(s.Points))
		}
	}
	from, err1 := time.Parse(time.RFC3339, c.Window.From)
	to, err2 := time.Parse(time.RFC3339, c.Window.To)
	if err1 != nil || err2 != nil || !to.After(from) {
		return nil, errors.New("the chart's time window is not valid")
	}
	faces, err := chartFonts()
	if err != nil {
		return nil, err
	}
	loc := time.FixedZone(c.Timezone, c.UTCOffsetSeconds)
	cv := &chartCanvas{img: image.NewRGBA(image.Rect(0, 0, chartPNGWidth, chartPNGHeight)), faces: faces}
	cv.rect(0, 0, chartPNGWidth, chartPNGHeight, chartWhite)

	thresholds := c.thresholds()
	title := c.Title
	if c.Timezone != "" {
		title += "  (" + c.Timezone + ")"
	}
	cv.text(title, 16, 28, chartTitleSize, chartInk)

	// Legend under the title, one entry a series and one a dashed line. An entry
	// that would pass the right edge starts a new row, and a label too long even
	// for a row of its own is cut short with an ellipsis.
	const (
		legendLeft, legendRight, legendRowH    = 16, chartPNGWidth - 16, 16
		legendSwatch, legendLineKey, legendGap = 20, 28, 24
	)
	type legendEntry struct {
		label string
		key   int // the width the swatch takes before the label
		draw  func(x, y int)
	}
	var entries []legendEntry
	for i, s := range c.Series {
		label := s.Label
		if s.Unit != "" {
			label += " (" + s.Unit + ")"
		}
		colour := watchChartColours[i]
		entries = append(entries, legendEntry{label, legendSwatch, func(x, y int) { cv.rect(x, y, x+14, y+12, colour) }})
	}
	for _, th := range thresholds {
		entries = append(entries, legendEntry{th.Label, legendLineKey, func(x, y int) {
			cv.line(float64(x), float64(y+6), float64(x+22), float64(y+6), 2, watchChartThresholdColour, 5)
		}})
	}
	lx, row := legendLeft, 0
	for _, e := range entries {
		label := cv.fitText(e.label, legendRight-legendLeft-e.key, chartTextSize)
		width := e.key + cv.textWidth(label, chartTextSize)
		if lx > legendLeft && lx+width > legendRight {
			lx, row = legendLeft, row+1
		}
		y := 40 + row*legendRowH
		e.draw(lx, y)
		cv.text(label, lx+e.key, y+11, chartTextSize, chartInk)
		lx += width + legendGap
	}

	const (
		left, right = 100, 24
		bottom      = 34
		gap         = 28
	)
	top := 66 + row*legendRowH
	panels := chartPanels(c)
	plotL, plotR := left, chartPNGWidth-right
	area := chartPNGHeight - top - bottom - gap*(len(panels)-1)
	fromMs, toMs := float64(from.UnixMilli()), float64(to.UnixMilli())
	xOf := func(ms float64) float64 { return float64(plotL) + (ms-fromMs)/(toMs-fromMs)*float64(plotR-plotL) }

	for pi, p := range panels {
		pTop := top + pi*(area/len(panels)+gap)
		pBottom := pTop + area/len(panels)
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, si := range p.series {
			for _, pt := range c.Series[si].Points {
				lo, hi = math.Min(lo, pt[1]), math.Max(hi, pt[1])
			}
		}
		if pi == 0 {
			for _, th := range thresholds {
				lo, hi = math.Min(lo, th.Value), math.Max(hi, th.Value)
			}
		}
		ticks := chartNiceTicks(lo, hi, 5)
		lo, hi = ticks[0], ticks[len(ticks)-1]
		yOf := func(v float64) float64 {
			return float64(pBottom) - (v-lo)/(hi-lo)*float64(pBottom-pTop)
		}
		for ti, tv := range ticks {
			y := yOf(tv)
			cv.line(float64(plotL), y, float64(plotR), y, 1, chartGridLine, 0)
			label := chartNumber(tv)
			if ti == len(ticks)-1 && p.unit != "" {
				label += " " + p.unit
			}
			cv.text(label, plotL-8-cv.textWidth(label, chartTextSize), int(y)+5, chartTextSize, chartMuted)
		}
		cv.line(float64(plotL), float64(pTop), float64(plotL), float64(pBottom), 1, chartAxisLine, 0)
		cv.line(float64(plotL), float64(pBottom), float64(plotR), float64(pBottom), 1, chartAxisLine, 0)

		xticks := chartTimeTicks(from, to, loc)
		for _, t := range xticks {
			x := xOf(float64(t.UnixMilli()))
			cv.line(x, float64(pTop), x, float64(pBottom), 1, chartGridLine, 0)
			if pi == len(panels)-1 {
				label := chartTimeLabel(t, to.Sub(from), loc)
				cv.text(label, int(x)-cv.textWidth(label, chartTextSize)/2, pBottom+20, chartTextSize, chartMuted)
			}
		}

		if pi == 0 {
			for _, th := range thresholds {
				y := yOf(th.Value)
				cv.line(float64(plotL), y, float64(plotR), y, 2, watchChartThresholdColour, 6)
			}
		}
		for _, si := range p.series {
			pts := c.Series[si].Points
			brk := 4 * chartMedianStep(pts)
			for i := 1; i < len(pts); i++ {
				if pts[i][0]-pts[i-1][0] > brk {
					continue // a gap in the record is left as a gap
				}
				cv.line(xOf(pts[i-1][0]), yOf(pts[i-1][1]), xOf(pts[i][0]), yOf(pts[i][1]), 3, watchChartColours[si], 0)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, cv.img); err != nil {
		return nil, fmt.Errorf("the chart could not be encoded: %w", err)
	}
	return buf.Bytes(), nil
}

func chartMedianStep(pts [][2]float64) float64 {
	steps := make([]float64, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		steps = append(steps, pts[i][0]-pts[i-1][0])
	}
	sort.Float64s(steps)
	return steps[len(steps)/2]
}

// chartNiceTicks picks round tick values that cover [lo, hi], about n of them.
func chartNiceTicks(lo, hi float64, n int) []float64 {
	if hi-lo < 1e-9 {
		pad := math.Max(1, math.Abs(lo)*0.01)
		lo, hi = lo-pad, hi+pad
	}
	raw := (hi - lo) / float64(n-1)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := mag
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		step = m * mag
		if step >= raw {
			break
		}
	}
	first, last := math.Floor(lo/step+1e-9), math.Ceil(hi/step-1e-9)
	var out []float64
	for k := first; k <= last; k++ {
		out = append(out, k*step)
	}
	return out
}

func chartNumber(v float64) string {
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}

// chartTimeTicks are round boat-clock times inside the window, at most seven.
func chartTimeTicks(from, to time.Time, loc *time.Location) []time.Time {
	span := to.Sub(from)
	steps := []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour}
	step := steps[len(steps)-1]
	for _, s := range steps {
		if span/s <= 7 {
			step = s
			break
		}
	}
	// Truncate works on absolute time; a zone with a half-hour offset needs the
	// local wall clock instead.
	_, off := from.In(loc).Zone()
	start := from.Add(time.Duration(off) * time.Second).Truncate(step).Add(-time.Duration(off) * time.Second)
	var out []time.Time
	for t := start; !t.After(to); t = t.Add(step) {
		if !t.Before(from) {
			out = append(out, t)
		}
	}
	return out
}

func chartTimeLabel(t time.Time, span time.Duration, loc *time.Location) string {
	if span > 36*time.Hour {
		return t.In(loc).Format("Mon 15:04")
	}
	return t.In(loc).Format("15:04")
}
