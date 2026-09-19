package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"
)

func testMessage() notificationMessage {
	return notificationMessage{
		Kind:    alarmEventRaised,
		RuleID:  "rule-1",
		Label:   "House bank low",
		Path:    "electrical.batteries.house.voltage",
		State:   alarmStateAlarm,
		Message: "House bank low: below 11.8 (11)",
		Value:   11.0,
		Vessel:  "Pikorua",
		At:      alarmNow,
	}
}

type capturedNotifyRequest struct {
	headers http.Header
	body    string
}

func capturingServer(t *testing.T, status int) (*httptest.Server, func() []capturedNotifyRequest) {
	t.Helper()

	var mu sync.Mutex
	var captured []capturedNotifyRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		captured = append(captured, capturedNotifyRequest{headers: r.Header.Clone(), body: string(body)})
		mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	return server, func() []capturedNotifyRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedNotifyRequest{}, captured...)
	}
}

func TestNtfyTransportPostsTitleAndPriority(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	transport := ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat-alarms"}}

	if err := transport.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := requests()
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
	if !strings.Contains(got[0].headers.Get("Title"), "Pikorua") {
		t.Fatalf("title should name the vessel, got %q", got[0].headers.Get("Title"))
	}
	if got[0].headers.Get("Priority") != "4" {
		t.Fatalf("alarm priority: got %q, want 4", got[0].headers.Get("Priority"))
	}
	if got[0].body != testMessage().Message {
		t.Fatalf("body: got %q", got[0].body)
	}
}

// An emergency has to get past a sleeping phone's do-not-disturb, which on ntfy
// means max priority.
func TestNtfyTransportUsesMaxPriorityForEmergency(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	transport := ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat-alarms"}}

	msg := testMessage()
	msg.State = alarmStateEmergency
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := requests()[0].headers.Get("Priority"); got != "5" {
		t.Fatalf("emergency priority: got %q, want 5", got)
	}
}

func TestNtfyTransportReportsNonSuccessStatus(t *testing.T) {
	server, _ := capturingServer(t, http.StatusInternalServerError)
	transport := ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat-alarms"}}

	if err := transport.Send(context.Background(), testMessage()); err == nil {
		t.Fatalf("expected an error on a 500 response")
	}
}

func TestWebhookTransportPostsTheMessageAsJSON(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	transport := webhookTransport{config: webhookConfig{URL: server.URL}}

	if err := transport.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var decoded notificationMessage
	if err := json.Unmarshal([]byte(requests()[0].body), &decoded); err != nil {
		t.Fatalf("webhook body is not valid JSON: %v", err)
	}
	if decoded.RuleID != "rule-1" || decoded.State != alarmStateAlarm {
		t.Fatalf("unexpected webhook payload: %+v", decoded)
	}
}

func TestSMTPTransportBuildsAddressedMessage(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotBody string

	transport := smtpTransport{
		config: smtpConfig{Host: "mail.example.com", Port: 587, From: "boat@example.com", To: []string{"skipper@example.com"}},
		send: func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
			gotAddr, gotFrom, gotTo, gotBody = addr, from, to, string(msg)
			return nil
		},
	}

	if err := transport.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotAddr != "mail.example.com:587" {
		t.Fatalf("addr: got %q", gotAddr)
	}
	if gotFrom != "boat@example.com" || len(gotTo) != 1 || gotTo[0] != "skipper@example.com" {
		t.Fatalf("envelope: from %q to %v", gotFrom, gotTo)
	}
	for _, want := range []string{"Subject: ", "Pikorua", "House bank low", "electrical.batteries.house.voltage"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("message missing %q:\n%s", want, gotBody)
		}
	}
}

// A converted unit (millibars per hour into pascals per second, say) is
// routinely a repeating decimal, and an operator reading an alarm email on
// their phone does not need 17 digits of it.
func TestBuildAlarmEmailRoundsValueToTwoDecimalPlaces(t *testing.T) {
	msg := testMessage()
	msg.Value = -100.0 / 3600.0

	body := string(buildAlarmEmail(smtpConfig{From: "boat@example.com"}, msg))

	if !strings.Contains(body, "Value: -0.03\r\n") {
		t.Fatalf("expected the email body to carry a rounded value, got:\n%s", body)
	}
	if strings.Contains(body, "0.0277") {
		t.Fatalf("email body still carries the unrounded value:\n%s", body)
	}
}

