package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

// newTestMotionAccumulator is what tests pass buildAPIRoutes in place of the
// one main() builds.
func newTestMotionAccumulator() *motionAccumulator { return newMotionAccumulator() }

func TestBuildAPIRoutesServesTheMotionFeedWithoutPanicking(t *testing.T) {
	routes := buildAPIRoutes(newTestSessionStore(t), newWorldImageryHTTPClient(), newTestMotionAccumulator())
	for _, r := range routes {
		if r.Method == "GET" && r.Path == "/api/signalk/motion-feed" {
			code, body := getJSON(t, r.Handler, r.Path, "")
			if code != 200 || body["min_hz"] != motionMinRateHz {
				t.Fatalf("motion-feed: %d %v", code, body)
			}
			return
		}
	}
	t.Fatal("GET /api/signalk/motion-feed is not in the route table")
}

func TestBuildAPIRoutesRefusesANilMotionAccumulator(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a nil accumulator would give a route that panics; construction must refuse it")
		}
	}()
	buildAPIRoutes(newTestSessionStore(t), newWorldImageryHTTPClient(), nil)
}

func statusAt(m *motionAccumulator) motionFeedStatus { return m.FeedStatus() }

func nearAbs(t *testing.T, name string, got *float64, want, tol float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > tol {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func wantInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %d", name, got, want)
	}
}

func wantReason(t *testing.T, name, got string, parts ...string) {
	t.Helper()
	if got == "" {
		t.Errorf("%s: want a reason, got none", name)
		return
	}
	for _, p := range parts {
		if !strings.Contains(got, p) {
			t.Errorf("%s reason %q lacks %q", name, got, p)
		}
	}
}

// connectedAccumulator is a connected stream on a clock the test moves.
func connectedAccumulator() (*motionAccumulator, *testClock) {
	c := &testClock{t: motionT0}
	m := newMotionAccumulatorClock(c.now)
	m.SetStreamConnected(true)
	return m, c
}

// The status shows what the recorder stored on its last window, which is the
// number its 8 Hz gate used.
func TestMotionFeedStatusShowsTheRateTheRecorderStoredAndWhen(t *testing.T) {
	m, c := connectedAccumulator()
	feedClocked(m, c, 0, 0, 12, 5, true, true)
	f, _ := m.TakeWindow()
	s := statusAt(m)
	if !s.Connected || s.Reason != "" {
		t.Fatalf("connected feed: %+v", s)
	}
	nearAbs(t, "attitude Hz", s.AttitudeHz, 10, 0.1)
	nearAbs(t, "heave Hz", s.HeaveHz, 10, 0.1)
	if want := math.Round(*f.MotionRateHz*10) / 10; *s.AttitudeHz != want {
		t.Errorf("attitude shown %v, stored %v", *s.AttitudeHz, want)
	}
	if s.MinHz != motionMinRateHz || s.AttitudeBelowMin || s.HeaveBelowMin {
		t.Errorf("10 Hz is not below the minimum: %+v", s)
	}
	if s.AttitudeReason != "" || s.HeaveReason != "" {
		t.Errorf("a healthy feed has no reason: %q %q", s.AttitudeReason, s.HeaveReason)
	}
	wantInt(t, "attitude recorded ago", s.AttitudeRecordedAgoS, 0)
	wantInt(t, "attitude last sample", s.AttitudeLastSampleS, 0)

	c.advance(3 * time.Second)
	s = statusAt(m)
	wantInt(t, "attitude recorded ago", s.AttitudeRecordedAgoS, 3)
	wantInt(t, "heave last sample", s.HeaveLastSampleS, 3)
	if s.AttitudeReason != "" {
		t.Errorf("3 s is not silence: %q", s.AttitudeReason)
	}
}

func TestMotionFeedStatusShowsNoRateBeforeTheRecorderHasTakenAWindow(t *testing.T) {
	m, c := connectedAccumulator()
	feedClocked(m, c, 0, 0, 12, 5, true, true)
	s := statusAt(m)
	if s.AttitudeHz != nil || s.AttitudeRecordedAgoS != nil {
		t.Errorf("nothing recorded yet, nothing to show: %+v", s)
	}
	wantReason(t, "attitude", s.AttitudeReason, "No position has been recorded")
	wantInt(t, "attitude last sample", s.AttitudeLastSampleS, 0)
}

func TestMotionFeedStatusSeesASlowFeedOnTheRecordedWindow(t *testing.T) {
	m, c := connectedAccumulator()
	w := 2 * math.Pi / 3
	for i := 0; i < 60; i++ { // 5 Hz for 12 s
		c.advance(200 * time.Millisecond)
		m.AddAttitude(atSec(float64(i)*0.2), 5*math.Sin(w*float64(i)*0.2), 0)
	}
	m.TakeWindow()
	s := statusAt(m)
	nearAbs(t, "attitude Hz", s.AttitudeHz, 5, 0.1)
	if !s.AttitudeBelowMin {
		t.Errorf("5 Hz is below the minimum: %+v", s)
	}
	wantReason(t, "attitude", s.AttitudeReason, "8 Hz")
	if s.HeaveHz != nil || s.HeaveBelowMin {
		t.Errorf("no heave samples: %+v", s)
	}
	wantReason(t, "heave", s.HeaveReason, "no heave")
}

