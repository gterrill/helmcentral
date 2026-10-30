package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

/*
Mate reviews an alarm when it fires (ADR 0237).

A rule with the "Mate reviews it" switch on starts one unattended Mate turn
when its alarm raises. The turn is the watch review's shape (watches_review.go):
the runner is driven directly with a short system prompt, read tools only, and
a submit tool that ends the turn, so the review is parsed from arguments and
never read out of prose. The alarm context Ask Mate builds (get_alarm_context,
ADR 0226) is its message.

The review becomes a Mate conversation titled after the alarm; the alarm card
links to it (alarmStatus.MateReview) and a follow-up notification goes to the
rule's own channels. The alarm's own notification is never touched: it was
queued when the alarm raised, and the follow-up is queued after the review.

Turning the switch on is the consent (ADR 0120). There is no spending limit.
A failure is recorded and shown on the card; it is not retried and no text
stands in for the review. Built-in checks and network alarms are not reviewed.
*/

const (
	alarmReviewToolName = "submit_alarm_review"

	// alarmReviewRepeatWindow is how long after a review the same rule's next
	// raise is left alone.
	alarmReviewRepeatWindow = time.Hour
	// alarmReviewDeadline is how long a review may take before it is recorded
	// as failed.
	alarmReviewDeadline = 3 * time.Minute

	maxAlarmReviewChars      = 1200
	maxAlarmReviewRuleChange = 300

	alarmReviewDone   = "done"
	alarmReviewFailed = "failed"
	alarmReviewActive = "running"

	alarmReviewRestarted = "Helmcentral restarted during the review"
	alarmReviewStopped   = "Helmcentral stopped during the review"

	alarmFollowupQueued         = "queued"
	alarmFollowupSkippedCleared = "skipped_cleared"
)

// alarmReviewLoadBackoff is how long a failed read of the stored reviews waits
// before it is tried again.
const alarmReviewLoadBackoff = 30 * time.Second

// alarmReviewShutdownWait is how long main waits for reviews to record that
// they were stopped.
const alarmReviewShutdownWait = 10 * time.Second

// alarmReviewReadTools are the tools an alarm review may call: reads only.
// Nothing that proposes, fills a form, starts a watch or draws a card, and no
// web search.
var alarmReviewReadTools = map[string]bool{
	"get_vessel_status":      true,
	"get_active_alarms":      true,
	"get_alarm_context":      true,
	"list_alarm_rules":       true,
	"check_signalk_paths":    true,
	"get_last_recorded":      true,
	"get_path_history":       true,
	"list_passages":          true,
	"get_cruise_summary":     true,
	"get_wind_forecast":      true,
	"get_tides":              true,
	"get_vessel_particulars": true,
	"get_planned_stay":       true,
}

func alarmReviewReadTool(name string) bool { return alarmReviewReadTools[name] }

// alarmReview is one review of one raise. Its json tags are what the alarm card
// shows of it; the rest never leaves the server.
type alarmReview struct {
	ID             string    `json:"-"`
	RuleID         string    `json:"-"`
	Title          string    `json:"-"`
	RaisedAt       time.Time `json:"-"`
	StartedAt      time.Time `json:"-"`
	FinishedAt     time.Time `json:"-"`
	State          string    `json:"state"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Summary        string    `json:"summary,omitempty"`
	Problem        string    `json:"problem,omitempty"`
	CostUSD        float64   `json:"cost_usd,omitempty"`
	CostKnown      bool      `json:"cost_known"`
	Model          string    `json:"-"`
	Followup       string    `json:"-"`
}

func createAlarmReviewsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS alarm_reviews (
			id              TEXT PRIMARY KEY,
			rule_id         TEXT NOT NULL,
			title           TEXT NOT NULL,
			raised_at       INTEGER NOT NULL,
			started_at      INTEGER NOT NULL,
			finished_at     INTEGER,
			state           TEXT NOT NULL,
			conversation_id TEXT NOT NULL DEFAULT '',
			summary         TEXT NOT NULL DEFAULT '',
			problem         TEXT NOT NULL DEFAULT '',
			cost_usd        REAL NOT NULL DEFAULT 0,
			cost_known      INTEGER NOT NULL DEFAULT 0,
			model           TEXT NOT NULL DEFAULT '',
			followup        TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create alarm_reviews table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_alarm_reviews_rule ON alarm_reviews(rule_id, started_at)`); err != nil {
		return fmt.Errorf("index alarm_reviews: %w", err)
	}
	return nil
}

