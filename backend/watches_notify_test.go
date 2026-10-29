package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Watch notifications (ADR 0227, Notifications): which channels a watch may
// use, when a run is worth telling the operator about, and what is said.

func notifyRuleFinding(section, rule, text string) watchFinding {
	return watchFinding{Section: section, Rule: rule, Text: text, By: watchByRules}
}

func notifyMateFinding(key string) watchFinding {
	return watchFinding{Section: "tanks", Rule: "mate_review", Text: "Mate's wording of " + key, By: watchByMate, Key: key}
}

func TestWatchFindingKey_RulesUseSectionAndRuleMateUsesSectionAndKey(t *testing.T) {
	a := notifyRuleFinding("power", "overnight_draw", "Drew 40 Ah overnight.")
	b := notifyRuleFinding("power", "overnight_draw", "Drew 55 Ah overnight.")
	if watchFindingKey(a) == "" || watchFindingKey(a) != watchFindingKey(b) {
		t.Errorf("a rule's finding is the same finding whatever its figures: %q vs %q", watchFindingKey(a), watchFindingKey(b))
	}
	if watchFindingKey(a) == watchFindingKey(notifyRuleFinding("tanks", "overnight_draw", "x")) {
		t.Error("another section is another finding")
	}
	m1 := notifyMateFinding("port_alternator_output")
	m2 := m1
	m2.Text = "The port alternator is giving less than the starboard one."
	if watchFindingKey(m1) != watchFindingKey(m2) {
		t.Errorf("the same issue is the same finding whatever Mate's wording: %q vs %q", watchFindingKey(m1), watchFindingKey(m2))
	}
	if watchFindingKey(m1) == watchFindingKey(notifyMateFinding("fridge_draw")) {
		t.Error("different issues must differ")
	}
	other := m1
	other.Section = "power"
	if watchFindingKey(m1) == watchFindingKey(other) {
		t.Error("the same key in another section is another finding")
	}
	if watchFindingKey(a) == watchFindingKey(notifyMateFinding("overnight_draw")) {
		t.Error("a Mate finding never collides with a rule's")
	}
}

func TestNormalizeWatchNotify_RefusesSignalKAndUnknownChannels(t *testing.T) {
	_, err := normalizeWatchNotify([]string{transportNtfy, transportSignalK})
	if err == nil || !strings.Contains(err.Error(), "notify:") || !strings.Contains(err.Error(), "SignalK") {
		t.Fatalf("SignalK is refused with a plain reason, got %v", err)
	}
	if _, err := normalizeWatchNotify([]string{"pigeon"}); err == nil || !strings.Contains(err.Error(), "pigeon") {
		t.Fatalf("an unknown channel is refused by name, got %v", err)
	}
	got, err := normalizeWatchNotify([]string{" webpush ", "ntfy", "ntfy"})
	if err != nil || strings.Join(got, ",") != "ntfy,webpush" {
		t.Fatalf("channels are trimmed, de-duplicated and put in a fixed order: %v %v", got, err)
	}
	got, err = normalizeWatchNotify([]string{"Ntfy", "SMTP", " WebPush"})
	if err != nil || strings.Join(got, ",") != "ntfy,smtp,webpush" {
		t.Fatalf("ids are case-insensitive and stored lower case: %v %v", got, err)
	}
	if _, err := normalizeWatchNotify([]string{"SignalK"}); err == nil || !strings.Contains(err.Error(), "SignalK") {
		t.Fatalf("SignalK in any case is refused, got %v", err)
	}
	none, err := normalizeWatchNotify(nil)
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("no channels is allowed and is an empty list, got %#v %v", none, err)
	}
}