// The same Value line converts into the operator's own unit when the message
// carries one -- a sailor reads mb/hr on their phone, not Pa/s -- matching
// the dashboard card's own conversion (frontend/src/lib/alarm-display.ts,
// mirrored here by alarm_units.go's formatAlarmReading).
func TestBuildAlarmEmailConvertsValueIntoOperatorUnit(t *testing.T) {
	msg := testMessage()
	msg.Value = -0.03
	msg.Unit = "Pa/s"

	body := string(buildAlarmEmail(smtpConfig{From: "boat@example.com"}, msg))

	if !strings.Contains(body, "Value: -1.1 mb/hr\r\n") {
		t.Fatalf("expected the email body to carry the converted value, got:\n%s", body)
	}
}

// SignalK clears a notification by writing null to its path, so a cleared
// alarm must not leave a stale one latched on the bus.
func TestSignalKNotifyTransportWritesNullOnClear(t *testing.T) {
	var gotPath string
	var gotValue any

	transport := signalKNotifyTransport{put: func(path string, value any) error {
		gotPath, gotValue = path, value
		return nil
	}}

	msg := testMessage()
	msg.Kind = alarmEventCleared
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotValue != nil {
		t.Fatalf("clearing must write null, got %v", gotValue)
	}
	if !strings.HasPrefix(gotPath, notificationsRoot+".") {
		t.Fatalf("path should be under notifications, got %q", gotPath)
	}
}

func TestSignalKNotifyTransportWritesStateAndMethod(t *testing.T) {
	var gotValue any
	transport := signalKNotifyTransport{put: func(_ string, value any) error {
		gotValue = value
		return nil
	}}

	if err := transport.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	payload, ok := gotValue.(map[string]any)
	if !ok {
		t.Fatalf("expected a notification object, got %T", gotValue)
	}
	if payload["state"] != alarmStateAlarm {
		t.Fatalf("state: got %v", payload["state"])
	}
	if _, present := payload["method"]; !present {
		t.Fatalf("a notification needs a method so consumers know how to alert")
	}
}

// TestPublishSignalKNotification_ReachableWithNoUserSessionPresent is the
// critical regression guard from docs/adr/0040: the alarm/notification path
// must keep working with every browser closed and nobody logged in, because
// it authenticates with Helmcentral's own service account
// (SIGNALK_USERNAME/SIGNALK_PASSWORD -> acquireSignalKToken/skTokenCache),
// never with a user session. This test deliberately never creates a
// sessionStore, never sets a session cookie, and never touches
// sessionCookieName anywhere — there is no session store or user cookie
// involved in the entire path being exercised, which is the point: nothing
// about this call has anything to authorize against but the service
// account, so it cannot depend on a session that doesn't exist here.
//
// The mechanism moved from a REST write to a delta publish
// (signalk_publish.go), but the guarantee is unchanged and is the reason this
// test survived the move.
func TestPublishSignalKNotification_ReachableWithNoUserSessionPresent(t *testing.T) {
	stub := newPublishStub(t)
	withServiceAccount(t, stub)

	err := publishSignalKNotification("notifications.anchor.drag", map[string]any{
		"state":   alarmStateAlarm,
		"message": "anchor drag detected",
		"method":  []string{"visual", "sound"},
	})
	if err != nil {
		t.Fatalf("publishSignalKNotification: expected success with no user session present, got: %v", err)
	}

	if skTokenCache == nil {
		t.Fatal("expected the service-account token cache to be populated by this call")
	}
}

