package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync"
	"testing"
)

// Charts in a watch report's notifications (ADR 0228).

func chartedWatchMessage(t *testing.T) notificationMessage {
	t.Helper()
	msg := testWatchMessage(t)
	msg.Watch.Findings = append(msg.Watch.Findings, watchFinding{Section: "tanks", Rule: "tank_change", Text: "Diesel is down 14 points.", By: watchByRules,
		ChartProblem: "No history to chart: nothing was recorded for Diesel in this window"})
	msg.Watch.Charts = []watchChart{testWatchChart()}
	msg.Watch.Charts[0].FindingIndex = 0
	return msg
}

func TestBuildWatchReportMessage_CarriesTheRunsCharts(t *testing.T) {
	run := watchRun{ID: "r1", Outcome: watchOutcomeRan, Charts: []watchChart{testWatchChart()}}
	m := buildWatchReportMessage(savedWatch{ID: "w1", Name: "N"}, run, "V", "")
	if len(m.Charts) != 1 || m.Charts[0].Title != "Pressure, last 12 hours" {
		t.Fatalf("charts %+v", m.Charts)
	}
	raw, _ := json.Marshal(buildWatchReportMessage(savedWatch{ID: "w1", Name: "N"}, watchRun{ID: "r2"}, "V", ""))
	if !strings.Contains(string(raw), `"charts":[]`) {
		t.Errorf("a report with no charts says charts: [] %s", raw)
	}
}

func TestWatchEmail_ChartsAreInlineImagesUnderTheirFinding(t *testing.T) {
	raw := buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"skip@example.com"}}, chartedWatchMessage(t))
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/related" {
		t.Fatalf("content type %q %v", msg.Header.Get("Content-Type"), err)
	}
	related := multipart.NewReader(msg.Body, params["boundary"])
	alt, err := related.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	amt, aparams, _ := mime.ParseMediaType(alt.Header.Get("Content-Type"))
	if amt != "multipart/alternative" {
		t.Fatalf("first part %q", alt.Header.Get("Content-Type"))
	}
	altR := multipart.NewReader(alt, aparams["boundary"])
	var plain, html string
	for {
		p, err := altR.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		switch ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type")); ct {
		case "text/plain":
			plain = string(b)
		case "text/html":
			html = string(b)
		}
	}
	if !strings.Contains(plain, "Drew a lot.") || !strings.Contains(plain, "No history to chart") {
		t.Errorf("plain part %q", plain)
	}
	img, err := related.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	cid := strings.Trim(img.Header.Get("Content-Id"), "<>")
	if ct := img.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") || cid == "" || img.Header.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatalf("image part headers %v", img.Header)
	}
	enc, _ := io.ReadAll(img)
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(enc)), ""))
	if err != nil {
		t.Fatal(err)
	}
	if b := decodeChartPNG(t, data).Bounds(); b.Dx() != 800 {
		t.Errorf("png %v", b)
	}
	if !strings.Contains(html, `src="cid:`+cid+`"`) {
		t.Errorf("the html does not show the image: %s", html)
	}
	if strings.Index(html, "Drew a lot.") > strings.Index(html, "cid:"+cid) || strings.Index(html, "cid:"+cid) > strings.Index(html, "Diesel is down") {
		t.Errorf("the chart sits under its own finding: %s", html)
	}
	if !strings.Contains(html, "No history to chart") {
		t.Errorf("the html says why a chart is missing: %s", html)
	}
	if _, err := related.NextPart(); err != io.EOF {
		t.Errorf("only one image was expected, err %v", err)
	}
}

func TestWatchEmail_NoChartsStaysPlainText(t *testing.T) {
	raw := string(buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"a@b.c"}}, testWatchMessage(t)))
	if !strings.Contains(raw, "Content-Type: text/plain; charset=utf-8") || strings.Contains(raw, "multipart") {
		t.Errorf("%s", raw)
	}
}

func TestWatchEmail_HTMLEscapesWhatTheOperatorTyped(t *testing.T) {
	msg := chartedWatchMessage(t)
	msg.Watch.Findings[0].Text = `<script>alert(1)</script> & "more"`
	got := watchEmailHTML(*msg.Watch)
	if strings.Contains(got, "<script>") || !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("an unescaped tag reached the HTML part: %s", got)
	}
}

type ntfyHit struct {
	method  string
	headers http.Header
	body    []byte
}

func fakeNtfy(t *testing.T, attachmentStatus int, attachmentBody string) (*httptest.Server, func() []ntfyHit) {
	t.Helper()
	var mu sync.Mutex
	var hits []ntfyHit
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits = append(hits, ntfyHit{r.Method, r.Header.Clone(), b})
		mu.Unlock()
		if r.Method == http.MethodPut && attachmentStatus != 0 {
			w.WriteHeader(attachmentStatus)
			_, _ = w.Write([]byte(attachmentBody))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.Close)
	return s, func() []ntfyHit { mu.Lock(); defer mu.Unlock(); return append([]ntfyHit{}, hits...) }
}

