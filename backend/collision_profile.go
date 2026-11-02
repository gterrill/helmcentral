package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	collisionPluginID         = "signalk-ais-target-prioritizer"
	collisionProfilesLoadPath = "/plugins/" + collisionPluginID + "/loadCollisionProfiles"
	collisionProfilesSavePath = "/plugins/" + collisionPluginID + "/saveCollisionProfiles"

	// collisionProfilePollInterval is how often a standing refusal re-reads the
	// plugin's profiles to see whether the operator has fixed it by hand. It is
	// slow on purpose: the alarm is a warning, not a race, and the read is an
	// authenticated call into another project's server.
	collisionProfilePollInterval = time.Minute

	// collisionSaveBackoffMax caps the wait between save retries while the
	// plugin keeps refusing the write.
	collisionSaveBackoffMax = 10 * time.Minute

	collisionProfileRuleID = "collision-profile"
	collisionProfileLabel  = "Collision Profile Not Changed"

	// collisionProfilePath is Helmcentral's own, alongside watchdogPath, and is
	// registered in ownedNotificationPaths so the bus watcher does not read it
	// back off the tree and raise it a second time.
	collisionProfilePath = "notifications.helmcentral.collisionProfile"
)

// vesselStateProfiles maps signalk-autostate's vocabulary onto the target
// prioritizer's profiles (ADR 0058).
//
// offshore is deliberately absent. Autostate has no notion of blue water, and
// inferring it from "moving" would be a guess dressed up as a decision, so it
// stays a manual choice.
var vesselStateProfiles = map[string]string{
	"anchored": "anchor",
	"moored":   "harbor",
	"sailing":  "coastal",
	"motoring": "coastal",
}

type collisionTier struct {
	Cpa   float64 `json:"cpa"`
	Tcpa  float64 `json:"tcpa"`
	Speed float64 `json:"speed"`
}

type collisionGuard struct {
	Range float64 `json:"range"`
	Speed float64 `json:"speed"`
}

type collisionProfile struct {
	Warning collisionTier  `json:"warning"`
	Danger  collisionTier  `json:"danger"`
	Guard   collisionGuard `json:"guard"`
}

// canFire reports whether any alarm in this profile is reachable at all.
//
// The plugin compares cpa < profile.cpa and range < profile.guard.range, so a
// profile whose thresholds are all zero can never trip: every comparison is
// against zero and fails. The shipped anchor profile is exactly this. It is not
// a quiet profile, it is a silent one, and adopting it on anchoring would take
// the collision alarm offline at the moment the boat is left unattended
// (ADR 0058).
//
// A guard ring on its own is a working profile, and is the shape ADR 0058
// actually recommends at anchor, so a zero CPA alone must not condemn it.
func (p collisionProfile) canFire() bool {
	return p.Warning.Cpa > 0 || p.Danger.Cpa > 0 || p.Guard.Range > 0
}

// parseCollisionProfiles reads the plugin's profile document, which is a flat
// object of profiles alongside a "current" naming the active one.
func parseCollisionProfiles(raw []byte) (string, map[string]collisionProfile, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", nil, fmt.Errorf("could not read the collision profiles: %w", err)
	}

	rawCurrent, ok := document["current"]
	if !ok {
		return "", nil, fmt.Errorf("the collision profile document names no current profile")
	}
	var current string
	if err := json.Unmarshal(rawCurrent, &current); err != nil {
		return "", nil, fmt.Errorf("could not read the current collision profile: %w", err)
	}

	profiles := map[string]collisionProfile{}
	for name, rawProfile := range document {
		if name == "current" {
			continue
		}
		var profile collisionProfile
		// Keys that are not profiles are skipped rather than fatal: this is
		// another project's document and it is free to grow settings that are
		// none of our business. Only the names we actually select are ever
		// read, and those are validated before use.
		if err := json.Unmarshal(rawProfile, &profile); err != nil {
			continue
		}
		profiles[name] = profile
	}
	return current, profiles, nil
}

