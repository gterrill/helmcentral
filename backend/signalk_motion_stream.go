package main

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// The motion stream is a second SignalK websocket, separate from the main one
// (signalk_stream.go), carrying only navigation.attitude and
// navigation.attitude.heave at the rate the sensor sends them.
//
// It is separate because the two streams want opposite things. The main
// stream caps every path at 1 Hz on purpose: nothing it feeds reads faster,
// and each frame costs a read and an unmarshal. A snap and pounding figure
// needs every sample the sensor sends, 10 Hz from the Furuno SCX-20 through the
// YachtDevices gateway, because the derivatives are taken across 1 to 1.5
// seconds of samples and a 1 Hz snapshot has two or three of them. So this
// stream subscribes with policy instant and no period, to two paths of our
// own vessel, and feeds a motionAccumulator instead of the shared snapshot.
// The main stream's subscription is unchanged (ADR 0295).
//
// The client is the main stream's, reconfigured: the same URL and token
// handling, read limit and backoff, with a different subscription and a
// different consumer.

// signalKMotionStreamPath has no sendMeta: the motion stream carries no unit
// or display hint, and the first burst would only be metadata for 400 paths
// it never subscribed to.
const signalKMotionStreamPath = "/signalk/v1/stream?subscribe=none"

const (
	// motionMaxFuture is how far ahead of receipt a sample's timestamp may be.
	// A sample stamped further ahead would hold its channel until the clock
	// caught up, so it is dropped.
	motionMaxFuture = 5 * time.Second
	// motionSourceSilence is how long a signal's chosen source must be quiet
	// before another source may take over.
	motionSourceSilence = 5 * time.Second
	// motionLogRepeat is the least time between two logs of the same problem.
	motionLogRepeat = time.Hour
	// motionLogQuiet is how long a problem must stay away for the next
	// occurrence to count as a new episode.
	motionLogQuiet = 5 * time.Minute
)

func motionSubscription() signalKSubscribeMessage {
	return signalKSubscribeMessage{
		Context: "vessels.self",
		Subscribe: []signalKSubscribeEntry{
			{Path: trackAttitudePath, Policy: "instant"},
			{Path: trackHeavePath, Policy: "instant"},
		},
	}
}

func newSignalKMotionClient(acc *motionAccumulator, settingsPath string) *signalKStreamClient {
	c := newStreamClientBase(settingsPath, signalKMotionStreamPath)
	feed := newMotionFeed(acc, func() time.Time { return time.Now().UTC() })
	c.name = "signalk motion stream"
	c.subscription = motionSubscription
	// A new connection starts the log and the source choice afresh.
	c.onOpen = func() func() {
		feed.reset()
		return func() {}
	}
	c.onDelta = feed.handle
	return c
}

// motionLog logs each problem once per episode. An episode ends when the
// problem has not recurred for motionLogQuiet or the connection restarts, so
// the next one is loud again. A problem that never stops repeats at most
// hourly: the sensor sends ten updates a second.
type motionLog struct {
	mu       sync.Mutex
	now      func() time.Time
	problems map[string]motionProblem
}

// motionProblem is when a problem was last written to the log and when it last
// happened.
type motionProblem struct{ logged, seen time.Time }

// note logs a fixed message once per episode of its own.
func (l *motionLog) note(reason string) {
	l.notef(reason, "%s", reason)
}

// notef is note for a message with details. key names the problem and stays
// the same however the details vary; the message is formatted only when the
// line is logged, since the sensor sends ten updates a second.
func (l *motionLog) notef(key, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.problems == nil {
		l.problems = map[string]motionProblem{}
	}
	now := l.now()
	p, wasLogged := l.problems[key]
	repeat := wasLogged && now.Sub(p.seen) < motionLogQuiet && now.Sub(p.logged) < motionLogRepeat
	p.seen = now
	if !repeat {
		p.logged = now
		log.Printf("signalk motion stream: %s", fmt.Sprintf(format, args...))
	}
	l.problems[key] = p
}

func (l *motionLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.problems = nil
}

// sourceLock holds one signal to the first source seen. A second source on the
// same path (two gateways, a plugin republishing) would interleave two
// independent clocks into one signal. The lock moves only after the chosen
// source has been silent for motionSourceSilence.
type sourceLock struct {
	have     bool
	source   string
	lastRecv time.Time
}

// allows says whether accept would take the source, without changing the lock.
func (s *sourceLock) allows(source string, now time.Time) bool {
	return !s.have || source == s.source || now.Sub(s.lastRecv) > motionSourceSilence
}

