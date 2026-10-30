package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strings"
	"testing"
)

// How a charted alarm reaches each channel (ADR 0235).

func chartedAlarm() notificationMessage {
	c := testWatchChart()
	c.Title = "Engine Room Hot, last 6 hours"
	return notificationMessage{Kind: alarmEventRaised, RuleID: "r1", Label: "Engine Room Hot", Path: "environment.inside.engineRoom.temperature",
		State: alarmStateAlarm, Message: "Engine room 71 °C", Value: 344.15, Unit: "K", Vessel: "Pikorua", At: alarmNow, Charts: []watchChart{c}}
}

func leftOutAlarm(reason string) notificationMessage {
	m := chartedAlarm()
	m.Charts = []watchChart{{Title: "Engine Room Hot, last 6 hours", Problem: reason}}
	return m
}

func readAlarmEmail(t *testing.T, raw []byte) (plain, html string, images [][]byte) {
	t.Helper()
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
	_, ap, _ := mime.ParseMediaType(alt.Header.Get("Content-Type"))
	altR := multipart.NewReader(alt, ap["boundary"])
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
	for {
		p, err := related.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		enc, _ := io.ReadAll(p)
		data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(enc)), ""))
		if err != nil {
			t.Fatal(err)
		}
		images = append(images, data)
	}
	return
}

func TestAlarmEmail_ChartIsAnInlineImageUnderTheBody(t *testing.T) {
	raw := buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"skip@example.com"}}, chartedAlarm())
	plain, html, images := readAlarmEmail(t, raw)
	if !strings.Contains(plain, "Engine room 71") || !strings.Contains(plain, "State: alarm") {
		t.Errorf("plain part %q", plain)
	}
	if len(images) != 1 || decodeChartPNG(t, images[0]).Bounds().Dx() != 800 {
		t.Fatalf("images %d", len(images))
	}
	if !strings.Contains(html, "Engine room 71") || !strings.Contains(html, `src="cid:`) {
		t.Errorf("html %s", html)
	}
	if strings.Index(html, "Engine room 71") > strings.Index(html, "cid:") {
		t.Errorf("the chart goes under the body: %s", html)
	}
	if !strings.Contains(string(raw), "Subject: [ALARM] Pikorua: Engine Room Hot") {
		t.Errorf("subject: %s", raw[:200])
	}
}

func TestAlarmEmail_LeftOutChartSaysWhyInBothParts(t *testing.T) {
	raw := string(buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"a@b.c"}}, leftOutAlarm("InfluxDB history is not available on this install")))
	if strings.Contains(raw, "multipart") || !strings.Contains(raw, "Chart left out: InfluxDB history is not available on this install") {
		t.Errorf("%s", raw)
	}
}

func TestAlarmEmail_NoChartIsTheMessageItWas(t *testing.T) {
	m := chartedAlarm()
	m.Charts = nil
	raw := string(buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"a@b.c"}}, m))
	if strings.Contains(raw, "multipart") || strings.Contains(raw, "Chart left out") {
		t.Errorf("%s", raw)
	}
}

func TestAlarmNtfy_ChartGoesAsAnAttachment(t *testing.T) {
	server, hits := fakeNtfy(t, 0, "")
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), chartedAlarm()); err != nil {
		t.Fatal(err)
	}
	got := hits()
	if len(got) != 1 || got[0].method != http.MethodPut {
		t.Fatalf("hits %+v", got)
	}
	h := got[0].headers
	if h.Get("Filename") != "engine-room-hot-last-6-hours.png" || h.Get("Title") != "[ALARM] Pikorua: Engine Room Hot" || h.Get("Priority") != "4" || h.Get("Tags") != "warning" ||
		h.Get("Message") != "Engine room 71 °C" && !strings.HasPrefix(h.Get("Message"), "=?") {
		t.Errorf("headers %v", h)
	}
	if decodeChartPNG(t, got[0].body).Bounds().Dx() != 800 {
		t.Error("the body is the PNG")
	}
}

