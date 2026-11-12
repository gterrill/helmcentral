package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Mate on the Inbox (ADR 0282): sorting on arrival and titles from the vision
// model. Nothing here reaches the network: the OpenRouter client and the
// place-name provider are fakes.

// ── fakes ────────────────────────────────────────────────────────────────

type fakeMateDoer struct {
	mu          sync.Mutex
	bodies      []string
	delay       time.Duration
	inflight    int
	maxInflight int
	// reply answers one request; nil replies {"kind":"scenery"}.
	reply func(call int, body string) (string, error)
}

func (d *fakeMateDoer) Do(req *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(req.Body)
	d.mu.Lock()
	d.bodies = append(d.bodies, string(raw))
	call := len(d.bodies)
	d.inflight++
	if d.inflight > d.maxInflight {
		d.maxInflight = d.inflight
	}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.inflight--
		d.mu.Unlock()
	}()
	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	content := `{"kind":"scenery"}`
	if d.reply != nil {
		var err error
		if content, err = d.reply(call, string(raw)); err != nil {
			return nil, err
		}
	}
	body, _ := json.Marshal(map[string]any{
		"id": "gen-1", "model": "fake",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": content}}},
	})
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}

func (d *fakeMateDoer) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.bodies)
}

func (d *fakeMateDoer) body(i int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bodies[i]
}

const testDocumentModel = "google/gemini-2.5-flash"

// withInboxMate switches Mate on for the test, answering through doer.
func withInboxMate(t *testing.T, doer openRouterDoer) {
	t.Helper()
	prev := globalInboxMate
	globalInboxMate = &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			return assistantReadiness{Enabled: true, Configured: true, Model: "chat/model", DocumentModel: testDocumentModel}, "test-key", nil
		},
		doer: doer,
	}
	t.Cleanup(func() { globalInboxMate = prev })
}

// withMateOff leaves Mate unconfigured, a normal state, with a doer that fails
// the test if anything calls it.
func withMateOff(t *testing.T) {
	t.Helper()
	prev := globalInboxMate
	globalInboxMate = &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			return assistantReadiness{Problem: "The assistant is switched off. Enable it in Settings → Assistant."}, "", nil
		},
		doer: doerFunc(func(*http.Request) (*http.Response, error) {
			t.Error("Mate was called while it is not set up")
			return nil, errors.New("unexpected call")
		}),
	}
	t.Cleanup(func() { globalInboxMate = prev })
}

func withFeatures(t *testing.T, fn func(lat, lon float64) ([]poiFeatureRaw, error)) {
	t.Helper()
	prev := inboxFeaturesNear
	inboxFeaturesNear = fn
	t.Cleanup(func() { inboxFeaturesNear = prev })
}

func withPlaceLookup(t *testing.T, fn func(lat, lon float64) string) {
	t.Helper()
	prev := inboxPlaceLookup
	inboxPlaceLookup = fn
	t.Cleanup(func() { inboxPlaceLookup = prev })
}

func classifierFor(w inboxWorld) *inboxClassifier {
	c := newInboxClassifier(w.inbox)
	c.retryDelay = 0
	return c
}

func uploadWithThumb(t *testing.T, shade uint8, thumb []byte, fields map[string]string) string {
	t.Helper()
	parts := []documentUploadField{
		{name: "file", filename: "shot.jpg", content: inboxTestJPEG(t, shade)},
		{name: "thumb", filename: "thumb.jpg", content: thumb},
	}
	for k, v := range fields {
		parts = append(parts, documentUploadField{name: k, content: []byte(v)})
	}
	c, rec := newDocumentUploadContext(t, parts)
	if err := uploadInboxHandler(c); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, out)
	}
	return out["item"].(map[string]any)["id"].(string)
}

func itemJSON(t *testing.T, id string) map[string]any {
	t.Helper()
	for _, it := range inboxList(t) {
		if it["id"] == id {
			return it
		}
	}
	t.Fatalf("item %s not listed", id)
	return nil
}

// ── sorting on arrival ───────────────────────────────────────────────────