func TestSelectTransportsFallsBackToAllWhenRuleNamesNone(t *testing.T) {
	all := []notificationTransport{
		webhookTransport{config: webhookConfig{URL: "http://a"}},
		ntfyTransport{config: ntfyConfig{Server: "http://b", Topic: "t"}},
	}

	if got := selectTransports(all, nil); len(got) != 2 {
		t.Fatalf("a rule naming no transports should reach all of them, got %d", len(got))
	}
	if got := selectTransports(all, []string{transportNtfy}); len(got) != 1 || got[0].ID() != transportNtfy {
		t.Fatalf("expected only ntfy, got %+v", got)
	}
}

func TestValidateAlarmTransportsRejectsIncompleteConfig(t *testing.T) {
	cases := []struct {
		name   string
		config alarmTransportConfig
	}{
		{"ntfy without topic", alarmTransportConfig{Ntfy: ntfyConfig{Enabled: true}}},
		{"smtp without host", alarmTransportConfig{SMTP: smtpConfig{Enabled: true, Port: 587, From: "a@b.c", To: []string{"d@e.f"}}}},
		{"smtp without recipients", alarmTransportConfig{SMTP: smtpConfig{Enabled: true, Host: "h", Port: 587, From: "a@b.c"}}},
		{"smtp bad port", alarmTransportConfig{SMTP: smtpConfig{Enabled: true, Host: "h", Port: 0, From: "a@b.c", To: []string{"d@e.f"}}}},
		{"webhook without url", alarmTransportConfig{Webhook: webhookConfig{Enabled: true}}},
		{"webhook non-http url", alarmTransportConfig{Webhook: webhookConfig{Enabled: true, URL: "ftp://x"}}},
	}

	for _, tc := range cases {
		config := tc.config
		if err := validateAlarmTransports(&config); err == nil {
			t.Fatalf("%s: expected a validation error", tc.name)
		}
	}
}

func TestValidateAlarmTransportsDefaultsNtfyServer(t *testing.T) {
	config := alarmTransportConfig{Ntfy: ntfyConfig{Enabled: true, Topic: "boat"}}

	if err := validateAlarmTransports(&config); err != nil {
		t.Fatalf("validateAlarmTransports: %v", err)
	}
	if config.Ntfy.Server != defaultNtfyServer {
		t.Fatalf("server should default to %q, got %q", defaultNtfyServer, config.Ntfy.Server)
	}
}

// A disabled transport must not be validated into failure — operators half-fill
// a form all the time, and refusing to save it is not helpful.
func TestValidateAlarmTransportsIgnoresDisabledSections(t *testing.T) {
	config := alarmTransportConfig{
		Ntfy:    ntfyConfig{Enabled: false},
		SMTP:    smtpConfig{Enabled: false, Port: 0},
		Webhook: webhookConfig{Enabled: false, URL: ""},
	}

	if err := validateAlarmTransports(&config); err != nil {
		t.Fatalf("disabled sections must not block a save: %v", err)
	}
}

func TestNotifyBackoffGrowsAndCaps(t *testing.T) {
	if got := notifyBackoffFor(1); got != notifyMinBackoff {
		t.Fatalf("first retry: got %s, want %s", got, notifyMinBackoff)
	}
	if got := notifyBackoffFor(2); got != 2*notifyMinBackoff {
		t.Fatalf("second retry: got %s, want %s", got, 2*notifyMinBackoff)
	}
	if got := notifyBackoffFor(50); got != notifyMaxBackoff {
		t.Fatalf("backoff must cap at %s, got %s", notifyMaxBackoff, got)
	}
}

// ── dispatcher ────────────────────────────────────────────────────────────────

type stubTransport struct {
	id     string
	mu     sync.Mutex
	fail   bool
	sent   int
	lastAt time.Time
}

func (s *stubTransport) ID() string { return s.id }

func (s *stubTransport) Send(context.Context, notificationMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return io.ErrUnexpectedEOF
	}
	s.sent++
	s.lastAt = time.Now()
	return nil
}

func (s *stubTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}

func (s *stubTransport) setFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
}

