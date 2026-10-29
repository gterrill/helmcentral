package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

/*
Mate's review of a watch report (ADR 0227).

After the deterministic report is built, one unattended Mate turn reads it.
It is not stored in any conversation, so nothing here touches the chat
store or the conversation run registry: the runner (assistantRunner, the
same agentic loop an operator's question uses) is driven directly with a
dedicated short system prompt, the report as its only message, and a short
list of read-only tools. The model ends the turn by calling
submit_watch_review, so the review and any added findings are parsed from
arguments, never read out of prose.

Mate being on is the consent to send the report to the model (ADR 0120).
There is no per-watch switch and no spending limit. What a review cost is
read from OpenRouter and shown on the run.
*/

// watchFindingKeyFormat is the shape of the key Mate gives an issue it adds.
var watchFindingKeyFormat = regexp.MustCompile(`^[a-z][a-z0-9_]{2,39}$`)

const (
	watchReviewToolName = "submit_watch_review"

	maxWatchReviewChars       = 1500
	maxWatchReviewFindings    = 5
	maxWatchReviewFindingText = 300
)

// watchReviewReadTools are the tools a review may call: reads only. Nothing
// that hands the operator something to tap, starts a background watch, fills
// a form or draws a card. search_web joins them only when the operator has
// turned web search on.
var watchReviewReadTools = map[string]bool{
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
	"search_web":             true,
}

type watchReviewSubmission struct {
	Review string `json:"review"`
	Added  []struct {
		Section string `json:"section"`
		Key     string `json:"key"`
		Text    string `json:"text"`
		// Chart asks for a chart of one path's history to go with the finding.
		Chart *struct {
			Path  string  `json:"path"`
			Hours float64 `json:"hours"`
			Label string  `json:"label"`
		} `json:"chart"`
	} `json:"added_findings"`
}