func TestInboxClassifierSortsAPhotoFromItsThumbnailAndSendsAPDFToDocumentsWithoutACall(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"kind":"equipment"}`, nil }}
	withInboxMate(t, doer)

	thumb := inboxTestJPEG(t, 33)
	photo := uploadWithThumb(t, 130, thumb, nil)
	pdfBytes, _ := os.ReadFile(testdataPath("two_page.pdf"))
	_, pb := inboxUpload(t, pdfBytes, "receipt.pdf", nil)
	pdf := pb["item"].(map[string]any)["id"].(string)

	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 1 {
		t.Fatalf("model calls: %d, want 1 (the PDF needs none)", doer.calls())
	}
	body := doer.body(0)
	if !strings.Contains(body, testDocumentModel) {
		t.Errorf("not the document model: %s", body)
	}
	if !strings.Contains(body, "image_url") || !strings.Contains(body, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(thumb)) {
		t.Errorf("the thumbnail was not what was sent")
	}
	if !strings.Contains(body, inboxClassifyPrompt[:40]) {
		t.Errorf("the classify prompt was not sent")
	}
	if got := itemJSON(t, photo); got["suggested_kind"] != "equipment" || got["classify_error"] != nil && got["classify_error"] != "" {
		t.Errorf("photo: %v", got)
	}
	if got := itemJSON(t, pdf); got["suggested_kind"] != "document" {
		t.Errorf("pdf: %v", got)
	}
	// A second pass has nothing left to do.
	if err := classifierFor(w).RunPending(context.Background()); err != nil || doer.calls() != 1 {
		t.Errorf("sorted items were sorted again: %d calls, %v", doer.calls(), err)
	}
}

func TestInboxClassifierMakesNoCallsWhenMateIsNotSetUp(t *testing.T) {
	w := newInboxWorld(t)
	withMateOff(t)
	photo := uploadForFiling(t, 131, nil)
	pdfBytes, _ := os.ReadFile(testdataPath("two_page.pdf"))
	_, pb := inboxUpload(t, pdfBytes, "r.pdf", nil)
	pdf := pb["item"].(map[string]any)["id"].(string)

	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatalf("an unconfigured Mate is not an error: %v", err)
	}
	for _, id := range []string{photo, pdf} {
		got := itemJSON(t, id)
		if got["suggested_kind"] != nil || (got["classify_error"] != nil && got["classify_error"] != "") {
			t.Errorf("%s: %v", id, got)
		}
	}
}

func TestInboxClassifierRetriesOnceThenSucceeds(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{reply: func(call int, _ string) (string, error) {
		if call == 1 {
			return "", errors.New("upstream hiccup")
		}
		return "```json\n{\"kind\":\"scenery\"}\n```", nil
	}}
	withInboxMate(t, doer)
	id := uploadForFiling(t, 132, nil)
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 2 {
		t.Errorf("calls: %d", doer.calls())
	}
	if got := itemJSON(t, id); got["suggested_kind"] != "scenery" || (got["classify_error"] != nil && got["classify_error"] != "") {
		t.Errorf("%v", got)
	}
}

func TestInboxClassifierRecordsTheErrorAfterTheRetryAndShowsIt(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return "", errors.New("openrouter request failed: 402 out of credit") }}
	withInboxMate(t, doer)
	id := uploadForFiling(t, 133, nil)

	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 2 {
		t.Errorf("one try and one retry, got %d calls", doer.calls())
	}
	got := itemJSON(t, id)
	msg, _ := got["classify_error"].(string)
	if got["suggested_kind"] != nil || !strings.Contains(msg, "out of credit") {
		t.Errorf("the failure must be visible, not a silent unsorted: %v", got)
	}
	// A recorded failure is not tried again by the next pass.
	if err := classifierFor(w).RunPending(context.Background()); err != nil || doer.calls() != 2 {
		t.Errorf("a failed item was retried: %d calls, %v", doer.calls(), err)
	}
}