func TestMotionFeedStatusGivesHeaveItsOwnBelowFloorReason(t *testing.T) {
	m, c := connectedAccumulator()
	for i := 0; i < 60; i++ { // 5 Hz heave
		c.advance(200 * time.Millisecond)
		m.AddHeave(atSec(float64(i)*0.2), 0.3*math.Sin(float64(i)))
	}
	m.TakeWindow()
	s := statusAt(m)
	if !s.HeaveBelowMin {
		t.Fatalf("5 Hz heave is below the minimum: %+v", s)
	}
	wantReason(t, "heave", s.HeaveReason, "heave", "8 Hz")
	if s.AttitudeReason == "" || strings.Contains(s.AttitudeReason, "8 Hz") {
		t.Errorf("attitude has none yet, which is a different reason: %q", s.AttitudeReason)
	}
}

// The recorder gates on continuous intervals of the sensor's own timestamps,
// so a dropout must not read as a slow feed.
func TestMotionFeedStatusIgnoresADropoutInATenHertzFeed(t *testing.T) {
	m, c := connectedAccumulator()
	ts := 0.0
	send := func(n int) {
		for i := 0; i < n; i++ {
			c.advance(100 * time.Millisecond)
			m.AddAttitude(atSec(ts), 5*math.Sin(2*math.Pi/3*ts), 0)
			ts += 0.1
		}
	}
	send(60)
	c.advance(5 * time.Second) // the dropout
	ts += 5
	send(50)
	m.TakeWindow()
	s := statusAt(m)
	nearAbs(t, "attitude Hz", s.AttitudeHz, 10, 0.1)
	if s.AttitudeBelowMin {
		t.Errorf("a dropout in a 10 Hz feed is not a slow feed: %+v", s)
	}
}

// Samples that arrive in bursts but are spaced 150 ms apart by the sensor are
// a 6.7 Hz feed, whatever the arrival clock counts.
func TestMotionFeedStatusWarnsOnBurstsSpacedUnderTheFloor(t *testing.T) {
	m, c := connectedAccumulator()
	ts := 0.0
	for sec := 0; sec < 12; sec++ {
		c.advance(time.Second)
		for i := 0; i < 10; i++ { // ten per second, arriving together
			m.AddAttitude(atSec(ts), 5*math.Sin(2*math.Pi/3*ts), 0)
			ts += 0.15
		}
	}
	m.TakeWindow()
	if s := statusAt(m); !s.AttitudeBelowMin {
		t.Errorf("sensor spacing of 150 ms is under the floor: %+v", s)
	}
}

// 7.97 Hz shows as 8.0 but is under the floor, and rounding happens once.
func TestMotionFeedStatusJudgesTheUnroundedRate(t *testing.T) {
	m, c := connectedAccumulator()
	for i := 0; i < 100; i++ {
		c.advance(125500 * time.Microsecond)
		ts := float64(i) * 0.1255
		m.AddAttitude(atSec(ts), 5*math.Sin(2*math.Pi/3*ts), 0)
	}
	m.TakeWindow()
	s := statusAt(m)
	nearAbs(t, "attitude Hz", s.AttitudeHz, 8.0, 0.05)
	if !s.AttitudeBelowMin {
		t.Errorf("7.97 Hz is under 8 Hz even though it displays as 8.0: %+v", s)
	}
}

func TestMotionFeedStatusNeverConnectedAndDisconnectedAreDifferentReasons(t *testing.T) {
	c := &testClock{t: motionT0}
	m := newMotionAccumulatorClock(c.now)
	c.advance(time.Minute)
	s := statusAt(m)
	if s.Connected || s.AttitudeHz != nil || s.HeaveHz != nil {
		t.Errorf("not connected means no rates: %+v", s)
	}
	wantReason(t, "never connected", s.Reason, "never connected")

	m.SetStreamConnected(true)
	m.SetStreamConnected(false)
	s = statusAt(m)
	wantReason(t, "dropped", s.Reason, "not connected")
	if strings.Contains(s.Reason, "never") {
		t.Errorf("it has connected before: %q", s.Reason)
	}
}

func TestMotionFeedStatusConnectedWithNoSamplesYetGivesAReasonPerSignal(t *testing.T) {
	m, c := connectedAccumulator()
	c.advance(30 * time.Second)
	s := statusAt(m)
	if !s.Connected || s.AttitudeHz != nil {
		t.Errorf("connected but silent: %+v", s)
	}
	wantReason(t, "attitude", s.AttitudeReason, "no attitude", "opened")
	wantReason(t, "heave", s.HeaveReason, "no heave", "opened")
	if s.AttitudeLastSampleS != nil {
		t.Errorf("no sample, no age: %v", *s.AttitudeLastSampleS)
	}
}

