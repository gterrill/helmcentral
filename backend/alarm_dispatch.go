package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"strings"
	"sync"
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

	// alarmDispatchQueueWarnDepth is not a cap -- the queue is deliberately
	// unbounded, per the fallback policy: alarm volume is tiny, so blocking
	// recordAlarmEvent (this queue's entire reason to exist) or dropping an
	// event outright are both worse than using a little more memory. Past
	// this depth something is genuinely wrong (every transport down at once
	// during a burst of transitions), and that belongs in the log, loudly.
	alarmDispatchQueueWarnDepth = 20
)

// queuedAlarmDispatch is one alarm transition waiting for alarmDispatcher.run
// to hand it to dispatch.
type queuedAlarmDispatch struct {
	event  alarmEvent
	vessel string
	// watch, when set, makes this a watch report for channels rather than an
	// alarm transition. Reports share the queue so they are delivered in order
	// and never hold up the evaluator.
	watch    *watchReportMessage
	channels []string
}

// alarmDispatcher sends alarm transitions to the configured transports, queuing
// anything that fails so a boat with no internet does not lose alarms.
type alarmDispatcher struct {
	transports func() []notificationTransport
	store      *alarmLogStore
	now        func() time.Time

	// watchDelivered is told when a watch report reaches a channel, so the
	// run can show it was notified. Nil in a dispatcher that records nothing.
	watchDelivered func(watchID, runID, channel string)
	// watchNotSent is told when a report that was due went to no channel at all.
	watchNotSent func(watchID, runID, reason string)

	// queueMu guards queue below. Separate from any lock store or transports
	// use, since enqueue must never wait on whatever run/dispatch happen to be
	// doing right now -- that would defeat the point of queueing at all.
	queueMu sync.Mutex
	queue   []queuedAlarmDispatch
	// wake nudges run when it is blocked waiting for the next item. Buffered
	// by one: a worker already awake will see the new item on its next pass
	// through the queue without needing another nudge.
	wake chan struct{}
}

func newAlarmDispatcher() *alarmDispatcher {
	return &alarmDispatcher{
		transports: func() []notificationTransport { return buildTransports(getAlarmTransports()) },
		store:      globalAlarmLogStore,
		now:        func() time.Time { return time.Now().UTC() },
		wake:       make(chan struct{}, 1),
		// A delivered watch report is noted on its run, so the Watches page
		// can say who was told.
		watchDelivered: func(watchID, runID, channel string) { recordWatchDelivery(watchID, runID, channel) },
		watchNotSent:   func(watchID, runID, reason string) { recordWatchNotSent(watchID, runID, reason) },
	}
}

var globalAlarmDispatcher *alarmDispatcher

// enqueue hands a transition to run for delivery and returns immediately,
// never blocking on a transport. recordAlarmEvent (alarm_service.go) calls
// this instead of dispatch directly so a slow or unreachable transport --
// dispatch runs every configured transport serially under a 15s context, and
// the SignalK transport alone polls for confirmation for most of that --
// never holds up the 1s alarm evaluator tick that produced the event, or the
// bus watcher and ghost-clearing pass that run immediately after it on the
// same tick (evaluateAlarmsOnce, alarm_service.go).
func (d *alarmDispatcher) enqueue(event alarmEvent, vessel string) {
	d.queueMu.Lock()
	d.queue = append(d.queue, queuedAlarmDispatch{event: event, vessel: vessel})
	depth := len(d.queue)
	d.queueMu.Unlock()

	select {
	case d.wake <- struct{}{}:
	default:
	}

	if depth > alarmDispatchQueueWarnDepth {
		log.Printf("alarm dispatch: queue depth %d and still climbing -- delivery is not keeping up with alarm volume", depth)
	}
}

// queueDepth reports how many transitions are waiting for run to deliver.
func (d *alarmDispatcher) queueDepth() int {
	d.queueMu.Lock()
	defer d.queueMu.Unlock()
	return len(d.queue)
}