func TestValidateWatchSend(t *testing.T) {
	for _, ok := range []string{watchSendFindings, watchSendEvery} {
		if err := validateWatchSend(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	if err := validateWatchSend("sometimes"); err == nil || !strings.Contains(err.Error(), "send:") {
		t.Errorf("an unknown send is refused, got %v", err)
	}
}

func TestDefaultWatchNotify_EveryEnabledChannelExceptSignalK(t *testing.T) {
	cfg := alarmTransportConfig{
		Ntfy: ntfyConfig{Enabled: true}, SMTP: smtpConfig{Enabled: true},
		SignalK: signalKNotifyConfig{Enabled: true}, WebPush: webPushConfig{Enabled: true},
	}
	if got := strings.Join(defaultWatchNotify(cfg), ","); got != "ntfy,smtp,webpush" {
		t.Errorf("got %q", got)
	}
	if got := defaultWatchNotify(alarmTransportConfig{}); got == nil || len(got) != 0 {
		t.Errorf("nothing enabled is an empty list, got %#v", got)
	}
}

func notifyTestWatch(send string) savedWatch {
	return savedWatch{ID: "w1", Name: "Overnight", Notify: []string{transportNtfy}, Send: send}
}

func notifyRanRun(findings ...watchFinding) watchRun {
	return watchRun{ID: "r1", WatchID: "w1", Outcome: watchOutcomeRan, Findings: findings}
}

func TestWatchNotifyDue(t *testing.T) {
	power := notifyRuleFinding("power", "overnight_draw", "Drew a lot.")
	anchor := notifyRuleFinding("anchor", "outside_radius", "Outside the radius.")
	pk, ak := watchFindingKey(power), watchFindingKey(anchor)
	cases := []struct {
		name string
		w    savedWatch
		run  watchRun
		told []string
		want bool
	}{
		{"findings: first finding", notifyTestWatch(watchSendFindings), notifyRanRun(power), nil, true},
		{"findings: clean run", notifyTestWatch(watchSendFindings), notifyRanRun(), nil, false},
		{"findings: same finding the operator was told of", notifyTestWatch(watchSendFindings), notifyRanRun(power), []string{pk}, false},
		{"findings: one new beside an old one", notifyTestWatch(watchSendFindings), notifyRanRun(power, anchor), []string{pk}, true},
		{"findings: a finding went away", notifyTestWatch(watchSendFindings), notifyRanRun(power), []string{pk, ak}, false},
		{"every: clean run", notifyTestWatch(watchSendEvery), notifyRanRun(), nil, true},
		{"every: same finding again", notifyTestWatch(watchSendEvery), notifyRanRun(power), []string{pk}, true},
		{"skipped never notifies (every)", notifyTestWatch(watchSendEvery), watchRun{Outcome: watchOutcomeSkipped}, nil, false},
		{"skipped never notifies (findings)", notifyTestWatch(watchSendFindings), watchRun{Outcome: watchOutcomeSkipped, Findings: []watchFinding{power}}, nil, false},
		{"no channels never notifies", savedWatch{Send: watchSendEvery}, notifyRanRun(power), nil, false},
	}
	for _, tc := range cases {
		if got := watchNotifyDue(tc.w, tc.run, tc.told); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ── a run decides whether to notify ─────────────────────────────────────

// notifyCapture stands in for the dispatcher. With deliver set it also does
// what the dispatcher does when a channel accepts a report.
type notifyCapture struct {
	reports  []watchReportMessage
	channels [][]string
	deliver  bool
}

func (c *notifyCapture) dispatch(m watchReportMessage, ch []string) {
	c.reports = append(c.reports, m)
	c.channels = append(c.channels, ch)
	if c.deliver {
		recordWatchDelivery(m.Watch.ID, m.Run.ID, ch[0])
	}
}

func (f *watchHostFixture) notifyOn(id string, channels []string, send string) *notifyCapture {
	f.t.Helper()
	w := f.get(id)
	w.Notify, w.Send = channels, send
	if err := f.docs.RunTx("set notify", func(tx *sql.Tx, _ time.Time) error { return updateSavedWatchTx(tx, w) }); err != nil {
		f.t.Fatal(err)
	}
	prev := globalDocumentStore
	globalDocumentStore = f.docs
	f.t.Cleanup(func() { globalDocumentStore = prev })
	c := &notifyCapture{deliver: true}
	f.host.dispatchReport = c.dispatch
	f.host.vesselName = func() string { return "Pikorua" }
	return c
}

func (f *watchHostFixture) nextHour() {
	f.clock = f.clock.Add(time.Hour)
	f.host.tick(context.Background())
}

func TestWatchRun_FindingsMode_AStandingFindingIsToldOnceAndAClearedOneIsToldAgainWhenItReturns(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	c := f.notifyOn("id-Hourly", []string{transportNtfy, transportWebPush}, watchSendFindings)
	power := notifyRuleFinding("power", "overnight_draw", "Drew a lot.")

	f.findings = nil
	f.host.tick(context.Background())
	if len(c.reports) != 0 {
		t.Fatalf("a clean run tells no one: %+v", c.reports)
	}

	f.findings = []watchFinding{power}
	f.nextHour()
	if len(c.reports) != 1 || strings.Join(c.channels[0], ",") != "ntfy,webpush" || c.reports[0].Run.ID == "" {
		t.Fatalf("a first finding notifies: %+v %v", c.reports, c.channels)
	}
	if c.reports[0].Vessel != "Pikorua" || !strings.HasPrefix(c.reports[0].Title, "Hourly: 1 finding") {
		t.Errorf("%+v", c.reports[0])
	}
	if runs := f.runsOf("id-Hourly"); runs[0].ID != c.reports[0].Run.ID {
		t.Errorf("the notification names the saved run: %q vs %q", runs[0].ID, c.reports[0].Run.ID)
	}

	f.nextHour()
	f.nextHour()
	if len(c.reports) != 1 {
		t.Fatalf("a standing finding says nothing again, however many runs: %d", len(c.reports))
	}

	f.findings = nil
	f.nextHour()
	f.findings = []watchFinding{power}
	f.nextHour()
	if len(c.reports) != 2 {
		t.Fatalf("a finding that cleared and came back is news again: %d", len(c.reports))
	}
}

func TestWatchRun_FindingsMode_ANewFindingBesideAStandingOneNotifiesWithTheWholeReport(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	c := f.notifyOn("id-Hourly", []string{transportNtfy}, watchSendFindings)
	power := notifyRuleFinding("power", "overnight_draw", "Drew a lot.")
	anchor := notifyRuleFinding("anchor", "outside_radius", "Outside the radius.")

	f.findings = []watchFinding{power, anchor}
	f.host.tick(context.Background())
	f.findings = []watchFinding{power}
	f.nextHour() // quiet: the anchor finding went away, the power one stands
	f.findings = []watchFinding{power, anchor}
	f.nextHour()
	if len(c.reports) != 2 || len(c.reports[1].Findings) != 2 {
		t.Fatalf("the anchor finding returned after a run without it: %+v", c.reports)
	}
	f.nextHour()
	if len(c.reports) != 2 {
		t.Fatalf("both stand now: %d", len(c.reports))
	}
}

func TestWatchRun_AReportNobodyReceivedIsNotCountedAsToldSoTheNextRunTriesAgain(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	c := f.notifyOn("id-Hourly", []string{transportNtfy}, watchSendFindings)
	c.deliver = false
	f.findings = []watchFinding{notifyRuleFinding("power", "overnight_draw", "Drew a lot.")}
	f.host.tick(context.Background())
	f.nextHour()
	if len(c.reports) != 2 {
		t.Fatalf("no channel accepted the first report, so the finding is still news: %d", len(c.reports))
	}
	c.deliver = true
	f.nextHour()
	f.nextHour()
	if len(c.reports) != 3 {
		t.Fatalf("once a report is accepted the finding stands quietly: %d", len(c.reports))
	}
}

func TestWatchRun_EveryMode_NotifiesEveryRanRunEvenWhenClean(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	c := f.notifyOn("id-Hourly", []string{transportSMTP}, watchSendEvery)
	f.host.tick(context.Background())
	f.findings = []watchFinding{notifyRuleFinding("power", "overnight_draw", "Drew a lot.")}
	f.nextHour()
	f.nextHour()
	if len(c.reports) != 3 || c.reports[0].Title != "Hourly: nothing to report" {
		t.Fatalf("%d reports, first %q", len(c.reports), c.reports[0].Title)
	}
}

func TestWatchRun_NoChannelsOrNoPositionNeverNotifies(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	c := f.notifyOn("id-Hourly", []string{}, watchSendEvery)
	f.findings = []watchFinding{notifyRuleFinding("power", "x", "A finding.")}
	f.host.tick(context.Background())
	if len(c.reports) != 0 {
		t.Fatalf("a watch with no channels notifies no one: %+v", c.reports)
	}

	f.notifyOn("id-Hourly", []string{transportNtfy}, watchSendEvery)
	f.host.dispatchReport = c.dispatch
	f.lonErr = errNoPositionForTest
	f.nextHour()
	if len(c.reports) != 0 {
		t.Fatalf("a skipped run never notifies: %+v", c.reports)
	}
}

var errNoPositionForTest = context.DeadlineExceeded

func TestWatchRun_RunNowNotifiesToo(t *testing.T) {
	f := newWatchHostFixture(t)
	w := f.add("Hourly", "0 * * * *")
	c := f.notifyOn("id-Hourly", []string{transportNtfy}, watchSendEvery)
	run, err := f.host.RunNow(context.Background(), w)
	if err != nil || run.ID == "" {
		t.Fatalf("run now: %+v %v", run, err)
	}
	if len(c.reports) != 1 || c.reports[0].Run.ID != run.ID {
		t.Fatalf("%+v", c.reports)
	}
}

func TestWatchRun_ANotificationWithNoChannelSwitchedOnIsNotMarkedAndSaysWhy_ThenSendsOnceItIsOn(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.notifyOn("id-Hourly", []string{transportNtfy}, watchSendFindings)
	ntfy := &recordingTransport{id: transportNtfy}
	d, _ := watchDispatcher(t) // ntfy is off in Settings
	d.watchDelivered = recordWatchDelivery
	d.watchNotSent = recordWatchNotSent
	f.host.dispatchReport = d.dispatchWatchReport
	f.findings = []watchFinding{notifyRuleFinding("power", "overnight_draw", "Drew a lot.")}

	f.host.tick(context.Background())
	first := f.runsOf("id-Hourly")[0]
	if len(first.Notified) != 0 || !strings.Contains(first.NotSent, "no channel") || len(first.ToldKeys) != 0 {
		t.Fatalf("nothing was sent, and the run says so: %+v", first)
	}

	d.transports = func() []notificationTransport { return []notificationTransport{ntfy} } // now it is on
	f.nextHour()
	if ntfy.count() != 1 {
		t.Fatalf("the finding was never told, so the next run tells it: %d", ntfy.count())
	}
	second := f.runsOf("id-Hourly")[0]
	if strings.Join(second.Notified, ",") != "ntfy" || second.NotSent != "" || len(second.ToldKeys) != 1 {
		t.Fatalf("%+v", second)
	}
	f.nextHour()
	if ntfy.count() != 1 {
		t.Fatalf("now it stands quietly: %d", ntfy.count())
	}
}

// ── the watch's own settings ────────────────────────────────────────────

func withTransports(t *testing.T, cfg alarmTransportConfig) {
	t.Helper()
	alarmTransportsMu.Lock()
	prev := alarmTransportsState
	alarmTransportsState = cfg
	alarmTransportsMu.Unlock()
	t.Cleanup(func() {
		alarmTransportsMu.Lock()
		alarmTransportsState = prev
		alarmTransportsMu.Unlock()
	})
}

func TestWatchREST_NotifyDefaultsToEveryEnabledChannelExceptSignalKAndCanBeSetAndChanged(t *testing.T) {
	newProposalEnv(t)
	withTransports(t, alarmTransportConfig{Ntfy: ntfyConfig{Enabled: true}, SignalK: signalKNotifyConfig{Enabled: true}, WebPush: webPushConfig{Enabled: true}})

	c, rec := watchRequestTo(http.MethodPost, "/api/watches", `{"name":"A","schedule":"0 6 * * *","sections":["power"]}`)
	if err := createWatchHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	var a savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	if strings.Join(a.Notify, ",") != "ntfy,webpush" || a.Send != watchSendFindings {
		t.Fatalf("defaults %+v", a)
	}

	c, rec = watchRequestTo(http.MethodPost, "/api/watches", `{"name":"B","schedule":"0 6 * * *","sections":["power"],"notify":["smtp"],"send":"every"}`)
	_ = createWatchHandler(c)
	var b savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusCreated || strings.Join(b.Notify, ",") != "smtp" || b.Send != watchSendEvery {
		t.Fatalf("explicit %d %+v", rec.Code, b)
	}

	c, rec = watchRequestTo(http.MethodPost, "/api/watches", `{"name":"C","schedule":"0 6 * * *","sections":["power"],"notify":[]}`)
	_ = createWatchHandler(c)
	var none savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &none)
	if rec.Code != http.StatusCreated || none.Notify == nil || len(none.Notify) != 0 {
		t.Fatalf("an explicit empty list is kept, not defaulted: %d %+v", rec.Code, none)
	}

	c, rec = watchRequestTo(http.MethodPatch, "/api/watches/"+a.ID, `{"notify":["ntfy","signalk"]}`, "id", a.ID)
	_ = patchWatchHandler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "SignalK") {
		t.Fatalf("SignalK is refused: %d %s", rec.Code, rec.Body)
	}
	c, rec = watchRequestTo(http.MethodPatch, "/api/watches/"+a.ID, `{"send":"hourly"}`, "id", a.ID)
	_ = patchWatchHandler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "send") {
		t.Fatalf("a bad send is refused: %d %s", rec.Code, rec.Body)
	}
	c, rec = watchRequestTo(http.MethodPatch, "/api/watches/"+a.ID, `{"notify":["webhook"],"send":"every"}`, "id", a.ID)
	_ = patchWatchHandler(c)
	var patched savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if rec.Code != http.StatusOK || strings.Join(patched.Notify, ",") != "webhook" || patched.Send != watchSendEvery {
		t.Fatalf("patch %d %+v", rec.Code, patched)
	}
	c, rec = watchRequestTo(http.MethodPatch, "/api/watches/"+a.ID, `{"name":"A2"}`, "id", a.ID)
	_ = patchWatchHandler(c)
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if strings.Join(patched.Notify, ",") != "webhook" || patched.Send != watchSendEvery {
		t.Fatalf("a patch that names neither leaves them: %+v", patched)
	}
}

func TestWatchRecord_ProposalsSetNotifyAndSend(t *testing.T) {
	env := newProposalEnv(t)
	withTransports(t, alarmTransportConfig{SMTP: smtpConfig{Enabled: true}})
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	if strings.Join(w.Notify, ",") != "smtp" || w.Send != watchSendFindings {
		t.Fatalf("a proposed watch starts with the enabled channels: %+v", w)
	}
	env.advance()
	env.applyOK(t, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"notify":["ntfy","webpush"],"send":"every"}}]}`)
	w = firstWatch(t, env.docs)
	if strings.Join(w.Notify, ",") != "ntfy,webpush" || w.Send != watchSendEvery {
		t.Fatalf("%+v", w)
	}
	msg := proposeError(t, env.deps, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"notify":["signalk"]}}]}`)
	if !strings.Contains(msg, "notify:") || !strings.Contains(msg, "SignalK") {
		t.Errorf("%q", msg)
	}
	msg = proposeError(t, env.deps, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"send":"never"}}]}`)
	if !strings.Contains(msg, "send:") {
		t.Errorf("%q", msg)
	}
	p := env.propose(t, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"send":"findings"}}]}`)
	if p.Ops[0].After["send"] != "findings" {
		t.Errorf("the card shows the field changed: %+v", p.Ops[0].After)
	}
}

