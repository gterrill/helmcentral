package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"
)

// How a watch report is delivered (ADR 0227, Notifications): its own message
// kind through the alarm transports and their retry queue, never SignalK.

const testLinkAddress = "http://boat.local:9091"

func testWatchMessage(t *testing.T) notificationMessage {
	t.Helper()
	review := "All quiet. Nothing else to add."
	w := savedWatch{ID: "w1", Name: "Overnight", Schedule: "0 * * * *"}
	run := watchRun{ID: "r1", WatchID: "w1", Outcome: watchOutcomeRan, BoatTime: "Thu 29 Oct 03:00 UTC+12", Review: &review,
		Report:   []byte(`{"kind":"sections","boat_time":"x","sections":[{"name":"power","title":"Power","lines":["House bank 13.1 V"]}]}`),
		Findings: []watchFinding{notifyRuleFinding("power", "overnight_draw", "Drew a lot.")}}
	m := buildWatchReportMessage(w, run, "Pikorua", testLinkAddress)
	return notificationMessage{Kind: notificationKindWatchReport, RuleID: watchQueueKey("w1"), Label: m.Title, Message: m.Summary,
		Vessel: "Pikorua", At: alarmNow, Link: m.Link, Watch: &m}
}

func TestWatchReport_NtfyIsNormalPriorityWithAReportTag(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	transport := ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}
	if err := transport.Send(context.Background(), testWatchMessage(t)); err != nil {
		t.Fatal(err)
	}
	got := requests()[0]
	if got.headers.Get("Title") != "Overnight: 1 finding" || got.headers.Get("Priority") != "3" {
		t.Errorf("title %q priority %q", got.headers.Get("Title"), got.headers.Get("Priority"))
	}
	if tags := got.headers.Get("Tags"); tags != "clipboard" {
		t.Errorf("tags %q", tags)
	}
	if !strings.HasPrefix(got.body, "Drew a lot.\nAll quiet.") || strings.Contains(got.body, "Nothing else") {
		t.Errorf("body %q", got.body)
	}
	if click := got.headers.Get("Click"); click != "http://boat.local:9091/watches/w1?run=r1" {
		t.Errorf("click %q", click)
	}
}

func TestWatchReport_NtfyHasNoClickWithoutAnAddress(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	msg := testWatchMessage(t)
	msg.Link = ""
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if click := requests()[0].headers.Get("Click"); click != "" {
		t.Errorf("click %q", click)
	}
}

func TestWatchReport_TitleCannotInjectHeaders(t *testing.T) {
	msg := testWatchMessage(t)
	msg.Label = "Night\r\nBcc: attacker@example.com\tcheck"
	msg.Watch.Title = msg.Label

	server, requests := capturingServer(t, http.StatusOK)
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	title := requests()[0].headers.Get("Title")
	if strings.ContainsAny(title, "\r\n\t") || requests()[0].headers.Get("Bcc") != "" {
		t.Errorf("title %q", title)
	}

	var body string
	mail := smtpTransport{
		config: smtpConfig{Host: "h", Port: 25, From: "boat@example.com", To: []string{"me@example.com"}},
		send:   func(_ string, _ smtp.Auth, _ string, _ []string, m []byte) error { body = string(m); return nil },
	}
	if err := mail.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	head := strings.SplitN(body, "\r\n\r\n", 2)[0]
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("a header was injected:\n%q", head)
		}
	}
	if strings.Count(head, "\r\n") != 4 {
		t.Errorf("one subject line and no extra header:\n%q", head)
	}
}

func TestWatchReport_NonASCIITitlesAreEncodedInHeaders(t *testing.T) {
	msg := testWatchMessage(t)
	msg.Label = "Nuit à bord: 1 finding"
	msg.Watch.Title = msg.Label

	server, requests := capturingServer(t, http.StatusOK)
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if title := requests()[0].headers.Get("Title"); !strings.HasPrefix(title, "=?UTF-8?") {
		t.Errorf("ntfy title %q", title)
	}
	var body string
	mail := smtpTransport{
		config: smtpConfig{Host: "h", Port: 25, From: "boat@example.com", To: []string{"me@example.com"}},
		send:   func(_ string, _ smtp.Auth, _ string, _ []string, m []byte) error { body = string(m); return nil },
	}
	if err := mail.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "Subject: =?utf-8?") && !strings.Contains(body, "Subject: =?UTF-8?") {
		t.Errorf("subject:\n%s", body)
	}
}