func TestWatchNtfy_FirstChartGoesAsAnAttachment(t *testing.T) {
	server, hits := fakeNtfy(t, 0, "")
	var notes []string
	msg := chartedWatchMessage(t)
	msg.note = func(s string) { notes = append(notes, s) }
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	got := hits()
	if len(got) != 1 || got[0].method != http.MethodPut {
		t.Fatalf("hits %+v", got)
	}
	h := got[0].headers
	if h.Get("Filename") != "pressure-last-12-hours.png" || h.Get("Title") != "Overnight: 1 finding" || h.Get("Priority") != "3" || h.Get("Tags") != "clipboard" ||
		h.Get("Click") != "http://boat.local:9091/watches/w1?run=r1" {
		t.Errorf("headers %v", h)
	}
	if m := h.Get("Message"); !strings.HasPrefix(m, "Drew a lot.") || strings.ContainsAny(m, "\r\n") {
		t.Errorf("message header %q", m)
	}
	if b := decodeChartPNG(t, got[0].body).Bounds(); b.Dx() != 800 || b.Dy() != 400 {
		t.Errorf("the body is the PNG: %v", b)
	}
	if len(notes) != 0 {
		t.Errorf("notes %v", notes)
	}
}

func TestWatchNtfy_AServerWithoutAttachmentsGetsTheTextAndTheRunSaysSo(t *testing.T) {
	server, hits := fakeNtfy(t, http.StatusBadRequest, `{"code":40013,"error":"attachments not allowed"}`)
	var notes []string
	msg := chartedWatchMessage(t)
	msg.note = func(s string) { notes = append(notes, s) }
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	got := hits()
	if len(got) != 2 || got[0].method != http.MethodPut || got[1].method != http.MethodPost || !strings.HasPrefix(string(got[1].body), "Drew a lot.") {
		t.Fatalf("hits %+v", got)
	}
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "ntfy: sent without the chart (") || !strings.Contains(notes[0], "attachments not allowed") {
		t.Errorf("notes %v", notes)
	}
}

func TestWatchNtfy_OtherFailuresAreNotMistakenForARefusedAttachment(t *testing.T) {
	server, hits := fakeNtfy(t, http.StatusInternalServerError, "boom")
	err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), chartedWatchMessage(t))
	if err == nil || len(hits()) != 1 {
		t.Fatalf("a server error is retried, not worked round: %v %d", err, len(hits()))
	}
}

func TestWatchNtfy_NoChartsIsTheSamePostAsBefore(t *testing.T) {
	server, hits := fakeNtfy(t, 0, "")
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), testWatchMessage(t)); err != nil {
		t.Fatal(err)
	}
	if got := hits(); len(got) != 1 || got[0].method != http.MethodPost {
		t.Errorf("hits %+v", got)
	}
}

func TestWatchWebhook_PayloadCarriesChartsWithAPNG(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	if err := (webhookTransport{config: webhookConfig{URL: server.URL}}).Send(context.Background(), chartedWatchMessage(t)); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Charts []struct {
			FindingIndex int    `json:"finding_index"`
			Title        string `json:"title"`
			Unit         string `json:"unit"`
			Series       []struct {
				Label  string       `json:"label"`
				Points [][2]float64 `json:"points"`
			} `json:"series"`
			Threshold *struct {
				Value float64 `json:"value"`
			} `json:"threshold"`
			PNG string `json:"png_base64"`
		} `json:"charts"`
	}
	if err := json.Unmarshal([]byte(requests()[0].body), &payload); err != nil || len(payload.Charts) != 1 {
		t.Fatalf("payload %v %s", err, requests()[0].body)
	}
	c := payload.Charts[0]
	if c.Title == "" || c.Unit != "hPa" || len(c.Series) != 2 || len(c.Series[0].Points) != 49 || c.Threshold == nil || c.Threshold.Value != 1008 {
		t.Errorf("chart %+v", c)
	}
	data, err := base64.StdEncoding.DecodeString(c.PNG)
	if err != nil || decodeChartPNG(t, data).Bounds().Dx() != 800 {
		t.Errorf("png_base64 is the PNG: %v", err)
	}
}

func TestWatchWebhook_TheQueuedCopyHoldsNoPNG(t *testing.T) {
	msg := chartedWatchMessage(t)
	raw, _ := json.Marshal(msg)
	if strings.Contains(string(raw), "png_base64") {
		t.Errorf("a PNG is drawn when it is sent, never stored in the retry queue")
	}
}