// collisionProfileSyncer keeps the target prioritizer's active profile in step
// with navigation.state (ADR 0058).
type collisionProfileSyncer struct {
	snapshot *signalKSnapshot

	// lastState is the navigation.state already acted on. The syncer is
	// edge-triggered on this rather than reconciling continuously, which is
	// what lets a manual override stick: under way the mapping says coastal, so
	// correcting every disagreement would stomp a deliberate switch to offshore
	// on the next tick and the setting would appear to refuse to hold.
	//
	// It is persisted (persistState) and restored at startup. Held in memory
	// alone, every restart would see the first state as a transition and
	// overwrite the operator's manual choice, on every deploy. It is persisted
	// only once a transition is settled, never when refused, so a restart
	// retries a refusal.
	lastState string

	// refused is the profile a previous transition declined to select, held so
	// the raise can be cleared later. Without it the alarm log keeps an open
	// row for a problem that has since been fixed.
	refused string

	// mu guards the standing-refusal fields below: check runs on the evaluator
	// goroutine while active is read by the alarm handlers.
	mu sync.Mutex

	// standing is true while a refusal is open, whatever profile it declined.
	standing bool

	// refusedAt is when the standing refusal was raised.
	refusedAt time.Time

	// refusedOn is the profile the plugin was on when the refusal was raised
	// or last reworded. Empty when that is unknown (the read failed, or the
	// refusal was adopted from an old row). It is persisted with the refusal
	// (persistRefusedOn) so a restart keeps it. The "moved by hand" rule needs
	// it and is skipped while it is empty: no baseline is guessed.
	refusedOn string

	// checkedAt is the time of the check in progress, used as the raised time.
	checkedAt time.Time

	// polledAt is when a standing refusal last re-read the profiles, kept so
	// the poll waits a full interval after a failed read too. Only check
	// touches it.
	polledAt time.Time

	// refusedMessage is the card body of the standing refusal, so a later
	// refusal with different words updates the card instead of leaving it stale.
	refusedMessage string

	load func() (string, map[string]collisionProfile, error)
	save func(profile string) error

	// persistState stores lastState so a restart is not a transition. Nil in
	// tests that do not need it.
	persistState func(state string) error

	// persistRefusedOn stores refusedOn beside lastState: written on refuse,
	// cleared on resolve. Nil in tests that do not need it.
	persistRefusedOn func(profile string) error

	// persistMu orders the writes to persistRefusedOn, outside mu.
	persistMu sync.Mutex

	// The fields below are touched only by check() on the alarm service loop.

	// saveRetry spaces out save retries after a failed save.
	saveRetry saveBackoff

	// unmappedSince is when the boat entered a state with no mapped profile,
	// zero while it is in a mapped one. A standing refusal resolves only once
	// the state has stayed unmapped for a poll interval, so a one-tick blip
	// does not clear and re-raise it.
	unmappedSince time.Time

	// lastSaveErr is the last save failure since the refusal began, so the card
	// can be reworded without another PUT while the retry is backed off.
	lastSaveErr error

	// lastRefusalLog and lastReadErr are the text last logged, so a repeated
	// refusal or a persistently failing read is logged once, not every poll.
	lastRefusalLog string
	lastReadErr    string
}

// saveBackoff is the wait between save retries: one poll interval after the
// first failure, doubling up to collisionSaveBackoffMax. Neither
// notifyBackoffFor nor placeNameBackoffState fits: one counts attempts for a
// queue, the other reads the wall clock and logs.
type saveBackoff struct {
	delay time.Duration
	until time.Time
}

func (b *saveBackoff) reset() { *b = saveBackoff{} }

// due reports whether a save may be tried at now.
func (b *saveBackoff) due(now time.Time) bool { return !now.Before(b.until) }

// fail records a failed save at now and schedules the next try.
func (b *saveBackoff) fail(now time.Time) {
	if b.delay == 0 {
		b.delay = collisionProfilePollInterval
	} else {
		b.delay = min(2*b.delay, collisionSaveBackoffMax)
	}
	b.until = now.Add(b.delay)
}

func newCollisionProfileSyncer(snapshot *signalKSnapshot) *collisionProfileSyncer {
	return &collisionProfileSyncer{
		snapshot: snapshot,
		load:     loadCollisionProfiles,
		save:     saveCollisionProfile,
	}
}