func TestInboxClassifierRefusesAnAnswerThatIsNotAKind(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"kind":"receipt"}`, nil }}
	withInboxMate(t, doer)
	id := uploadForFiling(t, 134, nil)
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := itemJSON(t, id)
	if got["suggested_kind"] != nil || got["classify_error"] == nil || got["classify_error"] == "" {
		t.Errorf("%v", got)
	}
}

func TestInboxClassifierRunsAtMostTwoCallsAtOnce(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{delay: 40 * time.Millisecond}
	withInboxMate(t, doer)
	for i := 0; i < 6; i++ {
		uploadForFiling(t, uint8(140+i), nil)
	}
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 6 || doer.maxInflight != 2 {
		t.Errorf("calls %d, most at once %d (want 6 and 2)", doer.calls(), doer.maxInflight)
	}
}

func TestInboxUploadWakesTheClassifierOnlyForANewItem(t *testing.T) {
	newInboxWorld(t)
	woken := 0
	prev := inboxClassifierWake
	inboxClassifierWake = func() { woken++ }
	t.Cleanup(func() { inboxClassifierWake = prev })

	uploadForFiling(t, 150, nil)
	if woken != 1 {
		t.Fatalf("woken %d", woken)
	}
	if code, body := inboxUpload(t, inboxTestJPEG(t, 150), "again.jpg", nil); code != 200 || body["status"] != "already_uploaded" {
		t.Fatal(code, body)
	}
	if woken != 1 {
		t.Errorf("a duplicate woke the classifier: %d", woken)
	}
}

func TestInboxListSaysWhetherMateIsSorting(t *testing.T) {
	newInboxWorld(t)
	withMateOff(t)
	_, body := getJSON(t, listInboxHandler, "/api/inbox", "")
	if body["mate_sorting"] != false {
		t.Errorf("off: %v", body["mate_sorting"])
	}
	withInboxMate(t, &fakeMateDoer{})
	_, body = getJSON(t, listInboxHandler, "/api/inbox", "")
	if body["mate_sorting"] != true {
		t.Errorf("on: %v", body["mate_sorting"])
	}
}

// ── the bearing reference is kept ────────────────────────────────────────

func TestInboxUploadKeepsTheBearingReference(t *testing.T) {
	w := newInboxWorld(t)
	id := uploadForFiling(t, 151, map[string]string{"bearing_deg": "80", "bearing_ref": "M"})
	it, _ := w.inbox.Get(id)
	if it.BearingRef != "M" {
		t.Errorf("ref %q", it.BearingRef)
	}
	if code, _ := inboxUpload(t, inboxTestJPEG(t, 152), "x.jpg", map[string]string{"bearing_deg": "80", "bearing_ref": "X"}); code != 400 {
		t.Errorf("a reference that is neither T nor M: %d", code)
	}
}

// ── titles ───────────────────────────────────────────────────────────────

// titleWorld puts a boat under way heading 090 at -20.25, 149.5 and returns the
// capture time to use.
func titleWorld(t *testing.T, sog float64) (inboxWorld, time.Time) {
	t.Helper()
	w := newInboxWorld(t)
	at := passageEpoch.Add(time.Hour)
	if err := w.passages.AppendTrackPoint(recordedTrackPoint{At: at.Add(-time.Minute), Lat: -20.25, Lon: 149.5, SOGKts: pfloat(sog), COGDeg: pfloat(90)}); err != nil {
		t.Fatal(err)
	}
	return w, at
}

func titlesFor(t *testing.T, ids ...string) (titles, errs map[string]any) {
	t.Helper()
	code, body := getJSON(t, inboxSuggestedTitlesHandler, "/api/inbox/suggested-titles?ids="+strings.Join(ids, ","), "")
	if code != 200 {
		t.Fatal(code, body)
	}
	titles, _ = body["titles"].(map[string]any)
	errs, _ = body["errors"].(map[string]any)
	return titles, errs
}

// featuresNorthOf places named features around -20.25, 149.5 by bearing and
// distance, so the test reads in the terms the prompt does.
func featureAt(name, category string, bearing, nm float64) poiFeatureRaw {
	lat, lon := destinationPoint(-20.25, 149.5, bearing*math.Pi/180, nm*1852)
	return poiFeatureRaw{Name: name, Category: category, Lat: lat, Lon: lon}
}

func TestInboxMateTitleGetsThePhotoHeadingAndFiveFeaturesWithSides(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"Dolphins off the bow"}`, nil }}
	withInboxMate(t, doer)
	asked := 0
	withFeatures(t, func(lat, lon float64) ([]poiFeatureRaw, error) {
		asked++
		return []poiFeatureRaw{
			featureAt("Cape Capricorn", "island", 50, 3.1),
			featureAt("Hummocky Island", "island", 140, 4),
			featureAt("Aft Bay", "bay", 270, 5),
			featureAt("Fourth Rock", "island", 100, 6),
			featureAt("Fifth Point", "bay", 120, 7),
			featureAt("Sixth Island", "island", 130, 8),
			featureAt("Seventh Island", "island", 135, 9),
		}, nil
	})
	withPlaceLookup(t, func(float64, float64) string { t.Error("the nearest-place lookup ran beside Mate"); return "" })

	// Camera pointing 80 true: Fourth Rock (100) and Cape Capricorn (50 is 30 off) - Rock is in the sector.
	id := uploadWithThumb(t, 160, inboxTestJPEG(t, 5), map[string]string{"capture_ms": ms(at), "bearing_deg": "80", "bearing_ref": "T"})
	titles, errs := titlesFor(t, id)
	if titles[id] != "Dolphins off the bow" || len(errs) != 0 {
		t.Fatalf("%v %v", titles, errs)
	}
	if asked != 1 || doer.calls() != 1 {
		t.Fatalf("feature fetches %d, model calls %d", asked, doer.calls())
	}
	body := doer.body(0)
	for _, want := range []string{
		testDocumentModel,
		"heading 090",
		"Cape Capricorn, 3.1 nm, 40° to port",
		"Hummocky Island, 4.0 nm, 50° to starboard",
		"Aft Bay, 5.0 nm, dead astern",
		"Fourth Rock",
		"Fifth Point",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the request lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Sixth Island") || strings.Contains(body, "Seventh Island") {
		t.Errorf("only the five nearest are sent")
	}
	if !strings.Contains(body, "in the direction the camera pointed") {
		t.Errorf("the sector mark is missing")
	}
	// The full-size copy goes, not the thumbnail.
	full := inboxTestJPEG(t, 160)
	if !strings.Contains(body, base64.StdEncoding.EncodeToString(full)) {
		t.Errorf("the stored photo was not what was sent")
	}
}