func testDispatcher(t *testing.T, transport notificationTransport) (*alarmDispatcher, *alarmLogStore) {
	t.Helper()
	store := newTestAlarmLog(t)
	return &alarmDispatcher{
		transports: func() []notificationTransport { return []notificationTransport{transport} },
		store:      store,
		now:        func() time.Time { return alarmNow },
		// Matches newAlarmDispatcher's own construction: dispatch and drain
		// (exercised by most tests below) never touch this, but enqueue/run
		// (the queue tests further down) do, and a nil channel there would
		// make run wait forever rather than see a newly queued item.
		wake: make(chan struct{}, 1),
	}, store
}

func raisedEvent() alarmEvent {
	rule := validRule()
	rule.ID = "rule-1"
	return alarmEvent{
		Kind:   alarmEventRaised,
		Rule:   rule,
		Status: alarmStatus{RuleID: "rule-1", State: alarmStateAlarm, Message: "House bank low"},
	}
}

func TestDispatcherDeliversImmediatelyAndQueuesNothing(t *testing.T) {
	transport := &stubTransport{id: transportWebhook}
	dispatcher, store := testDispatcher(t, transport)

	dispatcher.dispatch(raisedEvent(), "Pikorua")

	if transport.count() != 1 {
		t.Fatalf("expected one delivery, got %d", transport.count())
	}
	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Fatalf("a successful send must not queue, depth %d", depth)
	}
}

// The point of the queue: a boat with no internet must not lose alarms.
func TestDispatcherQueuesFailedDeliveryAndDrainsItLater(t *testing.T) {
	transport := &stubTransport{id: transportWebhook, fail: true}
	dispatcher, store := testDispatcher(t, transport)

	dispatcher.dispatch(raisedEvent(), "Pikorua")

	depth, _ := store.QueueDepth()
	if depth != 1 {
		t.Fatalf("a failed send must be queued, depth %d", depth)
	}

	// Internet comes back, and the backoff has elapsed.
	transport.setFail(false)
	dispatcher.now = func() time.Time { return alarmNow.Add(notifyMinBackoff + time.Second) }
	dispatcher.drain(context.Background())

	if transport.count() != 1 {
		t.Fatalf("expected the queued notification to be delivered, got %d", transport.count())
	}
	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Fatalf("a delivered notification must leave the queue, depth %d", depth)
	}
}

func TestDispatcherDoesNotRetryBeforeBackoffElapses(t *testing.T) {
	transport := &stubTransport{id: transportWebhook, fail: true}
	dispatcher, _ := testDispatcher(t, transport)

	dispatcher.dispatch(raisedEvent(), "Pikorua")
	transport.setFail(false)

	// Same instant: the first retry is not due yet.
	dispatcher.drain(context.Background())

	if transport.count() != 0 {
		t.Fatalf("retry fired before its backoff elapsed")
	}
}

// Retrying forever would eventually deliver a day-old alarm as if it were
// current, so stale deliveries are discarded.
func TestDispatcherDiscardsDeliveriesOlderThanTheMaxAge(t *testing.T) {
	transport := &stubTransport{id: transportWebhook, fail: true}
	dispatcher, store := testDispatcher(t, transport)

	dispatcher.dispatch(raisedEvent(), "Pikorua")
	if depth, _ := store.QueueDepth(); depth != 1 {
		t.Fatalf("expected the failure to queue")
	}

	dispatcher.now = func() time.Time { return time.Now().UTC().Add(notifyMaxAge + time.Hour) }
	dispatcher.drain(context.Background())

	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Fatalf("expected the stale delivery to be discarded, depth %d", depth)
	}
}

// signalKNotifyTransport.Send writes to msg.Path (alarm_notify.go), which for
// a bus-sourced event is the path another producer owns. Dispatching it
// through the SignalK transport would make Helmcentral overwrite Victron's
// own notification object -- destroying its id and status, and writing null
// over it on clear. Precedent: heartbeatSender.send skips the same transport
// for the mirror-image reason (alarm_watchdog.go).
func TestDispatcherSkipsSignalKTransportForBusSourcedEvents(t *testing.T) {
	signalk := &stubTransport{id: transportSignalK}
	webhook := &stubTransport{id: transportWebhook}
	dispatcher, _ := testDispatcher(t, signalk)
	dispatcher.transports = func() []notificationTransport {
		return []notificationTransport{signalk, webhook}
	}

	event := raisedEvent()
	event.Source = alarmSourceSignalK
	dispatcher.dispatch(event, "Pikorua")

	if signalk.count() != 0 {
		t.Fatalf("a bus-sourced event must never reach the SignalK transport, got %d sends", signalk.count())
	}
	if webhook.count() != 1 {
		t.Fatalf("other transports must still receive it, got %d", webhook.count())
	}
}