// adoptOpenRefusal restores the standing refusal from the alarm log's open
// row, so a restart neither raises a second row for the same problem nor
// leaves the old one open after a good load. The alarm shows as active again
// with its original raised time, and nothing is dispatched: it was sent when
// it was first raised.
//
// When the log holds several open rows (earlier restarts piled them up), the
// caller passes the newest. RecordStateChange and MarkCleared act on the same
// row, the newest open one for this rule, so updates and the clear land on
// the adopted row. Older rows are left as they are; no clear is invented.
func (s *collisionProfileSyncer) adoptOpenRefusal(open alarmLogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.standing, s.refusedMessage, s.refusedAt = true, open.Message, open.RaisedAt
}

// attachStateStore restores lastState from the store and persists it from
// then on, so the state the boat is still in after a restart is not acted on
// a second time.
func (s *collisionProfileSyncer) attachStateStore(load func() (string, error), save func(string) error) error {
	state, err := load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastState, s.persistState = state, save
	return nil
}

// attachRefusalStore restores the refused-on profile of an adopted refusal and
// persists it from then on. Call it after adoptOpenRefusal: a stored profile
// with no open refusal is stale and ignored.
func (s *collisionProfileSyncer) attachRefusalStore(load func() (string, error), save func(string) error) error {
	profile, err := load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.standing {
		s.refusedOn = profile
	}
	s.persistRefusedOn = save
	return nil
}

// storeRefusedOn writes the refused-on profile to the store. The caller does
// not hold mu, which active() takes: a database write must not block the alarm
// handlers. persistMu keeps writes in order. A failed write is logged: the cost
// is that a restart loses the baseline and skips the "moved by hand" rule.
// refuse and resolve are called only from check() on the alarm service loop,
// so persistMu ordering is enough.
func (s *collisionProfileSyncer) storeRefusedOn(profile string) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if s.persistRefusedOn == nil {
		return
	}
	if err := s.persistRefusedOn(profile); err != nil {
		log.Printf("collision profile: could not remember the refused-on profile %q: %v", profile, err)
	}
}

// remember stores the state once it has been acted on. A failed write is
// logged and not fatal: it must not stop the profile switch, and the cost is
// that a restart may treat this state as a transition again.
func (s *collisionProfileSyncer) remember(state string) {
	if s.persistState == nil {
		return
	}
	if err := s.persistState(state); err != nil {
		log.Printf("collision profile: could not remember navigation state %q: %v", state, err)
	}
}

// active is the standing refusal as an active alarm, empty when none stands.
func (s *collisionProfileSyncer) active() []alarmStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.standing {
		return nil
	}
	status := collisionProfileEvent(alarmEventRaised, alarmStateWarn, s.refusedMessage).Status
	status.RaisedAt = s.refusedAt
	return []alarmStatus{status}
}

func (s *collisionProfileSyncer) check(now time.Time) []alarmEvent {
	s.checkedAt = now
	state := vesselNavigationState(s.snapshot)
	if state == "" {
		return nil
	}
	// The one place that owns the unmapped clock: it starts when the boat is
	// first seen in a state with no profile and stops when it enters a mapped
	// one, so unmapped to unmapped does not restart it.
	want, mapped := vesselStateProfiles[state]
	if !mapped && s.unmappedSince.IsZero() {
		s.unmappedSince = now
	} else if mapped {
		s.unmappedSince = time.Time{}
	}
	if state == s.lastState {
		return s.pollStandingRefusal(now, state, want, mapped)
	}
	// Recorded before the work below, so a refusal is reported once for the
	// transition rather than retried on every tick for as long as it lasts.
	s.lastState = state

	if !mapped {
		// Nothing is wanted here. A standing refusal is resolved by the poll
		// once the state has stayed unmapped for an interval; clearing now
		// would flap on a one-tick blip.
		s.remember(state)
		return nil
	}

	current, profiles, err := s.load()
	if err != nil {
		// Noted so the poll does not log the same failure again. The refusal
		// below logs the error itself.
		s.noteRead(err, false)
		var statusErr *signalkStatusError
		if errors.As(err, &statusErr) && (statusErr.Status == http.StatusUnauthorized || statusErr.Status == http.StatusForbidden) {
			return s.refuse(want, "", err, fmt.Sprintf(
				"SignalK refused Helmcentral's login when reading the collision profiles, so the boat is %s on the old profile.", state))
		}
		return s.refuse(want, "", err, fmt.Sprintf(
			"Helmcentral could not read the collision profiles from SignalK, so the boat is %s on the old profile.", state))
	}
	s.noteRead(nil, true)
	// The operator's own pick wins on a transition too: the state can flip
	// before the next poll, and the pick must be neither overwritten nor taken
	// for the baseline.
	if s.movedByHand(current, profiles) {
		return s.settleByHand(state, current)
	}
	return s.evaluate(state, want, current, profiles)
}