// ── what the pages and Mate read back ───────────────────────────────────

func TestWatchHost_ProductionDeliveryHookMarksTheRunNotified(t *testing.T) {
	f := newWatchHostFixture(t)
	w := f.add("Hourly", "0 * * * *")
	f.ran(w.ID, f.clock)
	run := f.runsOf(w.ID)[0]
	prev := globalDocumentStore
	globalDocumentStore = f.docs
	t.Cleanup(func() { globalDocumentStore = prev })

	recordWatchDelivery(w.ID, run.ID, transportNtfy)
	recordWatchDelivery(w.ID, run.ID, transportWebPush)
	recordWatchDelivery(w.ID, run.ID, transportNtfy)
	got := f.runsOf(w.ID)[0]
	if strings.Join(got.Notified, ",") != "ntfy,webpush" {
		t.Fatalf("each channel once, in the order it arrived: %q", got.Notified)
	}

	recordWatchNotSent(w.ID, run.ID, "no channel is switched on")
	if got := f.runsOf(w.ID)[0]; got.NotSent != "no channel is switched on" {
		t.Fatalf("%+v", got)
	}
}

func TestWatchREST_RunsShowWhoWasToldAndWhatIsStillBeingRetried(t *testing.T) {
	env := newProposalEnv(t)
	store := newTestAlarmLog(t)
	prev := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = prev })

	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	for _, id := range []string{"r1", "r2"} {
		if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
			return insertWatchRunTx(tx, watchRun{ID: id, WatchID: w.ID, StartedAt: env.clock.Add(map[string]time.Duration{"r1": time.Hour, "r2": 2 * time.Hour}[id]), FinishedAt: env.clock, Outcome: watchOutcomeRan, CostKnown: true}, true)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.docs.RunTx("notified", func(tx *sql.Tx, _ time.Time) error { return addWatchRunNotifiedTx(tx, w.ID, "r1", transportNtfy) }); err != nil {
		t.Fatal(err)
	}
	m := testWatchMessage(t)
	m.Watch.Watch.ID, m.Watch.Run.ID = w.ID, "r2"
	payload, _ := json.Marshal(m)
	if err := store.Enqueue(transportSMTP, watchQueueKey(w.ID), payload, env.clock); err != nil {
		t.Fatal(err)
	}
	items, _ := store.QueuedForKey(watchQueueKey(w.ID))
	if err := store.RescheduleQueued(items[0].ID, 2, env.clock, "dial tcp: connection refused"); err != nil {
		t.Fatal(err)
	}

	c, rec := watchRequestTo(http.MethodGet, "/api/watches/"+w.ID, "", "id", w.ID)
	if err := getWatchHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%v %d %s", err, rec.Code, rec.Body)
	}
	var detail struct {
		Runs []watchRun `json:"runs"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	byID := map[string]watchRun{}
	for _, r := range detail.Runs {
		byID[r.ID] = r
	}
	if got := byID["r1"]; strings.Join(got.Notified, ",") != "ntfy" || len(got.NotifyPending) != 0 {
		t.Errorf("r1 %+v", got)
	}
	r2 := byID["r2"]
	if len(r2.Notified) != 0 || len(r2.NotifyPending) != 1 || r2.NotifyPending[0].Channel != transportSMTP ||
		!strings.Contains(r2.NotifyPending[0].Reason, "connection refused") {
		t.Errorf("r2 %+v", r2)
	}
}

func TestMateWatchTools_ShowNotifySendAndWhetherTheRunWasNotified(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, `{"operations":[{"type":"watch","action":"create","fields":{"name":"Morning report","schedule":"0 6 * * *","sections":["power"],"notify":["ntfy"],"send":"every"}}]}`)
	w := firstWatch(t, env.docs)
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "r1", WatchID: w.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan, CostKnown: true}, true)
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.docs.RunTx("notified", func(tx *sql.Tx, _ time.Time) error { return addWatchRunNotifiedTx(tx, w.ID, "r1", transportNtfy) }); err != nil {
		t.Fatal(err)
	}
	list, err := env.deps.executeListWatches(context.Background(), nil)
	if err != nil || !strings.Contains(list, `"notify":["ntfy"]`) || !strings.Contains(list, `"send":"every"`) {
		t.Fatalf("%s %v", list, err)
	}
	runs, err := env.deps.executeGetWatchRuns(context.Background(), json.RawMessage(`{"watch_id":"`+w.ID+`"}`))
	if err != nil || !strings.Contains(runs, `"notified":["ntfy"]`) {
		t.Fatalf("%s %v", runs, err)
	}
}

// ── control characters, the channels route, one run, the link address ──

func TestWatchNameRefusesControlCharacters(t *testing.T) {
	env := newProposalEnv(t)
	for name, bad := range map[string]string{"newline": "Night\nwatch", "return": "Night\r\nBcc: x", "tab": "Night\twatch", "escape": "Night\x1bwatch"} {
		args, _ := json.Marshal(map[string]any{"operations": []map[string]any{{"type": "watch", "action": "create",
			"fields": map[string]any{"name": bad, "schedule": "0 6 * * *", "sections": []string{"power"}}}}})
		if msg := proposeError(t, env.deps, string(args)); !strings.Contains(msg, "name:") || !strings.Contains(msg, "control") {
			t.Errorf("%s: %q", name, msg)
		}
		c, rec := watchRequestTo(http.MethodPost, "/api/watches", string(mustJSON(t, map[string]any{"name": bad, "schedule": "0 6 * * *", "sections": []string{"power"}})))
		_ = createWatchHandler(c)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s over REST: %d", name, rec.Code)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWatchREST_ChannelsRouteSaysWhichAreOnAndNothingElse(t *testing.T) {
	withTransports(t, alarmTransportConfig{
		Ntfy:    ntfyConfig{Enabled: true, Server: "https://ntfy.example", Topic: "secret-topic"},
		Webhook: webhookConfig{Enabled: true, URL: "https://hooks.example/token123"},
		SignalK: signalKNotifyConfig{Enabled: true},
	})
	c, rec := watchRequestTo(http.MethodGet, "/api/watches/channels", "")
	if err := watchChannelsHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%v %d", err, rec.Code)
	}
	var got struct {
		Offered []string `json:"offered"`
		Enabled []string `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Offered, ",") != "ntfy,smtp,webhook,webpush" || strings.Join(got.Enabled, ",") != "ntfy,webhook" {
		t.Errorf("%+v", got)
	}
	for _, leak := range []string{"secret-topic", "token123", "hooks.example", "ntfy.example"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("the route must not reveal %q: %s", leak, rec.Body)
		}
	}
	found := false
	for _, r := range watchRoutes() {
		if r.Path == "/api/watches/channels" {
			found = r.Method == http.MethodGet && r.Tier == tierRead
		}
	}
	if !found {
		t.Error("GET /api/watches/channels must be a read-tier route, so a read-write user can open the page")
	}
}

