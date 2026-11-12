package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Mate on the Inbox (ADR 0283): sorting on arrival and titles from the vision
// model. Nothing here reaches the network: the OpenRouter client and the
// place-name provider are fakes.

// ── fakes ────────────────────────────────────────────────────────────────

type fakeMateDoer struct {
	mu     sync.Mutex
	bodies []string
	delay  time.Duration
	// delayFor, when set, is the delay for one request, chosen from its body.
	delayFor    func(body string) time.Duration
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
	delay := d.delay
	if d.delayFor != nil {
		delay = d.delayFor(string(raw))
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
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
	withFetched(t, func(lat, lon float64) (inboxFetched, error) {
		features, err := fn(lat, lon)
		return inboxFetched{Features: features}, err
	})
}

// withFetched stands in for the provider including its truncation flag.
func withFetched(t *testing.T, fn func(lat, lon float64) (inboxFetched, error)) {
	t.Helper()
	prev, prevCache := inboxFeaturesNear, inboxFeatures
	inboxFeaturesNear = fn
	inboxFeatures = newInboxFeatureCache(inboxFeatureTTL) // nothing left over from another test
	t.Cleanup(func() { inboxFeaturesNear, inboxFeatures = prev, prevCache })
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

// uploadSortable stores a photo with a thumbnail, which is what Mate sorts from.
func uploadSortable(t *testing.T, shade uint8) string {
	t.Helper()
	return uploadWithThumb(t, shade, inboxTestJPEG(t, 3), nil)
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
	id := uploadSortable(t, 132)
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
	doer := &fakeMateDoer{reply: func(int, string) (string, error) {
		return "", errors.New("openrouter request failed: 402 out of credit")
	}}
	withInboxMate(t, doer)
	id := uploadSortable(t, 133)

	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 2 {
		t.Errorf("one try and one retry, got %d calls", doer.calls())
	}
	got := itemJSON(t, id)
	msg, _ := got["classify_error"].(string)
	if got["suggested_kind"] != nil || !strings.HasPrefix(msg, "Mate couldn't sort this yet. Trying again at ") {
		t.Errorf("the failure must be visible, in plain words, not a silent unsorted: %v", got)
	}
	// It waits for its back-off: the next pass does not try it again.
	if err := classifierFor(w).RunPending(context.Background()); err != nil || doer.calls() != 2 {
		t.Errorf("a failed item was retried before its back-off: %d calls, %v", doer.calls(), err)
	}
}

func TestInboxClassifierRefusesAnAnswerThatIsNotAKind(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"kind":"receipt"}`, nil }}
	withInboxMate(t, doer)
	id := uploadSortable(t, 134)
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
		uploadSortable(t, uint8(140+i))
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
	titles, errs, _ = titlesBody(t, ids...)
	return titles, errs
}

// titlesBody is POST /api/inbox/suggested-titles; it also returns the whole
// answer for the fields beside titles and errors.
func titlesBody(t *testing.T, ids ...string) (titles, errs, body map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string][]string{"ids": ids})
	code, body := send(t, inboxSuggestedTitlesHandler, http.MethodPost, string(raw))
	if code != 200 {
		t.Fatal(code, body)
	}
	titles, _ = body["titles"].(map[string]any)
	errs, _ = body["errors"].(map[string]any)
	return titles, errs, body
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
	id := uploadWithThumb(t, 160, inboxTestJPEG(t, 5), map[string]string{"capture_ms": ms(at), "bearing_deg": "80"})
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
		"course over ground 090° true",
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

func TestInboxMateTitleSaysWhenTheCourseIsNotKnown(t *testing.T) {
	_, at := titleWorld(t, 0.1) // at rest: a course over ground means nothing
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"Anchorage"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{featureAt("Some Island", "island", 100, 2)}, nil
	})
	id := uploadForFiling(t, 163, map[string]string{"capture_ms": ms(at)})
	titlesFor(t, id)
	body := doer.body(0)
	if !strings.Contains(body, "course over ground is not known") || !strings.Contains(body, "bearing 100° true") {
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
	withPlaceLookup(t, func(float64, float64) string {
		t.Error("a failed Mate call was replaced by the nearest place")
		return "Hook Island"
	})
	id := uploadForFiling(t, 165, map[string]string{"capture_ms": ms(at)})
	titles, errs := titlesFor(t, id)
	if titles[id] != "" {
		t.Errorf("title %q", titles[id])
	}
	if msg, _ := errs[id].(string); msg != "Mate couldn't reach the model, so no title was suggested." {
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
	if msg, _ := errs[id].(string); msg != "The place-name server didn't answer, so no title was suggested." || titles[id] != "" {
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

func withMateTitleTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := inboxMateTitleTimeout
	inboxMateTitleTimeout = d
	t.Cleanup(func() { inboxMateTitleTimeout = prev })
}

// BEHAVIOUR: each photo has its own time to be titled, and one that runs out is
// reported against that photo; the others are not held to its clock.
func TestInboxMateTitleTimesOutPerPhotoWithAnErrorWhileTheOthersSucceed(t *testing.T) {
	_, at := titleWorld(t, 5)
	slowPhoto := inboxTestJPEG(t, 177)
	doer := &fakeMateDoer{
		reply: func(int, string) (string, error) { return `{"title":"Quick one"}`, nil },
		delayFor: func(body string) time.Duration {
			if strings.Contains(body, base64.StdEncoding.EncodeToString(slowPhoto)) {
				return 2 * time.Second
			}
			return 0
		},
	}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	withMateTitleTimeout(t, 150*time.Millisecond)
	slow := uploadForFiling(t, 177, map[string]string{"capture_ms": ms(at)})
	fastA := uploadForFiling(t, 178, map[string]string{"capture_ms": ms(at.Add(time.Second))})
	fastB := uploadForFiling(t, 179, map[string]string{"capture_ms": ms(at.Add(2 * time.Second))})

	start := time.Now()
	titles, errs := titlesFor(t, slow, fastA, fastB)
	if time.Since(start) > time.Second {
		t.Errorf("waited %v for the slow photo", time.Since(start))
	}
	if msg, _ := errs[slow].(string); !strings.Contains(msg, "Mate didn't answer in time") || titles[slow] != "" {
		t.Errorf("the slow photo needs an error and no title: %v %v", titles, errs)
	}
	if titles[fastA] != "Quick one" || titles[fastB] != "Quick one" || errs[fastA] != nil || errs[fastB] != nil {
		t.Errorf("the quick photos should be titled: %v %v", titles, errs)
	}
}

// BEHAVIOUR: a title already made is not paid for again when the dialog reopens.
func TestInboxMateTitleIsRememberedSoAReopenedDialogMakesNoCall(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"Remembered"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	id := uploadForFiling(t, 184, map[string]string{"capture_ms": ms(at)})
	first, _ := titlesFor(t, id)
	second, errs := titlesFor(t, id)
	if first[id] != "Remembered" || second[id] != "Remembered" || len(errs) != 0 {
		t.Fatalf("%v %v %v", first, second, errs)
	}
	if doer.calls() != 1 {
		t.Errorf("model calls: %d, want 1 (the second request is answered from memory)", doer.calls())
	}
}

// BEHAVIOUR: a failed title is not remembered, so the next try asks again.
func TestInboxMateTitleFailureIsNotRemembered(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(call int, _ string) (string, error) {
		if call == 1 {
			return "", errors.New("openrouter request failed: 429")
		}
		return `{"title":"Second time lucky"}`, nil
	}}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	id := uploadForFiling(t, 185, map[string]string{"capture_ms": ms(at)})
	titlesFor(t, id)
	if titles, errs := titlesFor(t, id); titles[id] != "Second time lucky" || len(errs) != 0 {
		t.Errorf("%v %v", titles, errs)
	}
}

// BEHAVIOUR: Mate is told when the nearby list was cut short.
func TestInboxMateTitleSaysWhenTheFeatureListMayBeIncomplete(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"x"}`, nil }}
	withInboxMate(t, doer)
	withFetched(t, func(float64, float64) (inboxFetched, error) {
		return inboxFetched{Features: []poiFeatureRaw{featureAt("Some Island", "island", 100, 2)}, Truncated: true}, nil
	})
	id := uploadForFiling(t, 186, map[string]string{"capture_ms": ms(at)})
	titlesFor(t, id)
	if !strings.Contains(doer.body(0), "may be incomplete") {
		t.Errorf("the list was cut short and the prompt does not say so:\n%s", doer.body(0))
	}
}

