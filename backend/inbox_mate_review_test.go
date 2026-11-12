package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review fixes for Mate on the Inbox (ADR 0282).

// ── turning Mate on sorts what was waiting ───────────────────────────────

// BEHAVIOUR: items stored while Mate was off are sorted after Mate comes on
// and the list is read, with no new upload.
func TestInboxListWakesTheClassifierWhenMateIsOnAndItemsAreWaiting(t *testing.T) {
	w := newInboxWorld(t)
	withMateOff(t)
	id := uploadSortable(t, 100) // stored while Mate is off
	var woken atomic.Int32
	prev := inboxClassifierWake
	inboxClassifierWake = func() { woken.Add(1) }
	t.Cleanup(func() { inboxClassifierWake = prev })

	inboxList(t)
	if woken.Load() != 0 {
		t.Fatalf("the list woke the classifier while Mate was off: %d", woken.Load())
	}

	doer := &fakeMateDoer{}
	withInboxMate(t, doer)
	inboxList(t)
	if woken.Load() != 1 {
		t.Fatalf("Mate is on and an item is waiting, but the list woke the classifier %d times", woken.Load())
	}
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := itemJSON(t, id); got["suggested_kind"] != "scenery" {
		t.Errorf("the waiting item was not sorted: %v", got)
	}

	woken.Store(0)
	inboxList(t)
	if woken.Load() != 0 {
		t.Errorf("nothing is waiting, but the list woke the classifier")
	}
}

// BEHAVIOUR: saving settings drops the remembered reading of them and wakes the
// classifier, so switching Mate on sorts what is waiting at once.
func TestInboxSettingsSaveWakesTheClassifierAndRereadsMate(t *testing.T) {
	var reads atomic.Int32
	prev := globalInboxMate
	globalInboxMate = &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			reads.Add(1)
			return assistantReadiness{}, "", nil
		},
		doer: &fakeMateDoer{},
	}
	t.Cleanup(func() { globalInboxMate = prev })
	var woken atomic.Int32
	prevWake := inboxClassifierWake
	inboxClassifierWake = func() { woken.Add(1) }
	t.Cleanup(func() { inboxClassifierWake = prevWake })

	globalInboxMate.session()
	globalInboxMate.session()
	if reads.Load() != 1 {
		t.Fatalf("setup: settings read %d times", reads.Load())
	}
	inboxMateSettingsChanged()
	globalInboxMate.session()
	for i := 0; i < 400 && woken.Load() == 0; i++ { // the wake runs in the background
		time.Sleep(5 * time.Millisecond)
	}
	if woken.Load() != 1 || reads.Load() != 2 {
		t.Errorf("woken %d times, settings read %d times (want 1 and 2)", woken.Load(), reads.Load())
	}
}

// ── a big title request is bounded ───────────────────────────────────────

// BEHAVIOUR: Mate titles at most 20 photos in one request, in the order asked;
// the rest are told to ask again and cost no call.
func TestInboxMateTitlesAreCappedAtTwentyPerRequest(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"T"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	var ids []string
	for i := 0; i < 25; i++ {
		ids = append(ids, uploadForFiling(t, uint8(10+i), map[string]string{"capture_ms": ms(at.Add(time.Duration(i) * time.Second))}))
	}
	titles, errs := titlesFor(t, ids...)
	if doer.calls() != 20 {
		t.Errorf("model calls: %d, want 20", doer.calls())
	}
	for i, id := range ids {
		if i < 20 {
			if titles[id] != "T" || errs[id] != nil {
				t.Errorf("photo %d should be titled: %v %v", i, titles[id], errs[id])
			}
			continue
		}
		if titles[id] != "" || errs[id] != "Mate titles up to 20 photos at a time; open again for the rest" {
			t.Errorf("photo %d past the cap: title %q error %v", i, titles[id], errs[id])
		}
	}
}

// ── the title timeout starts at the call, not in the queue ───────────────