// A rule-sourced event (empty Source) is Helmcentral's own alarm and must
// still reach the SignalK transport -- that publish is the whole point of
// ADR 0038's addendum.
func TestDispatcherStillSendsRuleSourcedEventsToSignalKTransport(t *testing.T) {
	signalk := &stubTransport{id: transportSignalK}
	dispatcher, _ := testDispatcher(t, signalk)

	dispatcher.dispatch(raisedEvent(), "Pikorua")

	if signalk.count() != 1 {
		t.Fatalf("a rule-sourced event must still reach the SignalK transport, got %d sends", signalk.count())
	}
}

// Disabling a transport while deliveries are pending must not throw them away:
// re-enabling it should still deliver.
func TestDispatcherKeepsQueuedItemsForADisabledTransport(t *testing.T) {
	transport := &stubTransport{id: transportWebhook, fail: true}
	dispatcher, store := testDispatcher(t, transport)

	dispatcher.dispatch(raisedEvent(), "Pikorua")

	dispatcher.transports = func() []notificationTransport { return nil }
	dispatcher.now = func() time.Time { return alarmNow.Add(notifyMinBackoff + time.Second) }
	dispatcher.drain(context.Background())

	if depth, _ := store.QueueDepth(); depth != 1 {
		t.Fatalf("queued items must survive a disabled transport, depth %d", depth)
	}
}

// Web push carries no operator-supplied configuration at all: the keypair is
// generated into the secrets store and the devices live in their own SQLite
// file, so `enabled` is the whole of it. This pins that as a decision rather
// than an oversight — an empty validation branch would be ceremony.
func TestValidateAlarmTransportsAcceptsWebPushWithNoConfiguration(t *testing.T) {
	config := alarmTransportConfig{WebPush: webPushConfig{Enabled: true}}

	if err := validateAlarmTransports(&config); err != nil {
		t.Fatalf("web push has nothing to validate: %v", err)
	}
}

func TestBuildTransportsIncludesWebPushWhenEnabled(t *testing.T) {
	transports := buildTransports(alarmTransportConfig{WebPush: webPushConfig{Enabled: true}})

	if len(transports) != 1 || transports[0].ID() != transportWebPush {
		t.Fatalf("expected only the web push transport, got %+v", transports)
	}

	if got := buildTransports(alarmTransportConfig{}); len(got) != 0 {
		t.Fatalf("a disabled web push transport must not be built, got %+v", got)
	}
}

func clearedEvent() alarmEvent {
	rule := validRule()
	rule.ID = "rule-1"
	return alarmEvent{
		Kind:   alarmEventCleared,
		Rule:   rule,
		Status: alarmStatus{RuleID: "rule-1", State: alarmStateNormal, Message: "House bank recovered"},
	}
}

// The failure this pins was observed on the boat: the stream watchdog raised
// while SignalK was unreachable, so the raise queued; the stream came back, the
// clear went out, and one drain tick later the queued raise landed on top of it
// and left a false alarm on the bus that nothing would ever clear again.
//
// A transition is a statement about a rule's state *now*, so a newer one makes
// every older queued one obsolete, whatever the transport.
func TestDispatcherDropsAQueuedTransitionSupersededByANewerOne(t *testing.T) {
	transport := &stubTransport{id: transportSignalK, fail: true}
	dispatcher, store := testDispatcher(t, transport)

	dispatcher.dispatch(raisedEvent(), "Pikorua")
	if depth, _ := store.QueueDepth(); depth != 1 {
		t.Fatalf("a failed raise must be queued, depth %d", depth)
	}

	// The stream recovers and the clear is delivered.
	transport.setFail(false)
	dispatcher.dispatch(clearedEvent(), "Pikorua")

	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Fatalf("the clear supersedes the queued raise, depth %d", depth)
	}

	// Past the backoff, nothing stale may be redelivered on top of the clear.
	dispatcher.now = func() time.Time { return alarmNow.Add(notifyMinBackoff + time.Second) }
	dispatcher.drain(context.Background())

	if transport.count() != 1 {
		t.Fatalf("only the clear may reach the transport, got %d sends", transport.count())
	}
}