// causeClass is a stable name for a failure, so a log line is not repeated
// because an ephemeral port or timestamp in the error text changed: the HTTP
// status from SignalK if there is one, otherwise "other".
func causeClass(err error) string {
	var statusErr *signalkStatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("status %d", statusErr.Status)
	}
	return "other"
}

// noteRead records the outcome of reading the profiles, for the transition and
// the poll alike. A failure is logged (when logFailure is set) only when its
// class changes, and a recovery is logged once.
func (s *collisionProfileSyncer) noteRead(err error, logFailure bool) {
	if err == nil {
		if s.lastReadErr != "" {
			s.lastReadErr = ""
			log.Printf("collision profile: the profiles can be read again")
		}
		return
	}
	if class := causeClass(err); class != s.lastReadErr {
		s.lastReadErr = class
		if logFailure {
			log.Printf("collision profile: could not re-read the profiles to check the refusal: %v", err)
		}
	}
}

// evaluate decides what a state should do given what the plugin reports now.
// A transition and the poll of a standing refusal both run it, so a remedy
// applied while the refusal stands is acted on the way a fresh transition
// would act on it.
func (s *collisionProfileSyncer) evaluate(state, want, current string, profiles map[string]collisionProfile) []alarmEvent {
	profile, defined := profiles[want]
	// A plugin sitting on a wanted profile that cannot fire is not a fix: the
	// refusal is still true.
	if current == want && defined && profile.canFire() {
		s.remember(state)
		return s.resolve()
	}

	if !defined || !profile.canFire() {
		if current != want && s.movedByHand(current, profiles) {
			return s.settleByHand(state, current)
		}
		if !defined {
			return s.refuse(want, current, nil, fmt.Sprintf(
				"The %q collision profile is not defined in SignalK, so the boat is %s on the old profile.", want, state))
		}
		return s.refuse(want, current, nil, fmt.Sprintf(
			"The %q collision profile has every CPA and guard range at zero, so it would silence AIS collision alarms. The boat is %s on the old profile.", want, state))
	}
	// A failed save backs off: a plugin that allows reads but refuses the
	// write would otherwise take a failed PUT every minute. Reads and the
	// checks above still run on every poll.
	if !s.saveRetry.due(s.checkedAt) && s.lastSaveErr != nil {
		// No PUT, but the card still says what is true now: the state and the
		// reason may have moved since the last failure.
		return s.refuse(want, current, s.lastSaveErr, saveFailureMessage(want, state, s.lastSaveErr))
	}
	if err := s.save(want); err != nil {
		s.saveRetry.fail(s.checkedAt)
		s.lastSaveErr = err
		return s.refuse(want, current, err, saveFailureMessage(want, state, err))
	}

	log.Printf("collision profile: vessel is %s, selected %q", state, want)
	s.remember(state)
	return s.resolve()
}

// saveFailureMessage is the card body for a switch the plugin did not take.
func saveFailureMessage(want, state string, err error) string {
	var statusErr *signalkStatusError
	if errors.As(err, &statusErr) && (statusErr.Status == http.StatusUnauthorized || statusErr.Status == http.StatusForbidden) {
		return fmt.Sprintf(
			"SignalK refused Helmcentral's login when switching to the %q collision profile, so the boat is %s on the old profile.", want, state)
	}
	return fmt.Sprintf(
		"Helmcentral could not switch SignalK to the %q collision profile, so the boat is %s on the old profile.", want, state)
}

