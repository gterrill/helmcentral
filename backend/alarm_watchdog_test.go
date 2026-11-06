package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func connectedSnapshot(lastMessage time.Time) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	snapshot.setConnected(true)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "environment.depth.belowTransducer", Value: 3.0}}}},
	}, lastMessage)
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// A frozen dashboard looks like a calm boat, which is exactly the failure the
// watchdog exists to catch.
func TestWatchdogRaisesWhenStreamGoesSilent(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.check(alarmNow.Add(10 * time.Second)); ok {
		t.Fatalf("a live stream must not raise")
	}

	event, ok := watchdog.check(alarmNow.Add(2 * time.Minute))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("expected a raise after the silence threshold, got %+v", event)
	}
	if event.Status.State != alarmStateAlarm {
		t.Fatalf("state: got %q, want %q", event.Status.State, alarmStateAlarm)
	}
	if !strings.Contains(event.Status.Message, "No SignalK data") {
		t.Fatalf("message should say what is wrong, got %q", event.Status.Message)
	}
}

func TestWatchdogRaisesWhenDisconnected(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	snapshot.setConnected(false)
	watchdog := newStreamWatchdog(snapshot, time.Hour)

	event, ok := watchdog.check(alarmNow.Add(time.Second))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("a disconnected stream must raise even if the last message is recent, got %+v", event)
	}
}

// Edge-triggered like the rule engine: a persistently dead stream is one alarm,
// not one per tick.
func TestWatchdogRaisesOnlyOnceWhileStreamStaysSilent(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	raises := 0
	for i := 2; i < 10; i++ {
		if event, ok := watchdog.check(alarmNow.Add(time.Duration(i) * time.Minute)); ok && event.Kind == alarmEventRaised {
			raises++
		}
	}

	if raises != 1 {
		t.Fatalf("expected exactly 1 raise across a sustained outage, got %d", raises)
	}
}

func TestWatchdogClearsWhenStreamRecovers(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.check(alarmNow.Add(2 * time.Minute)); !ok {
		t.Fatalf("expected the outage to raise first")
	}

	// A fresh delta arrives.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "environment.depth.belowTransducer", Value: 3.1}}}},
	}, alarmNow.Add(3*time.Minute))

	event, ok := watchdog.check(alarmNow.Add(3 * time.Minute))
	if !ok || event.Kind != alarmEventCleared {
		t.Fatalf("expected a clear once data resumed, got %+v", event)
	}
	if event.Status.State != alarmStateNormal {
		t.Fatalf("cleared state: got %q, want %q", event.Status.State, alarmStateNormal)
	}
}

// A server still starting up has simply not spoken yet; alarming on that would
// fire on every boot. The grace is one silence threshold, counted from the
// watchdog's first look.
func TestWatchdogStaysQuietBeforeTheFirstMessageWithinGrace(t *testing.T) {
	snapshot := newSignalKSnapshot()
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.check(alarmNow); ok {
		t.Fatalf("must not alarm on the first look")
	}
	if _, ok := watchdog.check(alarmNow.Add(30 * time.Second)); ok {
		t.Fatalf("must not alarm inside the startup grace")
	}
}

// After a power cut with SignalK down, nothing ever connects. That is an
// outage, not a slow start.
func TestWatchdogRaisesWhenSignalKNeverConnectsAfterGrace(t *testing.T) {
	snapshot := newSignalKSnapshot()
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	watchdog.check(alarmNow)
	event, ok := watchdog.check(alarmNow.Add(2 * time.Minute))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("expected a raise once the grace passed with no data, got %+v", event)
	}
	if !strings.Contains(event.Status.Message, "not connected") {
		t.Fatalf("message should say it never connected, got %q", event.Status.Message)
	}
}

// Other vessels' AIS keeps the stream busy while the boat's own feed is dead.
func TestWatchdogRaisesWhenOnlyOtherVesselsAreHeard(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.urn:mrn:imo:mmsi:123456789",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 2.0}}}},
	}, alarmNow.Add(100*time.Second))

	event, ok := watchdog.check(alarmNow.Add(2 * time.Minute))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("AIS traffic must not hide a dead own-ship feed, got %+v", event)
	}
}

func TestWatchdogCountsTheSelfContextByItsFullName(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setConnected(true)
	snapshot.setSelfContext("vessels.urn:mrn:imo:mmsi:111111111")
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.urn:mrn:imo:mmsi:111111111",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 2.0}}}},
	}, alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.check(alarmNow.Add(10 * time.Second)); ok {
		t.Fatalf("own-ship data under its full context name must count as live")
	}
}

