package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

/*
Watch notifications (ADR 0227, Notifications).

A watch report reaches the operator's phone or inbox through the transports
alarms use, but as its own kind of message. It never passes through the alarm
presenter or the alarm log, never reaches the alarm banner, and never goes to
the SignalK transport, which would put a report on the bus as if it were an
alarm. A report that cannot be delivered is retried by the same queue and
logged when it fails, the same as an alarm.

A watch set to send "findings" tells the operator only when a run has a finding
the last notification did not carry, so a fault that stays does not notify
every hour. A watch set to send "every" report notifies each run that ran.
*/

const (
	watchSendFindings = "findings"
	watchSendEvery    = "every"

	notificationKindWatchReport = "watch_report"

	// watchNotifyFindingLines is how many findings the short message names.
	watchNotifyFindingLines = 3
	// watchNotifySentenceChars caps Mate's opening sentence in the short message.
	watchNotifySentenceChars = 200
)

// watchNotifyChannels are the transports a watch can use, in the order they
// are stored. SignalK is not one of them: it would put the report on the bus
// as an alarm.
var watchNotifyChannels = []string{transportNtfy, transportSMTP, transportWebhook, transportWebPush}

// watchPendingNotice is a delivery still being retried and why the last
// attempt failed.
type watchPendingNotice struct {
	Channel string `json:"channel"`
	Reason  string `json:"reason"`
}