func TestInboxMateTitleMarksOnlyTheFeaturesInTheCameraSector(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"x"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{featureAt("In View Island", "island", 100, 2), featureAt("Out Of View Bay", "bay", 200, 3)}, nil
	})
	id := uploadForFiling(t, 161, map[string]string{"capture_ms": ms(at), "bearing_deg": "95"})
	titlesFor(t, id)
	// The request body is JSON, so a line break inside the prompt is the two characters \n.
	var inLine, outLine string
	for _, line := range strings.Split(doer.body(0), `\n`) {
		switch {
		case strings.Contains(line, "In View Island"):
			inLine = line
		case strings.Contains(line, "Out Of View Bay"):
			outLine = line
		}
	}
	if !strings.Contains(inLine, "in the direction the camera pointed") {
		t.Errorf("In View Island is not marked: %q", inLine)
	}
	if outLine == "" || strings.Contains(outLine, "in the direction the camera pointed") {
		t.Errorf("Out Of View Bay should be listed and not marked: %q", outLine)
	}
}

func TestInboxMateTitleWithoutABearingMentionsNoCameraDirection(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"A fish"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{featureAt("Some Island", "island", 100, 2)}, nil
	})
	id := uploadForFiling(t, 162, map[string]string{"capture_ms": ms(at)})
	titlesFor(t, id)
	if strings.Contains(doer.body(0), "in the direction the camera pointed") {
		t.Error("no bearing, so no sector mark")
	}
}

func TestInboxMateTitleSaysWhenTheHeadingIsNotKnown(t *testing.T) {
	_, at := titleWorld(t, 0.1) // at rest: a course over ground means nothing
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"Anchorage"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{featureAt("Some Island", "island", 100, 2)}, nil
	})
	id := uploadForFiling(t, 163, map[string]string{"capture_ms": ms(at)})
	titlesFor(t, id)
	body := doer.body(0)
	if !strings.Contains(body, "heading is not known") || !strings.Contains(body, "bearing 100° true") {
		t.Errorf("%s", body)
	}
}

