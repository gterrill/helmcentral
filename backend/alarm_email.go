package main

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// An alarm's email (ADR 0235). With a chart it is built the way a watch report's
// is, by buildChartEmail; without one it is a plain text message.

// buildAlarmEmail is the email for any notification message.
func buildAlarmEmail(config smtpConfig, msg notificationMessage) []byte {
	switch {
	case msg.isWatchReport() && msg.Watch != nil:
		return buildWatchEmail(config, msg)
	case len(msg.Charts) > 0:
		return buildChartEmail(config, msg, func(charts []watchChart) (string, string) {
			m := msg
			m.Charts = charts
			return alarmEmailText(m), alarmEmailHTML(m)
		})
	}
	return plainTextEmail(config, msg.title(), alarmEmailText(msg))
}

// alarmEmailFields are the labelled lines under an alarm's message, read by the
// text and the HTML alike. There is no path: the message says what it is about.
func alarmEmailFields(msg notificationMessage) [][2]string {
	return [][2]string{
		{"State", msg.State},
		{"Value", formatAlarmReading(msg.Value, msg.Unit)},
		{"Time", msg.At.UTC().Format(time.RFC3339)},
	}
}

// alarmEmailText is an alarm's plain text body, with a line saying why when its
// chart was left out.
func alarmEmailText(msg notificationMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", msg.Message)
	for _, f := range alarmEmailFields(msg) {
		fmt.Fprintf(&b, "%-7s%s\n", f[0]+":", f[1])
	}
	for _, c := range msg.Charts {
		if c.Problem != "" {
			b.WriteString("Chart left out: " + c.Problem + "\n")
		}
	}
	return b.String()
}

// alarmEmailHTML is the same body as HTML, the chart under it.
func alarmEmailHTML(msg notificationMessage) string {
	var b strings.Builder
	b.WriteString("<h2>" + html.EscapeString(msg.title()) + "</h2><p>" + html.EscapeString(msg.Message) + "</p>")
	for _, f := range alarmEmailFields(msg) {
		b.WriteString(`<p style="margin:2px 0">` + f[0] + ": " + html.EscapeString(f[1]) + "</p>")
	}
	for i, c := range msg.Charts {
		if c.Problem != "" {
			b.WriteString(chartProblemHTML(c.Problem))
		} else {
			b.WriteString(chartImgHTML(i, c))
		}
	}
	return emailHTMLShell(b.String())
}