// A feed that was healthy and then stopped must not keep reading as healthy.
func TestMotionFeedStatusAStallShowsSilenceNotHealth(t *testing.T) {
	m, c := connectedAccumulator()
	feedClocked(m, c, 0, 0, 12, 5, true, true)
	m.TakeWindow()
	c.advance(6 * time.Second)
	m.TakeWindow() // the recorder keeps taking windows; this one is empty
	c.advance(2 * time.Second)
	s := statusAt(m)
	wantReason(t, "attitude", s.AttitudeReason, "attitude", "8 seconds")
	wantReason(t, "heave", s.HeaveReason, "heave", "8 seconds")
	nearAbs(t, "last recorded attitude Hz", s.AttitudeHz, 10, 0.1)
	wantInt(t, "attitude last sample", s.AttitudeLastSampleS, 8)
	wantInt(t, "attitude recorded ago", s.AttitudeRecordedAgoS, 8)
}

// A window taken on the old connection says nothing about the new one: its rate
// and below-floor verdict must not show until the recorder takes a window on
// this connection.
func TestMotionFeedStatusDropsARateRecordedBeforeTheStreamReconnected(t *testing.T) {
	m, c := connectedAccumulator()
	w := 2 * math.Pi / 3
	for i := 0; i < 60; i++ { // 5 Hz, below the floor
		c.advance(200 * time.Millisecond)
		m.AddAttitude(atSec(float64(i)*0.2), 5*math.Sin(w*float64(i)*0.2), 0)
	}
	m.TakeWindow()
	m.SetStreamConnected(false)
	c.advance(10 * time.Second)
	m.SetStreamConnected(true)
	c.advance(time.Second)
	m.AddAttitude(atSec(100), 1, 0)
	c.advance(2 * time.Second)

	s := statusAt(m)
	if s.AttitudeHz != nil || s.AttitudeBelowMin || s.AttitudeRecordedAgoS != nil {
		t.Errorf("the old window must not drive the line: %+v", s)
	}
	wantReason(t, "attitude", s.AttitudeReason, "No position has been recorded", "connected")
	if strings.Contains(s.AttitudeReason, "8 Hz") {
		t.Errorf("no verdict on this connection yet: %q", s.AttitudeReason)
	}
	wantInt(t, "attitude last sample", s.AttitudeLastSampleS, 2)
}

// Samples that never continue a run (the sensor's timestamps jump more than
// the grid bridges every time) give no rate. That is not silence.
func TestMotionFeedStatusSamplesWithUnusableTimestampsGiveNoRateAndSayWhy(t *testing.T) {
	m, c := connectedAccumulator()
	for i := 0; i < 20; i++ {
		c.advance(100 * time.Millisecond)
		m.AddAttitude(atSec(float64(i)*2), 1, 0)
	}
	m.TakeWindow()
	s := statusAt(m)
	if s.AttitudeHz != nil || s.AttitudeBelowMin {
		t.Errorf("no rate could be measured: %+v", s)
	}
	wantReason(t, "attitude", s.AttitudeReason, "no rate could be measured")
	if strings.Contains(s.AttitudeReason, "seconds") {
		t.Errorf("samples are arriving, so this is not silence: %q", s.AttitudeReason)
	}
}

func TestMotionFeedHandlerReturnsTheStatusTheServerJudged(t *testing.T) {
	m, c := connectedAccumulator()
	feedClocked(m, c, 0, 0, 12, 5, true, true)
	m.TakeWindow()
	code, body := getJSON(t, motionFeedHandler(m), "/api/signalk/motion-feed", "")
	if code != 200 {
		t.Fatal(code, body)
	}
	for _, key := range []string{
		"connected", "min_hz",
		"attitude_hz", "attitude_below_min", "attitude_recorded_ago_s", "attitude_last_sample_s",
		"heave_hz", "heave_below_min", "heave_recorded_ago_s", "heave_last_sample_s",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("response lacks %q: %v", key, body)
		}
	}
	if body["connected"] != true || body["min_hz"] != motionMinRateHz {
		t.Errorf("connected %v, min_hz %v", body["connected"], body["min_hz"])
	}
}

func TestMotionClientMarksTheStreamConnectedWhileItsConnectionLasts(t *testing.T) {
	acc := newMotionAccumulator()
	c := newSignalKMotionClient(acc, "unused.yaml")
	onClose := c.onOpen()
	if !acc.FeedStatus().Connected {
		t.Error("opening the stream must mark it connected")
	}
	onClose()
	if acc.FeedStatus().Connected {
		t.Error("closing the stream must mark it disconnected")
	}
}
