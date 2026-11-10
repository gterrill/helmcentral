package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"regexp"
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
	// Questions are what Mate asked the skipper, apart from the review. A run
	// with none sends an empty list.
	Questions []string `json:"questions"`
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
//
// A review that starts with a list has no opening, so a push takes the first
// line with its list marker removed instead.
func watchFirstSentence(s string) string {
	whole := s
	s, _ = splitWatchReview(whole)
	if s == "" {
		line, _, _ := strings.Cut(strings.TrimSpace(whole), "\n")
		s, _ = splitWatchReview(watchListMarker.ReplaceAllString(line, ""))
	}
	if utf8.RuneCountInString(s) > watchNotifySentenceChars {
		s = string([]rune(s)[:watchNotifySentenceChars-1]) + "…"
	}
	return s
}

var (
	// watchListMarker is a list item's marker at the start of a line.
	watchListMarker = regexp.MustCompile(`^[ \t]*(?:[-*+]|\d+[.)])[ \t\n]+`)
	watchMDLink     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// watchAbbreviations are words whose full stop does not end a sentence.
	watchAbbreviations = map[string]bool{
		"no": true, "approx": true, "st": true, "vs": true, "e.g": true,
		"i.e": true, "etc": true, "mr": true, "mrs": true, "dr": true,
	}
)

// splitWatchReview reads a review as its opening sentence and the rest.
//
// A leading "# " heading or "> " quote marker is dropped from the opening; a
// leading "-2%" or "#1" is not a marker and stays. A review that starts with a
// list item ("- ", "* ", "1. ") has no opening: the opening is "" and the rest
// is the whole review, untouched.
//
// The sentence is looked for in the first paragraph, which ends at a blank line
// or at a line that starts a list item (after a heading, at the end of its
// line). A single newline in the paragraph counts as a space, so a wrapped
// sentence is not cut. A sentence ends at ".", "!" or "?", optionally followed
// by closing characters (* _ ) " ' `), and then whitespace or the end of the
// paragraph, so "6.00 AM" does not split. A full stop after no, approx, St, vs,
// e.g, i.e, etc, Mr, Mrs or Dr does not end one either, and neither does any
// position inside an open "**" or backtick span or inside "[text](url)", so the
// rest never begins inside a span. A paragraph with no sentence end is all
// opening.
//
// The opening is plain text: "**", emphasis "*" and "_", backticks and link
// syntax are removed ("*" between digits or with spaces round it, and "_"
// inside a word, stay). It is uncapped. The rest keeps its markdown.
func splitWatchReview(review string) (opening, rest string) {
	s := strings.TrimSpace(review)
	if s == "" {
		return "", ""
	}
	s, heading := stripWatchReviewMarkers(s)
	if s == "" {
		return "", ""
	}
	if watchListMarker.MatchString(s) {
		return "", review
	}
	end := len(s)
	if heading {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			end = i
		}
	} else {
		off := 0
		for n, line := range strings.SplitAfter(s, "\n") {
			if n > 0 && (strings.TrimSpace(line) == "" || watchListMarker.MatchString(line)) {
				end = off
				break
			}
			off += len(line)
		}
	}
	para := s[:end]
	flat := strings.NewReplacer("\n", " ", "\r", " ").Replace(para)
	cut := watchSentenceEnd(flat)
	if cut < 0 {
		cut = end
	}
	opening = watchPlainText(strings.Join(strings.Fields(flat[:cut]), " "))
	return opening, strings.TrimSpace(s[cut:])
}

// stripWatchReviewMarkers drops leading "# " headings and "> " quotes (marker
// and a space), and says whether a heading was dropped.
func stripWatchReviewMarkers(s string) (string, bool) {
	heading := false
	for s != "" {
		switch {
		case s[0] == '>' && len(s) > 1 && (s[1] == ' ' || s[1] == '\t'):
			s = strings.TrimLeft(s[1:], " \t")
		case s[0] == '#':
			n := 0
			for n < len(s) && s[n] == '#' {
				n++
			}
			if n > 6 || n >= len(s) || (s[n] != ' ' && s[n] != '\t') {
				return s, heading
			}
			s = strings.TrimLeft(s[n:], " \t")
			heading = true
		default:
			return s, heading
		}
	}
	return s, heading
}

func isWatchASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isWatchDigit(c byte) bool       { return c >= '0' && c <= '9' }
func isWatchWordByte(c byte) bool {
	return isWatchASCIILetter(c) || isWatchDigit(c) || c >= 0x80
}

// watchDigitFollows reports whether a digit comes next in p at j, after any
// spaces.
func watchDigitFollows(p string, j int) bool {
	t := strings.TrimLeft(p[j:], " \t")
	return t != "" && isWatchDigit(t[0])
}