// Superseding is per rule: an unrelated alarm still waiting on the queue is
// nobody's business but its own.
func TestDispatcherLeavesQueuedTransitionsForOtherRulesAlone(t *testing.T) {
	transport := &stubTransport{id: transportWebhook, fail: true}
	dispatcher, store := testDispatcher(t, transport)

	dispatcher.dispatch(raisedEvent(), "Pikorua")

	other := raisedEvent()
	other.Rule.ID = "rule-2"
	other.Status.RuleID = "rule-2"
	dispatcher.dispatch(other, "Pikorua")

	if depth, _ := store.QueueDepth(); depth != 2 {
		t.Fatalf("two rules must hold two queued deliveries, depth %d", depth)
	}
}

// ── in-memory dispatch queue + worker (backend perf audit #9) ──────────────

// orderRecordingTransport records the Kind of each message it receives, in
// delivery order, so a test can pin the order multiple enqueued transitions
// actually reach the transport in.
type orderRecordingTransport struct {
	id string

	mu    sync.Mutex
	order []string
}

func (o *orderRecordingTransport) ID() string { return o.id }

func (o *orderRecordingTransport) Send(_ context.Context, msg notificationMessage) error {
	if msg.Kind == alarmEventRaised {
		// The raise is deliberately the slower delivery. With one worker
		// draining one FIFO, the clear (enqueued second) cannot reach the
		// transport before the raise no matter how long the raise's own Send
		// takes -- there is nothing else running concurrently that could
		// reorder them.
		time.Sleep(20 * time.Millisecond)
	}
	o.mu.Lock()
	o.order = append(o.order, msg.Kind)
	o.mu.Unlock()
	return nil
}

func (o *orderRecordingTransport) received() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.order...)
}

// TestAlarmDispatcherRunDeliversQueuedTransitionsInFIFOOrder is the ordering
// half of #9's requirements: a raise and its clear for the same rule must
// reach the transport in the order they were recorded, even when the first
// of the two is the slower delivery.
func TestAlarmDispatcherRunDeliversQueuedTransitionsInFIFOOrder(t *testing.T) {
	transport := &orderRecordingTransport{id: transportWebhook}
	dispatcher, _ := testDispatcher(t, transport)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.run(ctx)

	dispatcher.enqueue(raisedEvent(), "Pikorua")
	dispatcher.enqueue(clearedEvent(), "Pikorua")

	waitFor(t, 2*time.Second, "both queued deliveries to reach the transport", func() bool {
		return len(transport.received()) == 2
	})

	got := transport.received()
	if got[0] != alarmEventRaised || got[1] != alarmEventCleared {
		t.Fatalf("delivery order = %v, want [%s %s]", got, alarmEventRaised, alarmEventCleared)
	}
}

// TestAlarmDispatcherEnqueueDoesNotBlockOnASlowTransport is the non-blocking
// half of #9: enqueue must return immediately regardless of how long
// delivery for an item already in the queue takes.
func TestAlarmDispatcherEnqueueDoesNotBlockOnASlowTransport(t *testing.T) {
	release := make(chan struct{})
	transport := &blockingUntilReleasedTransport{id: transportWebhook, release: release}
	dispatcher, _ := testDispatcher(t, transport)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.run(ctx)

	// Occupy the worker with an item whose delivery will not complete until
	// this test releases it below.
	dispatcher.enqueue(raisedEvent(), "Pikorua")
	waitFor(t, time.Second, "the worker to start delivering the first item", func() bool {
		return transport.started()
	})

	started := time.Now()
	other := raisedEvent()
	other.Rule.ID = "rule-2"
	dispatcher.enqueue(other, "Pikorua")
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("enqueue took %s while the worker was busy; it must never block", elapsed)
	}

	close(release)
	waitFor(t, 2*time.Second, "both deliveries to complete once released", func() bool {
		return transport.count() >= 2
	})
}

