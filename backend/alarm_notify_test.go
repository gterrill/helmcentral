package main

import (
	"context"
	"encoding/json"
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