// movedByHand reports whether a refusal stands and the operator has put the
// plugin on a working profile other than the one it was on at the refusal.
// With no known baseline it is false: guessing one is how the warning once got
// stuck or cleared wrongly.
func (s *collisionProfileSyncer) movedByHand(current string, profiles map[string]collisionProfile) bool {
	s.mu.Lock()
	standing, refusedOn := s.standing, s.refusedOn
	s.mu.Unlock()
	if !standing || refusedOn == "" || current == refusedOn {
		return false
	}
	profile, defined := profiles[current]
	return defined && profile.canFire()
}

// settleByHand clears a refusal the operator settled by picking a profile.
func (s *collisionProfileSyncer) settleByHand(state, current string) []alarmEvent {
	log.Printf("collision profile: the plugin is on %q, chosen by hand, so the refusal is cleared", current)
	// The refusal path left this state unsaved so a restart would retry it.
	// Now that it is settled, save it: a restart would otherwise replay the
	// transition.
	s.remember(state)
	return s.resolve()
}

// pollStandingRefusal retries a refusal that has stood since the last
// transition.
//
// The syncer acts on a change of navigation.state (ADR 0058), so a boat that
// stays anchored never reloads the profiles. The remedies for a refusal are
// made in the plugin: fill in the wanted profile, or pick another working
// profile. While a refusal stands, and the state has not changed, the profiles
// are re-read about once a minute and the evaluation a transition runs is run
// again for the current state:
//   - the operator has put the plugin on a working profile other than the one
//     it was on at the refusal: leave their choice and clear;
//   - the wanted profile now works and the plugin is not on it: switch to it;
//   - the plugin is already on the wanted profile: clear;
//   - the state has no mapped profile and has stayed that way for a poll
//     interval: nothing is wanted, so clear;
//   - otherwise the refusal stands, and its words are updated if the reason
//     has changed.
//
// A failed save backs off, doubling from one poll interval up to
// collisionSaveBackoffMax. Only the save waits: the read and the checks above
// run every poll, and a skipped save still rewords the card from the last
// failure.
//
// A failed read never clears the warning, since a warning that clears on the
// strength of data we could not read would be a fallback. A failing read, and
// a recovery, are logged when the failure class changes, not once a poll.
func (s *collisionProfileSyncer) pollStandingRefusal(now time.Time, state, want string, mapped bool) []alarmEvent {
	s.mu.Lock()
	standing, since := s.standing, s.refusedAt
	s.mu.Unlock()
	if !standing {
		return nil
	}
	if !mapped && now.Sub(s.unmappedSince) < collisionProfilePollInterval {
		return nil
	}
	if s.polledAt.After(since) {
		since = s.polledAt
	}
	if now.Sub(since) < collisionProfilePollInterval {
		return nil
	}
	s.polledAt = now

	current, profiles, err := s.load()
	s.noteRead(err, true)
	if err != nil {
		return nil
	}
	if !mapped {
		// Nothing is wanted in a state with no profile, and the read worked.
		return s.resolve()
	}
	// The operator's own pick wins over the retry: switching would overwrite
	// it, and a login that still fails would keep warning about a boat they
	// have already sorted.
	if s.movedByHand(current, profiles) {
		return s.settleByHand(state, current)
	}
	return s.evaluate(state, want, current, profiles)
}