func TestInboxMateTitleDoesNotCallACompleteListIncomplete(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"x"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		return []poiFeatureRaw{featureAt("Some Island", "island", 100, 2)}, nil
	})
	id := uploadForFiling(t, 187, map[string]string{"capture_ms": ms(at)})
	titlesFor(t, id)
	if strings.Contains(doer.body(0), "may be incomplete") {
		t.Errorf("a complete list should not be called incomplete:\n%s", doer.body(0))
	}
}

// BEHAVIOUR: without Mate a cut-short list cannot name the nearest feature in
// the sector reliably, so the photo gets an error and no title.
func TestInboxSectorTitleFailsVisiblyWhenTheFeatureListWasCutShort(t *testing.T) {
	_, at := titleWorld(t, 5)
	withMateOff(t)
	withFetched(t, func(float64, float64) (inboxFetched, error) {
		return inboxFetched{Features: []poiFeatureRaw{featureAt("Ahead Island", "island", 92, 4)}, Truncated: true}, nil
	})
	withPlaceLookup(t, func(float64, float64) string {
		t.Error("masked the cut-short list with the nearest place")
		return "Hook Island"
	})
	id := uploadForFiling(t, 188, map[string]string{"capture_ms": ms(at), "bearing_deg": "90"})
	titles, errs := titlesFor(t, id)
	if msg, _ := errs[id].(string); !strings.Contains(msg, "too many named places nearby to pick one reliably") || titles[id] != "" {
		t.Errorf("%v %v", titles, errs)
	}
}