func TestWatchReport_WebhookSendsItsOwnPayloadNotTheAlarmOne(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	if err := (webhookTransport{config: webhookConfig{URL: server.URL}}).Send(context.Background(), testWatchMessage(t)); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(requests()[0].body), &got); err != nil {
		t.Fatal(err)
	}
	if got["kind"] != "watch_report" || got["vessel"] != "Pikorua" || got["path"] != "/watches/w1?run=r1" || got["link"] != "http://boat.local:9091/watches/w1?run=r1" {
		t.Errorf("%v", got)
	}
	for _, k := range []string{"rule_id", "state", "value", "label"} {
		if _, has := got[k]; has {
			t.Errorf("an alarm field %q in a watch report", k)
		}
	}
	watch := got["watch"].(map[string]any)
	run := got["run"].(map[string]any)
	if watch["id"] != "w1" || watch["name"] != "Overnight" || watch["schedule_sentence"] != "Every hour" || run["id"] != "r1" || run["outcome"] != "ran" {
		t.Errorf("%v %v", watch, run)
	}
	if len(got["findings"].([]any)) != 1 || got["review"] == nil || got["report"].(map[string]any)["sections"] == nil {
		t.Errorf("findings, review and report are data: %v", got)
	}
}

func TestWatchReport_EmailSubjectIsTheTitleAndBodyTheWholeReport(t *testing.T) {
	var body string
	transport := smtpTransport{
		config: smtpConfig{Host: "h", Port: 25, From: "boat@example.com", To: []string{"me@example.com"}},
		send:   func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error { body = string(msg); return nil },
	}
	if err := transport.Send(context.Background(), testWatchMessage(t)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Subject: Overnight: 1 finding\r\n", "Drew a lot.", "House bank 13.1 V", "Nothing else to add.", "http://boat.local:9091/watches/w1?run=r1"} {
		if !strings.Contains(body, want) {
			t.Errorf("email lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Path:") || strings.Contains(body, "State:") {
		t.Errorf("no alarm footer:\n%s", body)
	}
}

func TestWatchReport_WebPushCarriesTheShortMessageAndOpensTheRun(t *testing.T) {
	p := webPushPayloadFor(testWatchMessage(t))
	if p.Title != "Overnight: 1 finding" || p.Body != "Drew a lot.\nAll quiet." || p.URL != "/watches/w1?run=r1" || p.Kind != notificationKindWatchReport || p.Tag != "watch:w1" {
		t.Errorf("%+v", p)
	}
	urgency, _ := webPushUrgencyFor(testWatchMessage(t))
	if urgency != "normal" {
		t.Errorf("urgency %q", urgency)
	}
	long := testWatchMessage(t)
	long.Message = strings.Repeat("x", webPushBodyLimit+50)
	if got := webPushPayloadFor(long).Body; len([]rune(got)) != webPushBodyLimit {
		t.Errorf("the body is cut to the push limit, got %d", len([]rune(got)))
	}
}

func TestWatchReport_SignalKRefusesToPublishOne(t *testing.T) {
	called := false
	transport := signalKNotifyTransport{put: func(string, any) error { called = true; return nil }}
	err := transport.Send(context.Background(), testWatchMessage(t))
	if err == nil || called {
		t.Fatalf("err %v called %v", err, called)
	}
}

// ── dispatcher ──────────────────────────────────────────────────────────

type recordingTransport struct {
	id   string
	mu   sync.Mutex
	fail bool
	got  []notificationMessage
}

func (r *recordingTransport) ID() string { return r.id }

func (r *recordingTransport) Send(_ context.Context, m notificationMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return context.DeadlineExceeded
	}
	r.got = append(r.got, m)
	return nil
}

func (r *recordingTransport) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func reportFor(t *testing.T, watchID, runID, name string) watchReportMessage {
	t.Helper()
	m := testWatchMessage(t).Watch
	out := *m
	out.Watch.ID, out.Watch.Name, out.Run.ID = watchID, name, runID
	out.Title = name + ": 1 finding"
	return out
}

func watchDispatcher(t *testing.T, transports ...notificationTransport) (*alarmDispatcher, *alarmLogStore) {
	t.Helper()
	store := newTestAlarmLog(t)
	return &alarmDispatcher{
		transports: func() []notificationTransport { return transports },
		store:      store,
		now:        func() time.Time { return alarmNow },
		wake:       make(chan struct{}, 1),
	}, store
}

func TestDispatchWatchReport_SendsOnlyToChosenChannelsNeverSignalKAndRecordsWhatArrived(t *testing.T) {
	ntfy, mail := &recordingTransport{id: transportNtfy}, &recordingTransport{id: transportSMTP}
	bus := &recordingTransport{id: transportSignalK}
	d, store := watchDispatcher(t, ntfy, mail, bus)
	type delivered struct{ watch, run, channel string }
	var seen []delivered
	d.watchDelivered = func(w, r, c string) { seen = append(seen, delivered{w, r, c}) }

	d.dispatchWatchReport(reportFor(t, "w1", "r1", "Overnight"), []string{transportNtfy, transportSignalK})
	if ntfy.count() != 1 || mail.count() != 0 || bus.count() != 0 {
		t.Fatalf("ntfy %d mail %d bus %d", ntfy.count(), mail.count(), bus.count())
	}
	if ntfy.got[0].Kind != notificationKindWatchReport || ntfy.got[0].Watch == nil {
		t.Errorf("%+v", ntfy.got[0])
	}
	if len(seen) != 1 || seen[0] != (delivered{"w1", "r1", transportNtfy}) {
		t.Errorf("%+v", seen)
	}
	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Errorf("queue %d", depth)
	}
	if entries, _ := store.Recent(10); len(entries) != 0 {
		t.Errorf("a watch report is not an alarm and does not enter the alarm log: %+v", entries)
	}
}