// watchReportMessage is a watch report as it is sent. Its JSON is the webhook
// payload, so a receiver can tell it from an alarm by kind.
type watchReportMessage struct {
	Kind    string `json:"kind"`
	Vessel  string `json:"vessel"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Watch   struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		ScheduleSentence string `json:"schedule_sentence"`
	} `json:"watch"`
	Run struct {
		ID       string `json:"id"`
		BoatTime string `json:"boat_time"`
		Outcome  string `json:"outcome"`
	} `json:"run"`
	Findings []watchFinding `json:"findings"`
	// Charts are the findings' charts as data; finding_index says which finding
	// each belongs to. A webhook payload also carries each as png_base64.
	Charts []watchChart `json:"charts"`
	Report *watchReport `json:"report"`
	Review *string      `json:"review"`
	// Path is where the run is in the app. There is no public address to
	// build a full link from, so the receiver adds its own Helmcentral address.
	Path string `json:"path"`
	// Link is Path under the address set for links in Settings, "" when none is.
	Link string `json:"link,omitempty"`
}

// watchQueueKey is the retry queue's key for a watch's reports. It carries a
// prefix no alarm rule id has, so a report can only supersede another report
// of the same watch.
func watchQueueKey(watchID string) string { return "watch:" + watchID }

// defaultWatchNotify is the channels a new watch starts with: every enabled
// transport except SignalK.
func defaultWatchNotify(cfg alarmTransportConfig) []string {
	on := map[string]bool{
		transportNtfy: cfg.Ntfy.Enabled, transportSMTP: cfg.SMTP.Enabled,
		transportWebhook: cfg.Webhook.Enabled, transportWebPush: cfg.WebPush.Enabled,
	}
	out := []string{}
	for _, id := range watchNotifyChannels {
		if on[id] {
			out = append(out, id)
		}
	}
	return out
}

// normalizeWatchNotify checks the channels and puts them in a fixed order.
func normalizeWatchNotify(in []string) ([]string, error) {
	want := map[string]bool{}
	for _, id := range in {
		id = strings.ToLower(strings.TrimSpace(id))
		switch {
		case id == transportSignalK:
			return nil, fieldErr("notify", "a watch report cannot go to SignalK: it would appear on the bus as an alarm")
		case !containsString(watchNotifyChannels, id):
			return nil, fieldErr("notify", "%q is not a notification channel; the channels are %s", id, strings.Join(watchNotifyChannels, ", "))
		}
		want[id] = true
	}
	out := []string{}
	for _, id := range watchNotifyChannels {
		if want[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func validateWatchSend(s string) error {
	if s != watchSendFindings && s != watchSendEvery {
		return fieldErr("send", "must be %q (only when it finds something) or %q (every report)", watchSendFindings, watchSendEvery)
	}
	return nil
}

// watchFindingKey says which finding this is, across runs. A rule's finding is
// its section and rule, whatever its figures. One Mate added is its section and
// the key Mate gave the issue, so the same issue is the same finding in
// whatever words. A Mate finding with no key (none made after keys began) falls
// back to its wording without case, spacing or a closing full stop.
func watchFindingKey(f watchFinding) string {
	if f.By == watchByMate {
		if f.Key != "" {
			return "mate|" + f.Section + "|" + f.Key
		}
		text := strings.ToLower(strings.Join(strings.Fields(f.Text), " "))
		return "mate|" + f.Section + "|text:" + strings.TrimRight(text, ".!? ")
	}
	return "rule|" + f.Section + "|" + f.Rule
}

func watchFindingKeys(findings []watchFinding) []string {
	keys := make([]string, 0, len(findings))
	seen := map[string]bool{}
	for _, f := range findings {
		if k := watchFindingKey(f); !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

// watchNotifyDue says whether a run is worth telling the operator about.
// prevTold are the keys of the previous run's findings the operator is known to
// have been told about, so a finding is new when that run did not have it or
// never got it to them. A run that did not happen never is worth telling, and a
// watch with no channels has no one to tell.
func watchNotifyDue(w savedWatch, run watchRun, prevTold []string) bool {
	if run.Outcome != watchOutcomeRan || len(w.Notify) == 0 {
		return false
	}
	if w.Send == watchSendEvery {
		return true
	}
	told := map[string]bool{}
	for _, k := range prevTold {
		told[k] = true
	}
	for _, k := range watchFindingKeys(run.Findings) {
		if !told[k] {
			return true
		}
	}
	return false
}

// watchFirstSentence is the opening sentence of Mate's review, bare of markdown.
func watchFirstSentence(s string) string {
	s = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "#*>-_ \t"))
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	for i := 0; i+1 < len(s); i++ {
		if (s[i] == '.' || s[i] == '!' || s[i] == '?') && s[i+1] == ' ' {
			s = s[:i+1]
			break
		}
	}
	s = strings.TrimSpace(strings.ReplaceAll(s, "**", ""))
	if utf8.RuneCountInString(s) > watchNotifySentenceChars {
		s = string([]rune(s)[:watchNotifySentenceChars-1]) + "…"
	}
	return s
}

// watchHeaderText makes a title safe to put in a header line: control
// characters, which would end the line or add a header, become spaces.
func watchHeaderText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// watchEncodedHeaderText is watchHeaderText as an RFC 2047 encoded word when it
// holds anything that is not plain ASCII, which ntfy and mail both read.
func watchEncodedHeaderText(s string) string {
	return mime.QEncoding.Encode("UTF-8", watchHeaderText(s))
}

func buildWatchReportMessage(w savedWatch, run watchRun, vessel, linkBase string) watchReportMessage {
	m := watchReportMessage{Kind: notificationKindWatchReport, Vessel: vessel, Findings: nonNilFindings(run.Findings), Review: run.Review, Charts: nonNilCharts(run.Charts)}
	m.Watch.ID, m.Watch.Name, m.Watch.ScheduleSentence = w.ID, w.Name, watchSentenceOrCron(w)
	m.Run.ID, m.Run.BoatTime, m.Run.Outcome = run.ID, run.BoatTime, run.Outcome
	m.Path = "/watches/" + w.ID + "?run=" + run.ID
	if linkBase != "" {
		m.Link = strings.TrimRight(linkBase, "/") + m.Path
	}
	if len(run.Report) > 0 {
		var rep watchReport
		if err := json.Unmarshal(run.Report, &rep); err != nil {
			log.Printf("watches: %q: the report of run %s is unreadable, so the notification carries no report: %v", w.Name, run.ID, err)
		} else {
			m.Report = &rep
		}
	}

	n := len(run.Findings)
	switch n {
	case 0:
		m.Title = w.Name + ": nothing to report"
	case 1:
		m.Title = w.Name + ": 1 finding"
	default:
		m.Title = fmt.Sprintf("%s: %d findings", w.Name, n)
	}

	var lines []string
	for i, f := range run.Findings {
		if i == watchNotifyFindingLines {
			more := n - watchNotifyFindingLines
			lines = append(lines, fmt.Sprintf("%d more %s.", more, watchPlural(more, "finding", "findings")))
			break
		}
		lines = append(lines, f.Text)
	}
	if run.Review != nil {
		if s := watchFirstSentence(*run.Review); s != "" {
			lines = append(lines, s)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "Report from "+run.BoatTime+".")
	}
	m.Summary = strings.Join(lines, "\n")
	return m
}

// watchEmailText is the whole report as plain text, for the email body.
func watchEmailText(m watchReportMessage) string {
	var b strings.Builder
	b.WriteString(m.Title + "\n")
	if m.Run.BoatTime != "" {
		b.WriteString("Report from " + m.Run.BoatTime + "\n")
	}
	if m.Vessel != "" {
		b.WriteString(m.Vessel + "\n")
	}
	b.WriteString("\n")
	if m.Link != "" {
		b.WriteString("Open the report: " + m.Link + "\n\n")
	}
	if len(m.Findings) > 0 {
		b.WriteString("Findings\n")
		for i, f := range m.Findings {
			b.WriteString("- " + f.Text + "\n")
			if f.ChartProblem != "" {
				b.WriteString("  (" + f.ChartProblem + ")\n")
			}
			for _, c := range m.Charts {
				if c.FindingIndex == i && c.Problem != "" {
					b.WriteString("  Chart left out: " + c.Problem + "\n")
				}
			}
		}
		b.WriteString("\n")
	}
	if m.Review != nil && strings.TrimSpace(*m.Review) != "" {
		b.WriteString("Mate's review\n" + strings.TrimSpace(*m.Review) + "\n\n")
	}
	if m.Report != nil {
		for _, s := range m.Report.Sections {
			b.WriteString(s.Title + "\n")
			if s.Unavailable != "" {
				b.WriteString(s.Unavailable + "\n")
			}
			for _, l := range s.Lines {
				b.WriteString(l + "\n")
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// afterRun tells the operator about a saved run when the watch's settings say
// to. The watch is read again so a change made while the run was building is
// honoured. Nothing is remembered as told here: a finding counts as told only
// once a channel has accepted a report that carried it (recordWatchDelivery),
// so a report that went nowhere is tried again by the next run. What this does
// remember is which of the previous run's told findings still stand.
func (h *watchHost) afterRun(run watchRun) {
	if h.dispatchReport == nil {
		return
	}
	var w savedWatch
	var prevTold []string
	if err := h.store().Read(func(q sqlQueryer) error {
		var err error
		if w, err = savedWatchByIDQ(q, run.WatchID); err != nil {
			return err
		}
		prev, ok, err := lastRanRunBeforeQ(q, run.WatchID, run.StartedAt, run.ID)
		if err != nil {
			return err
		}
		if ok {
			prevTold = prev.ToldKeys
		}
		return nil
	}); err != nil {
		if !errors.Is(err, errWatchNotFound) {
			log.Printf("watches: could not read %q to decide whether to notify, so no notification was made for run %s: %v", run.WatchID, run.ID, err)
		}
		return
	}
	keys := watchFindingKeys(run.Findings)
	inherited := intersectStrings(prevTold, keys)
	if err := h.store().RunTx("remember told findings", func(tx *sql.Tx, _ time.Time) error {
		return setWatchRunToldTx(tx, run.WatchID, run.ID, inherited)
	}); err != nil {
		log.Printf("watches: %q: could not remember which findings run %s repeats, so the next run may tell them again: %v", w.Name, run.ID, err)
	}
	if !watchNotifyDue(w, run, prevTold) {
		return
	}
	vessel := ""
	if h.vesselName != nil {
		vessel = h.vesselName()
	}
	h.dispatchReport(buildWatchReportMessage(w, run, vessel, getAlarmTransports().LinkAddress), w.Notify)
}

func intersectStrings(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	out := []string{}
	for _, x := range a {
		if in[x] {
			out = append(out, x)
		}
	}
	return out
}

// recordWatchDelivery notes on the run that a channel received its report. A
// run that cannot be marked is logged: the report did arrive, so the failure
// is only in what the run page shows.
func recordWatchDelivery(watchID, runID, channel string) {
	store := globalDocumentStore
	if store == nil {
		log.Printf("watches: %s delivered run %s but the record store is not open, so the run will not show it", channel, runID)
		return
	}
	err := store.RunTx("record watch delivery", func(tx *sql.Tx, _ time.Time) error {
		return addWatchRunNotifiedTx(tx, watchID, runID, channel)
	})
	if err != nil && !errors.Is(err, errWatchRunNotFound) {
		log.Printf("watches: %s delivered run %s but it could not be recorded on the run: %v", channel, runID, err)
	}
}

// attachWatchNotifyPending adds to each run the deliveries still being
// retried for its watch, read from the alarm retry queue.
func attachWatchNotifyPending(watchID string, runs []watchRun) error {
	if globalAlarmLogStore == nil || len(runs) == 0 {
		return nil
	}
	queued, err := globalAlarmLogStore.QueuedForKey(watchQueueKey(watchID))
	if err != nil {
		return err
	}
	for _, item := range queued {
		var msg notificationMessage
		if err := json.Unmarshal(item.Payload, &msg); err != nil || msg.Watch == nil {
			continue
		}
		reason := item.LastError
		if reason == "" {
			reason = "the first attempt has not been made yet"
		}
		for i := range runs {
			if runs[i].ID == msg.Watch.Run.ID {
				runs[i].NotifyPending = append(runs[i].NotifyPending, watchPendingNotice{Channel: item.Transport, Reason: reason})
			}
		}
	}
	return nil
}

// recordWatchNotSent notes on the run why its report went nowhere.
func recordWatchNotSent(watchID, runID, reason string) {
	store := globalDocumentStore
	if store == nil {
		log.Printf("watches: run %s was not sent (%s) but the record store is not open, so the run will not say so", runID, reason)
		return
	}
	err := store.RunTx("record watch not sent", func(tx *sql.Tx, _ time.Time) error {
		return setWatchRunNotSentTx(tx, watchID, runID, reason)
	})
	if err != nil && !errors.Is(err, errWatchRunNotFound) {
		log.Printf("watches: run %s was not sent (%s) and that could not be recorded on the run: %v", runID, reason, err)
	}
}

type watchPreviousMateKey struct{}

// withWatchPreviousMate hands a review the findings Mate added to the previous
// run, so it can give the same issue the same key.
func withWatchPreviousMate(ctx context.Context, previous []watchFinding) context.Context {
	var mine []watchFinding
	for _, f := range previous {
		if f.By == watchByMate {
			mine = append(mine, f)
		}
	}
	return context.WithValue(ctx, watchPreviousMateKey{}, mine)
}

func watchPreviousMateFrom(ctx context.Context) []watchFinding {
	v, _ := ctx.Value(watchPreviousMateKey{}).([]watchFinding)
	return v
}

func nonNilCharts(c []watchChart) []watchChart {
	if c == nil {
		return []watchChart{}
	}
	return c
}

// recordWatchNotifyNote notes on the run where a delivery was not as meant.
func recordWatchNotifyNote(watchID, runID, note string) {
	store := globalDocumentStore
	if store == nil {
		log.Printf("watches: run %s: %s, but the record store is not open, so the run will not say so", runID, note)
		return
	}
	err := store.RunTx("record watch delivery note", func(tx *sql.Tx, _ time.Time) error {
		return addWatchRunNotifyNoteTx(tx, watchID, runID, note)
	})
	if err != nil && !errors.Is(err, errWatchRunNotFound) {
		log.Printf("watches: run %s: %s, and that could not be recorded on the run: %v", runID, note, err)
	}
}