// With no named feature in range the thumbnail is enough to caption; with one
// to check against, the 2560 px copy is sent.
func TestInboxMateTitleSendsTheThumbnailWhenNoNamedFeatureIsInRange(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"Open water"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	thumb := inboxTestJPEG(t, 9)
	id := uploadWithThumb(t, 189, thumb, map[string]string{"capture_ms": ms(at)})
	titlesFor(t, id)
	body := doer.body(0)
	if !strings.Contains(body, base64.StdEncoding.EncodeToString(thumb)) || strings.Contains(body, base64.StdEncoding.EncodeToString(inboxTestJPEG(t, 189))) {
		t.Errorf("the thumbnail, and not the full copy, should be what was sent")
	}
}

// BEHAVIOUR: one limit of three on Mate calls, shared by sorting and titles.
func TestInboxSortingAndTitlesTogetherNeverExceedThreeMateCalls(t *testing.T) {
	w := newInboxWorld(t)
	at := passageEpoch.Add(time.Hour)
	if err := w.passages.AppendTrackPoint(recordedTrackPoint{At: at.Add(-time.Minute), Lat: -20.25, Lon: 149.5, SOGKts: pfloat(5), COGDeg: pfloat(90)}); err != nil {
		t.Fatal(err)
	}
	doer := &fakeMateDoer{delay: 80 * time.Millisecond, reply: func(_ int, body string) (string, error) {
		if strings.Contains(body, "passage replay") {
			return `{"title":"T"}`, nil
		}
		return `{"kind":"scenery"}`, nil
	}}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	var ids []string
	for i := 0; i < 6; i++ {
		ids = append(ids, uploadWithThumb(t, uint8(190+i), inboxTestJPEG(t, 3), map[string]string{"capture_ms": ms(at.Add(time.Duration(i) * time.Second))}))
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := classifierFor(w).RunPending(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	titlesFor(t, ids[:4]...)
	wg.Wait()
	if doer.calls() < 10 {
		t.Fatalf("expected 6 sorts and 4 titles, got %d calls", doer.calls())
	}
	if doer.maxInflight != 3 {
		t.Errorf("most Mate calls at once: %d, want exactly 3", doer.maxInflight)
	}
}

// BEHAVIOUR: an image with no thumbnail is not sent full size to be sorted.
func TestInboxClassifierDoesNotSendAFullSizePhotoWithNoThumbnail(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{}
	withInboxMate(t, doer)
	id := uploadForFiling(t, 196, nil) // no thumb
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 0 {
		t.Errorf("the full photo was sent: %d calls", doer.calls())
	}
	got := itemJSON(t, id)
	if msg, _ := got["classify_error"].(string); got["suggested_kind"] != nil || !strings.HasPrefix(msg, "Mate couldn't sort this yet. Trying again at ") || strings.Contains(msg, "preview") {
		t.Errorf("%v", got)
	}
}

// BEHAVIOUR: when Mate's settings cannot be read, Run backs off and tries
// again by itself instead of waiting for the next upload.
func TestInboxClassifierRunBacksOffAfterAnErrorAndRetriesWithoutAWake(t *testing.T) {
	w := newInboxWorld(t)
	id := uploadSortable(t, 197)
	var reads atomic.Int32
	prev := globalInboxMate
	globalInboxMate = &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			if reads.Add(1) <= 2 {
				return assistantReadiness{}, "", errors.New("settings unreadable")
			}
			return assistantReadiness{Enabled: true, Configured: true, Model: "chat/model", DocumentModel: testDocumentModel}, "test-key", nil
		},
		doer: &fakeMateDoer{},
	}
	t.Cleanup(func() { globalInboxMate = prev })

	c := classifierFor(w)
	c.errorBackoff = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if it, err := w.inbox.Get(id); err == nil && it.SuggestedKind != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if it, _ := w.inbox.Get(id); it.SuggestedKind != "scenery" {
		t.Errorf("Run stalled after the error: kind %q after %d readiness reads", it.SuggestedKind, reads.Load())
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
	withPlaceLookup(t, func(float64, float64) string {
		t.Error("nearest-place used although a feature is in the sector")
		return "Wrong"
	})
	id := uploadForFiling(t, 180, map[string]string{"capture_ms": ms(at), "bearing_deg": "90"})
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
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		t.Error("features fetched for a photo with no bearing")
		return nil, nil
	})
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
	withPlaceLookup(t, func(float64, float64) string {
		t.Error("masked the failure with the nearest place")
		return "Hook Island"
	})
	id := uploadForFiling(t, 183, map[string]string{"capture_ms": ms(at), "bearing_deg": "90"})
	titles, errs := titlesFor(t, id)
	if msg, _ := errs[id].(string); msg != "The place-name server didn't answer, so no title was suggested." || titles[id] != "" {
		t.Errorf("%v %v", titles, errs)
	}
}

