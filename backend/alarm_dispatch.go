package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"time"
)

const (
	// notifyDrainInterval is how often queued deliveries are retried.
	notifyDrainInterval = 15 * time.Second

	notifyMinBackoff = 30 * time.Second
	notifyMaxBackoff = 30 * time.Minute

	// A delivery still failing after this long is discarded: retrying forever
	// would eventually deliver a day-old alarm as if it were current, which is
	// its own kind of wrong. The drop is logged loudly.
	notifyMaxAge = 24 * time.Hour
)

// alarmDispatcher sends alarm transitions to the configured transports, queuing
// anything that fails so a boat with no internet does not lose alarms.
type alarmDispatcher struct {
	transports func() []notificationTransport
	store      *alarmLogStore
	now        func() time.Time
}

func newAlarmDispatcher() *alarmDispatcher {
	return &alarmDispatcher{
		transports: func() []notificationTransport { return buildTransports(getAlarmTransports()) },
		store:      globalAlarmLogStore,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

var globalAlarmDispatcher *alarmDispatcher

// dispatch attempts immediate delivery and queues each failure for retry.
// Escalations are notified too — that is the point of escalating.
func (d *alarmDispatcher) dispatch(event alarmEvent, vessel string) {
	all := d.transports()
	selected := selectTransports(all, event.Rule.Notify)

	// A bus-sourced event's path is owned by whatever producer raised it, not
	// by Helmcentral. signalKNotifyTransport.Send writes to msg.Path
	// (alarm_notify.go), so sending one through it would make Helmcentral
	// overwrite that producer's own notification object -- destroying its id
	// and status, and writing null over it on clear. Precedent:
	// heartbeatSender.send skips the same transport for the mirror-image
	// reason (alarm_watchdog.go).
	if event.Source == alarmSourceSignalK {
		filtered := selected[:0:0]
		for _, transport := range selected {
			if transport.ID() == transportSignalK {
				continue
			}
			filtered = append(filtered, transport)
		}
		selected = filtered
	}

	if len(selected) == 0 {
		return
	}

	msg := notificationFor(event, vessel, d.now())
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("alarm notify: cannot encode %s: %v", event.Rule.Label, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, transport := range selected {
		if err := transport.Send(ctx, msg); err != nil {
			// Loud, per the fallback policy: a retry that happens silently is
			// indistinguishable from one that never happened.
			log.Printf("alarm notify: %s failed for %q, queued for retry: %v", transport.ID(), event.Rule.Label, err)
			if queueErr := d.store.Enqueue(transport.ID(), payload, d.now().Add(notifyMinBackoff)); queueErr != nil {
				log.Printf("alarm notify: could not queue %s: %v", transport.ID(), queueErr)
			}
			continue
		}
		log.Printf("alarm notify: %s delivered %q", transport.ID(), event.Rule.Label)
	}
}

// drain retries queued deliveries whose backoff has elapsed.
func (d *alarmDispatcher) drain(ctx context.Context) {
	now := d.now()

	if dropped, err := d.store.DropQueuedOlderThan(now.Add(-notifyMaxAge)); err != nil {
		log.Printf("alarm notify: could not expire queue: %v", err)
	} else if dropped > 0 {
		log.Printf("alarm notify: discarded %d notification(s) undeliverable for over %s", dropped, notifyMaxAge)
	}

	due, err := d.store.DueNotifications(now, 20)
	if err != nil {
		log.Printf("alarm notify: could not read queue: %v", err)
		return
	}
	if len(due) == 0 {
		return
	}

	byID := map[string]notificationTransport{}
	for _, transport := range d.transports() {
		byID[transport.ID()] = transport
	}

	for _, item := range due {
		transport, ok := byID[item.Transport]
		if !ok {
			// The transport was disabled while deliveries were pending. Keep
			// them queued rather than dropping: re-enabling should deliver.
			continue
		}

		var msg notificationMessage
		if err := json.Unmarshal(item.Payload, &msg); err != nil {
			log.Printf("alarm notify: discarding unreadable queued item %s: %v", item.ID, err)
			d.store.DeleteQueued(item.ID)
			continue
		}

		if err := transport.Send(ctx, msg); err != nil {
			attempts := item.Attempts + 1
			next := now.Add(notifyBackoffFor(attempts))
			log.Printf("alarm notify: %s retry %d failed for %q, next attempt in %s: %v",
				item.Transport, attempts, msg.Label, notifyBackoffFor(attempts), err)
			d.store.RescheduleQueued(item.ID, attempts, next, err.Error())
			continue
		}

		log.Printf("alarm notify: %s delivered %q from queue after %d attempt(s)", item.Transport, msg.Label, item.Attempts+1)
		d.store.DeleteQueued(item.ID)
	}
}

// notifyBackoffFor doubles from notifyMinBackoff up to notifyMaxBackoff.
func notifyBackoffFor(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	backoff := time.Duration(float64(notifyMinBackoff) * math.Pow(2, float64(attempts-1)))
	if backoff > notifyMaxBackoff || backoff <= 0 {
		return notifyMaxBackoff
	}
	return backoff
}

// startNotificationDrainer retries queued deliveries until ctx is cancelled.
func startNotificationDrainer(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if globalAlarmDispatcher != nil {
				globalAlarmDispatcher.drain(ctx)
			}
		}
	}
}
