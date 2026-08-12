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
// fire on every boot.
func TestWatchdogStaysQuietBeforeTheFirstMessage(t *testing.T) {
	snapshot := newSignalKSnapshot()
	watchdog := newStreamWatchdog(snapshot, time.Minute)

	if _, ok := watchdog.check(alarmNow.Add(time.Hour)); ok {
		t.Fatalf("must not alarm before the stream has ever delivered anything")
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
