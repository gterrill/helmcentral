package main

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// The motion feed's state for Settings, SignalK: whether the motion stream
// (signalk_motion_stream.go) is open, what rate the recorder measured on its
// last finished window, and how long ago a sample last arrived. The rate is the
// one stored on the track point and the one the 8 Hz gate used (ADR 0295), so
// the line shows what the recording got, not a second measure that could
// disagree with it. Below motionMinRateHz the recorder keeps no snap or
// pounding figures, so the operator needs to see that before a passage, not
// find it missing after. The server judges the rate against that floor and
// sends the verdict and the reasons, so the page holds no copy of the number.

// motionSilence is how long without a sample counts as the feed having stopped.
const motionSilence = 5 * time.Second

// recordedRate is what the recorder last measured for one signal.
type recordedRate struct {
	// hz is the rate stored on the point, nil when the window had samples but no
	// rate could be measured from them.
	hz *float64
	// at is when the recorder took that window; zero before it has taken one
	// with samples. A window with no samples leaves the last one standing.
	at time.Time
}

// motionFeedState is what the motion client and the recorder add to the
// accumulator's own timing (started, lastAttitude, lastHeave).
type motionFeedState struct {
	connected     bool
	everConnected bool
	connectedAt   time.Time
	attitude      recordedRate
	heave         recordedRate
}

// remember keeps the figures of a window the recorder has just taken. A window
// that held no sample of the signal says nothing about its rate.
func (r *recordedRate) remember(samples int, rate *float64, at time.Time) {
	if samples == 0 {
		return
	}
	r.hz, r.at = rate, at
}

// motionFeedStatus is GET /api/signalk/motion-feed. A rate that is not known is
// null, never zero.
type motionFeedStatus struct {
	Connected bool `json:"connected"`

	// Reason says why the stream is not open, and is empty when it is.
	Reason string `json:"reason,omitempty"`

	// MinHz is the slowest rate the recorder keeps snap and pounding figures
	// from.
	MinHz float64 `json:"min_hz"`

	// AttitudeHz and HeaveHz are the rates stored on the recorder's last
	// finished window, rounded once to a tenth for display. BelowMin judges the
	// unrounded rate. RecordedAgoS is the age of that window and LastSampleS the
	// time since a sample last arrived, in seconds on Helmcentral's clock.
	AttitudeHz           *float64 `json:"attitude_hz"`
	AttitudeBelowMin     bool     `json:"attitude_below_min"`
	AttitudeRecordedAgoS *int     `json:"attitude_recorded_ago_s"`
	AttitudeLastSampleS  *int     `json:"attitude_last_sample_s"`
	AttitudeReason       string   `json:"attitude_reason,omitempty"`

	HeaveHz           *float64 `json:"heave_hz"`
	HeaveBelowMin     bool     `json:"heave_below_min"`
	HeaveRecordedAgoS *int     `json:"heave_recorded_ago_s"`
	HeaveLastSampleS  *int     `json:"heave_last_sample_s"`
	HeaveReason       string   `json:"heave_reason,omitempty"`
}

// SetStreamConnected is called by the motion stream when its websocket opens
// and closes. The main stream keeps this state on the shared snapshot
// (markStreamConnected); the motion stream has no snapshot, only this
// accumulator, so it keeps it here.
func (m *motionAccumulator) SetStreamConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.feed.connected = connected
	if connected {
		m.feed.everConnected = true
		m.feed.connectedAt = m.now()
	}
}

// signalStatus is one signal's half of the status.
type signalStatus struct {
	hz       *float64
	below    bool
	recorded *int
	last     *int
	reason   string
}

// FeedStatus reports the stream's state now.
func (m *motionAccumulator) FeedStatus() motionFeedStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	f := &m.feed
	s := motionFeedStatus{Connected: f.connected, MinHz: motionMinRateHz}
	if !f.connected {
		if f.everConnected {
			s.Reason = "Helmcentral is not connected to the SignalK motion stream."
		} else {
			s.Reason = "Helmcentral has never connected to the SignalK motion stream."
		}
		return s
	}
	a := m.signalStatus("attitude", f.attitude, m.lastAttitude, now,
		"Roll sharpness, pounding and sideways g at the helm weren't worked out")
	h := m.signalStatus("heave", f.heave, m.lastHeave, now,
		"Pounding from heave wasn't recorded")
	s.AttitudeHz, s.AttitudeBelowMin, s.AttitudeRecordedAgoS, s.AttitudeLastSampleS, s.AttitudeReason = a.hz, a.below, a.recorded, a.last, a.reason
	s.HeaveHz, s.HeaveBelowMin, s.HeaveRecordedAgoS, s.HeaveLastSampleS, s.HeaveReason = h.hz, h.below, h.recorded, h.last, h.reason
	return s
}

// signalStatus works out one signal's figures and its reason. The reasons are
// checked in the order that tells the operator the most basic fault first: a
// stream that has sent nothing, then one that has gone quiet, then samples
// without a usable rate, then a rate under the floor. The caller holds the lock.
func (m *motionAccumulator) signalStatus(name string, rec recordedRate, lastSample, now time.Time, cost string) signalStatus {
	var out signalStatus
	secs := func(d time.Duration) *int {
		n := int(d.Seconds())
		return &n
	}
	// A window taken before this connection opened describes an earlier one.
	if rec.at.Before(m.feed.connectedAt) {
		rec = recordedRate{}
	}
	if rec.hz != nil {
		shown := math.Round(*rec.hz*10) / 10
		out.hz = &shown
		out.below = *rec.hz < motionMinRateHz
	}
	if !rec.at.IsZero() {
		out.recorded = secs(now.Sub(rec.at))
	}
	if !lastSample.IsZero() {
		out.last = secs(now.Sub(lastSample))
	}

	silence := now.Sub(lastSample)
	switch {
	case lastSample.IsZero() || lastSample.Before(m.feed.connectedAt):
		out.reason = fmt.Sprintf("Connected, but SignalK has sent no %s since the stream opened.", name)
	case silence > motionSilence:
		out.reason = fmt.Sprintf("No %s has arrived for %d seconds.", name, int(silence.Seconds()))
	case rec.at.IsZero():
		out.reason = fmt.Sprintf("No position has been recorded since the motion feed (re)connected, so the %s rate isn't known yet.", name)
	case rec.hz == nil:
		out.reason = fmt.Sprintf("%s samples are arriving, but no rate could be measured: none followed on from the one before, so their timestamps can't be used.", capitalise(name))
	case out.below:
		out.reason = fmt.Sprintf("The last recorded position had %s under %g Hz, so %s.", name, motionMinRateHz, cost)
	}
	return out
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	if startsAcronym(s) {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// motionFeedHandler is GET /api/signalk/motion-feed for the accumulator the
// motion stream feeds.
func motionFeedHandler(acc *motionAccumulator) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, acc.FeedStatus())
	}
}