// ── heartbeat ─────────────────────────────────────────────────────────────────

func TestHeartbeatIsDisabledWhenIntervalIsZero(t *testing.T) {
	sender := &heartbeatSender{interval: 0}

	if sender.due(alarmNow) {
		t.Fatalf("a zero interval must disable the heartbeat")
	}
}

func TestHeartbeatIsDueImmediatelyThenOnInterval(t *testing.T) {
	sender := &heartbeatSender{interval: time.Hour}

	if !sender.due(alarmNow) {
		t.Fatalf("the first heartbeat should go out promptly")
	}

	sender.lastSent = alarmNow
	if sender.due(alarmNow.Add(30 * time.Minute)) {
		t.Fatalf("not due until the interval has elapsed")
	}
	if !sender.due(alarmNow.Add(time.Hour)) {
		t.Fatalf("due once the interval has elapsed")
	}
}

func TestHeartbeatReportsAllClearAndActiveAlarms(t *testing.T) {
	transport := &stubTransport{id: transportWebhook}
	sender := &heartbeatSender{
		dispatcher: &alarmDispatcher{transports: func() []notificationTransport {
			return []notificationTransport{transport}
		}},
		interval: time.Hour,
	}

	sender.send(context.Background(), alarmNow, 0, alarmStateNormal)
	if transport.count() != 1 {
		t.Fatalf("expected a heartbeat to be sent, got %d", transport.count())
	}
}

// The heartbeat's whole meaning is timeliness, so a failure must not be queued:
// delivering yesterday's "still alive" on reconnect is worse than useless.
func TestHeartbeatFailureIsNotQueued(t *testing.T) {
	transport := &stubTransport{id: transportWebhook, fail: true}
	store := newTestAlarmLog(t)
	sender := &heartbeatSender{
		dispatcher: &alarmDispatcher{
			transports: func() []notificationTransport { return []notificationTransport{transport} },
			store:      store,
			now:        func() time.Time { return alarmNow },
		},
		interval: time.Hour,
	}

	sender.send(context.Background(), alarmNow, 0, alarmStateNormal)

	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Fatalf("a failed heartbeat must not be queued for retry, depth %d", depth)
	}
}

// A heartbeat delivered on the boat proves nothing about whether the boat can
// be reached, which is the entire question it exists to answer.
func TestHeartbeatSkipsTheOnBoatSignalKTransport(t *testing.T) {
	signalk := &stubTransport{id: transportSignalK}
	webhook := &stubTransport{id: transportWebhook}
	sender := &heartbeatSender{
		dispatcher: &alarmDispatcher{transports: func() []notificationTransport {
			return []notificationTransport{signalk, webhook}
		}},
		interval: time.Hour,
	}

	sender.send(context.Background(), alarmNow, 0, alarmStateNormal)

	if signalk.count() != 0 {
		t.Fatalf("the on-boat transport must be skipped, got %d sends", signalk.count())
	}
	if webhook.count() != 1 {
		t.Fatalf("off-boat transports must still receive it, got %d", webhook.count())
	}
}

// Resetting the threshold to zero must restore the default. A guard that
// skipped zero left the running watchdog on its previous value while the API
// reported the default — config and behaviour silently disagreeing.
func TestWatchdogSilenceAfterTreatsZeroAsDefault(t *testing.T) {
	if got := watchdogSilenceAfter(watchdogConfig{StreamSilenceSeconds: 0}); got != defaultStreamSilenceSeconds*time.Second {
		t.Fatalf("zero should resolve to the default, got %s", got)
	}
	if got := watchdogSilenceAfter(watchdogConfig{StreamSilenceSeconds: 30}); got != 30*time.Second {
		t.Fatalf("configured value: got %s, want 30s", got)
	}
}

// feedStream delivers one ordinary delta, the way a healthy stream keeps the
// snapshot's lastMessage moving.
func feedStream(snapshot *signalKSnapshot, at time.Time, depth float64) {
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "environment.depth.belowTransducer", Value: depth}}}},
	}, at)
}

// raiseWatchdogNotification puts Helmcentral's own watchdog notification on the
// snapshot in the raised state, the way it arrives back off the bus.
func raiseWatchdogNotification(snapshot *signalKSnapshot, at time.Time) {
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  watchdogPath,
			Value: map[string]any{"state": alarmStateAlarm, "message": "No SignalK data for 8m52s"},
		}}}},
	}, at)
}