// popQueued removes and returns the oldest queued transition, if any.
func (d *alarmDispatcher) popQueued() (queuedAlarmDispatch, bool) {
	d.queueMu.Lock()
	defer d.queueMu.Unlock()
	if len(d.queue) == 0 {
		return queuedAlarmDispatch{}, false
	}
	item := d.queue[0]
	d.queue = d.queue[1:]
	return item, true
}

// run is the single worker draining this dispatcher's queue, in order, until
// ctx is cancelled. One worker over one FIFO is deliberate, not an
// oversight: a raise and its clear for the same rule must reach transports in
// the order recordAlarmEvent recorded them, and only a single sequential
// consumer guarantees that regardless of how long delivery for the item
// ahead of it takes. Started from main.go with streamCtx, alongside
// startNotificationDrainer.
//
// The ctx check runs once per item rather than once per drained batch, so a
// shutdown is not held hostage by a long backlog: at most one in-flight
// dispatch call (bounded by dispatch's own 15s context) finishes before this
// returns, and it may return with items still queued -- their count is
// logged rather than delivered, since AGENTS.md's fallback policy calls for
// surfacing a failure to deliver loudly, not silently retrying past process
// lifetime.
func (d *alarmDispatcher) run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			if remaining := d.queueDepth(); remaining > 0 {
				log.Printf("alarm dispatch: stopping with %d queued delivery(s) undelivered", remaining)
			}
			return
		}

		item, ok := d.popQueued()
		if !ok {
			select {
			case <-ctx.Done():
				continue // loop back to the ctx.Err() check above, which logs and returns
			case <-d.wake:
			}
			continue
		}

		if item.watch != nil {
			d.dispatchWatchReport(*item.watch, item.channels)
			continue
		}
		d.dispatch(item.event, item.vessel)
	}
}

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
		// This transition supersedes anything still queued for the same rule.
		// Dropping first means a failure below re-queues only the newest one.
		if dropped, err := d.store.DropQueuedForRule(transport.ID(), event.Rule.ID); err != nil {
			log.Printf("alarm notify: could not drop superseded %s deliveries for %q: %v", transport.ID(), event.Rule.Label, err)
		} else if dropped > 0 {
			log.Printf("alarm notify: dropped %d queued %s delivery(s) for %q, superseded by this %s",
				dropped, transport.ID(), event.Rule.Label, event.Kind)
		}

		if err := transport.Send(ctx, msg); err != nil {
			// Loud, per the fallback policy: a retry that happens silently is
			// indistinguishable from one that never happened.
			log.Printf("alarm notify: %s failed for %q, queued for retry: %v", transport.ID(), event.Rule.Label, err)
			if queueErr := d.store.Enqueue(transport.ID(), event.Rule.ID, payload, d.now().Add(notifyMinBackoff)); queueErr != nil {
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

	// The queue above has always been bounded; alarm_log never was. Anything
	// that can publish a notifications.* path onto the bus could grow that
	// table without limit, so it gets the same treatment on the same sweep.
	if dropped, err := d.store.DropLogOlderThan(now.Add(-alarmLogRetention)); err != nil {
		log.Printf("alarm notify: could not expire alarm log: %v", err)
	} else if dropped > 0 {
		log.Printf("alarm notify: discarded %d alarm log row(s) older than %s", dropped, alarmLogRetention)
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

		if msg.isWatchReport() && transport.ID() == transportSignalK {
			// Never delivered, even if a report was somehow queued for it.
			log.Printf("watch report: discarding a queued %s delivery of %q: a watch report is never published to SignalK", transport.ID(), msg.Label)
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
		if msg.isWatchReport() && msg.Watch != nil {
			d.noteWatchDelivered(*msg.Watch, item.Transport)
		}
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

// enqueueWatchReport hands a watch report to run for delivery to channels and
// returns at once.
func (d *alarmDispatcher) enqueueWatchReport(report watchReportMessage, channels []string) {
	d.queueMu.Lock()
	d.queue = append(d.queue, queuedAlarmDispatch{watch: &report, channels: channels})
	depth := len(d.queue)
	d.queueMu.Unlock()

	select {
	case d.wake <- struct{}{}:
	default:
	}
	if depth > alarmDispatchQueueWarnDepth {
		log.Printf("alarm dispatch: queue depth %d and still climbing -- delivery is not keeping up", depth)
	}
}

func (d *alarmDispatcher) noteWatchNotSent(report watchReportMessage, reason string) {
	if d.watchNotSent != nil {
		d.watchNotSent(report.Watch.ID, report.Run.ID, reason)
	}
}

// supersedeQueuedReports drops the reports of the same watch still queued for a
// transport that the newer one says everything of. A queued report that
// mentions a finding the newer one leaves out stays, and is delivered in order:
// a bilge alarm at 02:00 is not made untrue by a quiet report at 03:00.
func (d *alarmDispatcher) supersedeQueuedReports(transportID, key string, newer watchReportMessage) {
	queued, err := d.store.QueuedForKey(key)
	if err != nil {
		log.Printf("watch report: could not read the queued reports of %q, so none was superseded: %v", newer.Watch.Name, err)
		return
	}
	have := map[string]bool{}
	for _, k := range watchFindingKeys(newer.Findings) {
		have[k] = true
	}
	for _, item := range queued {
		if item.Transport != transportID {
			continue
		}
		var old notificationMessage
		if err := json.Unmarshal(item.Payload, &old); err != nil || old.Watch == nil {
			log.Printf("watch report: a queued %s report of %q is unreadable and was kept: %v", transportID, newer.Watch.Name, err)
			continue
		}
		covered := true
		for _, k := range watchFindingKeys(old.Watch.Findings) {
			if !have[k] {
				covered = false
				break
			}
		}
		if !covered {
			continue
		}
		if err := d.store.DeleteQueued(item.ID); err != nil {
			log.Printf("watch report: could not drop the superseded %s report of %q: %v", transportID, newer.Watch.Name, err)
			continue
		}
		log.Printf("watch report: dropped a queued %s report of %q, superseded by a newer one that says all of it", transportID, newer.Watch.Name)
	}
}

func (d *alarmDispatcher) noteWatchDelivered(report watchReportMessage, channel string) {
	if d.watchDelivered != nil {
		d.watchDelivered(report.Watch.ID, report.Run.ID, channel)
	}
}

// dispatchWatchReport sends a watch report to the channels the watch chose that
// are switched on, and queues each failure for retry. It does not fall back to
// every transport when none matches, and it never uses SignalK. A newer report
// of the same watch supersedes an older one still queued.
func (d *alarmDispatcher) dispatchWatchReport(report watchReportMessage, channels []string) {
	if len(channels) == 0 {
		d.noteWatchNotSent(report, "this watch sends to no channel")
		return
	}
	var selected []notificationTransport
	for _, transport := range selectTransports(d.transports(), channels) {
		if transport.ID() != transportSignalK {
			selected = append(selected, transport)
		}
	}
	if len(selected) == 0 {
		log.Printf("watch report: %q was not sent: none of its channels (%s) is switched on in Settings", report.Title, strings.Join(channels, ", "))
		d.noteWatchNotSent(report, "no channel is switched on for this watch ("+strings.Join(channels, ", ")+")")
		return
	}

	msg := notificationMessage{Kind: notificationKindWatchReport, RuleID: watchQueueKey(report.Watch.ID), Label: report.Title,
		Message: report.Summary, Vessel: report.Vessel, At: d.now(), Watch: &report}
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("watch report: cannot encode %q: %v", report.Title, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key := watchQueueKey(report.Watch.ID)
	for _, transport := range selected {
		d.supersedeQueuedReports(transport.ID(), key, report)
		if err := transport.Send(ctx, msg); err != nil {
			log.Printf("watch report: %s failed for %q, queued for retry: %v", transport.ID(), report.Title, err)
			if queueErr := d.store.Enqueue(transport.ID(), key, payload, d.now().Add(notifyMinBackoff)); queueErr != nil {
				log.Printf("watch report: could not queue %s: %v", transport.ID(), queueErr)
			}
			continue
		}
		log.Printf("watch report: %s delivered %q", transport.ID(), report.Title)
		d.noteWatchDelivered(report, transport.ID())
	}
}