// BEHAVIOUR: a title waiting for a shared Mate slot is not charged for the wait.
func TestInboxMateTitleTimeoutStartsWhenTheSlotIsHeld(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{delay: 150 * time.Millisecond, reply: func(int, string) (string, error) { return `{"title":"T"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	withMateTitleTimeout(t, 250*time.Millisecond)
	// Four titles and three slots: the fourth waits 150 ms, then calls for 150 ms.
	var ids []string
	for i := 0; i < 4; i++ {
		ids = append(ids, uploadForFiling(t, uint8(60+i), map[string]string{"capture_ms": ms(at.Add(time.Duration(i) * time.Second))}))
	}
	titles, errs := titlesFor(t, ids...)
	for _, id := range ids {
		if titles[id] != "T" || errs[id] != nil {
			t.Errorf("%s: title %q error %v (the wait for a slot used up the call's time)", id, titles[id], errs[id])
		}
	}
}

// ── a failed readiness check does not fail the list ──────────────────────

func TestInboxListSurvivesMateSettingsThatCannotBeRead(t *testing.T) {
	newInboxWorld(t)
	id := uploadSortable(t, 101)
	prev := globalInboxMate
	globalInboxMate = &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			return assistantReadiness{}, "", errors.New("read /data/settings.yaml: permission denied")
		},
		doer: &fakeMateDoer{},
	}
	t.Cleanup(func() { globalInboxMate = prev })

	code, body := getJSON(t, listInboxHandler, "/api/inbox", "")
	if code != 200 {
		t.Fatalf("the list failed: %d %v", code, body)
	}
	if body["mate_sorting"] != nil {
		t.Errorf("mate_sorting should be null when the settings cannot be read: %v", body["mate_sorting"])
	}
	msg, _ := body["mate_error"].(string)
	if msg != "Mate's settings couldn't be read; sorting is paused" {
		t.Errorf("mate_error: %q", msg)
	}
	if items, _ := body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != id {
		t.Errorf("the items must still be listed: %v", body["items"])
	}
}

// ── a pass always moves on ───────────────────────────────────────────────

// BEHAVIOUR: a page of items whose results cannot be saved does not end the
// pass early, and the failure is returned so Run backs off.
func TestInboxClassifierPassMovesPastItemsThatCannotBeSaved(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{}
	withInboxMate(t, doer)
	total := inboxClassifyPage + 5
	for i := 0; i < total; i++ {
		uploadSortable(t, uint8(100+i))
	}
	if _, err := w.inbox.db.Exec(`CREATE TRIGGER inbox_read_only BEFORE UPDATE ON inbox_items BEGIN SELECT RAISE(ABORT, 'database is read-only'); END`); err != nil {
		t.Fatal(err)
	}
	err := classifierFor(w).RunPending(context.Background())
	if doer.calls() != total {
		t.Errorf("the pass sorted %d of %d items", doer.calls(), total)
	}
	if err == nil {
		t.Errorf("results that could not be saved must be returned so Run backs off")
	}
}

// ── a made title survives a restart ──────────────────────────────────────

func TestInboxMateTitleSurvivesARestart(t *testing.T) {
	w, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"Kept"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	id := uploadForFiling(t, 102, map[string]string{"capture_ms": ms(at)})
	if first, _ := titlesFor(t, id); first[id] != "Kept" {
		t.Fatalf("%v", first)
	}

	// A restart: the database is opened again and nothing is left in memory.
	var path string
	if err := w.inbox.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	reopened, err := newInboxStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	prev := globalInboxStore
	globalInboxStore = reopened
	t.Cleanup(func() { globalInboxStore = prev })

	second, errs := titlesFor(t, id)
	if second[id] != "Kept" || len(errs) != 0 {
		t.Fatalf("%v %v", second, errs)
	}
	if doer.calls() != 1 {
		t.Errorf("model calls: %d, want 1 (the title was stored)", doer.calls())
	}
}

// ── review pass 2 ────────────────────────────────────────────────────────

// BEHAVIOUR: the feature the camera pointed at is never cut from the five. The
// in-sector features (up to two nearest) go, plus the nearest others, five in all.
func TestInboxMateTitleKeepsASectorFeatureThatRanksSixth(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"x"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{
			featureAt("Other One", "island", 200, 1),
			featureAt("Other Two", "island", 210, 2),
			featureAt("Other Three", "island", 220, 3),
			featureAt("Other Four", "island", 230, 4),
			featureAt("Other Five", "island", 240, 5),
			featureAt("Target Island", "island", 80, 6),
			featureAt("Other Seven", "island", 250, 7),
		}, nil
	})
	id := uploadForFiling(t, 190, map[string]string{"capture_ms": ms(at), "bearing_deg": "80"})
	titlesFor(t, id)
	var targetLine string
	for _, line := range strings.Split(doer.body(0), `\n`) {
		if strings.Contains(line, "Target Island") && strings.HasPrefix(line, "- ") {
			targetLine = line
		}
	}
	if !strings.Contains(targetLine, "in the direction the camera pointed") {
		t.Errorf("the feature the camera faced was cut or not marked: %q", targetLine)
	}
	body := doer.body(0)
	for _, want := range []string{"Other One", "Other Two", "Other Three", "Other Four"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s (a nearest other) is missing", want)
		}
	}
	if strings.Contains(body, "Other Five") || strings.Contains(body, "Other Seven") {
		t.Errorf("more than five features were sent")
	}
}

// BEHAVIOUR: at most two in-sector features are kept; the rest of the five are
// the nearest others.
func TestInboxMateTitleKeepsAtMostTwoSectorFeatures(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"x"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{
			featureAt("Ahead One", "island", 80, 1),
			featureAt("Ahead Two", "island", 82, 2),
			featureAt("Ahead Three", "island", 84, 3),
			featureAt("Side One", "island", 200, 4),
			featureAt("Side Two", "island", 210, 5),
			featureAt("Side Three", "island", 220, 6),
			featureAt("Side Four", "island", 230, 7),
		}, nil
	})
	id := uploadForFiling(t, 191, map[string]string{"capture_ms": ms(at), "bearing_deg": "80"})
	titlesFor(t, id)
	body := doer.body(0)
	for _, want := range []string{"Ahead One", "Ahead Two", "Side One", "Side Two", "Side Three"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s is missing:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Ahead Three") || strings.Contains(body, "Side Four") {
		t.Errorf("wrong features cut:\n%s", body)
	}
}

// BEHAVIOUR: within one request a failed fetch is remembered for its cell, so
// five photos in one failing cell cause one fetch. Across requests it is not
// cached, so the next request asks again.
func TestInboxTitleRequestFetchesAFailingCellOnce(t *testing.T) {
	for _, mateOn := range []bool{true, false} {
		t.Run(fmt.Sprintf("mate=%v", mateOn), func(t *testing.T) {
			_, at := titleWorld(t, 5)
			if mateOn {
				withInboxMate(t, &fakeMateDoer{})
			} else {
				withMateOff(t)
			}
			var fetches atomic.Int32
			withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
				fetches.Add(1)
				time.Sleep(20 * time.Millisecond) // long enough for the other workers to ask too
				return nil, errors.New(rawFeatureError)
			})
			var ids []string
			for i := 0; i < 5; i++ {
				ids = append(ids, uploadForFiling(t, uint8(192+i), map[string]string{"capture_ms": ms(at.Add(time.Duration(i) * time.Second)), "bearing_deg": "90"}))
			}
			_, errs := titlesFor(t, ids...)
			if fetches.Load() != 1 {
				t.Errorf("%d fetches for five photos in one failing cell, want 1", fetches.Load())
			}
			for _, id := range ids {
				if errs[id] != "The place-name server didn't answer, so no title was suggested." {
					t.Errorf("%s: %v", id, errs[id])
				}
			}
			titlesFor(t, ids[0])
			if fetches.Load() != 2 {
				t.Errorf("a failure was kept across requests: %d fetches, want 2", fetches.Load())
			}
		})
	}
}

// BEHAVIOUR: Mate's settings that cannot be read do not fail the titles. Every
// row gets the title it would get without Mate, and the answer says so once.
func TestInboxTitlesSurviveMateSettingsThatCannotBeRead(t *testing.T) {
	_, at := titleWorld(t, 5)
	prev := globalInboxMate
	doer := &fakeMateDoer{}
	globalInboxMate = &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			return assistantReadiness{}, "", errors.New("read /data/settings.yaml: permission denied")
		},
		doer: doer,
	}
	t.Cleanup(func() { globalInboxMate = prev })
	withPlaceLookup(t, func(float64, float64) string { return "Hook Island" })
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{featureAt("Sector Island", "island", 100, 2)}, nil
	})
	place := uploadForFiling(t, 197, map[string]string{"capture_ms": ms(at)})
	sector := uploadForFiling(t, 198, map[string]string{"capture_ms": ms(at), "bearing_deg": "100"})

	titles, errs, body := titlesBody(t, place, sector)
	if titles[place] != "Hook Island" || titles[sector] != "Sector Island" || len(errs) != 0 {
		t.Errorf("titles %v errors %v", titles, errs)
	}
	if body["mate_error"] != "Mate's settings couldn't be read; these are place-name titles" {
		t.Errorf("mate_error: %v", body["mate_error"])
	}
	if doer.calls() != 0 {
		t.Errorf("Mate was called")
	}

	// With the settings readable there is no notice.
	withMateOff(t)
	if _, _, body := titlesBody(t, place); body["mate_error"] != nil {
		t.Errorf("mate_error with readable settings: %v", body["mate_error"])
	}
}

// BEHAVIOUR: reading Mate's settings does not hold the lock, so a settings save
// (forget) is not stuck behind a slow read, and a read that began before the save
// does not bring back what the save dropped.
func TestInboxMateSessionReadsSettingsOutsideTheLock(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	m := &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			if reads.Add(1) == 1 {
				close(entered)
				<-release
				return assistantReadiness{}, "", nil // the old settings: Mate off
			}
			return assistantReadiness{Enabled: true, Configured: true, DocumentModel: testDocumentModel}, "key", nil
		},
		doer: &fakeMateDoer{},
	}
	done := make(chan struct{})
	go func() { m.session(); close(done) }()
	<-entered

	forgot := make(chan struct{})
	go func() { m.forget(); close(forgot) }()
	select {
	case <-forgot:
	case <-time.After(500 * time.Millisecond):
		close(release)
		t.Fatal("forget() waited for a settings read in progress")
	}
	close(release)
	<-done

	if _, on, err := m.session(); err != nil || !on {
		t.Errorf("the read begun before the save was kept after it: on=%v err=%v", on, err)
	}
}

// ── degrees are 0 to 360 ─────────────────────────────────────────────────

func TestInboxBearingsAreNormalisedInThePromptAndInDescribe(t *testing.T) {
	course, camera := -10.0, -10.0
	msg := inboxTitleMessage(inboxTitleInput{CourseDeg: &course, CameraDeg: &camera,
		Features: []inboxFeature{{Name: "Cape", DistanceNm: 2, BearingDeg: 350}}})
	for _, want := range []string{"course over ground 350° true", "The phone faced 350° true"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q missing from:\n%s", want, msg)
		}
	}
	round := 359.6
	msg = inboxTitleMessage(inboxTitleInput{CourseDeg: &round})
	if !strings.Contains(msg, "course over ground 000° true") {
		t.Errorf("359.6 should round to 000:\n%s", msg)
	}
	f := inboxFeature{Name: "Cape", DistanceNm: 3.1, BearingDeg: -10}
	if got := f.describe(nil); got != "Cape, 3.1 nm, bearing 350° true" {
		t.Errorf("describe(nil): %q", got)
	}
	f.BearingDeg = 359.6
	if got := f.describe(nil); got != "Cape, 3.1 nm, bearing 000° true" {
		t.Errorf("describe(nil) at 359.6: %q", got)
	}
}

// ── no raw internals in a title's error ──────────────────────────────────

const rawFeatureError = `place-names provider "osm-overpass": plugin osm-overpass.wasm: fetch_poi call failed: dial tcp 10.1.2.3:443: i/o timeout`

func TestInboxTitleFeatureLookupFailureIsInOperatorWords(t *testing.T) {
	for _, mateOn := range []bool{true, false} {
		t.Run(fmt.Sprintf("mate=%v", mateOn), func(t *testing.T) {
			_, at := titleWorld(t, 5)
			if mateOn {
				withInboxMate(t, &fakeMateDoer{})
			} else {
				withMateOff(t)
			}
			withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, errors.New(rawFeatureError) })
			id := uploadForFiling(t, 103, map[string]string{"capture_ms": ms(at), "bearing_deg": "90"})
			_, errs := titlesFor(t, id)
			want := "The place-name server didn't answer, so no title was suggested."
			if errs[id] != want {
				t.Errorf("error %q, want %q", errs[id], want)
			}
		})
	}
}

func TestInboxMateTitleCallFailureIsInOperatorWords(t *testing.T) {
	_, at := titleWorld(t, 5)
	withInboxMate(t, &fakeMateDoer{reply: func(int, string) (string, error) {
		return "", errors.New("dial tcp 10.1.2.3:443: connect: connection refused")
	}})
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	id := uploadForFiling(t, 104, map[string]string{"capture_ms": ms(at)})
	_, errs := titlesFor(t, id)
	msg, _ := errs[id].(string)
	if msg == "" || strings.Contains(msg, "dial") || strings.Contains(msg, "openrouter") || strings.Contains(msg, "10.1.2.3") {
		t.Errorf("raw error text reached the operator: %q", msg)
	}
}