// refuse leaves the plugin alone and raises Helmcentral's own alarm instead.
// Staying loud on the wrong profile is a worse night and a better outcome than
// going quiet on the right one.
func (s *collisionProfileSyncer) refuse(profile, current string, cause error, message string) []alarmEvent {
	// Logged when the message or the cause text differs from the last line, so
	// a 403 after a 401 shows but a refusal repeated each poll does not. The
	// cause is compared by class, not text, which can carry a changing port.
	key := message
	logged := message
	if cause != nil {
		key += "|" + causeClass(cause)
		logged += ": " + cause.Error()
	}
	if key != s.lastRefusalLog {
		s.lastRefusalLog = key
		log.Printf("collision profile: refusing %q: %s", profile, logged)
	}
	s.mu.Lock()
	if s.standing && message == s.refusedMessage {
		// One refusal is standing already, in the same words. Raising again on
		// every navigation state change piled up hundreds of open rows in the
		// alarm log.
		s.mu.Unlock()
		return nil
	}
	if s.standing {
		// The standing card is updated in place when the words change, so it
		// never shows an old state or cause.
		s.refused, s.refusedMessage = profile, message
		// A failed read passes no current profile; it must not wipe the known
		// one.
		if current != "" {
			s.refusedOn = current
		}
		refusedOn := s.refusedOn
		s.mu.Unlock()
		if current != "" {
			s.storeRefusedOn(refusedOn)
		}
		return []alarmEvent{collisionProfileEvent(alarmEventChanged, alarmStateWarn, message)}
	}
	s.standing, s.refused, s.refusedMessage, s.refusedAt, s.refusedOn = true, profile, message, s.checkedAt, current
	s.mu.Unlock()
	s.storeRefusedOn(current)
	return []alarmEvent{collisionProfileEvent(alarmEventRaised, alarmStateWarn, message)}
}

func (s *collisionProfileSyncer) resolve() []alarmEvent {
	s.saveRetry.reset()
	s.lastRefusalLog, s.lastReadErr, s.lastSaveErr = "", "", nil
	s.mu.Lock()
	if !s.standing {
		s.mu.Unlock()
		return nil
	}
	s.standing, s.refused, s.refusedMessage, s.refusedAt, s.refusedOn = false, "", "", time.Time{}, ""
	s.mu.Unlock()
	s.storeRefusedOn("")
	return []alarmEvent{collisionProfileEvent(alarmEventCleared, alarmStateNormal, "")}
}

func collisionProfileEvent(kind, state, message string) alarmEvent {
	phase := alarmPhaseActive
	if kind == alarmEventCleared {
		phase = alarmPhaseNormal
	}

	status := alarmStatus{
		RuleID:  collisionProfileRuleID,
		Label:   collisionProfileLabel,
		Path:    collisionProfilePath,
		Phase:   phase,
		State:   state,
		Message: message,
	}
	return alarmEvent{
		Kind: kind,
		Rule: alarmRule{
			ID:      collisionProfileRuleID,
			Enabled: true,
			Label:   collisionProfileLabel,
			Path:    collisionProfilePath,
			State:   state,
		},
		Status: status,
		Source: alarmSourceRule,
	}
}

// vesselNavigationState reads navigation.state, which signalk-autostate keeps
// current from anchor position, movement and engine state.
func vesselNavigationState(snapshot *signalKSnapshot) string {
	state, _ := snapshot.liveSelfTelemetry(time.Now().UTC()).Sample("navigation.state").Value.(string)
	return state
}

func loadCollisionProfiles() (string, map[string]collisionProfile, error) {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return "", nil, fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	status, body, err := signalkRequestJSONWithAuthBody(
		buildSignalKURL(address, port), settingsPath,
		collisionProfilesLoadPath, http.MethodGet, nil,
	)
	if err != nil {
		return "", nil, err
	}
	if status < 200 || status >= 300 {
		return "", nil, &signalkStatusError{Status: status, Body: string(body)}
	}
	return parseCollisionProfiles(body)
}

// signalkStatusError is a non-2xx answer from SignalK, kept as a status so a
// refused login can be told from any other failure without reading the body.
type signalkStatusError struct {
	Status int
	Body   string
}

func (e *signalkStatusError) Error() string {
	return fmt.Sprintf("signalk returned status %d: %s", e.Status, e.Body)
}

// saveCollisionProfile switches the active profile and nothing else.
//
// The plugin's save handler is Object.assign(collisionProfiles, req.body), a
// shallow merge, so a body of exactly {"current": name} leaves all four
// threshold sets untouched. Those are the operator's, informed by their own
// waters (ADR 0058).
func saveCollisionProfile(profile string) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	status, body, err := signalkRequestJSONWithAuthBody(
		buildSignalKURL(address, port), settingsPath,
		collisionProfilesSavePath, http.MethodPut,
		map[string]string{"current": profile},
	)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &signalkStatusError{Status: status, Body: string(body)}
	}
	return nil
}
