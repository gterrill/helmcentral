package main

import (
	"testing"
	"time"
)

// Feedback on the captured month (ADR 0276). A fault is bent into the held-out
// week: the model trained before it flags it; what the operator answers, or
// what a repair says, changes the next training. The data is the committed
// slice, not a boat's.

func TestReplay_FeedbackChangesWhatTheNextTrainingLearns(t *testing.T) {
	feed, capture := loadCaptureFeed(t)
	end, err := time.Parse(time.RFC3339, capture.To)
	if err != nil {
		t.Fatal(err)
	}
	split := end.Add(-7 * 24 * time.Hour)
	before := replayTrain(t, feed, split)

	// Oil pressure on the port engine at 60% for 20 minutes, in a steady stretch
	// of the held-out week.
	start := firstSteadyStretch(t, feed, "port", before, split, end, 70)
	oilFrom, oilTo := start.Add(5*time.Minute), start.Add(25*time.Minute)
	bent := map[string][]normalMinuteRow{}
	for path, rows := range feed {
		bent[path] = append([]normalMinuteRow(nil), rows...)
	}
	for i, r := range bent["propulsion.port.oilPressure"] {
		if !r.At.Before(oilFrom) && r.At.Before(oilTo) {
			bent["propulsion.port.oilPressure"][i].Value = r.Value * 0.6
		}
	}

	// faultStretches are the stretches of the week that name the bent reading.
	faultStretches := func(model normalSubsystemModel, answers ...normalAnswer) []unusualStretch {
		var out []unusualStretch
		for _, s := range replayScoreWith(t, bent, model, split, end, answers).Stretches {
			if s.Subject == "port" && s.Top.Key == "oilPressure" && s.Start.Before(oilTo) && s.End.After(oilFrom) {
				out = append(out, s)
			}
		}
		return out
	}
	flagged := faultStretches(before)
	if len(flagged) == 0 {
		t.Fatal("setup: the model trained before the week should flag the bent oil pressure")
	}
	answersFor := func(answer string) []normalAnswer {
		var out []normalAnswer
		for _, s := range flagged {
			out = append(out, normalAnswer{StretchID: s.ID, Subsystem: s.Subsystem, Subject: s.Subject, From: s.Start, To: s.End, Answer: answer})
		}
		return out
	}
	port := func(m normalSubsystemModel) (excluded int) {
		for _, r := range m.Regimes {
			if r.Subject == "port" {
				excluded += r.ExcludedMinutes
			}
		}
		return excluded
	}

	// Trained on the whole month, with nothing said, the bent minutes are trimmed
	// as an outlier and the week still shows the fault.
	unanswered := replayTrainWith(t, bent, end, nil, nil)
	if len(faultStretches(unanswered)) == 0 {
		t.Fatal("setup: without an answer the fault should still be flagged")
	}

	t.Run("a stretch answered real is left out of the next training", func(t *testing.T) {
		m := replayTrainWith(t, bent, end, answersFor(normalAnswerReal), nil)
		want := 0
		for _, s := range flagged {
			want += s.Minutes
		}
		if got := port(m) - port(unanswered); got != want {
			t.Fatalf("port left out %d more minutes than without the answer, want the stretch's %d", got, want)
		}
		var named bool
		for _, e := range m.Excluded {
			named = named || (e.Reason == normalReasonAnsweredReal && e.Ref == flagged[0].ID)
		}
		if !named {
			t.Fatalf("excluded = %+v", m.Excluded)
		}
		if len(faultStretches(m)) == 0 {
			t.Fatal("a fault the operator confirmed must keep being flagged")
		}
	})

	t.Run("a stretch answered normal survives reweighting and stops being flagged", func(t *testing.T) {
		m := replayTrainWith(t, bent, end, answersFor(normalAnswerNormal), nil)
		// The answer clears the stretch twice over: the next training learns its
		// minutes as ordinary, and scoring never flags a minute inside the range.
		// Without the score-time rule a last few minutes sat a hair over the line.
		if left := faultStretches(m, answersFor(normalAnswerNormal)...); len(left) != 0 {
			t.Fatalf("still flagged after the operator said it is normal: %+v", left)
		}
		for _, s := range replayScoreWith(t, bent, m, split, end, answersFor(normalAnswerNormal)).Stretches {
			for _, f := range flagged {
				if s.Subject == f.Subject && s.Start.Before(f.End) && s.End.After(f.Start) {
					t.Fatalf("a stretch %s to %s sits inside the answered range", s.Start, s.End)
				}
			}
		}
		if len(m.Labelled) != len(flagged) || len(m.Excluded) != 0 {
			t.Fatalf("labelled %+v excluded %+v", m.Labelled, m.Excluded)
		}
		var kept, trimmed int
		for _, r := range m.Regimes {
			if r.Subject == "port" {
				trimmed += r.Trimmed
			}
		}
		for _, r := range unanswered.Regimes {
			if r.Subject == "port" {
				kept += r.Trimmed
			}
		}
		if trimmed >= kept {
			t.Fatalf("answered normal trimmed %d port minutes, unanswered trimmed %d: the answer should keep some", trimmed, kept)
		}
	})

	t.Run("a repair's fault range is left out of the next training", func(t *testing.T) {
		day := oilFrom.UTC().Format("2006-01-02")
		entry := maintenanceRepair{ID: "log1", EquipmentID: "eq-port", StartedAt: day, PerformedAt: day, Description: "New oil pressure sender"}
		m := replayTrainWith(t, bent, end, nil, []maintenanceRepair{entry})
		var found bool
		for _, e := range m.Excluded {
			found = found || (e.Reason == normalReasonRepair && e.Ref == "log1" && e.Subject == "port" && e.Title == "New oil pressure sender")
		}
		if !found {
			t.Fatalf("excluded = %+v", m.Excluded)
		}
		if port(m) < port(unanswered)+20 {
			t.Fatalf("the repair left out %d port minutes, only %d more than without it", port(m), port(m)-port(unanswered))
		}
		for _, e := range m.Excluded {
			if e.Subject != "port" {
				t.Fatalf("a repair on the port item leaves out port only: %+v", e)
			}
		}
	})
}