func TestDispatchWatchReport_NoChannelsOrNoneSwitchedOnSendsNothing(t *testing.T) {
	ntfy := &recordingTransport{id: transportNtfy}
	d, store := watchDispatcher(t, ntfy)
	d.dispatchWatchReport(reportFor(t, "w1", "r1", "Overnight"), nil)
	d.dispatchWatchReport(reportFor(t, "w1", "r1", "Overnight"), []string{transportWebPush})
	if ntfy.count() != 0 {
		t.Fatalf("an empty selection must not fall back to every transport: %d", ntfy.count())
	}
	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Errorf("queue %d", depth)
	}
}

func reportWith(t *testing.T, watchID, runID string, findings ...watchFinding) watchReportMessage {
	t.Helper()
	m := reportFor(t, watchID, runID, "Overnight")
	m.Findings = findings
	return m
}

func TestDispatchWatchReport_FailureIsQueuedAndANewerReportCoveringTheSameFindingsSupersedesIt(t *testing.T) {
	ntfy := &recordingTransport{id: transportNtfy, fail: true}
	d, store := watchDispatcher(t, ntfy)
	bilge := notifyRuleFinding("alarms", "alarms_raised", "Bilge alarm.")
	power := notifyRuleFinding("power", "overnight_draw", "Drew a lot.")

	d.dispatchWatchReport(reportWith(t, "w1", "r1", bilge), []string{transportNtfy})
	d.dispatchWatchReport(reportWith(t, "w2", "r5", power), []string{transportNtfy})
	if depth, _ := store.QueueDepth(); depth != 2 {
		t.Fatalf("both are queued, got %d", depth)
	}
	// An alarm rule whose id merely looks similar is left alone.
	if err := store.Enqueue(transportNtfy, "w1", []byte(`{}`), alarmNow); err != nil {
		t.Fatal(err)
	}
	d.dispatchWatchReport(reportWith(t, "w1", "r2", bilge, power), []string{transportNtfy})
	if depth, _ := store.QueueDepth(); depth != 3 {
		t.Fatalf("w1's older report is replaced by r2, which says everything it did. The other watch and the alarm stay: %d", depth)
	}
	pending, err := store.QueuedForKey(watchQueueKey("w1"))
	if err != nil || len(pending) != 1 || !strings.Contains(string(pending[0].Payload), `"r2"`) {
		t.Fatalf("%+v %v", pending, err)
	}
}