func TestInboxMateTitleIsCutToEightyCharacters(t *testing.T) {
	_, at := titleWorld(t, 5)
	withInboxMate(t, &fakeMateDoer{reply: func(int, string) (string, error) {
		return `{"title":"` + strings.Repeat("é", 200) + `"}`, nil
	}})
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	id := uploadForFiling(t, 164, map[string]string{"capture_ms": ms(at)})
	titles, _ := titlesFor(t, id)
	if got := titles[id].(string); len([]rune(got)) != signTitleMax {
		t.Errorf("%d characters", len([]rune(got)))
	}
}

func TestInboxMateTitleFailureIsShownNotReplacedByAPlaceName(t *testing.T) {
	_, at := titleWorld(t, 5)
	withInboxMate(t, &fakeMateDoer{reply: func(int, string) (string, error) { return "", errors.New("openrouter request failed: 429") }})
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	withPlaceLookup(t, func(float64, float64) string { t.Error("a failed Mate call was replaced by the nearest place"); return "Hook Island" })
	id := uploadForFiling(t, 165, map[string]string{"capture_ms": ms(at)})
	titles, errs := titlesFor(t, id)
	if titles[id] != "" {
		t.Errorf("title %q", titles[id])
	}
	if msg, _ := errs[id].(string); !strings.Contains(msg, "429") {
		t.Errorf("errors %v", errs)
	}
}

func TestInboxMateTitleStopsWhenTheFeaturesCannotBeFetched(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, errors.New("overpass is down") })
	id := uploadForFiling(t, 166, map[string]string{"capture_ms": ms(at)})
	titles, errs := titlesFor(t, id)
	if msg, _ := errs[id].(string); !strings.Contains(msg, "overpass is down") || titles[id] != "" {
		t.Errorf("%v %v", titles, errs)
	}
	if doer.calls() != 0 {
		t.Errorf("the model was asked without the features")
	}
}