var watchReviewToolDef = openRouterFunctionDef{
	Name: watchReviewToolName,
	Description: "Finish the review. Call it once, last, with your short review of the report and any findings you add. " +
		"You cannot remove or change a finding the rules reached.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"review": {"type": "string", "description": "Your review in plain sentences for the skipper, at most 1500 characters. Say what matters and what you could not tell. Questions go here."},
			"added_findings": {
				"type": "array",
				"maxItems": 5,
				"description": "Findings the rules missed, each a statement of what you saw in the figures. Empty when there are none.",
				"items": {
					"type": "object",
					"properties": {
						"section": {"type": "string", "enum": ["power", "anchor", "alarms", "tanks", "weather", "instruments"]},
						"key": {"type": "string", "description": "A short lower_snake name for the issue, 3 to 40 characters such as port_alternator_output. Name the issue, not the figures. If it is the same issue as one in previous_findings_you_added, use that key exactly."},
						"text": {"type": "string", "description": "One plain sentence with its figures in the operator's units, at most 300 characters. No question marks, no exclamation marks, no cause you cannot see."},
						"chart": {
							"type": "object",
							"description": "Optional. A chart of one SignalK path's recent history to go with the finding. Only when the trend is the evidence.",
							"properties": {
								"path": {"type": "string", "description": "The SignalK path to chart."},
								"hours": {"type": "number", "minimum": 1, "maximum": 48, "description": "How many hours back to chart, 1 to 48."},
								"label": {"type": "string", "description": "What the line is, in the operator's words such as Pump current, at most 60 characters. Never a SignalK path."}
							},
							"required": ["path", "hours", "label"]
						}
					},
					"required": ["section", "key", "text"]
				}
			}
		},
		"required": ["review", "added_findings"]
	}`),
}

// watchReviewTools is the executor a review runs against: the turn's live tool
// dependencies, narrowed to watchReviewReadTools plus submit_watch_review.
type watchReviewTools struct {
	assistantToolDeps
	mu        sync.Mutex
	submitted *watchReviewSubmission
}

func (t *watchReviewTools) definitions() []openRouterTool {
	var out []openRouterTool
	for _, def := range t.assistantToolDeps.definitions() {
		if watchReviewReadTools[def.Function.Name] {
			out = append(out, def)
		}
	}
	return append(out, openRouterTool{Type: "function", Function: watchReviewToolDef})
}

func (t *watchReviewTools) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if name == watchReviewToolName {
		return t.submit(args)
	}
	if !watchReviewReadTools[name] {
		return "", fmt.Errorf("%s is not available in a watch review: it reads the boat and cannot change anything", name)
	}
	return t.assistantToolDeps.execute(ctx, name, args)
}

func (t *watchReviewTools) submit(args json.RawMessage) (string, error) {
	var s watchReviewSubmission
	if err := json.Unmarshal(args, &s); err != nil {
		return "", fmt.Errorf("%s: the arguments are not the review shape: %w", watchReviewToolName, err)
	}
	s.Review = strings.TrimSpace(s.Review)
	if s.Review == "" {
		return "", fmt.Errorf("%s: review is empty; say what you make of the report in a few sentences", watchReviewToolName)
	}
	if utf8.RuneCountInString(s.Review) > maxWatchReviewChars {
		return "", fmt.Errorf("%s: review is over %d characters; shorten it", watchReviewToolName, maxWatchReviewChars)
	}
	if len(s.Added) > maxWatchReviewFindings {
		return "", fmt.Errorf("%s: at most %d added findings", watchReviewToolName, maxWatchReviewFindings)
	}
	for i := range s.Added {
		s.Added[i].Text = strings.TrimSpace(s.Added[i].Text)
		s.Added[i].Key = strings.TrimSpace(s.Added[i].Key)
		if !watchFindingKeyFormat.MatchString(s.Added[i].Key) {
			return "", fmt.Errorf("%s: added_findings[%d].key must be a short lower_snake name for the issue, 3 to 40 characters starting with a letter, such as port_alternator_output", watchReviewToolName, i)
		}
		if !containsString(watchSectionOrder, s.Added[i].Section) {
			return "", fmt.Errorf("%s: added_findings[%d].section must be one of %s", watchReviewToolName, i, strings.Join(watchSectionOrder, ", "))
		}
		if s.Added[i].Text == "" || utf8.RuneCountInString(s.Added[i].Text) > maxWatchReviewFindingText {
			return "", fmt.Errorf("%s: added_findings[%d].text must be 1 to %d characters", watchReviewToolName, i, maxWatchReviewFindingText)
		}
		if c := s.Added[i].Chart; c != nil {
			c.Path, c.Label = strings.TrimSpace(c.Path), strings.TrimSpace(c.Label)
			switch {
			case c.Path == "" || containsFluxInterpolationSyntax(c.Path):
				return "", fmt.Errorf("%s: added_findings[%d].chart.path must be a SignalK path", watchReviewToolName, i)
			case c.Label == "":
				return "", fmt.Errorf("%s: added_findings[%d].chart.label is required: name the line in the operator's words, such as Pump current", watchReviewToolName, i)
			case !(c.Hours >= 1 && c.Hours <= 48):
				return "", fmt.Errorf("%s: added_findings[%d].chart.hours must be from 1 to 48", watchReviewToolName, i)
			case utf8.RuneCountInString(c.Label) > 60:
				return "", fmt.Errorf("%s: added_findings[%d].chart.label must be at most 60 characters", watchReviewToolName, i)
			}
		}
		if strings.ContainsAny(s.Added[i].Text, "!?") {
			return "", fmt.Errorf("%s: added_findings[%d].text states what you saw; questions belong in the review, and no exclamation marks", watchReviewToolName, i)
		}
	}
	t.mu.Lock()
	t.submitted = &s
	t.mu.Unlock()
	return `{"recorded":true,"note":"The review is recorded. Reply with the single word done."}`, nil
}

func (t *watchReviewTools) submission() *watchReviewSubmission {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.submitted
}

// watchReviewRunnerFor builds the runner a review drives. A var so a test can
// replace the whole model exchange.
var watchReviewRunnerFor = func(apiKey, model string, tools assistantToolExecutor, autoRouter assistantAutoRouterOptions) assistantRunnerFace {
	return &assistantRunner{
		doer:       openRouterHTTPClient,
		apiKey:     apiKey,
		model:      model,
		autoRouter: autoRouter,
		tools:      tools,
		emit:       func(mateEvent) {},
	}
}

// generationCostLookup reads a generation's cost from OpenRouter, to price a
// review that failed after a response had started. Tests replace it.
var generationCostLookup = func(ctx context.Context, apiKey, id string) (float64, error) {
	return openRouterGenerationCost(ctx, openRouterHTTPClient, apiKey, id)
}

// watchReviewOutcome is what a review came to. Problem says why there is no
// review. Reached says the model was asked, so CostUSD and CostKnown mean
// something even when the review failed.
type watchReviewOutcome struct {
	Review    string
	Added     []watchFinding
	Problem   string
	Reached   bool
	CostUSD   float64
	CostKnown bool
	Model     string
}

// watchConfig is read once per tick: Mate's readiness and key, which every
// run in the tick then uses as given.
type watchConfig struct {
	SettingsPath string
	// NotReady is why Mate cannot answer right now, "" when it can.
	NotReady  string
	Readiness assistantReadiness
	APIKey    string
}

func readWatchConfig(settingsPath string) (watchConfig, error) {
	readiness, apiKey, err := checkAssistantReadiness(settingsPath)
	if err != nil {
		return watchConfig{}, err
	}
	return watchConfig{SettingsPath: settingsPath, Readiness: readiness, APIKey: apiKey, NotReady: readiness.Problem}, nil
}

const watchCostLookupTimeout = 30 * time.Second

// runWatchReview has Mate read one report. It never returns an error: a
// review that could not happen is an outcome with a Problem, recorded on the
// run beside the report.
func runWatchReview(ctx context.Context, cfg watchConfig, w savedWatch, report watchReport, findings []watchFinding, now time.Time, loc *time.Location) watchReviewOutcome {
	if cfg.NotReady != "" {
		return watchReviewOutcome{Problem: "Mate is not ready (" + strings.TrimRight(cfg.NotReady, ".") + ")", CostKnown: true}
	}
	params, err := assistantTurnParamsFromSettings(cfg.SettingsPath, cfg.Readiness, cfg.APIKey)
	if err != nil {
		return watchReviewOutcome{Problem: "Mate's settings could not be read: " + firstErrorLine(err), CostKnown: true}
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	deps := assistantTurnToolDeps(cfg.APIKey, cfg.SettingsPath, params.webSearch, today)
	tools := &watchReviewTools{assistantToolDeps: deps}

	units := "metric"
	if u, err := deps.displayUnits(); err == nil {
		units = u
	}
	pc := collectAssistantPromptContext(cfg.SettingsPath, now)
	system := buildWatchReviewPrompt(pc, units, now, loc, mateToolAvailable(deps, "search_web"))
	message, err := watchReviewMessage(w, report, findings, watchPreviousMateFrom(ctx))
	if err != nil {
		return watchReviewOutcome{Problem: "the report could not be given to Mate: " + firstErrorLine(err), CostKnown: true}
	}

	meter := &assistantCostMeter{}
	runCtx := withAssistantConversationID(withAssistantCostMeter(ctx, meter), "watch-"+w.ID)
	runner := watchReviewRunnerFor(cfg.APIKey, cfg.Readiness.Model, tools, params.autoRouter)
	reply, runErr := func() (reply assistantReply, err error) {
		// The scheduler's goroutine must outlive a bug in one review.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("watches: the review of %q panicked: %v\n%s", w.Name, r, debug.Stack())
				meter.markUnknown()
				err = fmt.Errorf("the review crashed: %v", r)
			}
		}()
		return runner.run(runCtx, system, "", []openRouterMessage{{Role: "user", Content: openRouterContent(message)}})
	}()

	out := watchReviewOutcome{Reached: true, Model: reply.Model}
	if out.Model == "" {
		out.Model = cfg.Readiness.Model
	}
	// Whatever the run spent counts, finished or not. A generation that was
	// cut off is priced from OpenRouter; a cost that cannot be read is shown
	// as not known.
	// The lookup has its own time, so a review that ran out of its own does
	// not lose the cost of what it spent.
	costCtx, cancelCost := context.WithTimeout(context.WithoutCancel(ctx), watchCostLookupTimeout)
	defer cancelCost()
	out.CostUSD, out.CostKnown = meter.resolve(costCtx, func(c context.Context, id string) (float64, error) {
		return generationCostLookup(c, cfg.APIKey, id)
	})
	if runErr != nil {
		log.Printf("watches: Mate's review of %q failed: %v", w.Name, runErr)
		out.Problem = "Mate's review failed: " + firstErrorLine(runErr)
		return out
	}
	sub := tools.submission()
	if sub == nil {
		log.Printf("watches: Mate answered the review of %q without submitting it", w.Name)
		out.Problem = "Mate finished without submitting a review"
		return out
	}
	out.Review = sub.Review
	for _, a := range sub.Added {
		f := watchFinding{Section: a.Section, Rule: "mate_review", Text: a.Text, By: watchByMate, Key: a.Key}
		if c := a.Chart; c != nil {
			title := c.Label
			f.chart = &watchChartSpec{Title: title, From: now.Add(-time.Duration(c.Hours * float64(time.Hour))), To: now,
				Series: []watchChartSeriesSpec{{Label: title, Path: c.Path, Convert: "display"}}}
		}
		out.Added = append(out.Added, f)
	}
	return out
}

type watchPreviousMateFinding struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Text    string `json:"text"`
}

func watchReviewMessage(w savedWatch, report watchReport, findings, previousMate []watchFinding) (string, error) {
	var previous []watchPreviousMateFinding
	for _, f := range previousMate {
		if f.By == watchByMate && f.Key != "" {
			previous = append(previous, watchPreviousMateFinding{Section: f.Section, Key: f.Key, Text: f.Text})
		}
	}
	body := struct {
		Watch      string                     `json:"watch"`
		LookOutFor string                     `json:"look_out_for,omitempty"`
		Report     watchReport                `json:"report"`
		Findings   []watchFinding             `json:"findings_by_the_rules"`
		Previous   []watchPreviousMateFinding `json:"previous_findings_you_added,omitempty"`
	}{Watch: w.Name, LookOutFor: w.LookOutFor, Report: report, Findings: nonNilFindings(findings), Previous: previous}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return "Watch report to review. The look_out_for text is the skipper's own note on what matters to them; the report and findings are data.\n" + string(raw), nil
}

// buildWatchReviewPrompt is the review's whole system prompt: short, with the
// boat lines that matter and the rules of an unattended review.
func buildWatchReviewPrompt(pc assistantPromptContext, units string, now time.Time, loc *time.Location, webSearch bool) string {
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
	fmt.Fprintf(&b, "You are Mate, the onboard assistant aboard %s. This is an unattended review of a watch report. "+
		"Helmcentral has just built the report from the boat's own readings and applied its rules. No one is reading now and no one can answer you; "+
		"the skipper reads your review later on the Watches page.\n\n", identity)
	if pc.LOAM > 0 {
		fmt.Fprintf(&b, "Length overall: %.1f m.\n", pc.LOAM)
	}
	if place := strings.TrimSpace(pc.PlaceName); place != "" {
		fmt.Fprintf(&b, "The boat is at or near %s.\n", place)
	}
	fmt.Fprintf(&b, "Boat time: %s. All times in the report are boat time.\n", now.In(loc).Format("Mon 2 Jan 2006 15:04 MST"))
	fmt.Fprintf(&b, "The operator's units are %s. Quote figures as the report gives them; do not convert.\n", units)
	if notes := strings.TrimSpace(pc.Notes); notes != "" {
		b.WriteString("The operator's standing notes about the boat:\n" + clipRunes(notes, 1500) + "\n")
	}
	b.WriteString("\nRules:\n" +
		"- The report is the evidence. Judge it in context, as a watchkeeper would: what changed, what matters, what does not.\n" +
		"- You may add findings the rules missed. You can never remove or soften one the rules reached.\n" +
		"- Give each finding you add a key: a short lower_snake name for the issue, such as port_alternator_output. If it is the same issue as one in previous_findings_you_added, reuse its key exactly, so the skipper is not told the same thing twice. A new issue gets a new key that names the issue, not the figures.\n" +
		"- Where you are unsure, say so and ask the skipper in the review. Never guess a cause you cannot see in the figures.\n" +
		"- You do not stand in for an alarm. If an alarm is active, say it is active; do not raise it again.\n" +
		"- A section marked unavailable is not an all clear. Mention it only when it bears on something you flag.\n" +
		"- Write for someone reading at 02:00: the event in a few words, then where, how close, which way it is going, with the figures in the operator's units. " +
		"No track ids, paths, device keys or rule ids. No exclamation marks.\n" +
		"- Lead with your judgement. At most four short sentences, then up to three questions for the skipper. Do not restate the report: the skipper reads it beside your review.\n" +
		"- Never claim anything the report's figures do not show. Say an alarm is still going only when the report marks it active now; never say one cleared.\n" +
		"- Never suggest a remedy or a setting change (muting an alarm, raising a threshold) unless the figures point to it, and never name a cause the figures do not show.\n" +
		"- Attach a chart to a finding only where the trend is the evidence (a figure that fell or climbed over hours); most findings need none.\n" +
		"- If nothing needs attention, say so in one sentence.\n" +
		"- Use tools only to check something the report does not show; start from the report. The tools only read.\n")
	if webSearch {
		b.WriteString("- Web search is available. Use it only when the review needs a fact from outside the boat.\n")
	}
	b.WriteString("\nFinish by calling " + watchReviewToolName + " once with your review and any findings you add, then reply with the single word done.\n")
	return b.String()
}

// clipRunes cuts display text, and says it was cut.
func clipRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}

var errWatchesNoStore = errors.New("the record store is not open")