// watchSentenceEnd is the index just after the first sentence of p, a single
// line, or -1 when it has none.
func watchSentenceEnd(p string) int {
	bold, tick := 0, 0
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			bold ^= 1
			i++
		case c == '`':
			tick ^= 1
		case c == '[' && tick == 0:
			if cl := strings.IndexByte(p[i+1:], ']'); cl >= 0 && i+1+cl+1 < len(p) && p[i+1+cl+1] == '(' {
				if cp := strings.IndexByte(p[i+1+cl+2:], ')'); cp >= 0 {
					i = i + 1 + cl + 2 + cp
				}
			}
		case c == '.' || c == '!' || c == '?':
			j, b, t := i+1, bold, tick
			for j < len(p) && strings.IndexByte("*_)\"'`", p[j]) >= 0 {
				switch {
				case p[j] == '*' && j+1 < len(p) && p[j+1] == '*':
					b ^= 1
					j += 2
					continue
				case p[j] == '`':
					t ^= 1
				}
				j++
			}
			if b != 0 || t != 0 || (j < len(p) && p[j] != ' ' && p[j] != '\t') {
				continue
			}
			if c == '.' {
				k := i
				for k > 0 && (isWatchASCIILetter(p[k-1]) || p[k-1] == '.') {
					k--
				}
				// "no." is an abbreviation only before a number, as in
				// "No. 2"; "Shore power: no." is an answer.
				word := strings.ToLower(strings.TrimLeft(p[k:i], "."))
				if watchAbbreviations[word] && (word != "no" || watchDigitFollows(p, j)) {
					continue
				}
			}
			return j
		}
	}
	return -1
}

// watchPlainText removes markdown from an opening.
func watchPlainText(s string) string {
	s = watchMDLink.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '*' || c == '_' {
			prev, next := byte(' '), byte(' ')
			if i > 0 {
				prev = s[i-1]
			}
			if i+1 < len(s) {
				next = s[i+1]
			}
			keep := prev == ' ' && next == ' '
			if c == '*' {
				keep = keep || isWatchDigit(prev) && isWatchDigit(next)
			} else {
				keep = keep || isWatchWordByte(prev) && isWatchWordByte(next)
			}
			if !keep {
				continue
			}
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
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
	m := watchReportMessage{Kind: notificationKindWatchReport, Vessel: vessel, Findings: nonNilFindings(run.Findings), Review: run.Review,
		Questions: nonNilStrings(run.Questions), Charts: nonNilCharts(run.Charts)}
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

	switch issues := watchIssueCount(run.Findings); issues {
	case 0:
		m.Title = w.Name + ": nothing to report"
	default:
		m.Title = fmt.Sprintf("%s: %d to look at", w.Name, issues)
	}

	var lines []string
	ordered := watchFindingOrder(run.Findings)
	for shown, i := range ordered {
		if shown == watchNotifyFindingLines {
			more := len(ordered) - watchNotifyFindingLines
			lines = append(lines, fmt.Sprintf("%d more %s.", more, watchPlural(more, "finding", "findings")))
			break
		}
		lines = append(lines, run.Findings[i].Text)
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

// unusual is the report's Unusual block, nil when the run has none to show. The
// email carries it after the findings. It is never counted in the title.
func (m watchReportMessage) unusual() *watchUnusual {
	if m.Report == nil {
		return nil
	}
	return m.Report.Unusual
}

// watchIssueCount is how many things a run has to look at: its findings that
// do not extend another. A finding of Mate's that adds to a rule's finding is
// more about the same issue.
func watchIssueCount(findings []watchFinding) int {
	n := 0
	for _, f := range findings {
		if f.Extends == nil {
			n++
		}
	}
	return n
}

// watchFindingOrder is the order to read a run's findings in: each finding that
// extends another right after it. It returns indexes into findings, which are
// not reordered, because a chart names its finding by index. The review is
// checked when it is stored, so every extends names one of the rules' findings,
// which come first and extend nothing.
func watchFindingOrder(findings []watchFinding) []int {
	under := map[int][]int{}
	var top []int
	for i, f := range findings {
		if f.Extends != nil {
			under[*f.Extends] = append(under[*f.Extends], i)
			continue
		}
		top = append(top, i)
	}
	order := make([]int, 0, len(findings))
	for _, i := range top {
		order = append(order, i)
		order = append(order, under[i]...)
	}
	return order
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
		for _, i := range watchFindingOrder(m.Findings) {
			f := m.Findings[i]
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
	if u := m.unusual(); u != nil {
		b.WriteString("Unusual\n")
		for _, l := range u.Lines {
			b.WriteString("- " + l + "\n")
		}
		if u.Note != "" {
			b.WriteString(u.Note + "\n")
		}
		b.WriteString("\n")
	}
	if m.Review != nil && strings.TrimSpace(*m.Review) != "" {
		b.WriteString("Mate's review\n" + strings.TrimSpace(*m.Review) + "\n\n")
	}
	if len(m.Questions) > 0 {
		b.WriteString("Questions\n")
		for _, q := range m.Questions {
			b.WriteString("- " + q + "\n")
		}
		b.WriteString("\n")
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

// watchPreviousRun is what the next review is told about the last one: the
// findings Mate added (so it can give the same issue the same key), the
// review's own text, and what the skipper said in the chats opened on that
// run. Replies to any older run are not carried.
type watchPreviousRun struct {
	Mate      []watchFinding
	Review    string
	Questions []string
	Replies   []string
}

// mateFindings keeps the findings Mate added to a run.
func mateFindings(all []watchFinding) []watchFinding {
	var mine []watchFinding
	for _, f := range all {
		if f.By == watchByMate {
			mine = append(mine, f)
		}
	}
	return mine
}

type watchPreviousRunKey struct{}

func withWatchPreviousRun(ctx context.Context, prev watchPreviousRun) context.Context {
	return context.WithValue(ctx, watchPreviousRunKey{}, prev)
}

func watchPreviousRunFrom(ctx context.Context) watchPreviousRun {
	v, _ := ctx.Value(watchPreviousRunKey{}).(watchPreviousRun)
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