func (s *sourceLock) accept(source string, now time.Time) (ok, switched bool) {
	switch {
	case !s.have:
		s.have, s.source, s.lastRecv = true, source, now
		return true, false
	case source == s.source:
		s.lastRecv = now
		return true, false
	case now.Sub(s.lastRecv) > motionSourceSilence:
		s.source, s.lastRecv = source, now
		return true, true
	}
	return false, false
}

// motionFeed feeds attitude and heave updates to an accumulator, stamped with
// the update's own SignalK timestamp. Arrival time is no use: frames reach us
// 13 to 274 ms apart for a sensor that sent them 100 ms apart.
//
// An update with no usable timestamp, one stamped more than motionMaxFuture
// ahead of receipt, a different source from the one chosen, or an attitude
// missing roll or pitch is skipped and logged. Nothing is guessed in its
// place. It is used by one stream goroutine.
type motionFeed struct {
	acc     *motionAccumulator
	now     func() time.Time
	log     motionLog
	att, hv sourceLock
}

func newMotionFeed(acc *motionAccumulator, now func() time.Time) *motionFeed {
	return &motionFeed{acc: acc, now: now, log: motionLog{now: now}}
}

// reset starts the log episodes and the source choice again, as on reconnect.
func (f *motionFeed) reset() {
	f.log.reset()
	f.att, f.hv = sourceLock{}, sourceLock{}
	// The next source may keep a different clock, so neither channel may carry
	// the old one's history.
	f.acc.RestartAttitude()
	f.acc.RestartHeave()
}

func (f *motionFeed) handle(delta signalKDelta) {
	received := f.now()
	for _, update := range delta.Updates {
		var stamp time.Time
		stampOK := false
		if update.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, update.Timestamp); err == nil {
				stamp, stampOK = t, true
			}
		}

		for _, v := range update.Values {
			switch v.Path {
			case trackAttitudePath:
				obj, ok := v.Value.(map[string]any)
				if !ok {
					f.log.note("attitude: navigation.attitude is not an object")
					continue
				}
				roll, rollOK := numericTelemetryValue(obj["roll"])
				pitch, pitchOK := numericTelemetryValue(obj["pitch"])
				if !rollOK || !pitchOK {
					f.log.note("attitude: navigation.attitude lacks roll or pitch")
					continue
				}
				f.feed("attitude", &f.att, update.SourceRef, stamp, stampOK, received, f.acc.RestartAttitude,
					func() sampleOutcome { return f.acc.AddAttitude(stamp, roll*180/math.Pi, pitch*180/math.Pi) })
			case trackHeavePath:
				heave, ok := numericTelemetryValue(v.Value)
				if !ok {
					f.log.note("heave: navigation.attitude.heave is not a number")
					continue
				}
				f.feed("heave", &f.hv, update.SourceRef, stamp, stampOK, received, f.acc.RestartHeave,
					func() sampleOutcome { return f.acc.AddHeave(stamp, heave) })
			}
		}
	}
}

// feed takes one parsed sample of a signal: it checks the timestamp, holds the
// signal to its source (restart runs when the source changes), adds the sample
// and logs a clock that stepped back.
func (f *motionFeed) feed(signal string, lock *sourceLock, source string, stamp time.Time, stampOK bool, received time.Time, restart func(), add func() sampleOutcome) {
	// The lock is asked first, so a foreign source's bad stamps are reported as
	// an ignored source, not as a fault of the chosen signal. It is only taken
	// once the stamp is usable: a frame we cannot use must not choose the source.
	if !lock.allows(source, received) {
		f.log.notef(signal+": source ignored", "%s from %q ignored, locked to %q", signal, source, lock.source)
		return
	}
	if !f.stampUsable(signal, stamp, stampOK, received) {
		return
	}
	_, switched := lock.accept(source, received)
	if switched {
		restart()
		f.log.notef(signal+": source changed", "%s: source changed to %q after the previous one went quiet; filter restarted", signal, source)
	}
	if add() == sampleRestarted {
		f.log.note(signal + ": the sensor's clock stepped back; filter restarted")
	}
}

func (f *motionFeed) stampUsable(signal string, stamp time.Time, ok bool, received time.Time) bool {
	if !ok {
		f.log.note(signal + ": the update has no parseable timestamp")
		return false
	}
	if stamp.After(received.Add(motionMaxFuture)) {
		f.log.note(signal + ": the update is stamped more than 5 s ahead of receipt")
		return false
	}
	return true
}