func TestDispatchWatchReport_NoteFromATransportReachesTheRunHook(t *testing.T) {
	server, _ := fakeNtfy(t, http.StatusBadRequest, `{"error":"attachments not allowed"}`)
	d, _ := watchDispatcher(t, ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}})
	var got []string
	d.watchNote = func(w, r, note string) { got = append(got, w+"/"+r+": "+note) }
	var delivered []string
	d.watchDelivered = func(_, _, c string) { delivered = append(delivered, c) }
	m := chartedWatchMessage(t)
	d.dispatchWatchReport(*m.Watch, []string{transportNtfy})
	if len(delivered) != 1 || len(got) != 1 || !strings.HasPrefix(got[0], "w1/r1: ntfy: sent without the chart (") {
		t.Fatalf("delivered %v notes %v", delivered, got)
	}
}

// A chart that cannot be drawn never stops a report going out (ADR 0228): the
// report goes without it and says so, the same on every channel.

func brokenChart(findingIndex int) watchChart {
	c := testWatchChart()
	c.FindingIndex = findingIndex
	c.Title = "Diesel level"
	c.Series = []watchChartSeries{{Label: "Diesel", Unit: "%", Points: [][2]float64{{1, 1}}}}
	c.Threshold = nil
	return c
}

func TestWatchEmail_AChartThatCannotBeDrawnIsLeftOutAndSaid(t *testing.T) {
	msg := chartedWatchMessage(t)
	msg.Watch.Charts = append(msg.Watch.Charts, brokenChart(1))
	var notes []string
	msg.note = func(s string) { notes = append(notes, s) }
	raw := buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"skip@example.com"}}, msg)
	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	related := multipart.NewReader(parsed.Body, params["boundary"])
	alt, _ := related.NextPart()
	_, aparams, _ := mime.ParseMediaType(alt.Header.Get("Content-Type"))
	altR := multipart.NewReader(alt, aparams["boundary"])
	var plain, html string
	for {
		p, err := altR.NextPart()
		if err == io.EOF {
			break
		}
		b, _ := io.ReadAll(p)
		if ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type")); ct == "text/plain" {
			plain = string(b)
		} else {
			html = string(b)
		}
	}
	for name, body := range map[string]string{"plain": plain, "html": html} {
		if !strings.Contains(body, "Chart left out: ") {
			t.Errorf("%s part does not say the chart was left out: %s", name, body)
		}
	}
	if _, err := related.NextPart(); err != nil {
		t.Errorf("the chart that could be drawn is still attached: %v", err)
	}
	if _, err := related.NextPart(); err != io.EOF {
		t.Errorf("only the good chart is attached, err %v", err)
	}
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "email: sent without the chart (") {
		t.Errorf("notes %v", notes)
	}
}

func TestWatchEmail_EveryChartBrokenIsStillAnEmailWithTheLine(t *testing.T) {
	msg := chartedWatchMessage(t)
	msg.Watch.Charts = []watchChart{brokenChart(0)}
	raw := string(buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"a@b.c"}}, msg))
	if !strings.Contains(raw, "Drew a lot.") || !strings.Contains(raw, "Chart left out: ") {
		t.Errorf("%s", raw)
	}
}

func TestWatchNtfy_AChartThatCannotBeDrawnGoesAsTextWithTheLine(t *testing.T) {
	server, hits := fakeNtfy(t, 0, "")
	var notes []string
	msg := chartedWatchMessage(t)
	msg.Watch.Charts = []watchChart{brokenChart(0)}
	msg.note = func(s string) { notes = append(notes, s) }
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), msg); err != nil {
		t.Fatalf("the report is not held back by its chart: %v", err)
	}
	got := hits()
	if len(got) != 1 || got[0].method != http.MethodPost || !strings.Contains(string(got[0].body), "Chart left out: ") || !strings.HasPrefix(string(got[0].body), "Drew a lot.") {
		t.Fatalf("hits %+v", got)
	}
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "ntfy: sent without the chart (") {
		t.Errorf("notes %v", notes)
	}
}

func TestWatchWebhook_AChartThatCannotBeDrawnHasAProblemInsteadOfAPNG(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	msg := chartedWatchMessage(t)
	msg.Watch.Charts = append(msg.Watch.Charts, brokenChart(1))
	var notes []string
	msg.note = func(s string) { notes = append(notes, s) }
	if err := (webhookTransport{config: webhookConfig{URL: server.URL}}).Send(context.Background(), msg); err != nil {
		t.Fatalf("the report is not held back by its chart: %v", err)
	}
	var payload struct {
		Charts []struct {
			FindingIndex int    `json:"finding_index"`
			PNG          string `json:"png_base64"`
			Problem      string `json:"problem"`
		} `json:"charts"`
	}
	if err := json.Unmarshal([]byte(requests()[0].body), &payload); err != nil || len(payload.Charts) != 2 {
		t.Fatalf("payload %v %s", err, requests()[0].body)
	}
	if payload.Charts[0].PNG == "" || payload.Charts[0].Problem != "" {
		t.Errorf("good chart %+v", payload.Charts[0])
	}
	if payload.Charts[1].PNG != "" || payload.Charts[1].Problem == "" {
		t.Errorf("broken chart %+v", payload.Charts[1])
	}
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "webhook: sent without the chart (") {
		t.Errorf("notes %v", notes)
	}
}