// blockingUntilReleasedTransport blocks every Send until release is closed,
// recording that at least one Send has begun so a test can wait for the
// worker to be busy rather than guessing with a sleep.
type blockingUntilReleasedTransport struct {
	id      string
	release chan struct{}

	mu      sync.Mutex
	begun   bool
	sentN   int
}

func (b *blockingUntilReleasedTransport) ID() string { return b.id }

func (b *blockingUntilReleasedTransport) Send(ctx context.Context, _ notificationMessage) error {
	b.mu.Lock()
	b.begun = true
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}

	b.mu.Lock()
	b.sentN++
	b.mu.Unlock()
	return nil
}

func (b *blockingUntilReleasedTransport) started() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.begun
}

func (b *blockingUntilReleasedTransport) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sentN
}

// slowFixedDelayTransport sleeps a fixed duration on every Send and counts
// completed deliveries, so a test can bound how much of a backlog run drains
// before it notices ctx cancellation.
type slowFixedDelayTransport struct {
	id    string
	delay time.Duration

	mu   sync.Mutex
	sent int
}

func (s *slowFixedDelayTransport) ID() string { return s.id }

func (s *slowFixedDelayTransport) Send(_ context.Context, _ notificationMessage) error {
	time.Sleep(s.delay)
	s.mu.Lock()
	s.sent++
	s.mu.Unlock()
	return nil
}

func (s *slowFixedDelayTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}

// TestAlarmDispatcherRunStopsWithoutDrainingTheEntireBacklog pins the
// shutdown contract: run checks ctx between items, not only before or after
// a full drain, so a long backlog does not hold up shutdown -- it is
// expected to leave items undelivered and log how many, rather than forcing
// every queued item through first.
func TestAlarmDispatcherRunStopsWithoutDrainingTheEntireBacklog(t *testing.T) {
	transport := &slowFixedDelayTransport{id: transportWebhook, delay: 20 * time.Millisecond}
	dispatcher, _ := testDispatcher(t, transport)

	const backlog = 50 // 50 * 20ms = 1s to fully drain
	for i := 0; i < backlog; i++ {
		event := raisedEvent()
		event.Rule.ID = fmt.Sprintf("rule-%d", i)
		event.Status.RuleID = event.Rule.ID
		dispatcher.enqueue(event, "Pikorua")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		dispatcher.run(ctx)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond) // let a handful of items through, nowhere near all 50
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after ctx cancellation")
	}

	sent := transport.count()
	if sent >= backlog {
		t.Fatalf("run drained the entire %d-item backlog instead of stopping early after cancellation, sent %d", backlog, sent)
	}
	if remaining := dispatcher.queueDepth(); remaining == 0 {
		t.Fatalf("expected items to remain queued after an early cancellation, got 0")
	}
}

// Retention only matters if something calls it. K-2 from the 2026-09-19
// security audit added DropLogOlderThan; this asserts drain actually runs it,
// so an unbounded alarm_log cannot come back by the store method quietly
// losing its only caller.
func TestDispatcherDrainExpiresOldAlarmLogRows(t *testing.T) {
	transport := &stubTransport{id: transportWebhook}
	dispatcher, store := testDispatcher(t, transport)

	store.RecordRaised(raisedEntry("rule-ancient", alarmNow.Add(-2*alarmLogRetention)))
	store.RecordRaised(raisedEntry("rule-recent", alarmNow))

	dispatcher.drain(context.Background())

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected drain to expire the row past retention, got %d row(s)", len(entries))
	}
	if entries[0].RuleID != "rule-recent" {
		t.Fatalf("drain expired the wrong row, kept %q", entries[0].RuleID)
	}
}