func TestInboxMateTitlesFetchFeaturesOncePerAreaAndAskPerPhoto(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"A"}`, nil }}
	withInboxMate(t, doer)
	fetches := 0
	var mu sync.Mutex
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { mu.Lock(); fetches++; mu.Unlock(); return nil, nil })
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, uploadForFiling(t, uint8(170+i), map[string]string{"capture_ms": ms(at.Add(time.Duration(i) * time.Second))}))
	}
	titlesFor(t, ids...)
	if fetches != 1 || doer.calls() != 3 {
		t.Errorf("feature fetches %d (want 1), model calls %d (want 3)", fetches, doer.calls())
	}
}

func TestInboxMateTitlesSkipPDFsAndPhotosWithNoPosition(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	pdfBytes, _ := os.ReadFile(testdataPath("two_page.pdf"))
	_, pb := inboxUpload(t, pdfBytes, "r.pdf", map[string]string{"capture_ms": ms(at)})
	pdf := pb["item"].(map[string]any)["id"].(string)
	noTrack := uploadForFiling(t, 175, map[string]string{"capture_ms": ms(passageEpoch.Add(-72 * time.Hour))})
	noTime := uploadForFiling(t, 176, nil)
	titles, errs := titlesFor(t, pdf, noTrack, noTime)
	if doer.calls() != 0 || len(errs) != 0 || titles[pdf] != "" || titles[noTrack] != "" || titles[noTime] != "" {
		t.Errorf("calls %d titles %v errs %v", doer.calls(), titles, errs)
	}
}

func TestInboxMateTitlesGiveUpAtTheDeadlineAndLeaveBlanks(t *testing.T) {
	_, at := titleWorld(t, 5)
	withInboxMate(t, &fakeMateDoer{delay: 2 * time.Second})
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	prev := inboxMateTitleDeadline
	inboxMateTitleDeadline = 100 * time.Millisecond
	t.Cleanup(func() { inboxMateTitleDeadline = prev })
	id := uploadForFiling(t, 177, map[string]string{"capture_ms": ms(at)})
	start := time.Now()
	titles, errs := titlesFor(t, id)
	if time.Since(start) > time.Second || titles[id] != "" || len(errs) != 0 {
		t.Errorf("took %v, titles %v errs %v", time.Since(start), titles, errs)
	}
}

// ── titles without Mate ──────────────────────────────────────────────────

func TestInboxTitleWithABearingNamesTheNearestFeatureInTheSector(t *testing.T) {
	_, at := titleWorld(t, 5)
	withMateOff(t)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{
			featureAt("Behind Island", "island", 270, 1),
			featureAt("Off To The Side Bay", "bay", 140, 2),
			featureAt("Ahead Island", "island", 95, 4),
			featureAt("Further Ahead Rock", "island", 92, 8),
		}, nil
	})
	withPlaceLookup(t, func(float64, float64) string { t.Error("nearest-place used although a feature is in the sector"); return "Wrong" })
	id := uploadForFiling(t, 180, map[string]string{"capture_ms": ms(at), "bearing_deg": "90", "bearing_ref": "T"})
	titles, errs := titlesFor(t, id)
	if titles[id] != "Ahead Island" || len(errs) != 0 {
		t.Fatalf("%v %v", titles, errs)
	}
}

func TestInboxTitleWithABearingFallsBackToTheNearestPlaceWhenNothingIsInTheSector(t *testing.T) {
	_, at := titleWorld(t, 5)
	withMateOff(t)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{featureAt("Behind Island", "island", 270, 1), featureAt("Beyond Range Island", "island", 90, 11)}, nil
	})
	withPlaceLookup(t, func(float64, float64) string { return "Hook Island" })
	id := uploadForFiling(t, 181, map[string]string{"capture_ms": ms(at), "bearing_deg": "90"})
	titles, _ := titlesFor(t, id)
	if titles[id] != "Hook Island" {
		t.Errorf("%v", titles)
	}
}

func TestInboxTitleWithoutABearingNeverFetchesFeatures(t *testing.T) {
	_, at := titleWorld(t, 5)
	withMateOff(t)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { t.Error("features fetched for a photo with no bearing"); return nil, nil })
	withPlaceLookup(t, func(float64, float64) string { return "Hook Island" })
	id := uploadForFiling(t, 182, map[string]string{"capture_ms": ms(at)})
	if titles, _ := titlesFor(t, id); titles[id] != "Hook Island" {
		t.Errorf("%v", titles)
	}
}

func TestInboxTitleWithABearingShowsAFeatureFetchFailure(t *testing.T) {
	_, at := titleWorld(t, 5)
	withMateOff(t)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, errors.New("overpass is down") })
	withPlaceLookup(t, func(float64, float64) string { t.Error("masked the failure with the nearest place"); return "Hook Island" })
	id := uploadForFiling(t, 183, map[string]string{"capture_ms": ms(at), "bearing_deg": "90"})
	titles, errs := titlesFor(t, id)
	if msg, _ := errs[id].(string); !strings.Contains(msg, "overpass is down") || titles[id] != "" {
		t.Errorf("%v %v", titles, errs)
	}
}

// ── the prompts ──────────────────────────────────────────────────────────

func TestInboxTitlePromptPinsItsKeyInstructions(t *testing.T) {
	for _, want := range []string{
		"at most 80 characters",
		"only if the photo shows",
		"Dolphins off the bow",
		"one title",
		"No note",
		`{"title"`,
	} {
		if !strings.Contains(inboxTitlePrompt, want) {
			t.Errorf("the title prompt no longer says %q", want)
		}
	}
	// The sector note is added only for a photo that carries a bearing.
	if !strings.Contains(inboxTitleSectorNote, "in the direction the camera pointed") || strings.Contains(inboxTitlePrompt, "in the direction the camera pointed") {
		t.Errorf("the sector wording belongs in the sector note alone")
	}
}

func TestInboxClassifyPromptNamesTheThreeKinds(t *testing.T) {
	for _, want := range []string{"scenery", "document", "equipment", `{"kind"`, "receipt"} {
		if !strings.Contains(inboxClassifyPrompt, want) {
			t.Errorf("the classify prompt no longer says %q", want)
		}
	}
}
