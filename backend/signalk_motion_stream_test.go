package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func motionFrameFrom(source string, ts time.Time, rollRad, pitchRad float64, heave *float64) string {
	values := fmt.Sprintf(`{"path":"navigation.attitude","value":{"roll":%g,"pitch":%g,"yaw":1.0}}`, rollRad, pitchRad)
	if heave != nil {
		values += fmt.Sprintf(`,{"path":"navigation.attitude.heave","value":%g}`, *heave)
	}
	return fmt.Sprintf(`{"context":"vessels.urn:mrn:signalk:uuid:abc","updates":[{"timestamp":%q,"$source":%q,"values":[%s]}]}`,
		ts.UTC().Format("2006-01-02T15:04:05.000Z"), source, values)
}

func motionFrame(ts time.Time, rollRad, pitchRad float64, heave *float64) string {
	return motionFrameFrom("YachtDevices.9", ts, rollRad, pitchRad, heave)
}

func parseDelta(t *testing.T, line string) signalKDelta {
	t.Helper()
	var d signalKDelta
	if err := json.Unmarshal([]byte(line), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func testMotionClient(acc *motionAccumulator) *signalKStreamClient {
	client := newSignalKMotionClient(acc, "")
	client.minBackoff = time.Millisecond
	client.maxBackoff = 5 * time.Millisecond
	return client
}

// captureLog collects what the package logs for the length of a test.
func captureMotionLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// The URL the motion client actually dials: no implicit subscription, no meta.
func TestMotionClientDialsTheStreamWithoutImplicitSubscriptionOrMeta(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte("signalk:\n  address: boat.local\n  port: 3000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	streamURL, httpBase, err := newSignalKMotionClient(newMotionAccumulator(), settings).resolveURLs()
	if err != nil {
		t.Fatal(err)
	}
	if streamURL != "ws://boat.local:3000/signalk/v1/stream?subscribe=none" {
		t.Errorf("url %q", streamURL)
	}
	if !strings.HasPrefix(httpBase, "http://boat.local:3000") {
		t.Errorf("http base %q", httpBase)
	}
}

// The motion stream asks for exactly two paths of our own vessel, every update,
// with no rate cap of either kind. The main stream's cap is deliberate; this
// consumer needs every sample.
func TestMotionStreamSubscribesInstantToAttitudeAndHeaveOnly(t *testing.T) {
	received := make(chan string, 1)
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		select {
		case received <- string(data):
		default:
		}
		<-ctx.Done()
	})
	defer stub.close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go testMotionClient(newMotionAccumulator()).connectOnce(ctx, stub.wsURL(), "")

	var raw string
	select {
	case raw = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("no subscription sent")
	}
	var msg struct {
		Context   string `json:"context"`
		Subscribe []struct {
			Path   string `json:"path"`
			Policy string `json:"policy"`
		} `json:"subscribe"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, raw)
	}
	if msg.Context != "vessels.self" {
		t.Errorf("context %q, want vessels.self", msg.Context)
	}
	paths := map[string]bool{}
	for _, e := range msg.Subscribe {
		paths[e.Path] = true
		if e.Policy != "instant" {
			t.Errorf("%s policy %q, want instant", e.Path, e.Policy)
		}
	}
	if len(msg.Subscribe) != 2 || !paths["navigation.attitude"] || !paths["navigation.attitude.heave"] {
		t.Errorf("subscribe entries %+v, want attitude and heave only", msg.Subscribe)
	}
	if strings.Contains(raw, "period") || strings.Contains(raw, "minPeriod") {
		t.Errorf("an instant subscription must carry no period: %q", raw)
	}
}

// The frames arrive in one burst, the way a stalled socket delivers them; the
// sensor's timestamps still say 10 Hz.
func TestMotionStreamFeedsTheAccumulatorFromSourceTimestamps(t *testing.T) {
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_, _, _ = c.Read(ctx) // the subscription
		for i := 0; i < 101; i++ {
			ts := start.Add(time.Duration(i) * 100 * time.Millisecond)
			roll := 5 * math.Pi / 180 * math.Sin(2*math.Pi/3*float64(i)/10)
			heave := 0.3 * math.Sin(2*math.Pi/3*float64(i)/10)
			_ = writeFrame(ctx, c, motionFrame(ts, roll, 0, &heave))
		}
		<-ctx.Done()
	})
	defer stub.close()

	acc := newMotionAccumulator()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go testMotionClient(acc).connectOnce(ctx, stub.wsURL(), "")

	var f motionFigures
	waitFor(t, 2*time.Second, "the burst to be accumulated", func() bool {
		// Take drains the window, so keep the last non-empty result.
		if got := acc.Take(); got.MotionRateHz != nil && *got.MotionRateHz > 0 {
			f = got
			return true
		}
		return false
	})
	if f.MotionRateHz == nil || math.Abs(*f.MotionRateHz-10) > 0.01 {
		t.Errorf("rate %v, want 10 from source timestamps, not burst arrival", f.MotionRateHz)
	}
}

func TestMotionStreamSkipsAFrameWithoutAUsableTimestamp(t *testing.T) {
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_, _, _ = c.Read(ctx)
		_ = writeFrame(ctx, c, `{"context":"vessels.self","updates":[{"values":[{"path":"navigation.attitude","value":{"roll":0.1,"pitch":0.1}}]}]}`)
		_ = writeFrame(ctx, c, `{"context":"vessels.self","updates":[{"timestamp":"garbage","values":[{"path":"navigation.attitude","value":{"roll":0.1,"pitch":0.1}}]}]}`)
		for i := 0; i < 20; i++ {
			_ = writeFrame(ctx, c, motionFrame(start.Add(time.Duration(i)*100*time.Millisecond), 0.01*float64(i), 0, nil))
		}
		<-ctx.Done()
	})
	defer stub.close()

	acc := newMotionAccumulator()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go testMotionClient(acc).connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "good frames to land", func() bool {
		f := acc.Take()
		// 20 good samples span 1.9 s: a rate of exactly 10 means the two bad
		// frames were never counted.
		return f.MotionRateHz != nil && math.Abs(*f.MotionRateHz-10) < 0.01
	})
}

var feedEpoch = time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)

// testFeed is a feed whose receive clock the test holds.
func testFeed(acc *motionAccumulator) (*motionFeed, *time.Time) {
	clock := feedEpoch.Add(time.Hour)
	return newMotionFeed(acc, func() time.Time { return clock }), &clock
}

// Roll and pitch arrive in radians and the accumulator holds degrees.
func TestMotionFeedConvertsRadiansToDegrees(t *testing.T) {
	acc := newMotionAccumulator()
	feed, _ := testFeed(acc)
	for i := 0; i < 101; i++ {
		roll := 5 * math.Pi / 180 * math.Sin(2*math.Pi/3*float64(i)/10)
		feed.handle(parseDelta(t, motionFrame(feedEpoch.Add(time.Duration(i)*100*time.Millisecond), roll, 0, nil)))
	}
	f := acc.Take()
	if f.RollRMSDeg == nil || math.Abs(*f.RollRMSDeg-5/math.Sqrt2) > 0.1 {
		t.Errorf("roll rms %v, want %.2f degrees", f.RollRMSDeg, 5/math.Sqrt2)
	}
}

func TestMotionFeedKeepsOnlyAttitudeWithBothAngles(t *testing.T) {
	acc := newMotionAccumulator()
	feed, _ := testFeed(acc)
	for i := 0; i < 12; i++ {
		line := fmt.Sprintf(`{"updates":[{"timestamp":%q,"values":[{"path":"navigation.attitude","value":{"roll":0.1}}]}]}`,
			feedEpoch.Add(time.Duration(i)*100*time.Millisecond).Format(time.RFC3339Nano))
		feed.handle(parseDelta(t, line))
	}
	if f := acc.Take(); f != (motionFigures{}) {
		t.Errorf("an attitude with no pitch must not be used: %+v", f)
	}
}

// A timestamp far ahead of receipt would hold the channel until the clock
// caught up. It is dropped, and logged.
func TestMotionFeedDropsASampleStampedInTheFuture(t *testing.T) {
	buf := captureMotionLog(t)
	acc := newMotionAccumulator()
	feed, clock := testFeed(acc)
	for i := 0; i < 20; i++ {
		feed.handle(parseDelta(t, motionFrame(feedEpoch.Add(time.Duration(i)*100*time.Millisecond), 0.01*float64(i), 0, nil)))
	}
	feed.handle(parseDelta(t, motionFrame(clock.Add(10*time.Minute), 1, 0, nil)))
	for i := 20; i < 40; i++ {
		feed.handle(parseDelta(t, motionFrame(feedEpoch.Add(time.Duration(i)*100*time.Millisecond), 0.01*float64(i), 0, nil)))
	}
	f := acc.Take()
	if f.MotionRateHz == nil || math.Abs(*f.MotionRateHz-10) > 0.01 {
		t.Errorf("rate %v: the future sample froze or polluted the channel", f.MotionRateHz)
	}
	if !strings.Contains(buf.String(), "ahead of receipt") {
		t.Errorf("a dropped future sample must be logged: %q", buf.String())
	}
}

// Two sources on one path would interleave two clocks. The first seen is kept.
func TestMotionFeedLocksEachSignalToTheFirstSource(t *testing.T) {
	buf := captureMotionLog(t)
	acc := newMotionAccumulator()
	feed, clock := testFeed(acc)
	at := func(i int) time.Time { return feedEpoch.Add(time.Duration(i) * 100 * time.Millisecond) }

	for i := 0; i < 30; i++ {
		feed.handle(parseDelta(t, motionFrameFrom("A.1", at(i), 0.01, 0, nil)))
		*clock = clock.Add(100 * time.Millisecond)
		// B publishes the same path with a clock a few seconds out.
		feed.handle(parseDelta(t, motionFrameFrom("B.2", at(i).Add(3*time.Second), 0.5, 0, nil)))
	}
	f := acc.Take()
	if f.RollRMSDeg == nil || *f.RollRMSDeg > 0.01 {
		t.Errorf("roll rms %v: source B leaked into the signal", f.RollRMSDeg)
	}
	nearFig(t, "rate", f.MotionRateHz, 10, 0.01)

	// A goes quiet for more than 5 s; B takes over, and the switch is logged.
	*clock = clock.Add(6 * time.Second)
	for i := 0; i < 30; i++ {
		feed.handle(parseDelta(t, motionFrameFrom("B.2", at(100+i), 0.5, 0, nil)))
		*clock = clock.Add(100 * time.Millisecond)
	}
	f = acc.Take()
	if f.MotionRateHz == nil {
		t.Error("B must be accepted after A has been silent")
	}
	if !strings.Contains(buf.String(), `attitude from "B.2" ignored, locked to "A.1"`) {
		t.Errorf("an update turned away by the lock must be logged: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "source changed") {
		t.Errorf("a source switch must be logged: %q", buf.String())
	}
}

// A reconnect forgets the source lock, so the channels must start afresh too:
// the new source's clock may be behind the old one's.
func TestMotionFeedResetRestartsTheChannels(t *testing.T) {
	acc := newMotionAccumulator()
	feed, _ := testFeed(acc)
	for i := 0; i < 20; i++ {
		feed.handle(parseDelta(t, motionFrameFrom("A.1", feedEpoch.Add(time.Duration(i)*100*time.Millisecond), 0.01, 0, nil)))
	}
	feed.reset()
	// 1 s behind the last sample: dropped as a duplicate unless the channel restarted.
	feed.handle(parseDelta(t, motionFrameFrom("B.2", feedEpoch.Add(time.Second), 0.01, 0, nil)))
	_, counts := acc.TakeWindow()
	if counts.AttitudeSamples != 21 {
		t.Errorf("attitude samples %d, want 21: the channel kept the old source's history", counts.AttitudeSamples)
	}
}

// A foreign source's bad stamp is an ignored source, not a timestamp fault.
func TestMotionFeedChecksTheSourceLockBeforeTheTimestamp(t *testing.T) {
	buf := captureMotionLog(t)
	acc := newMotionAccumulator()
	feed, _ := testFeed(acc)
	feed.handle(parseDelta(t, motionFrameFrom("A.1", feedEpoch, 0.01, 0, nil)))
	feed.handle(parseDelta(t, `{"updates":[{"timestamp":"garbage","$source":"B.2","values":[{"path":"navigation.attitude","value":{"roll":0.1,"pitch":0.1}}]}]}`))
	if strings.Contains(buf.String(), "no parseable timestamp") {
		t.Errorf("a foreign source's bad stamp was blamed on the signal: %q", buf)
	}
	if !strings.Contains(buf.String(), `attitude from "B.2" ignored`) {
		t.Errorf("want the source reported as ignored: %q", buf)
	}
}

func TestMotionLogRepeatsAtMostHourlyAndPerEpisode(t *testing.T) {
	buf := captureMotionLog(t)
	clock := feedEpoch
	l := motionLog{now: func() time.Time { return clock }}
	count := func() int { return strings.Count(buf.String(), "problem") }

	l.note("problem")
	l.note("problem")
	clock = clock.Add(time.Minute)
	l.note("problem")
	if count() != 1 {
		t.Fatalf("one episode, one line: %d", count())
	}
	// Still going 61 minutes later: the hourly repeat.
	for i := 0; i < 61; i++ {
		clock = clock.Add(time.Minute)
		l.note("problem")
	}
	if count() != 2 {
		t.Errorf("a continuing problem repeats hourly: %d", count())
	}
	// Away for 10 minutes, then back: a new episode logs at once.
	clock = clock.Add(10 * time.Minute)
	l.note("problem")
	if count() != 3 {
		t.Errorf("a recurrence after a quiet spell is a new episode: %d", count())
	}
	// A reconnect starts afresh.
	l.reset()
	l.note("problem")
	if count() != 4 {
		t.Errorf("reset must allow a repeat: %d", count())
	}
}
