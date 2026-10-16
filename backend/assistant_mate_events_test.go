package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateMateFixtures rewrites testdata/mate_events/*.sse from what Go emits:
// go test -run TestMateEventFixtures -update. Go owns the bytes; the
// frontend test decodes the same files (ADR 0189).
var updateMateFixtures = flag.Bool("update", false, "rewrite the Mate event golden fixtures")

type mateFlushBuffer struct{ bytes.Buffer }

func (*mateFlushBuffer) Flush() {}

// mateFixtureMessage is a real stored reply (a form draft attached) trimmed
// for size, read from message_source.json.
func mateFixtureMessage(t *testing.T) mateMessageEvent {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "mate_events", "message_source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ev mateMessageEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.Message.FormDrafts) == 0 {
		t.Fatal("message_source.json must keep a form draft so the optional fields stay covered")
	}
	return ev
}

func TestMateEventFixtures(t *testing.T) {
	cases := []struct {
		name string
		ev   mateEvent
	}{
		{"status_text", mateStatusEvent{Text: "Checking the tide at Mackay"}},
		{"status_waiting", mateWaitingStatus()},
		{"delta", mateDeltaEvent{Text: "Tongue Bay first, "}},
		{"retract", mateRetractEvent{}},
		{"message", mateFixtureMessage(t)},
		{"error", mateErrorEvent{Error: "stopped"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cancel := context.WithCancel(context.Background())
			run := newAssistantRun(cancel)
			run.append(tc.ev)
			run.finish()
			var out mateFlushBuffer
			streamAssistantRun(context.Background(), run, 0, &out)

			path := filepath.Join("testdata", "mate_events", tc.name+".sse")
			if *updateMateFixtures {
				if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("wire bytes changed for %s\n got: %q\nwant: %q", tc.name, out.Bytes(), want)
			}
		})
	}
}