func TestDispatchWatchReport_ANewerReportThatLeavesOutAFindingDoesNotReplaceTheOlder(t *testing.T) {
	ntfy := &recordingTransport{id: transportNtfy, fail: true}
	d, store := watchDispatcher(t, ntfy)
	bilge := notifyRuleFinding("alarms", "alarms_raised", "Bilge alarm at 02:00.")

	d.dispatchWatchReport(reportWith(t, "w1", "r-0200", bilge), []string{transportNtfy})
	d.dispatchWatchReport(reportWith(t, "w1", "r-0300"), []string{transportNtfy}) // 03:00, nothing to report
	pending, _ := store.QueuedForKey(watchQueueKey("w1"))
	if len(pending) != 2 || !strings.Contains(string(pending[0].Payload), "r-0200") || !strings.Contains(string(pending[1].Payload), "r-0300") {
		t.Fatalf("the 02:00 bilge report must still be delivered, in order: %+v", pending)
	}
}

func TestDrain_RetriesAWatchReportAndRecordsItsDeliveryButNeverSendsOneToSignalK(t *testing.T) {
	ntfy := &recordingTransport{id: transportNtfy, fail: true}
	bus := &recordingTransport{id: transportSignalK}
	d, store := watchDispatcher(t, ntfy, bus)
	var seen []string
	d.watchDelivered = func(w, r, c string) { seen = append(seen, w+"/"+r+"/"+c) }
	now := alarmNow
	d.now = func() time.Time { return now }

	d.dispatchWatchReport(reportFor(t, "w1", "r1", "Overnight"), []string{transportNtfy})
	// A report somehow queued for the bus is discarded, loudly, not sent.
	payload, _ := json.Marshal(testWatchMessage(t))
	if err := store.Enqueue(transportSignalK, watchQueueKey("w1"), payload, now); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Minute)
	d.drain(context.Background())
	if bus.count() != 0 {
		t.Fatalf("SignalK got a watch report")
	}
	if depth, _ := store.QueueDepth(); depth != 1 {
		t.Fatalf("the bus item is dropped and the ntfy one stays: %d", depth)
	}
	pending, _ := store.QueuedForKey(watchQueueKey("w1"))
	if len(pending) != 1 || pending[0].LastError == "" || pending[0].Attempts != 1 {
		t.Fatalf("%+v", pending)
	}

	ntfy.mu.Lock()
	ntfy.fail = false
	ntfy.mu.Unlock()
	now = now.Add(time.Hour)
	d.drain(context.Background())
	if ntfy.count() != 1 || len(seen) != 1 || seen[0] != "w1/r1/ntfy" {
		t.Fatalf("count %d seen %v", ntfy.count(), seen)
	}
	if depth, _ := store.QueueDepth(); depth != 0 {
		t.Fatalf("queue %d", depth)
	}
}

func TestEnqueueWatchReport_GoesThroughTheWorkerQueue(t *testing.T) {
	ntfy := &recordingTransport{id: transportNtfy}
	d, _ := watchDispatcher(t, ntfy)
	d.enqueueWatchReport(reportFor(t, "w1", "r1", "Overnight"), []string{transportNtfy})
	if d.queueDepth() != 1 {
		t.Fatalf("depth %d", d.queueDepth())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for ntfy.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if ntfy.count() != 1 {
		t.Fatalf("delivered %d", ntfy.count())
	}
}
