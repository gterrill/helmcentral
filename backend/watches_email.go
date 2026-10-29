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
	b.WriteString(`<!doctype html><html><body style="font-family:sans-serif;color:#212529;max-width:820px">`)
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
					b.WriteString(`<p style="color:#5a626b">Chart left out: ` + e(c.Problem) + "</p>")
				} else if c.FindingIndex == i {
					b.WriteString(`<p><img src="cid:` + chartCID(ci) + `" alt="` + e(c.Title) + `" width="800" style="max-width:100%;height:auto"></p>`)
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
	b.WriteString("</body></html>")
	return b.String()
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
	m := *msg.Watch
	var pngs [][]byte
	m.Charts, pngs = renderChartsForSend(m.Charts, "email", msg.note)
	drawn := 0
	for _, p := range pngs {
		if p != nil {
			drawn++
		}
	}
	if drawn > 0 {
		raw, err := assembleWatchEmail(config, msg, m, pngs)
		if err == nil {
			return raw
		}
		// Building the message in memory failed: the report still goes, as text.
		log.Printf("watch report: the charts of %q could not be put in the email, so it is sent as text: %v", msg.Label, err)
		if msg.note != nil {
			msg.note("email: sent without the charts (" + firstErrorLine(err) + ")")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", config.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(config.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", watchEncodedHeaderText(msg.title()))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(watchEmailText(m), "\n", "\r\n"))
	return []byte(b.String())
}

func assembleWatchEmail(config smtpConfig, msg notificationMessage, m watchReportMessage, pngs [][]byte) ([]byte, error) {
	var body bytes.Buffer
	related := multipart.NewWriter(&body)
	var alt bytes.Buffer
	altW := multipart.NewWriter(&alt)
	if err := writeQPPart(altW, "text/plain", watchEmailText(m)); err != nil {
		return nil, err
	}
	if err := writeQPPart(altW, "text/html", watchEmailHTML(m)); err != nil {
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
	for i, c := range m.Charts {
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
	fmt.Fprintf(&head, "Subject: %s\r\n", watchEncodedHeaderText(msg.title()))
	head.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&head, "Content-Type: multipart/related; type=\"multipart/alternative\"; boundary=\"%s\"\r\n\r\n", related.Boundary())
	return append([]byte(head.String()), body.Bytes()...), nil
}