// ── the pieces on their own ──────────────────────────────────────────────

func TestInboxTitleBuilderChoosesTheWayOnceAndSharesAPlaceLookupPerCell(t *testing.T) {
	bearing := 90.0
	at := &trackFix{Lat: -20.25, Lon: 149.5}
	nearby := &trackFix{Lat: -20.25004, Lon: 149.50004} // the same 100 m cell
	elsewhere := &trackFix{Lat: -20.5, Lon: 149.5}

	// Without Mate: a photo with a bearing has its own sector task; the others
	// share a place task per cell.
	b := newInboxTitleBuilder(nil, newInboxFeatureCache(inboxFeatureTTL), nil)
	b.add(inboxItem{ID: "a", MIME: "image/jpeg"}, at)
	b.add(inboxItem{ID: "b", MIME: "image/jpeg"}, nearby)
	b.add(inboxItem{ID: "c", MIME: "image/jpeg"}, elsewhere)
	b.add(inboxItem{ID: "d", MIME: "image/jpeg", BearingDeg: &bearing}, at)
	if len(b.tasks) != 3 || strings.Join(b.tasks[0].ids, ",") != "a,b" || b.tasks[2].ids[0] != "d" {
		t.Fatalf("without Mate: %d tasks, first for %v", len(b.tasks), b.tasks[0].ids)
	}
	if b.tasks[0].timeout != inboxPlaceTitleTimeout {
		t.Errorf("a place lookup gets the place timeout, got %v", b.tasks[0].timeout)
	}

	// With Mate: one task per image and a PDF has none. The task has no overall
	// timeout: the call times itself once it holds a slot.
	m := newInboxTitleBuilder(&inboxMateSession{}, newInboxFeatureCache(inboxFeatureTTL), nil)
	m.add(inboxItem{ID: "a", MIME: "image/jpeg"}, at)
	m.add(inboxItem{ID: "b", MIME: "image/jpeg"}, nearby)
	m.add(inboxItem{ID: "p", MIME: "application/pdf"}, at)
	if len(m.tasks) != 2 || m.tasks[0].timeout != 0 || m.tasks[0].timeoutMsg != "Mate didn't answer in time" {
		t.Errorf("with Mate: %d tasks, %+v", len(m.tasks), m.tasks[0])
	}
}