func TestWatchREST_GetOneRunWithItsDeliveryState(t *testing.T) {
	env := newProposalEnv(t)
	store := newTestAlarmLog(t)
	prev := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = prev })
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "old", WatchID: w.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan, CostKnown: true}, true)
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.docs.RunTx("notified", func(tx *sql.Tx, _ time.Time) error { return addWatchRunNotifiedTx(tx, w.ID, "old", transportNtfy) }); err != nil {
		t.Fatal(err)
	}
	c, rec := watchRequestTo(http.MethodGet, "/api/watches/"+w.ID+"/runs/old", "", "id", w.ID, "runId", "old")
	if err := getWatchRunHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%v %d %s", err, rec.Code, rec.Body)
	}
	var run watchRun
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	if run.ID != "old" || strings.Join(run.Notified, ",") != "ntfy" {
		t.Errorf("%+v", run)
	}
	c, rec = watchRequestTo(http.MethodGet, "/api/watches/"+w.ID+"/runs/nope", "", "id", w.ID, "runId", "nope")
	_ = getWatchRunHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("an unknown run is a 404, got %d", rec.Code)
	}
}

func TestTransportConfig_LinkAddressIsAnHTTPURLWithAHostOrEmpty(t *testing.T) {
	for _, bad := range []string{"boat.local:9091", "ftp://boat.local", "http://", "javascript:alert(1)", "http://boat.local/x\r\nHost: y", "http://u:p@boat.local"} {
		cfg := alarmTransportConfig{LinkAddress: bad}
		if err := validateAlarmTransports(&cfg); err == nil || !strings.Contains(err.Error(), "address for links") {
			t.Errorf("%q should be refused, got %v", bad, err)
		}
	}
	cfg := alarmTransportConfig{LinkAddress: "  http://boat.local:9091/  "}
	if err := validateAlarmTransports(&cfg); err != nil || cfg.LinkAddress != "http://boat.local:9091" {
		t.Errorf("trimmed, no trailing slash: %q %v", cfg.LinkAddress, err)
	}
	empty := alarmTransportConfig{}
	if err := validateAlarmTransports(&empty); err != nil || empty.LinkAddress != "" {
		t.Errorf("empty is the default and is allowed: %q %v", empty.LinkAddress, err)
	}
}