const alarmReviewColumns = `id, rule_id, title, raised_at, started_at, finished_at, state, conversation_id, summary, problem, cost_usd, cost_known, model, followup`

func scanAlarmReview(row interface{ Scan(...any) error }) (alarmReview, error) {
	var (
		r               alarmReview
		raised, started int64
		finished        sql.NullInt64
		known           int
	)
	if err := row.Scan(&r.ID, &r.RuleID, &r.Title, &raised, &started, &finished, &r.State, &r.ConversationID,
		&r.Summary, &r.Problem, &r.CostUSD, &known, &r.Model, &r.Followup); err != nil {
		return alarmReview{}, err
	}
	r.RaisedAt, r.StartedAt = alarmRuleFromStoreTime(raised), alarmRuleFromStoreTime(started)
	if finished.Valid {
		r.FinishedAt = alarmRuleFromStoreTime(finished.Int64)
	}
	r.CostKnown = known != 0
	return r, nil
}

// alarmReviewer runs the reviews. The func fields are what tests replace.
type alarmReviewer struct {
	config     func() (watchConfig, error)
	now        func() time.Time
	assistant  func() *assistantStore
	dispatcher func() *alarmDispatcher
	linkBase   func() string
	// stillRaised says whether the raise a review was made for is still
	// the live one.
	stillRaised func(ruleID string, raisedAt time.Time) bool
	contextFor  func(ctx context.Context, deps assistantToolDeps, ruleID string) (string, error)
	deadline    time.Duration

	wg sync.WaitGroup
	// ctx ends when Helmcentral stops (stop), which ends every review under way.
	ctx    context.Context
	cancel context.CancelFunc
	// mu serialises claims and writes. latest is the newest review of each rule, replaced
	// whole on every write so the card path reads it without a lock; nil until
	// the first successful read of the table.
	mu     sync.Mutex
	latest atomic.Pointer[map[string]alarmReview]
	// loadErr and retryAt are the last failed read and when to try again, so a
	// broken table is logged once and not read on every payload.
	loadErr error
	retryAt time.Time
}

var globalAlarmReviewer *alarmReviewer

func newAlarmReviewer(settingsPath string) *alarmReviewer {
	ctx, cancel := context.WithCancel(context.Background())
	return &alarmReviewer{
		ctx:         ctx,
		cancel:      cancel,
		config:      func() (watchConfig, error) { return readWatchConfig(settingsPath) },
		now:         func() time.Time { return time.Now().UTC() },
		assistant:   func() *assistantStore { return globalAssistantStore },
		dispatcher:  func() *alarmDispatcher { return globalAlarmDispatcher },
		linkBase:    func() string { return getAlarmTransports().LinkAddress },
		stillRaised: alarmStillRaised,
		contextFor: func(ctx context.Context, deps assistantToolDeps, ruleID string) (string, error) {
			raw, _ := json.Marshal(assistantAlarmContextArgs{AlarmID: ruleID})
			return deps.executeGetAlarmContext(ctx, raw)
		},
		deadline: alarmReviewDeadline,
	}
}

// alarmStillRaised is whether the engine holds this raise of the rule live.
func alarmStillRaised(ruleID string, raisedAt time.Time) bool {
	for _, a := range globalAlarmEngine.active() {
		if a.RuleID == ruleID && a.RaisedAt.Equal(raisedAt) {
			return true
		}
	}
	return false
}

// wait blocks until every started review has finished. Tests use it.
func (r *alarmReviewer) wait() { r.wg.Wait() }