func TestInboxFeatureCacheFetchesOncePerCellUntilItExpiresAndKeepsNoFailure(t *testing.T) {
	var fetches atomic.Int32
	failing := true
	withFetched(t, func(float64, float64) (inboxFetched, error) {
		fetches.Add(1)
		if failing {
			return inboxFetched{}, errors.New("overpass is down")
		}
		return inboxFetched{Truncated: true}, nil
	})
	fc := newInboxFeatureCache(time.Hour)
	if _, err := fc.get(-20.25, 149.5); err == nil {
		t.Fatal("the failure was hidden")
	}
	failing = false
	got, err := fc.get(-20.25, 149.5) // asked again, because a failure is not kept
	if err != nil || !got.Truncated || fetches.Load() != 2 {
		t.Fatalf("%v %v after %d fetches", got, err, fetches.Load())
	}
	fc.get(-20.2501, 149.5001) // the same 0.02 degree cell
	if fetches.Load() != 2 {
		t.Errorf("a second photo in the cell fetched again: %d", fetches.Load())
	}

	short := newInboxFeatureCache(time.Millisecond)
	short.get(-20.25, 149.5)
	time.Sleep(5 * time.Millisecond)
	short.get(-20.25, 149.5)
	if fetches.Load() != 4 {
		t.Errorf("an expired cell was not fetched again: %d", fetches.Load())
	}
}

func TestInboxMateSettingsAreReadOnceInAWhileAndAnErrorIsNotRemembered(t *testing.T) {
	var reads atomic.Int32
	failNext := true
	m := &inboxMate{readiness: func() (assistantReadiness, string, error) {
		reads.Add(1)
		if failNext {
			failNext = false
			return assistantReadiness{}, "", errors.New("unreadable")
		}
		return assistantReadiness{Enabled: true, Configured: true, Model: "chat/model", DocumentModel: testDocumentModel}, "key", nil
	}}
	if _, _, err := m.session(); err == nil {
		t.Fatal("the failure was hidden")
	}
	for i := 0; i < 3; i++ {
		if _, ok, err := m.session(); err != nil || !ok {
			t.Fatalf("%v %v", ok, err)
		}
	}
	if reads.Load() != 2 {
		t.Errorf("settings read %d times, want 2 (the failure, then once for three asks)", reads.Load())
	}
}

func TestInboxClassifierReadsPagesUntilNothingIsWaiting(t *testing.T) {
	w := newInboxWorld(t)
	withInboxMate(t, &fakeMateDoer{})
	total := inboxClassifyPage + 5
	for i := 0; i < total; i++ {
		// No thumbnail, so each is recorded as unsortable without a call.
		if _, err := w.inbox.Insert(inboxItem{SHA256: fmt.Sprintf("%064d", i), Filename: "x.jpg", MIME: "image/jpeg", SizeBytes: 10}); err != nil {
			t.Fatal(err)
		}
	}
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if left, err := w.inbox.Unclassified(); err != nil || len(left) != 0 {
		t.Errorf("%d still waiting after a pass: %v", len(left), err)
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
		"course over ground",
	} {
		if !strings.Contains(inboxTitlePrompt, want) {
			t.Errorf("the title prompt no longer says %q", want)
		}
	}
	// A track fix gives a course over ground, which is not the way the bow points.
	if strings.Contains(inboxTitlePrompt, "heading") || strings.Contains(inboxTitleMessage(inboxTitleInput{}), "heading") {
		t.Errorf("the title prompt calls the course over ground a heading")
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