// The failure this pins was seen on the boat: a raise queued while SignalK was
// unreachable landed one drain tick after the clear, so the bus kept an alarm
// the watchdog had already decided was over. Because the edge had been taken,
// nothing would ever emit that clear again and the dashboard showed a false
// alarm indefinitely.
func TestWatchdogClearsAStaleNotificationLeftOnTheBus(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	// The stream is healthy and the watchdog agrees, but the bus disagrees.
	raiseWatchdogNotification(snapshot, alarmNow)
	if _, ok := watchdog.check(alarmNow.Add(10 * time.Second)); ok {
		t.Fatalf("a live stream must not produce a transition")
	}

	event, ok := watchdog.reconcile(alarmNow.Add(10 * time.Second))
	if !ok || event.Kind != alarmEventCleared {
		t.Fatalf("a stale notification on the bus must be cleared, got %+v", event)
	}
	if event.Status.State != alarmStateNormal {
		t.Fatalf("state: got %q, want %q", event.Status.State, alarmStateNormal)
	}
}

// Reconciling must not undo the watchdog's own work: while it believes the
// stream is dead, the notification on the bus is correct.
func TestWatchdogDoesNotReconcileAwayALiveAlarm(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.check(alarmNow.Add(2 * time.Minute)); !ok {
		t.Fatalf("expected the outage to raise")
	}
	raiseWatchdogNotification(snapshot, alarmNow)

	if event, ok := watchdog.reconcile(alarmNow.Add(3 * time.Minute)); ok {
		t.Fatalf("a genuinely raised alarm must survive reconciliation, got %+v", event)
	}
}

// A clear travels to the server and back through the delta stream, so the bus
// lags a transition by a round trip. Without a grace period the watchdog would
// keep republishing a clear that is merely in flight.
func TestWatchdogDoesNotReconcileWhileAClearIsStillInFlight(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.check(alarmNow.Add(2 * time.Minute)); !ok {
		t.Fatalf("expected the outage to raise")
	}
	raiseWatchdogNotification(snapshot, alarmNow)

	// Data resumes, the watchdog clears, but the bus has not caught up yet.
	recovered := alarmNow.Add(3 * time.Minute)
	feedStream(snapshot, recovered, 3.1)

	event, ok := watchdog.check(recovered)
	if !ok || event.Kind != alarmEventCleared {
		t.Fatalf("expected the recovery to clear, got %+v", event)
	}

	soon := recovered.Add(watchdogCheckInterval)
	feedStream(snapshot, soon, 3.2)
	if _, ok := watchdog.reconcile(soon); ok {
		t.Fatalf("a clear still in flight must not be republished")
	}

	later := recovered.Add(watchdogReconcileGrace + time.Second)
	feedStream(snapshot, later, 3.3)
	if _, ok := watchdog.reconcile(later); !ok {
		t.Fatalf("past the grace period a bus that still disagrees must be repaired")
	}
}

// Before the first message there is nothing to judge, so a notification found
// on the bus at startup is not yet known to be stale.
func TestWatchdogDoesNotReconcileBeforeTheStreamHasSaidAnything(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setConnected(true)
	snapshot.setSelfContext("vessels.self")
	raiseWatchdogNotification(snapshot, alarmNow)
	// applyDelta stamps the message times, so wind them back to "nothing judged yet".
	snapshot.lastMessage = time.Time{}
	snapshot.lastSelfMessage = time.Time{}

	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.reconcile(alarmNow.Add(time.Hour)); ok {
		t.Fatalf("a stream that has never spoken cannot prove a notification stale")
	}
}

// The watchdog publishes its own alarm as a notification under the boat's
// context, and the server echoes it back. That echo is not instrument data.
func TestWatchdogIgnoresNotificationEchoesOnTheSelfContext(t *testing.T) {
	snapshot := connectedSnapshot(alarmNow)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	raiseWatchdogNotification(snapshot, alarmNow.Add(100*time.Second))

	event, ok := watchdog.check(alarmNow.Add(2 * time.Minute))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("an echoed notification must not count as live instrument data, got %+v", event)
	}
}

func TestWatchdogMessageSaysWhenSignalKIsUpButSilentAboutThisBoat(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setConnected(true)
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	watchdog.check(alarmNow)
	event, ok := watchdog.check(alarmNow.Add(2 * time.Minute))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("expected a raise, got %+v", event)
	}
	if strings.Contains(event.Status.Message, "not connected") {
		t.Fatalf("SignalK is connected, so the message must not say otherwise: %q", event.Status.Message)
	}
	if !strings.Contains(event.Status.Message, "this boat") {
		t.Fatalf("the message should say no data has come from this boat, got %q", event.Status.Message)
	}
}