func TestAlarmNtfy_RefusedAttachmentGoesAsTextAndIsLogged(t *testing.T) {
	server, hits := fakeNtfy(t, http.StatusBadRequest, `{"error":"attachments not allowed"}`)
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), chartedAlarm()); err != nil {
		t.Fatal(err)
	}
	got := hits()
	if len(got) != 2 || got[1].method != http.MethodPost || string(got[1].body) != "Engine room 71 °C" {
		t.Fatalf("hits %+v", got)
	}
}

func TestAlarmNtfy_LeftOutChartIsTextWithTheLine(t *testing.T) {
	server, hits := fakeNtfy(t, 0, "")
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), leftOutAlarm("nothing was recorded for Engine Room Hot in this window")); err != nil {
		t.Fatal(err)
	}
	got := hits()
	if len(got) != 1 || got[0].method != http.MethodPost || !strings.HasSuffix(string(got[0].body), "\nChart left out: nothing was recorded for Engine Room Hot in this window") {
		t.Fatalf("hits %+v", got)
	}
}

func TestAlarmWebhook_ChartsEntryHasThePictureOrTheProblem(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	transport := webhookTransport{config: webhookConfig{URL: server.URL}}
	if err := transport.Send(context.Background(), chartedAlarm()); err != nil {
		t.Fatal(err)
	}
	var decoded notificationMessage
	if err := json.Unmarshal([]byte(requests()[0].body), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != alarmEventRaised || len(decoded.Charts) != 1 || decoded.Charts[0].PNGBase64 == "" || decoded.Charts[0].Problem != "" {
		t.Fatalf("payload %s", requests()[0].body)
	}
	data, err := base64.StdEncoding.DecodeString(decoded.Charts[0].PNGBase64)
	if err != nil || decodeChartPNG(t, data).Bounds().Dx() != 800 {
		t.Errorf("png: %v", err)
	}

	if err := transport.Send(context.Background(), leftOutAlarm("history is slow")); err != nil {
		t.Fatal(err)
	}
	body := requests()[1].body
	if !strings.Contains(body, `"problem":"history is slow"`) || strings.Contains(body, "png_base64") {
		t.Errorf("payload %s", body)
	}
}

func TestAlarmWebhook_NoChartMeansNoChartsKey(t *testing.T) {
	server, requests := capturingServer(t, http.StatusOK)
	if err := (webhookTransport{config: webhookConfig{URL: server.URL}}).Send(context.Background(), testMessage()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(requests()[0].body, `"charts"`) {
		t.Errorf("payload %s", requests()[0].body)
	}
}

func TestAlarmEmail_HasNoPathLine(t *testing.T) {
	raw := buildAlarmEmail(smtpConfig{From: "boat@example.com", To: []string{"a@b.c"}}, chartedAlarm())
	plain, html, _ := readAlarmEmail(t, raw)
	for _, part := range []string{plain, html} {
		if strings.Contains(part, "environment.inside") || strings.Contains(part, "Path") {
			t.Errorf("a path reached the email: %s", part)
		}
		if !strings.Contains(part, "State") || !strings.Contains(part, "Value") || !strings.Contains(part, "Time") {
			t.Errorf("state, value and time stay: %s", part)
		}
	}
	m := chartedAlarm()
	m.Charts = nil
	if strings.Contains(string(buildAlarmEmail(smtpConfig{From: "a@b.c", To: []string{"a@b.c"}}, m)), "Path") {
		t.Error("the plain email has no path either")
	}
}

func TestAlarmNtfy_OnlyTheChartItSendsCanPutANoteOnTheRun(t *testing.T) {
	server, _ := fakeNtfy(t, 0, "")
	var notes []string
	msg := chartedWatchMessage(t)
	broken := testWatchChart()
	broken.Series = nil // cannot be drawn
	msg.Watch.Charts = append(msg.Watch.Charts, broken)
	msg.note = func(s string) { notes = append(notes, s) }
	if err := (ntfyTransport{config: ntfyConfig{Server: server.URL, Topic: "boat"}}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("a second chart ntfy never sends put a note on the run: %v", notes)
	}
}