// start settles what a restart left behind: a review still "running" died with
// the last process, so it is marked failed, and then the stored reviews are read
// so the cards and the repeat rule have them from the first tick.
func (r *alarmReviewer) start() error {
	store, err := alarmRuleStore()
	if err != nil {
		return err
	}
	if err := store.RunTx("fail interrupted alarm reviews", func(tx *sql.Tx, _ time.Time) error {
		_, err := tx.Exec(`UPDATE alarm_reviews SET state = ?, problem = ?, finished_at = ? WHERE state = ?`,
			alarmReviewFailed, alarmReviewRestarted, alarmRuleStoreTime(r.now()), alarmReviewActive)
		return err
	}); err != nil {
		return fmt.Errorf("fail interrupted alarm reviews: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err = r.reviewsLocked()
	return err
}

// stop ends every review under way, which records each as stopped, and waits up
// to wait for them to finish writing.
func (r *alarmReviewer) stop(wait time.Duration) {
	r.cancel()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(wait):
		log.Printf("alarm review: %s after Helmcentral stopped, a review was still finishing; it is marked failed when Helmcentral next starts", wait)
	}
}

// interrupted is a review that ended because Helmcentral did, not because the
// alarm was looked at: it says nothing about whether a review was due.
func (r alarmReview) interrupted() bool {
	return r.State == alarmReviewFailed && (r.Problem == alarmReviewRestarted || r.Problem == alarmReviewStopped)
}

// reviews is the newest review of each rule, read from the table the first
// time. A failed read is logged once, and not tried again for
// alarmReviewLoadBackoff; until then it returns the same error.
func (r *alarmReviewer) reviews() (map[string]alarmReview, error) {
	if m := r.latest.Load(); m != nil {
		return *m, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reviewsLocked()
}

// reviewsLocked is reviews with r.mu held.
func (r *alarmReviewer) reviewsLocked() (map[string]alarmReview, error) {
	if m := r.latest.Load(); m != nil {
		return *m, nil
	}
	if r.loadErr != nil && r.now().Before(r.retryAt) {
		return nil, r.loadErr
	}
	latest, err := r.readReviews()
	if err != nil {
		if r.loadErr == nil {
			log.Printf("alarm review: could not read the stored reviews, so no card shows one and no alarm is reviewed; trying again in %s: %v", alarmReviewLoadBackoff, err)
		}
		r.loadErr, r.retryAt = err, r.now().Add(alarmReviewLoadBackoff)
		return nil, err
	}
	r.loadErr = nil
	r.latest.Store(&latest)
	return latest, nil
}

// withReview publishes a copy of the reviews with rev in place. r.mu must be held.
func (r *alarmReviewer) withReview(rev alarmReview) {
	next := map[string]alarmReview{rev.RuleID: rev}
	if cur := r.latest.Load(); cur != nil {
		for k, v := range *cur {
			if k != rev.RuleID {
				next[k] = v
			}
		}
	}
	r.latest.Store(&next)
}

func (r *alarmReviewer) readReviews() (map[string]alarmReview, error) {
	store, err := alarmRuleStore()
	if err != nil {
		return nil, err
	}
	latest := map[string]alarmReview{}
	if err := store.Read(func(q sqlQueryer) error {
		rows, err := q.Query(`SELECT ` + alarmReviewColumns + ` FROM alarm_reviews a
			WHERE started_at = (SELECT MAX(started_at) FROM alarm_reviews WHERE rule_id = a.rule_id)`)
		if err != nil {
			return fmt.Errorf("read alarm reviews: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			rev, err := scanAlarmReview(rows)
			if err != nil {
				return fmt.Errorf("read alarm reviews: %w", err)
			}
			latest[rev.RuleID] = rev
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	return latest, nil
}

func (r *alarmReviewer) latestFor(ruleID string) (alarmReview, bool, error) {
	all, err := r.reviews()
	if err != nil {
		return alarmReview{}, false, err
	}
	rev, ok := all[ruleID]
	return rev, ok, nil
}

// attach puts each alarm's review on its card. A review belongs to the raise it
// was made for, so a later raise of the same rule starts with a bare card.
func (r *alarmReviewer) attach(alarms []alarmStatus) []alarmStatus {
	if len(alarms) == 0 {
		return alarms
	}
	// This runs on the alarm payload, so it only reads what start and the
	// reviews themselves have published: no lock and no database.
	published := r.latest.Load()
	if published == nil {
		return alarms
	}
	for i := range alarms {
		if rev, ok := (*published)[alarms[i].RuleID]; ok && rev.RaisedAt.Equal(alarms[i].RaisedAt) {
			alarms[i].MateReview = &rev
		}
	}
	return alarms
}

// withAlarmReviews is attach on the process-wide reviewer, when there is one.
func withAlarmReviews(alarms []alarmStatus) []alarmStatus {
	if globalAlarmReviewer == nil {
		return alarms
	}
	return globalAlarmReviewer.attach(alarms)
}

// onRaise starts a review for a rule alarm that raised, when its rule asks for
// one. It runs on the alarm evaluator's path, so it checks only what the event
// carries and hands everything else, the settings, the database and the model,
// to a goroutine.
func (r *alarmReviewer) onRaise(event alarmEvent, now time.Time) {
	if event.Kind != alarmEventRaised || (event.Source != "" && event.Source != alarmSourceRule) || !event.Rule.MateReviews {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.handle(event, now)
	}()
}

// handle is one raise's review. Mate being off means no review and no record:
// turning Mate off withdraws the consent the switch relied on.
func (r *alarmReviewer) handle(event alarmEvent, now time.Time) {
	if r.ctx.Err() != nil {
		return
	}
	cfg, cfgErr := r.config()
	if cfgErr == nil && !cfg.Readiness.Enabled {
		log.Printf("alarm review: %q is set to be reviewed, but Mate is off, so it was not", event.Rule.Label)
		return
	}
	rev, ok := r.claim(event, now)
	if !ok {
		return
	}
	if cfgErr != nil {
		r.fail(rev, "Mate's settings could not be read: "+firstErrorLine(cfgErr))
		return
	}
	r.run(rev, event, cfg)
}

// claim records that a review has started, unless the rule had one within the
// repeat window. The record is what stops a second raise reviewing again, and
// claims are taken one at a time, so raises that arrive together are reviewed
// once. A review that ended with Helmcentral does not count.
func (r *alarmReviewer) claim(event alarmEvent, now time.Time) (alarmReview, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.reviewsLocked()
	if err != nil {
		log.Printf("alarm review: %q was not reviewed, the reviews could not be read: %v", event.Rule.Label, err)
		return alarmReview{}, false
	}
	if last, ok := all[event.Rule.ID]; ok && !last.interrupted() && now.Sub(last.StartedAt) < alarmReviewRepeatWindow {
		log.Printf("alarm review: %q raised again within %s of its last review, so it is not reviewed again", event.Rule.Label, alarmReviewRepeatWindow)
		return alarmReview{}, false
	}
	rev := alarmReview{ID: uuid.NewString(), RuleID: event.Rule.ID, Title: event.Rule.Label, RaisedAt: event.Status.RaisedAt, StartedAt: now, State: alarmReviewActive}
	if rev.RaisedAt.IsZero() {
		// An alarm raised with no time is a bug upstream. The review says so
		// rather than carrying on with a time it made up.
		log.Printf("alarm review: %q was raised with no raise time, so it was not reviewed", event.Rule.Label)
		rev.RaisedAt, rev.FinishedAt = now, now
		rev.State, rev.Problem = alarmReviewFailed, "the alarm was raised with no raise time, so Mate could not review it"
		if err := r.insertReview(rev); err != nil {
			log.Printf("alarm review: could not record that %q had no raise time: %v", event.Rule.Label, err)
			return alarmReview{}, false
		}
		r.withReview(rev)
		return alarmReview{}, false
	}
	if err := r.insertReview(rev); err != nil {
		log.Printf("alarm review: %q was not reviewed, the review could not be recorded: %v", event.Rule.Label, err)
		return alarmReview{}, false
	}
	r.withReview(rev)
	return rev, true
}

func (r *alarmReviewer) insertReview(rev alarmReview) error {
	store, err := alarmRuleStore()
	if err != nil {
		return err
	}
	var finished any
	if !rev.FinishedAt.IsZero() {
		finished = alarmRuleStoreTime(rev.FinishedAt)
	}
	return store.RunTx("insert alarm review", func(tx *sql.Tx, _ time.Time) error {
		_, err := tx.Exec(`INSERT INTO alarm_reviews (`+alarmReviewColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rev.ID, rev.RuleID, rev.Title, alarmRuleStoreTime(rev.RaisedAt), alarmRuleStoreTime(rev.StartedAt), finished, rev.State,
			rev.ConversationID, rev.Summary, rev.Problem, rev.CostUSD, boolToInt(rev.CostKnown), rev.Model, rev.Followup)
		return err
	})
}

func (r *alarmReviewer) updateReview(rev alarmReview) error {
	store, err := alarmRuleStore()
	if err != nil {
		return err
	}
	return store.RunTx("update alarm review", func(tx *sql.Tx, _ time.Time) error {
		_, err := tx.Exec(`UPDATE alarm_reviews SET finished_at = ?, state = ?, conversation_id = ?, summary = ?, problem = ?,
			cost_usd = ?, cost_known = ?, model = ?, followup = ? WHERE id = ?`,
			alarmRuleStoreTime(rev.FinishedAt), rev.State, rev.ConversationID, rev.Summary, rev.Problem, rev.CostUSD, boolToInt(rev.CostKnown), rev.Model, rev.Followup, rev.ID)
		return err
	})
}

// finish records how a review ended.
func (r *alarmReviewer) finish(rev alarmReview) {
	rev.FinishedAt = r.now()
	if err := r.updateReview(rev); err != nil {
		// The card would otherwise say the review is still running for good.
		log.Printf("alarm review: could not record how the review of %q ended (%s): %v", rev.Title, rev.State, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur := r.latest.Load(); cur != nil && (*cur)[rev.RuleID].ID == rev.ID {
		r.withReview(rev)
	}
}

func (r *alarmReviewer) fail(rev alarmReview, problem string) {
	log.Printf("alarm review: the review of %q failed: %s", rev.Title, problem)
	rev.State, rev.Problem = alarmReviewFailed, problem
	r.finish(rev)
}

// alarmReviewTools is the executor a review runs against: the turn's tool
// dependencies narrowed to the read tools plus the submit tool.
type alarmReviewTools struct {
	assistantToolDeps
	rule      alarmRule
	mu        sync.Mutex
	submitted *alarmReviewSubmission
}

type alarmReviewSubmission struct {
	Review     string `json:"review"`
	RuleChange string `json:"suggested_rule_change"`
}

var alarmReviewToolDef = openRouterFunctionDef{
	Name: alarmReviewToolName,
	Description: "Finish the review. Call it once, last, with your review of the alarm. " +
		"You cannot change a rule: a suggested change is described in words for the skipper to make.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"review": {"type": "string", "description": "Your review in plain sentences for the watchkeeper, at most 1200 characters. The first sentence says what the reading is doing and whether it needs the watchkeeper now."},
			"suggested_rule_change": {"type": "string", "description": "Optional. At most one change to the alarm's rule, in words, such as raising the clear point to 12.4 V. Leave it out when the rule is right."}
		},
		"required": ["review"]
	}`),
}

func (t *alarmReviewTools) definitions() []openRouterTool {
	var out []openRouterTool
	for _, def := range t.assistantToolDeps.definitions() {
		if alarmReviewReadTool(def.Function.Name) {
			out = append(out, def)
		}
	}
	return append(out, openRouterTool{Type: "function", Function: alarmReviewToolDef})
}

func (t *alarmReviewTools) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if name == alarmReviewToolName {
		return t.submit(args)
	}
	if !alarmReviewReadTool(name) {
		return "", fmt.Errorf("%s is not available in an alarm review: it reads the boat and cannot change anything", name)
	}
	return t.assistantToolDeps.execute(ctx, name, args)
}

// alarmReviewPathLike matches a dotted SignalK path.
var alarmReviewPathLike = regexp.MustCompile(`\b[a-z][A-Za-z0-9]*(\.[A-Za-z0-9_]+){2,}\b`)

func (t *alarmReviewTools) submit(args json.RawMessage) (string, error) {
	var s alarmReviewSubmission
	if err := json.Unmarshal(args, &s); err != nil {
		return "", fmt.Errorf("%s: the arguments are not the review shape: %w", alarmReviewToolName, err)
	}
	s.Review, s.RuleChange = strings.TrimSpace(s.Review), strings.TrimSpace(s.RuleChange)
	if s.Review == "" {
		return "", fmt.Errorf("%s: review is empty; say what the reading is doing and whether it needs the watchkeeper now", alarmReviewToolName)
	}
	if utf8.RuneCountInString(s.Review) > maxAlarmReviewChars {
		return "", fmt.Errorf("%s: review is over %d characters; shorten it", alarmReviewToolName, maxAlarmReviewChars)
	}
	if utf8.RuneCountInString(s.RuleChange) > maxAlarmReviewRuleChange {
		return "", fmt.Errorf("%s: suggested_rule_change is over %d characters; shorten it", alarmReviewToolName, maxAlarmReviewRuleChange)
	}
	for field, text := range map[string]string{"review": s.Review, "suggested_rule_change": s.RuleChange} {
		switch {
		case strings.ContainsAny(text, "!?"):
			return "", fmt.Errorf("%s: %s has an exclamation or question mark; state what you see, severity is shown by the card", alarmReviewToolName, field)
		case t.rule.ID != "" && strings.Contains(text, t.rule.ID), t.rule.Path != "" && strings.Contains(text, t.rule.Path), alarmReviewPathLike.MatchString(text):
			return "", fmt.Errorf("%s: %s names a path, rule id or device key; say it in the operator's words", alarmReviewToolName, field)
		}
	}
	t.mu.Lock()
	t.submitted = &s
	t.mu.Unlock()
	return `{"recorded":true,"note":"The review is recorded. Reply with the single word done."}`, nil
}

func (t *alarmReviewTools) submission() *alarmReviewSubmission {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.submitted
}

// run is one review from start to finish. It never returns an error: whatever
// went wrong is recorded on the review.
func (r *alarmReviewer) run(rev alarmReview, event alarmEvent, cfg watchConfig) {
	now := r.now()
	// The turn's "today" and the prompt's times are the boat's. With no fix the
	// boat's zone is not known, so they are UTC and the prompt says so.
	loc, locErr := watchBoatLocation()
	boatKnown := locErr == nil
	if !boatKnown {
		loc = time.UTC
	}
	var tools *alarmReviewTools
	turn := runUnattendedReview(r.ctx, unattendedTurn{
		// WebSearch is off: a review reads the boat and nothing else.
		Cfg: cfg, Now: now, Loc: loc, Deadline: r.deadline,
		ConversationID: "alarm-review-" + rev.ID, LogPrefix: "alarm review", Name: rev.Title,
		Build: func(ctx context.Context, deps assistantToolDeps, units string, pc assistantPromptContext) (unattendedInput, string) {
			alarmContext, err := r.contextFor(ctx, deps, rev.RuleID)
			if err != nil {
				return unattendedInput{}, "the alarm's details could not be read: " + firstErrorLine(err)
			}
			tools = &alarmReviewTools{assistantToolDeps: deps, rule: event.Rule}
			return unattendedInput{
				System: buildAlarmReviewPrompt(pc, units, now, loc, boatKnown),
				Message: "This alarm has just gone off. Review it. The alarm id for get_alarm_context is " + rev.RuleID + "; do not repeat it in the review. " +
					"What follows is what get_alarm_context returned a moment ago.\n" + alarmContext,
				Tools:     tools,
				Submitted: func() bool { return tools.submission() != nil },
			}, ""
		},
	})
	rev.Model, rev.CostUSD, rev.CostKnown = turn.Model, turn.CostUSD, turn.CostKnown
	switch {
	case r.ctx.Err() != nil && turn.Problem != "":
		r.fail(rev, alarmReviewStopped)
		return
	case turn.Problem != "":
		r.fail(rev, turn.Problem)
		return
	}
	r.complete(rev, event, tools.submission())
}

// complete stores the review as a conversation and sends the follow-up.
func (r *alarmReviewer) complete(rev alarmReview, event alarmEvent, sub *alarmReviewSubmission) {
	chats := r.assistant()
	if chats == nil {
		r.fail(rev, "the review could not be saved: the conversation store is not open")
		return
	}
	body := sub.Review
	if sub.RuleChange != "" {
		body += "\n\nA change to the rule to consider: " + sub.RuleChange
	}
	conv, err := chats.CreateConversation(rev.Title)
	if err == nil {
		_, err = chats.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: fmt.Sprintf("Why did %s go off?", rev.Title)})
	}
	if err == nil {
		_, err = chats.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: body, Model: rev.Model, CostUSD: rev.CostUSD})
	}
	if err != nil {
		r.fail(rev, "the review could not be saved as a conversation: "+firstErrorLine(err))
		return
	}
	rev.State, rev.ConversationID, rev.Summary = alarmReviewDone, conv.ID, watchFirstSentence(sub.Review)

	if !r.stillRaised(rev.RuleID, rev.RaisedAt) {
		rev.Followup = alarmFollowupSkippedCleared
		log.Printf("alarm review: %q cleared before its review finished, so no follow-up was sent", rev.Title)
		r.finish(rev)
		return
	}
	if d := r.dispatcher(); d != nil {
		d.enqueueAlarmReview(r.followUp(rev), event.Rule.Notify)
		rev.Followup = alarmFollowupQueued
	} else {
		log.Printf("alarm review: no dispatcher, so the follow-up for %q was not sent", rev.Title)
	}
	r.finish(rev)
}

// followUp is the notification that tells the watchkeeper Mate has reviewed
// the alarm: the first sentence and, when the Address for links is set, where
// to read the rest.
func (r *alarmReviewer) followUp(rev alarmReview) notificationMessage {
	msg := notificationMessage{
		Kind: notificationKindAlarmReview, RuleID: alarmReviewQueueKey(rev.RuleID), Label: "Mate on " + rev.Title,
		Message: rev.Summary, Vessel: vesselNameForNotifications(), At: r.now(), Path: "/mate/" + rev.ConversationID,
	}
	if base := strings.TrimRight(r.linkBase(), "/"); base != "" {
		msg.Link = base + msg.Path
		msg.Message += "\n" + msg.Link
	}
	return msg
}

// buildAlarmReviewPrompt is the review's whole system prompt.
func buildAlarmReviewPrompt(pc assistantPromptContext, units string, now time.Time, loc *time.Location, boatKnown bool) string {
	var b strings.Builder
	vessel := strings.TrimSpace(pc.VesselName)
	if vessel == "" {
		vessel = "the vessel"
	}
	identity := vessel
	if m := strings.TrimSpace(pc.BoatModel); m != "" {
		identity += ", a " + m
	}
	if h := assistantHullTypePhrase(pc.HullType); h != "" {
		identity += " (" + h + ")"
	}
	fmt.Fprintf(&b, "You are Mate, the onboard assistant aboard %s. This is an unattended review of an alarm that has just gone off. "+
		"No one is reading now and no one can answer you; the watchkeeper reads your review when they pick up the alarm.\n\n", identity)
	if pc.LOAM > 0 {
		fmt.Fprintf(&b, "Length overall: %.1f m.\n", pc.LOAM)
	}
	if place := strings.TrimSpace(pc.PlaceName); place != "" {
		fmt.Fprintf(&b, "The boat is at or near %s.\n", place)
	}
	if boatKnown {
		fmt.Fprintf(&b, "Boat time: %s. All times in the alarm's details are boat time.\n", clockWithZone(now, loc, "Mon 2 Jan 2006 15:04"))
	} else {
		fmt.Fprintf(&b, "The boat has no position fix yet, so its own time zone is not known. The time is %s UTC. Times in the alarm's details are UTC, not boat time; say so if a time matters.\n", now.UTC().Format("Mon 2 Jan 2006 15:04"))
	}
	fmt.Fprintf(&b, "The operator's units are %s. Quote figures as the details give them; do not convert.\n", units)
	if notes := strings.TrimSpace(pc.Notes); notes != "" {
		b.WriteString("The operator's standing notes about the boat:\n" + clipRunes(notes, 1500) + "\n")
	}
	b.WriteString("\nRules:\n" +
		"- Start from the alarm's details. Say what the reading is doing and whether it needs the watchkeeper now, in that order. The first sentence is sent to the phone on its own, so it must carry both.\n" +
		"- Use the named source. When the details name where a reading comes from, use it; never guess a cause the figures do not show, and say so when you cannot tell.\n" +
		"- Say whether the reading looks real or like a sensor problem only from evidence: how fresh it is, whether it moved smoothly, sat flat or jumped, what else changed, how often it has come before.\n" +
		"- Write for someone reading at 02:00, in the operator's words and units: what, where, how close, which way it is going. No paths, rule ids or device keys. No exclamation or question marks; the card shows severity.\n" +
		"- Do not restate the alarm's title or body as your own, and do not stand in for the alarm.\n" +
		"- At most one suggested change to the rule, in words only, in suggested_rule_change. You cannot propose or make it in this turn, and you only suggest one when the figures show the threshold is wrong for how the boat behaves.\n" +
		"- At most four short sentences. If nothing needs the watchkeeper, say so in the first sentence.\n" +
		"- Use tools only to check something the details do not show. The tools only read.\n")
	b.WriteString("\nFinish by calling " + alarmReviewToolName + " once with your review, then reply with the single word done.\n")
	return b.String()
}
