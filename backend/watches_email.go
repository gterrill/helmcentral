package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"log"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"
)

// A watch report with charts goes as multipart/related: a multipart/alternative
// holding the plain text and an HTML version, then each chart as an inline PNG
// the HTML refers to by Content-ID (ADR 0228).

func chartCID(i int) string { return fmt.Sprintf("watch-chart-%d@helmcentral", i) }

// watchEmailHTML is the report as HTML, each chart under its own finding.
func watchEmailHTML(m watchReportMessage) string {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString("<h2>" + e(m.Title) + "</h2>")
	if m.Run.BoatTime != "" {
		b.WriteString("<p>Report from " + e(m.Run.BoatTime) + "</p>")
	}
	if m.Link != "" {
		b.WriteString(`<p><a href="` + e(m.Link) + `">Open the report</a></p>`)
	}
	if len(m.Findings) > 0 {
		b.WriteString("<h3>Findings</h3><ul>")
		for i, f := range m.Findings {
			b.WriteString("<li><p>" + e(f.Text) + "</p>")
			for ci, c := range m.Charts {
				if c.FindingIndex == i && c.Problem != "" {
					b.WriteString(chartProblemHTML(c.Problem))
				} else if c.FindingIndex == i {
					b.WriteString(chartImgHTML(ci, c))
				}
			}
			if f.ChartProblem != "" {
				b.WriteString(`<p style="color:#5a626b">` + e(f.ChartProblem) + "</p>")
			}
			b.WriteString("</li>")
		}
		b.WriteString("</ul>")
	}
	if m.Review != nil && strings.TrimSpace(*m.Review) != "" {
		b.WriteString("<h3>Mate's review</h3><p>" + strings.ReplaceAll(e(strings.TrimSpace(*m.Review)), "\n", "<br>") + "</p>")
	}
	if m.Report != nil {
		for _, s := range m.Report.Sections {
			b.WriteString("<h3>" + e(s.Title) + "</h3>")
			if s.Unavailable != "" {
				b.WriteString("<p>" + e(s.Unavailable) + "</p>")
			}
			for _, l := range s.Lines {
				b.WriteString("<p style=\"margin:2px 0\">" + e(l) + "</p>")
			}
		}
	}
	return emailHTMLShell(b.String())
}

func writeQPPart(w *multipart.Writer, contentType, body string) error {
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType + "; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
}

// wrapBase64 is data as base64 in 76 column lines, as mail wants it.
func wrapBase64(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	return b.String()
}

// buildWatchEmailWithCharts builds the whole message. A chart that cannot be
// drawn is left out and the message says so. With none drawn, the message is
// the plain one with those lines.
func buildWatchEmailWithCharts(config smtpConfig, msg notificationMessage) []byte {
	return buildChartEmail(config, msg, func(charts []watchChart) (string, string) {
		m := *msg.Watch
		m.Charts = charts
		return watchEmailText(m), watchEmailHTML(m)
	})
}

// buildChartEmail is a message with charts, for a watch report or an alarm:
// body gives the plain and HTML parts for the charts as they stand. A chart that
// cannot be drawn is left out and the message says so. If the message cannot be
// put together, or none was drawn, it goes as text, with the reason on each
// chart and a note on the run.
func buildChartEmail(config smtpConfig, msg notificationMessage, body func(charts []watchChart) (text, htmlBody string)) []byte {
	charts, pngs := msg.renderedCharts("email", 0)
	drawn := false
	for _, p := range pngs {
		drawn = drawn || p != nil
	}
	if drawn {
		text, htmlBody := body(charts)
		raw, err := assembleChartEmail(config, msg.title(), text, htmlBody, charts, pngs)
		if err == nil {
			return raw
		}
		reason := "the chart could not be put in the email: " + firstErrorLine(err)
		log.Printf("notify: the charts of %q could not be put in the email, so it is sent as text: %v", msg.Label, err)
		if msg.note != nil {
			msg.note("email: sent without the charts (" + firstErrorLine(err) + ")")
		}
		charts = append([]watchChart(nil), charts...)
		for i := range charts {
			if pngs[i] != nil {
				charts[i].Problem = reason
			}
		}
	}
	text, _ := body(charts)
	return plainTextEmail(config, msg.title(), text)
}

// plainTextEmail is a text-only message. The subject goes through the header
// encoder, since a title can hold what the operator typed.
func plainTextEmail(config smtpConfig, subject, text string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", config.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(config.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", watchEncodedHeaderText(subject))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(text, "\n", "\r\n"))
	return []byte(b.String())
}

// emailHTMLShell wraps a body in the page both kinds of message use.
func emailHTMLShell(body string) string {
	return `<!doctype html><html><body style="font-family:sans-serif;color:#212529;max-width:820px">` + body + "</body></html>"
}

// chartImgHTML is chart i as an inline image, by Content-ID.
func chartImgHTML(i int, c watchChart) string {
	return `<p><img src="cid:` + chartCID(i) + `" alt="` + html.EscapeString(c.Title) + `" width="800" style="max-width:100%;height:auto"></p>`
}

// chartProblemHTML is the line saying a chart was left out.
func chartProblemHTML(problem string) string {
	return `<p style="color:#5a626b">Chart left out: ` + html.EscapeString(problem) + "</p>"
}

// assembleChartEmail is a message with a plain and an HTML body and each drawn
// chart as an inline PNG, shared by watch reports and alarms. A nil entry in
// pngs is a chart left out and is not attached.
func assembleChartEmail(config smtpConfig, subject, text, htmlBody string, charts []watchChart, pngs [][]byte) ([]byte, error) {
	var body bytes.Buffer
	related := multipart.NewWriter(&body)
	var alt bytes.Buffer
	altW := multipart.NewWriter(&alt)
	if err := writeQPPart(altW, "text/plain", text); err != nil {
		return nil, err
	}
	if err := writeQPPart(altW, "text/html", htmlBody); err != nil {
		return nil, err
	}
	if err := altW.Close(); err != nil {
		return nil, err
	}
	part, err := related.CreatePart(textproto.MIMEHeader{"Content-Type": {`multipart/alternative; boundary="` + altW.Boundary() + `"`}})
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(alt.Bytes()); err != nil {
		return nil, err
	}
	for i, c := range charts {
		if pngs[i] == nil {
			continue
		}
		img, err := related.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"image/png"},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Id":                {"<" + chartCID(i) + ">"},
			"Content-Disposition":       {`inline; filename="` + watchChartFilename(c.Title) + `"`},
		})
		if err != nil {
			return nil, err
		}
		if _, err := img.Write([]byte(wrapBase64(pngs[i]))); err != nil {
			return nil, err
		}
	}
	if err := related.Close(); err != nil {
		return nil, err
	}

	var head strings.Builder
	fmt.Fprintf(&head, "From: %s\r\n", config.From)
	fmt.Fprintf(&head, "To: %s\r\n", strings.Join(config.To, ", "))
	fmt.Fprintf(&head, "Subject: %s\r\n", watchEncodedHeaderText(subject))
	head.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&head, "Content-Type: multipart/related; type=\"multipart/alternative\"; boundary=\"%s\"\r\n\r\n", related.Boundary())
	return append([]byte(head.String()), body.Bytes()...), nil
}
