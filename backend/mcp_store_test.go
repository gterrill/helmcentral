package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestMCPStore(t *testing.T) *outsideAssistantStore {
	t.Helper()
	s, err := newOutsideAssistantStore(filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMCPStore_TokenIsShownOnceAndStoredHashed(t *testing.T) {
	s := newTestMCPStore(t)
	rec, secret, err := s.CreateToken("Claude Code", []string{"get_vessel_status"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, mcpTokenPrefix) || len(secret) < len(mcpTokenPrefix)+43 {
		t.Fatalf("secret shape: %q", secret)
	}
	if rec.Name != "Claude Code" || rec.CreatedAt.IsZero() || rec.LastUsedAt != nil {
		t.Fatalf("record: %+v", rec)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM mcp_tokens WHERE token_hash LIKE '%' || ? || '%'`, secret).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("the secret itself is stored")
	}
}

func TestMCPStore_AuthenticateGoodBadRevoked(t *testing.T) {
	s := newTestMCPStore(t)
	rec, secret, err := s.CreateToken("laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Authenticate(secret)
	if err != nil || !ok || got.ID != rec.ID {
		t.Fatalf("good token: %+v ok=%v err=%v", got, ok, err)
	}
	if _, ok, _ := s.Authenticate(secret + "x"); ok {
		t.Fatal("bad token accepted")
	}
	if _, ok, _ := s.Authenticate(""); ok {
		t.Fatal("empty token accepted")
	}
	if err := s.RevokeToken(rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Authenticate(secret); ok {
		t.Fatal("revoked token accepted")
	}
}

func TestMCPStore_AuthenticateStampsLastUsed(t *testing.T) {
	s := newTestMCPStore(t)
	clock := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	_, secret, _ := s.CreateToken("laptop", nil)
	clock = clock.Add(time.Hour)
	if _, ok, err := s.Authenticate(secret); !ok || err != nil {
		t.Fatal(ok, err)
	}
	toks, err := s.ListTokens()
	if err != nil || len(toks) != 1 || toks[0].LastUsedAt == nil || !toks[0].LastUsedAt.Equal(clock) {
		t.Fatalf("tokens: %+v err=%v", toks, err)
	}
}

func TestMCPStore_RevokeUnknownIsAnError(t *testing.T) {
	s := newTestMCPStore(t)
	if err := s.RevokeToken("nope"); err == nil {
		t.Fatal("revoking an unknown token succeeded")
	}
}

func TestMCPStore_ToolListSeededAndSwitchable(t *testing.T) {
	s := newTestMCPStore(t)
	rec, _, _ := s.CreateToken("a", []string{"get_vessel_status", "get_active_alarms"})
	tools, err := s.ToolsFor(rec.ID)
	if err != nil || strings.Join(tools, ",") != "get_active_alarms,get_vessel_status" {
		t.Fatalf("seeded tools: %v err=%v", tools, err)
	}
	if err := s.SetTools(rec.ID, []string{"list_passages"}); err != nil {
		t.Fatal(err)
	}
	tools, _ = s.ToolsFor(rec.ID)
	if strings.Join(tools, ",") != "list_passages" {
		t.Fatalf("after switch: %v", tools)
	}
	if err := s.SetTools("nope", nil); err == nil {
		t.Fatal("setting tools on an unknown token succeeded")
	}
}

func TestMCPStore_ActivityWrittenListedAndPruned(t *testing.T) {
	s := newTestMCPStore(t)
	now := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	old := now.Add(-31 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	for _, e := range []mcpActivity{
		{At: old, Client: "laptop", Kind: "tool", Name: "get_vessel_status", Outcome: "ok", DurationMS: 12},
		{At: fresh, Client: "laptop", Kind: "tool", Name: "get_active_alarms", Outcome: "tool_error", DurationMS: 30},
	} {
		if err := s.RecordActivity(e); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.PruneActivity()
	if err != nil || n != 1 {
		t.Fatalf("pruned %d err=%v", n, err)
	}
	got, err := s.ListActivity(50)
	if err != nil || len(got) != 1 || got[0].Name != "get_active_alarms" || got[0].Outcome != "tool_error" || got[0].DurationMS != 30 {
		t.Fatalf("activity: %+v err=%v", got, err)
	}
}

func TestMCPStore_ActivityNewestFirst(t *testing.T) {
	s := newTestMCPStore(t)
	base := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base.Add(time.Hour) }
	for i, name := range []string{"first", "second", "third"} {
		_ = s.RecordActivity(mcpActivity{At: base.Add(time.Duration(i) * time.Minute), Client: "c", Kind: "tool", Name: name, Outcome: "ok"})
	}
	got, _ := s.ListActivity(2)
	if len(got) != 2 || got[0].Name != "third" || got[1].Name != "second" {
		t.Fatalf("order: %+v", got)
	}
}

func TestMCPStore_LastUsedStampedAtMostOncePerMinute(t *testing.T) {
	s := newTestMCPStore(t)
	clock := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	rec, secret, _ := s.CreateToken("laptop", nil)
	_, _, _ = s.Authenticate(secret)
	first := clock
	clock = clock.Add(30 * time.Second)
	_, _, _ = s.Authenticate(secret)
	if got, _ := s.GetToken(rec.ID); got.LastUsedAt == nil || !got.LastUsedAt.Equal(first) {
		t.Fatalf("stamped again within a minute: %+v", got.LastUsedAt)
	}
	clock = clock.Add(31 * time.Second)
	_, _, _ = s.Authenticate(secret)
	if got, _ := s.GetToken(rec.ID); !got.LastUsedAt.Equal(clock) {
		t.Fatalf("not stamped after a minute: %+v", got.LastUsedAt)
	}
}

func TestMCPStore_GetToken(t *testing.T) {
	s := newTestMCPStore(t)
	rec, _, _ := s.CreateToken("a", nil)
	got, err := s.GetToken(rec.ID)
	if err != nil || got.Name != "a" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.GetToken("nope"); !errors.Is(err, errMCPTokenNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestMCPStore_QueuedActivityIsWrittenOffTheCallersPath(t *testing.T) {
	s := newTestMCPStore(t)
	for i := 0; i < 20; i++ {
		s.QueueActivity(mcpActivity{At: s.now(), Client: "c", Kind: "tool", Name: "n", Outcome: "ok"})
	}
	got, err := s.ListActivity(50) // waits for what is queued
	if err != nil || len(got) != 20 {
		t.Fatalf("got %d err=%v", len(got), err)
	}
	if s.DroppedActivity() != 0 {
		t.Fatalf("dropped %d", s.DroppedActivity())
	}
}

func TestMCPStore_ListActivityDoesNotWaitOnLaterTraffic(t *testing.T) {
	s := newTestMCPStore(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if len(s.queue) < mcpActivityBuffer/2 { // steady traffic, short of the drop point
					s.QueueActivity(mcpActivity{At: s.now(), Client: "busy", Kind: "tool", Name: "n", Outcome: "ok"})
				}
			}
		}()
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 20; i++ {
			name := fmt.Sprintf("mine-%d", i)
			s.QueueActivity(mcpActivity{At: s.now(), Client: "me", Kind: "tool", Name: name, Outcome: "ok"})
			got, err := s.ListActivity(1 << 20)
			if err != nil {
				done <- err
				return
			}
			found := false
			for _, e := range got {
				found = found || e.Name == name
			}
			if !found {
				done <- fmt.Errorf("%s was queued before ListActivity and is not in its result", name)
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(20 * time.Second):
		t.Error("ListActivity did not return under steady traffic")
	}
	close(stop)
	wg.Wait()
}

func TestMCPStore_FullActivityBufferCountsTheDrop(t *testing.T) {
	s, err := newOutsideAssistantStoreSized(filepath.Join(t.TempDir(), "h.sqlite"), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.mu.Lock() // the writer cannot get on
	for i := 0; i < 10; i++ {
		s.QueueActivity(mcpActivity{At: s.now(), Client: "c", Kind: "tool", Name: "n", Outcome: "ok"})
	}
	s.mu.Unlock()
	dropped := s.DroppedActivity()
	if dropped == 0 {
		t.Fatal("a full buffer dropped nothing, or dropped silently")
	}
	got, _ := s.ListActivity(50)
	if int64(len(got))+dropped != 10 {
		t.Fatalf("written %d + dropped %d != 10", len(got), dropped)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestMCPStore_RandomFailureIsNotANameProblem(t *testing.T) {
	s := newTestMCPStore(t)
	s.rand = failingReader{}
	_, _, err := s.CreateToken("a", nil)
	if err == nil || errors.Is(err, errMCPTokenInvalid) {
		t.Fatalf("err: %v", err)
	}
	_, _, err = s.CreateToken(" ", nil)
	if !errors.Is(err, errMCPTokenInvalid) {
		t.Fatalf("blank name: %v", err)
	}
}
